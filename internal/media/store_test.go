package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) (*FileStore, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "media")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	store, err := OpenFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store, root
}

func mustID(t *testing.T) string {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMediaIDsAreRandomAndValidated(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := mustID(t)
		if !IDPattern.MatchString(id) || seen[id] {
			t.Fatalf("bad or repeated id %q", id)
		}
		seen[id] = true
	}
	for _, bad := range []string{"", "med_", "med_" + strings.Repeat("A", 32), "med_" + strings.Repeat("0", 31), "../med_" + strings.Repeat("0", 32), "med_" + strings.Repeat("0", 32) + "/x", "img_" + strings.Repeat("0", 32)} {
		if IDPattern.MatchString(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestOpenFileStoreRefusesUnsafeRoots(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	_ = os.WriteFile(file, nil, 0o600)
	link := filepath.Join(dir, "link")
	_ = os.Symlink(dir, link)
	open := filepath.Join(dir, "open")
	_ = os.Mkdir(open, 0o750)
	_ = os.Chmod(open, 0o777)
	for name, root := range map[string]string{
		"empty": "", "relative": "media", "root": "/", "unclean": dir + "/../" + filepath.Base(dir),
		"missing": filepath.Join(dir, "missing"), "file": file, "symlink": link, "world-writable": open,
	} {
		if _, err := OpenFileStore(root); err == nil {
			t.Errorf("%s: accepted %q", name, root)
		}
	}
	if os.Geteuid() != 0 {
		readOnly := filepath.Join(dir, "readonly")
		_ = os.Mkdir(readOnly, 0o550)
		if _, err := OpenFileStore(readOnly); err == nil {
			t.Error("read-only root accepted")
		}
	}
}

func TestPutStoresImmutableObjectsUnderTheirIDOnly(t *testing.T) {
	store, root := newStore(t)
	id := mustID(t)
	if err := store.Put(id, []byte("first")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "objects", id[4:6], id)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != fileMode {
		t.Fatalf("object at %s: %v %v", path, info, err)
	}
	// The same ID is never written twice; the stored bytes stay.
	if err := store.Put(id, []byte("second")); !errors.Is(err, ErrStorage) {
		t.Fatalf("overwrite: %v", err)
	}
	file, size, err := store.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(file)
	file.Close()
	if string(data) != "first" || size != 5 {
		t.Fatalf("read %q (%d)", data, size)
	}
	if temps, _ := os.ReadDir(filepath.Join(root, "tmp")); len(temps) != 0 {
		t.Fatalf("temporary files left: %v", temps)
	}
	if err := store.Remove(id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Open(id); !errors.Is(err, ErrNotStored) {
		t.Fatalf("after remove: %v", err)
	}
	if err := store.Remove(id); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}

func TestThumbnailsLiveOutsideObjects(t *testing.T) {
	store, root := newStore(t)
	id, sum := mustID(t), strings.Repeat("a", 64)
	if err := store.Put(id, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := store.PutThumbnail(id, sum, []byte("first")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "thumbnails", id[4:6], id, sum)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != fileMode {
		t.Fatalf("thumbnail at %s: %v %v", path, info, err)
	}
	// The same name is never written twice; the stored bytes stay.
	if err := store.PutThumbnail(id, sum, []byte("second")); !errors.Is(err, ErrStorage) {
		t.Fatalf("overwrite: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "first" {
		t.Fatalf("read %q", data)
	}
	if temps, _ := os.ReadDir(filepath.Join(root, "tmp")); len(temps) != 0 {
		t.Fatalf("temporary files left: %v", temps)
	}
	objects, err := store.Objects()
	if err != nil || len(objects) != 1 || objects[0].ID != id {
		t.Fatalf("objects %+v %v", objects, err)
	}
	// Reconciliation, also in older binaries, never sees a thumbnail.
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(path, old, old)
	rows := func(context.Context) (map[string]bool, error) { return map[string]bool{id: false}, nil }
	if _, err := Reconcile(context.Background(), store, rows, time.Now(), time.Hour, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("reconciliation removed a thumbnail: %v", err)
	}
	if err := store.RemoveThumbnails(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("thumbnail directory left: %v", err)
	}
	if err := store.RemoveThumbnails(id); err != nil {
		t.Fatalf("second remove: %v", err)
	}
	if _, _, err := store.Open(id); err != nil {
		t.Fatalf("removing thumbnails touched the original: %v", err)
	}
}

func TestThumbnailPathsNeverComeFromInput(t *testing.T) {
	store, root := newStore(t)
	id, sum := mustID(t), strings.Repeat("b", 64)
	for name, input := range map[string][2]string{
		"traversal id":     {"../outside", sum},
		"traversal digest": {id, "../../outside"},
		"short digest":     {id, "abc"},
		"upper digest":     {id, strings.Repeat("B", 64)},
	} {
		if err := store.PutThumbnail(input[0], input[1], []byte("x")); !errors.Is(err, ErrStorage) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := store.RemoveThumbnails("../outside"); err != nil {
		t.Fatalf("malformed id: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "thumbnails")); len(entries) != 0 {
		t.Fatalf("thumbnails written: %v", entries)
	}
}

func TestPathsNeverComeFromInput(t *testing.T) {
	store, root := newStore(t)
	outside := filepath.Join(filepath.Dir(root), "outside")
	_ = os.WriteFile(outside, []byte("secret"), 0o600)
	for _, id := range []string{"../outside", "../../outside", outside, "objects/../../outside", "med_../../outside"} {
		if err := store.Put(id, []byte("x")); !errors.Is(err, ErrStorage) {
			t.Errorf("put %q: %v", id, err)
		}
		if _, _, err := store.Open(id); !errors.Is(err, ErrNotStored) {
			t.Errorf("open %q: %v", id, err)
		}
		_ = store.Remove(id)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "secret" {
		t.Fatal("a file outside the root was touched")
	}
	if err := store.RemoveTemp("../outside"); err == nil {
		t.Fatal("RemoveTemp accepted a path outside tmp")
	}
}

func TestPutFailureLeavesNothingBehind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not bind root")
	}
	store, root := newStore(t)
	id := mustID(t)
	shard := filepath.Join(root, "objects", id[4:6])
	_ = os.Mkdir(shard, 0o750)
	_ = os.Chmod(shard, 0o550)
	defer os.Chmod(shard, 0o750)
	if err := store.Put(id, []byte("x")); !errors.Is(err, ErrStorage) {
		t.Fatalf("put into a read-only shard: %v", err)
	}
	if temps, _ := os.ReadDir(filepath.Join(root, "tmp")); len(temps) != 0 {
		t.Fatal("temporary file left after a failed publish")
	}
	if _, _, err := store.Open(id); !errors.Is(err, ErrNotStored) {
		t.Fatal("a failed write is visible")
	}
	_ = os.Chmod(filepath.Join(root, "tmp"), 0o550)
	defer os.Chmod(filepath.Join(root, "tmp"), 0o750)
	if err := store.Put(mustID(t), []byte("x")); !errors.Is(err, ErrStorage) {
		t.Fatalf("put without a writable tmp: %v", err)
	}
}

func TestObjectsListsOnlyWhatTheStoreOwns(t *testing.T) {
	store, root := newStore(t)
	id := mustID(t)
	_ = store.Put(id, []byte("x"))
	wrongShard := "med_" + strings.Repeat("a", 32)
	_ = os.MkdirAll(filepath.Join(root, "objects", "zz"), 0o750)
	for _, name := range []string{
		filepath.Join("objects", "zz", wrongShard), filepath.Join("objects", id[4:6], "notes.txt"),
		"stray.jpg", filepath.Join("tmp", "probe-left"),
	} {
		_ = os.WriteFile(filepath.Join(root, name), []byte("foreign"), 0o640)
	}
	_ = os.WriteFile(filepath.Join(root, "tmp", "upload-123"), []byte("partial"), 0o640)
	objects, err := store.Objects()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, object := range objects {
		names = append(names, object.Name)
	}
	want := []string{filepath.Join("objects", id[4:6], id), filepath.Join("tmp", "upload-123")}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("listed %v, want %v", names, want)
	}
}

func TestReconcileClassifiesAndRemovesOnlyOwnedLeftovers(t *testing.T) {
	store, root := newStore(t)
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	put := func(age time.Time) string {
		id := mustID(t)
		if err := store.Put(id, []byte("bytes")); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(filepath.Join(root, "objects", id[4:6], id), age, age)
		return id
	}
	healthy, deleted, orphan, youngOrphan := put(old), put(old), put(old), put(now)
	missing := mustID(t)
	staleTemp, youngTemp := filepath.Join(root, "tmp", "upload-old"), filepath.Join(root, "tmp", "upload-new")
	_ = os.WriteFile(staleTemp, nil, 0o640)
	_ = os.Chtimes(staleTemp, old, old)
	_ = os.WriteFile(youngTemp, nil, 0o640)
	foreign := filepath.Join(root, "objects", "README")
	_ = os.WriteFile(foreign, []byte("operator note"), 0o640)
	_ = os.Chtimes(foreign, old, old)
	rows := func(context.Context) (map[string]bool, error) {
		return map[string]bool{healthy: false, deleted: true, missing: false}, nil
	}

	report, err := Reconcile(context.Background(), store, rows, now, 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
	if report.Healthy != 1 || !report.Problems() {
		t.Fatalf("report %+v", report)
	}
	check("missing", report.Missing, missing)
	check("deleted", report.Deleted, deleted)
	check("orphans", report.Orphans, orphan)
	check("young orphans", report.YoungOrphans, youngOrphan)
	check("stale temp", report.StaleTemp, filepath.Join("tmp", "upload-old"))
	check("young temp", report.YoungTemp, filepath.Join("tmp", "upload-new"))
	if len(report.Removed) != 0 {
		t.Fatal("a dry run removed something")
	}
	for _, id := range []string{deleted, orphan} {
		if _, _, err := store.Open(id); err != nil {
			t.Fatalf("dry run removed %s", id)
		}
	}

	if _, err := Reconcile(context.Background(), store, rows, now, time.Minute, true); err == nil {
		t.Fatal("apply with a short grace period")
	}
	report, err = Reconcile(context.Background(), store, rows, now, 24*time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Removed) != 3 {
		t.Fatalf("removed %v", report.Removed)
	}
	for id, present := range map[string]bool{healthy: true, youngOrphan: true, deleted: false, orphan: false} {
		if _, _, err := store.Open(id); (err == nil) != present {
			t.Errorf("%s present=%v after apply", id, err == nil)
		}
	}
	for path, present := range map[string]bool{staleTemp: false, youngTemp: true, foreign: true} {
		if _, err := os.Stat(path); (err == nil) != present {
			t.Errorf("%s present=%v after apply", path, err == nil)
		}
	}
}

func TestReconcileKeepsOrphansWhenTheDatabaseLooksWrong(t *testing.T) {
	store, root := newStore(t)
	id := mustID(t)
	_ = store.Put(id, bytes.Repeat([]byte("x"), 10))
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(filepath.Join(root, "objects", id[4:6], id), old, old)
	empty := func(context.Context) (map[string]bool, error) { return map[string]bool{}, nil }
	report, err := Reconcile(context.Background(), store, empty, time.Now(), 24*time.Hour, true)
	if err != nil || report.Refused == "" || len(report.Removed) != 0 {
		t.Fatalf("%+v %v", report, err)
	}
	if _, _, err := store.Open(id); err != nil {
		t.Fatal("orphan removed against an empty database")
	}
}

func TestOpenFileStoreReadOnlyCreatesNothing(t *testing.T) {
	root := t.TempDir()
	if _, err := OpenFileStoreReadOnly(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("read-only open created %+v", entries)
	}
	if _, err := OpenFileStoreReadOnly("relative/path"); err == nil {
		t.Fatal("relative root accepted")
	}
}
