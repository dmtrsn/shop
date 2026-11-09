package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sweetshop/internal/domain"
)

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// tracked is one asset's reconciliation fixture.
type tracked struct {
	id    string
	dir   string
	path  string
	sum   string
	bytes []byte
}

func storeThumbnail(t *testing.T, store *FileStore, root, id, content string) tracked {
	t.Helper()
	data := []byte(content)
	sum := digestOf(data)
	if err := store.PutThumbnail(id, sum, data); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, thumbnailsDir, id[4:6], id)
	return tracked{id: id, dir: dir, path: filepath.Join(dir, sum), sum: sum, bytes: data}
}

func age(t *testing.T, path string, by time.Duration) {
	t.Helper()
	when := time.Now().Add(-by)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func liveState(id string, side int, row *domain.MediaThumbnail) domain.MediaThumbnailState {
	return domain.MediaThumbnailState{Asset: domain.StoredMedia{ID: id}, Width: side, Height: side, Thumbnail: row}
}

func rowOf(file tracked, recipe string) *domain.MediaThumbnail {
	return &domain.MediaThumbnail{Recipe: recipe, SizeBytes: int64(len(file.bytes)), SHA256: file.sum}
}

func staticThumbnailRows(states ...domain.MediaThumbnailState) ThumbnailRows {
	return func(context.Context) ([]domain.MediaThumbnailState, error) { return states, nil }
}

func staticRows(ids ...string) Rows {
	return func(context.Context) (map[string]bool, error) {
		rows := map[string]bool{}
		for _, id := range ids {
			rows[id] = false
		}
		return rows, nil
	}
}

func TestReplaceThumbnailCreatesRepairsAndNeverLeavesTemporaryFiles(t *testing.T) {
	store, root := newStore(t)
	id := mustID(t)
	data := []byte("thumbnail")
	sum := digestOf(data)
	if err := store.ReplaceThumbnail(id, sum, data); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, thumbnailsDir, id[4:6], id, sum)
	if err := os.WriteFile(path, []byte("damaged"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceThumbnail(id, sum, data); err != nil {
		t.Fatal(err)
	}
	if read, _ := os.ReadFile(path); string(read) != "thumbnail" {
		t.Fatalf("read %q", read)
	}
	if temps, _ := os.ReadDir(filepath.Join(root, tmpDir)); len(temps) != 0 {
		t.Fatalf("temporary files left: %v", temps)
	}
	file, size, err := store.OpenThumbnail(id, sum)
	if err != nil || size != int64(len(data)) {
		t.Fatalf("open %d %v", size, err)
	}
	file.Close()
	if _, _, err := store.OpenThumbnail(id, strings.Repeat("0", 64)); !errors.Is(err, ErrNotStored) {
		t.Fatalf("absent: %v", err)
	}
	for _, input := range [][2]string{{"../" + id, sum}, {id, "../" + sum}, {id, strings.ToUpper(sum)}, {id, ""}} {
		if _, _, err := store.OpenThumbnail(input[0], input[1]); !errors.Is(err, ErrNotStored) {
			t.Errorf("open %v: %v", input, err)
		}
		if err := store.ReplaceThumbnail(input[0], input[1], data); !errors.Is(err, ErrStorage) {
			t.Errorf("replace %v: %v", input, err)
		}
	}
}

func TestThumbnailsListsOnlyTheStoresOwnFiles(t *testing.T) {
	store, root := newStore(t)
	id := mustID(t)
	file := storeThumbnail(t, store, root, id, "kept")
	base := filepath.Join(root, thumbnailsDir)
	other := mustID(t)
	wrongShard := "ff"
	if other[4:6] == wrongShard {
		wrongShard = "00"
	}
	_ = os.WriteFile(filepath.Join(file.dir, "notes.txt"), []byte("x"), 0o640)
	_ = os.WriteFile(filepath.Join(base, "stray"), []byte("x"), 0o640)
	_ = os.MkdirAll(filepath.Join(base, wrongShard, other), 0o750)
	_ = os.WriteFile(filepath.Join(base, wrongShard, other, strings.Repeat("a", 64)), []byte("x"), 0o640)
	_ = os.Symlink(file.path, filepath.Join(file.dir, strings.Repeat("c", 64)))
	listed, err := store.Thumbnails()
	if err != nil || len(listed) != 1 || listed[0].ID != id || listed[0].SHA256 != file.sum {
		t.Fatalf("listed %+v %v", listed, err)
	}
	empty := filepath.Join(t.TempDir(), "media")
	_ = os.Mkdir(empty, 0o750)
	readOnly, err := OpenFileStoreReadOnly(empty)
	if err != nil {
		t.Fatal(err)
	}
	if listed, err := readOnly.Thumbnails(); err != nil || len(listed) != 0 {
		t.Fatalf("no tree: %+v %v", listed, err)
	}
}

func TestRemoveThumbnailKeepsTheRestOfTheAsset(t *testing.T) {
	store, root := newStore(t)
	id := mustID(t)
	first, second := storeThumbnail(t, store, root, id, "first"), storeThumbnail(t, store, root, id, "second")
	if err := store.RemoveThumbnail(id, first.sum); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(second.path); err != nil {
		t.Fatal("removed a sibling")
	}
	if err := store.RemoveThumbnail(id, second.sum); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(second.dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty directory left: %v", err)
	}
	if err := store.RemoveThumbnail(id, second.sum); err != nil {
		t.Fatalf("second remove: %v", err)
	}
	if err := store.RemoveThumbnail("../"+id, second.sum); err != nil {
		t.Fatalf("invalid name: %v", err)
	}
}

func TestVerifyThumbnailProvesSizeAndDigest(t *testing.T) {
	store, root := newStore(t)
	id := mustID(t)
	file := storeThumbnail(t, store, root, id, "intact")
	if err := VerifyThumbnail(store, id, file.sum, int64(len(file.bytes))); err != nil {
		t.Fatal(err)
	}
	if err := VerifyThumbnail(store, id, file.sum, 99); !errors.Is(err, ErrThumbnailDamaged) {
		t.Fatalf("size: %v", err)
	}
	if err := os.WriteFile(file.path, []byte("damage"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := VerifyThumbnail(store, id, file.sum, int64(len(file.bytes))); !errors.Is(err, ErrThumbnailDamaged) {
		t.Fatalf("same size, other bytes: %v", err)
	}
	if err := VerifyThumbnail(store, id, strings.Repeat("0", 64), 1); !errors.Is(err, ErrNotStored) {
		t.Fatalf("missing: %v", err)
	}
}

// thumbnailScene has one file per classification.
type thumbnailScene struct {
	store                                                                  *FileStore
	states                                                                 []domain.MediaThumbnailState
	ids                                                                    []string
	healthy, outdated, corrupt, deleted, orphan, youngOrphan, stale, young tracked
	current, pendingID, smallID, missingID                                 string
}

func newThumbnailScene(t *testing.T) *thumbnailScene {
	t.Helper()
	store, root := newStore(t)
	s := &thumbnailScene{store: store}
	add := func(content string) tracked { return storeThumbnail(t, store, root, mustID(t), content) }
	const day = 48 * time.Hour
	s.healthy = add("healthy")
	s.outdated = add("outdated")
	s.corrupt = add("corrupt")
	if err := os.WriteFile(s.corrupt.path, []byte("c0rrupt"), fileMode); err != nil {
		t.Fatal(err)
	}
	// Every current file is old: only its row protects it from removal.
	for _, file := range []tracked{s.healthy, s.outdated, s.corrupt} {
		age(t, file.path, day)
	}
	s.deleted = add("deleted")
	s.orphan, s.youngOrphan = add("orphan"), add("young orphan")
	age(t, s.orphan.path, day)
	s.stale = storeThumbnail(t, store, root, s.healthy.id, "stale output")
	age(t, s.stale.path, day)
	s.young = storeThumbnail(t, store, root, s.outdated.id, "young stale output")
	s.pendingID, s.smallID, s.missingID = mustID(t), mustID(t), mustID(t)
	s.states = []domain.MediaThumbnailState{
		liveState(s.healthy.id, 900, rowOf(s.healthy, ThumbnailRecipe)),
		liveState(s.outdated.id, 900, rowOf(s.outdated, "fit640-q80-v0")),
		liveState(s.corrupt.id, 900, rowOf(s.corrupt, ThumbnailRecipe)),
		liveState(s.pendingID, 900, nil),
		liveState(s.smallID, 640, nil),
		liveState(s.missingID, 900, &domain.MediaThumbnail{Recipe: ThumbnailRecipe, SizeBytes: 1, SHA256: strings.Repeat("d", 64)}),
		{Asset: domain.StoredMedia{ID: s.deleted.id}, Width: 900, Height: 900, Deleted: true, Thumbnail: rowOf(s.deleted, ThumbnailRecipe)},
	}
	for _, state := range s.states {
		if !state.Deleted {
			s.ids = append(s.ids, state.Asset.ID)
		}
	}
	return s
}

func (s *thumbnailScene) reconcile(t *testing.T, grace time.Duration, apply bool) (Report, error) {
	t.Helper()
	rows := func(context.Context) (map[string]bool, error) { return map[string]bool{}, nil }
	return Reconcile(context.Background(), s.store, rows, staticThumbnailRows(s.states...), time.Now(), grace, apply)
}

func TestReconcileClassifiesEveryThumbnailState(t *testing.T) {
	s := newThumbnailScene(t)
	report, err := s.reconcile(t, 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	name := func(file tracked) string { return file.id + "/" + file.sum }
	expect := map[string]struct {
		got, want []string
	}{
		"pending":      {report.PendingThumbnails, []string{s.pendingID}},
		"outdated":     {report.OutdatedThumbnails, []string{s.outdated.id}},
		"missing":      {report.MissingThumbnails, []string{s.missingID}},
		"corrupt":      {report.CorruptThumbnails, []string{s.corrupt.id}},
		"deleted":      {report.DeletedThumbnails, []string{name(s.deleted)}},
		"orphan":       {report.OrphanThumbnails, []string{name(s.orphan)}},
		"young orphan": {report.YoungOrphanThumbnails, []string{name(s.youngOrphan)}},
		"stale":        {report.StaleThumbnails, []string{name(s.stale)}},
		"young stale":  {report.YoungStaleThumbnails, []string{name(s.young)}},
	}
	for label, tc := range expect {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s: got %v want %v", label, tc.got, tc.want)
		}
	}
	if report.HealthyThumbnails != 2 || !report.Problems() || !report.DamagedThumbnails() || len(report.Removed) != 0 {
		t.Fatalf("report %+v", report)
	}
}

func TestReconcilePendingOutdatedAndYoungThumbnailsAreNotProblems(t *testing.T) {
	store, root := newStore(t)
	pending, outdatedID, youngID := mustID(t), mustID(t), mustID(t)
	outdated := storeThumbnail(t, store, root, outdatedID, "outdated")
	storeThumbnail(t, store, root, youngID, "young")
	ids := staticRows()
	report, err := Reconcile(context.Background(), store, ids, staticThumbnailRows(
		liveState(pending, 900, nil), liveState(outdatedID, 900, rowOf(outdated, "fit640-q80-v0")),
	), time.Now(), time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Problems() || len(report.PendingThumbnails) != 1 || len(report.OutdatedThumbnails) != 1 || len(report.YoungOrphanThumbnails) != 1 {
		t.Fatalf("report %+v", report)
	}
}

func TestReconcileApplyRemovesOnlyThumbnailDebrisAndDerivesNothing(t *testing.T) {
	s := newThumbnailScene(t)
	report, err := s.reconcile(t, 24*time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []tracked{s.deleted, s.orphan, s.stale} {
		if _, err := os.Stat(file.path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still present: %v", file.path, err)
		}
	}
	for _, file := range []tracked{s.healthy, s.outdated, s.corrupt, s.youngOrphan, s.young} {
		if _, err := os.Stat(file.path); err != nil {
			t.Errorf("removed %s: %v", file.path, err)
		}
	}
	if data, _ := os.ReadFile(s.corrupt.path); string(data) != "c0rrupt" {
		t.Fatal("verify repaired a thumbnail; only the backfill may")
	}
	if len(report.Removed) != 3 || report.Refused != "" {
		t.Fatalf("report %+v", report)
	}
	if files, _ := s.store.Thumbnails(); len(files) != 5 {
		t.Fatalf("files %+v", files)
	}
}

func TestReconcileNeverRemovesThumbnailOrphansWithoutRowsToCompare(t *testing.T) {
	for name, states := range map[string]func(string) []domain.MediaThumbnailState{
		"no media rows at all": func(string) []domain.MediaThumbnailState { return nil },
		"assets without any thumbnail row": func(id string) []domain.MediaThumbnailState {
			return []domain.MediaThumbnailState{liveState(id, 900, nil)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, root := newStore(t)
			id := mustID(t)
			file := storeThumbnail(t, store, root, id, "orphan")
			age(t, file.path, 48*time.Hour)
			report, err := Reconcile(context.Background(), store, staticRows(id), staticThumbnailRows(states(id)...), time.Now(), time.Hour, true)
			if err != nil || report.Refused == "" || len(report.Removed) != 0 {
				t.Fatalf("report %+v %v", report, err)
			}
			if _, err := os.Stat(file.path); err != nil {
				t.Fatalf("orphan removed: %v", err)
			}
		})
	}
	t.Run("a failed database read removes nothing", func(t *testing.T) {
		store, root := newStore(t)
		id := mustID(t)
		file := storeThumbnail(t, store, root, id, "orphan")
		age(t, file.path, 48*time.Hour)
		failing := func(context.Context) ([]domain.MediaThumbnailState, error) { return nil, fmt.Errorf("database down") }
		if _, err := Reconcile(context.Background(), store, staticRows(id), failing, time.Now(), time.Hour, true); err == nil {
			t.Fatal("expected an error")
		}
		if _, err := os.Stat(file.path); err != nil {
			t.Fatalf("orphan removed: %v", err)
		}
	})
	t.Run("a deleted asset's file is still removed", func(t *testing.T) {
		store, root := newStore(t)
		id := mustID(t)
		file := storeThumbnail(t, store, root, id, "debris")
		deleted := domain.MediaThumbnailState{Asset: domain.StoredMedia{ID: id}, Deleted: true}
		report, err := Reconcile(context.Background(), store, staticRows(id), staticThumbnailRows(deleted), time.Now(), time.Hour, true)
		if err != nil || len(report.Removed) != 1 {
			t.Fatalf("report %+v %v", report, err)
		}
		if _, err := os.Stat(file.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("still present: %v", err)
		}
	})
}
