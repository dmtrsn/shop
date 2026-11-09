package application

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sweetshop/internal/domain"
	"sweetshop/internal/media"
)

func (f *fakeFiles) ReplaceThumbnail(id, sum string, data []byte) error {
	return f.PutThumbnail(id, sum, data)
}
func (f *fakeFiles) OpenThumbnail(string, string) (io.ReadSeekCloser, int64, error) {
	return nil, 0, media.ErrNotStored
}
func (f *fakeFiles) RemoveThumbnail(string, string) error { return nil }
func (f *fakeFiles) Thumbnails() ([]media.ThumbnailFile, error) {
	return nil, nil
}

// backfillStore embeds the port: any call but the two the backfill may make
// panics, so a staff mutation or a revision bump cannot pass unnoticed.
type backfillStore struct {
	MediaStore
	states  map[string]*domain.MediaThumbnailState
	saved   []string
	saveErr map[string]error
	onSave  func(id string)
}

func (s *backfillStore) LiveMediaThumbnailStates(_ context.Context, after string, limit int) ([]domain.MediaThumbnailState, error) {
	ids := make([]string, 0, len(s.states))
	for id := range s.states {
		if id > after {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	result := make([]domain.MediaThumbnailState, 0, len(ids))
	for _, id := range ids {
		result = append(result, *s.states[id])
	}
	return result, nil
}

func (s *backfillStore) SaveMediaThumbnail(_ context.Context, id string, thumbnail domain.MediaThumbnail) error {
	if s.onSave != nil {
		s.onSave(id)
	}
	if err := s.saveErr[id]; err != nil {
		return err
	}
	s.saved = append(s.saved, id)
	s.states[id].Thumbnail = &thumbnail
	return nil
}

type backfillFixture struct {
	library *Media
	store   *backfillStore
	files   *media.FileStore
	root    string
}

func newBackfillFixture(t *testing.T) *backfillFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "media")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	files, err := media.OpenFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	store := &backfillStore{states: map[string]*domain.MediaThumbnailState{}, saveErr: map[string]error{}}
	return &backfillFixture{library: NewMedia(store, files, slog.New(slog.NewTextHandler(io.Discard, nil))), store: store, files: files, root: root}
}

func normalizedOriginal(t *testing.T, width, height int) media.Normalized {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	normalized, err := media.Normalize(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}

// addAsset registers a stored original and returns its ID.
func (f *backfillFixture) addAsset(t *testing.T, side int) (string, media.Normalized) {
	t.Helper()
	id, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	normalized := normalizedOriginal(t, side, side/2)
	if err := f.files.Put(id, normalized.Data); err != nil {
		t.Fatal(err)
	}
	f.store.states[id] = &domain.MediaThumbnailState{
		Asset: domain.StoredMedia{ID: id, ContentType: normalized.ContentType, SizeBytes: int64(len(normalized.Data)), SHA256: normalized.SHA256},
		Width: normalized.Width, Height: normalized.Height}
	return id, normalized
}

func (f *backfillFixture) objectPath(id string) string {
	return filepath.Join(f.root, "objects", id[4:6], id)
}

func (f *backfillFixture) thumbnailDir(id string) string {
	return filepath.Join(f.root, "thumbnails", id[4:6], id)
}

// tree lists every file under the media root.
func (f *backfillFixture) tree(t *testing.T) []string {
	t.Helper()
	found := []string{}
	if err := filepath.Walk(f.root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			found = append(found, path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return found
}

func (f *backfillFixture) run(t *testing.T, apply bool, limit int) ThumbnailBackfillReport {
	t.Helper()
	report, err := f.library.BackfillThumbnails(context.Background(), apply, limit)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestBackfillDryRunReportsAndWritesNothing(t *testing.T) {
	f := newBackfillFixture(t)
	missing, normalized := f.addAsset(t, 800)
	small, _ := f.addAsset(t, 640)
	before := f.tree(t)
	report := f.run(t, false, 0)
	if report.Mode != "dry-run" || report.Inspected != 2 || report.NotApplicable != 1 || report.NeedsGeneration != 1 || report.Generated != 0 ||
		report.OriginalBytesInspected != int64(len(normalized.Data)) || report.ThumbnailBytesGenerated != 0 || !report.Pending() {
		t.Fatalf("report %+v", report)
	}
	if after := f.tree(t); len(after) != len(before) || len(f.store.saved) != 0 {
		t.Fatalf("a dry run wrote: %v saved %v (%s %s)", after, f.store.saved, missing, small)
	}
}

func TestBackfillLeavesAnImageNoLargerThanTheRecipeAlone(t *testing.T) {
	f := newBackfillFixture(t)
	id, _ := f.addAsset(t, 640)
	report := f.run(t, true, 0)
	if report.NotApplicable != 1 || report.Generated != 0 || report.Pending() || len(f.store.saved) != 0 {
		t.Fatalf("report %+v saved %v", report, f.store.saved)
	}
	if _, err := os.Stat(f.thumbnailDir(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("thumbnail directory: %v", err)
	}
}

func TestBackfillGeneratesTheSameBytesAsTheRecipeAndIsIdempotent(t *testing.T) {
	f := newBackfillFixture(t)
	id, normalized := f.addAsset(t, 800)
	report := f.run(t, true, 0)
	expected, ok, err := media.DeriveThumbnail(normalized.Data)
	if err != nil || !ok {
		t.Fatal(err)
	}
	row := f.store.states[id].Thumbnail
	if report.Generated != 1 || report.ThumbnailBytesGenerated != int64(len(expected.Data)) || report.Pending() ||
		row == nil || *row != thumbnailRow(expected) {
		t.Fatalf("report %+v row %+v", report, row)
	}
	stored, err := os.ReadFile(filepath.Join(f.thumbnailDir(id), expected.SHA256))
	if err != nil || !bytes.Equal(stored, expected.Data) {
		t.Fatalf("stored thumbnail: %v", err)
	}
	second := f.run(t, true, 0)
	if second.Complete != 1 || second.Generated != 0 || second.Regenerated != 0 || second.OriginalBytesInspected != 0 || len(f.store.saved) != 1 {
		t.Fatalf("second run %+v saved %v", second, f.store.saved)
	}
}

func TestBackfillFinishesARunInterruptedBeforeItsRow(t *testing.T) {
	f := newBackfillFixture(t)
	id, normalized := f.addAsset(t, 800)
	expected, _, _ := media.DeriveThumbnail(normalized.Data)
	if err := f.files.PutThumbnail(id, expected.SHA256, expected.Data); err != nil {
		t.Fatal(err)
	}
	if report := f.run(t, true, 0); report.Generated != 1 || report.Failed != 0 || f.store.states[id].Thumbnail == nil {
		t.Fatalf("report %+v", report)
	}
}

func TestBackfillRegeneratesWhatIsOutdatedMissingOrCorrupt(t *testing.T) {
	expectRepaired := func(t *testing.T, f *backfillFixture, id string, normalized media.Normalized) {
		t.Helper()
		report := f.run(t, true, 0)
		expected, _, _ := media.DeriveThumbnail(normalized.Data)
		if report.Regenerated != 1 || report.Failed != 0 || *f.store.states[id].Thumbnail != thumbnailRow(expected) {
			t.Fatalf("report %+v row %+v", report, f.store.states[id].Thumbnail)
		}
		stored, err := os.ReadFile(filepath.Join(f.thumbnailDir(id), expected.SHA256))
		if err != nil || !bytes.Equal(stored, expected.Data) {
			t.Fatalf("stored thumbnail: %v", err)
		}
		if again := f.run(t, true, 0); again.Complete != 1 || again.Regenerated != 0 {
			t.Fatalf("rerun %+v", again)
		}
	}
	current := func(t *testing.T, f *backfillFixture, normalized media.Normalized) (media.Thumbnail, domain.MediaThumbnail) {
		t.Helper()
		expected, _, err := media.DeriveThumbnail(normalized.Data)
		if err != nil {
			t.Fatal(err)
		}
		return expected, thumbnailRow(expected)
	}

	t.Run("old recipe keeps the old file for reconciliation", func(t *testing.T) {
		f := newBackfillFixture(t)
		id, normalized := f.addAsset(t, 800)
		_, row := current(t, f, normalized)
		old := []byte("output of an older recipe")
		oldSum := strings.Repeat("e", 64)
		if err := f.files.PutThumbnail(id, oldSum, old); err != nil {
			t.Fatal(err)
		}
		row.Recipe, row.SHA256, row.SizeBytes = "fit640-q80-v0", oldSum, int64(len(old))
		f.store.states[id].Thumbnail = &row
		dry := f.run(t, false, 0)
		if dry.NeedsRegeneration != 1 || f.store.states[id].Thumbnail.Recipe != "fit640-q80-v0" {
			t.Fatalf("dry run %+v", dry)
		}
		expectRepaired(t, f, id, normalized)
		if _, err := os.Stat(filepath.Join(f.thumbnailDir(id), oldSum)); err != nil {
			t.Fatal("the backfill removed the stale file; reconciliation owns that")
		}
	})
	t.Run("missing file", func(t *testing.T) {
		f := newBackfillFixture(t)
		id, normalized := f.addAsset(t, 800)
		_, row := current(t, f, normalized)
		f.store.states[id].Thumbnail = &row
		expectRepaired(t, f, id, normalized)
	})
	t.Run("corrupt file of the same size", func(t *testing.T) {
		f := newBackfillFixture(t)
		id, normalized := f.addAsset(t, 800)
		expected, row := current(t, f, normalized)
		f.store.states[id].Thumbnail = &row
		if err := f.files.PutThumbnail(id, row.SHA256, bytes.Repeat([]byte("x"), len(expected.Data))); err != nil {
			t.Fatal(err)
		}
		expectRepaired(t, f, id, normalized)
	})
	t.Run("corrupt file of another size", func(t *testing.T) {
		f := newBackfillFixture(t)
		id, normalized := f.addAsset(t, 800)
		_, row := current(t, f, normalized)
		f.store.states[id].Thumbnail = &row
		if err := f.files.PutThumbnail(id, row.SHA256, []byte("truncated")); err != nil {
			t.Fatal(err)
		}
		expectRepaired(t, f, id, normalized)
	})
}

func TestBackfillNeverDerivesFromADamagedOriginalAndOtherAssetsProceed(t *testing.T) {
	f := newBackfillFixture(t)
	sized, _ := f.addAsset(t, 800)
	digested, normalized := f.addAsset(t, 800)
	missing, _ := f.addAsset(t, 800)
	healthy, _ := f.addAsset(t, 800)
	if err := os.Truncate(f.objectPath(sized), 10); err != nil {
		t.Fatal(err)
	}
	flipped := bytes.Clone(normalized.Data)
	flipped[len(flipped)/2] ^= 0xff
	if err := os.Chmod(f.objectPath(digested), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.objectPath(digested), flipped, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.objectPath(missing)); err != nil {
		t.Fatal(err)
	}
	for _, apply := range []bool{false, true} {
		report := f.run(t, apply, 0)
		kinds := map[string]string{}
		for _, failure := range report.Failures {
			kinds[failure.ID] = failure.Kind
		}
		if report.Failed != 3 || !report.Pending() || kinds[sized] != ThumbnailOriginalDamaged || kinds[digested] != ThumbnailOriginalDamaged || kinds[missing] != ThumbnailOriginalMissing {
			t.Fatalf("apply=%v report %+v", apply, report)
		}
	}
	for _, id := range []string{sized, digested, missing} {
		if _, err := os.Stat(f.thumbnailDir(id)); !errors.Is(err, os.ErrNotExist) || f.store.states[id].Thumbnail != nil {
			t.Fatalf("%s got a thumbnail from damaged bytes", id)
		}
	}
	if f.store.states[healthy].Thumbnail == nil {
		t.Fatal("a damaged asset stopped the run")
	}
}

func TestBackfillRemovesTheFileWhenTheParentWasDeletedMeanwhile(t *testing.T) {
	f := newBackfillFixture(t)
	id, _ := f.addAsset(t, 800)
	f.store.saveErr[id] = domain.ErrNotFound
	report := f.run(t, true, 0)
	if report.ParentDeleted != 1 || report.Failed != 0 || report.Generated != 0 || f.store.states[id].Thumbnail != nil {
		t.Fatalf("report %+v", report)
	}
	if _, err := os.Stat(f.thumbnailDir(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file of a deleted asset left: %v", err)
	}
}

func TestBackfillLeavesAFileAfterADatabaseFailureAndCarriesOn(t *testing.T) {
	f := newBackfillFixture(t)
	failing, _ := f.addAsset(t, 800)
	healthy, _ := f.addAsset(t, 800)
	f.store.saveErr[failing] = errors.New("database down")
	report := f.run(t, true, 0)
	if report.Failed != 1 || report.Failures[0].Kind != ThumbnailDatabaseError || report.Generated != 1 || f.store.states[healthy].Thumbnail == nil {
		t.Fatalf("report %+v", report)
	}
	if strings.Contains(report.Failures[0].Kind, f.root) {
		t.Fatal("the report names a path")
	}
	delete(f.store.saveErr, failing)
	if report := f.run(t, true, 0); report.Generated != 1 || report.Complete != 1 || report.Pending() {
		t.Fatalf("rerun %+v", report)
	}
}

func TestBackfillLimitCountsGeneratedImagesAndARerunReachesFurther(t *testing.T) {
	f := newBackfillFixture(t)
	for i := 0; i < 3; i++ {
		f.addAsset(t, 800)
	}
	first := f.run(t, true, 1)
	if first.Generated != 1 || !first.LimitReached || !first.Pending() {
		t.Fatalf("first %+v", first)
	}
	second := f.run(t, true, 1)
	if second.Generated != 1 || second.Complete != 1 || !second.LimitReached {
		t.Fatalf("second %+v", second)
	}
	third := f.run(t, true, 5)
	if third.Generated != 1 || third.Complete != 2 || third.LimitReached || third.Pending() {
		t.Fatalf("third %+v", third)
	}
	dry := newBackfillFixture(t)
	for i := 0; i < 3; i++ {
		dry.addAsset(t, 800)
	}
	if report := dry.run(t, false, 2); report.NeedsGeneration != 2 || !report.LimitReached {
		t.Fatalf("dry run %+v", report)
	}
}

func TestBackfillStopsWhenCancelledWithoutBlamingAnAsset(t *testing.T) {
	f := newBackfillFixture(t)
	f.addAsset(t, 800)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := f.library.BackfillThumbnails(ctx, true, 0)
	if !errors.Is(err, context.Canceled) || report.Failed != 0 || report.Generated != 0 {
		t.Fatalf("report %+v %v", report, err)
	}
}
