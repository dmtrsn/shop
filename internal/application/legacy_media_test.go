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
	"reflect"
	"testing"

	"sweetshop/internal/domain"
	"sweetshop/internal/media"
)

type fakeLegacyStore struct {
	MediaStore
	products    []domain.LegacyMediaProduct
	refs        map[string]*domain.LegacyPathReferences
	byDigest    map[string]domain.StoredMedia
	assets      []domain.MediaAsset
	thumbnails  []domain.MediaThumbnail
	migrated    map[string]domain.ProductMedia
	retired     []string
	attachE     error
	locks       int
	releaseErr  error
	productsErr error
	released    bool
}

func (s *fakeLegacyStore) LockLegacyMigration(context.Context) (func() error, error) {
	s.locks++
	return func() error { s.released = true; return s.releaseErr }, nil
}
func (s *fakeLegacyStore) LegacyMediaProducts(context.Context) ([]domain.LegacyMediaProduct, error) {
	return s.products, s.productsErr
}
func (s *fakeLegacyStore) LiveMediaBySHA256(_ context.Context, sum string) (domain.StoredMedia, error) {
	if stored, ok := s.byDigest[sum]; ok {
		return stored, nil
	}
	return domain.StoredMedia{}, domain.ErrNotFound
}
func (s *fakeLegacyStore) MigrateLegacyProductMedia(_ context.Context, _ domain.StaffMutation, _ domain.LegacyMediaProduct, value domain.ProductMedia) error {
	if s.attachE != nil {
		return s.attachE
	}
	s.migrated[value.ProductID] = value
	return nil
}
func (s *fakeLegacyStore) RetireLegacyProductMedia(_ context.Context, _ domain.StaffMutation, id string) error {
	s.retired = append(s.retired, id)
	return nil
}
func (s *fakeLegacyStore) LegacyMediaReferences(context.Context) (map[string]*domain.LegacyPathReferences, error) {
	return s.refs, nil
}
func (s *fakeLegacyStore) CreateMediaAsset(_ context.Context, _ domain.StaffMutation, asset domain.MediaAsset, thumbnail *domain.MediaThumbnail) (domain.MediaAsset, error) {
	s.assets = append(s.assets, asset)
	if thumbnail != nil {
		s.thumbnails = append(s.thumbnails, *thumbnail)
	}
	s.byDigest[asset.SHA256] = domain.StoredMedia{ID: asset.ID, ContentType: asset.ContentType, SizeBytes: asset.SizeBytes, SHA256: asset.SHA256}
	return asset, nil
}

func TestLegacyMigrationCleanupErrorPrecedence(t *testing.T) {
	primary := errors.New("list products failed")
	cleanup := errors.New("unlock failed")
	for _, tc := range []struct {
		name             string
		primary, cleanup error
	}{
		{"success", nil, nil},
		{"operation error", primary, nil},
		{"cleanup error", nil, cleanup},
		{"both errors", primary, cleanup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newLegacyStore()
			store.productsErr, store.releaseErr = tc.primary, tc.cleanup
			migration, _, _ := legacyHarness(t, store)
			report, err := migration.Run(context.Background(), true, staffActor(), "cleanup-test")
			if !store.released || report.Mode != "apply" {
				t.Fatalf("cleanup skipped or report lost: released=%v report=%+v", store.released, report)
			}
			if tc.primary == nil && tc.cleanup == nil && err != nil {
				t.Fatal(err)
			}
			for _, want := range []error{tc.primary, tc.cleanup} {
				if want != nil && !errors.Is(err, want) {
					t.Fatalf("lost error %v: %v", want, err)
				}
			}
			if tc.primary != nil && tc.cleanup == nil && err != tc.primary {
				t.Fatalf("primary error changed: %v", err)
			}
			if tc.primary != nil && tc.cleanup != nil && err.Error() != primary.Error()+"\n"+cleanup.Error() {
				t.Fatalf("operation error must come first: %v", err)
			}
		})
	}
}

func samplePNG(t *testing.T, value uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.NRGBA{value, uint8(x * 10), uint8(y * 10), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// largePNG is larger than the thumbnail recipe's maximum side.
func largePNG(t *testing.T, value uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.SetNRGBA(x, y, color.NRGBA{value, uint8(x), uint8(y), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLegacyApplyDerivesThumbnailsOnlyForNewlyCreatedAssets(t *testing.T) {
	store := newLegacyStore(
		domain.LegacyMediaProduct{ID: "first", Image: "/assets/new.png", Gallery: []string{"/assets/older.png"}},
		domain.LegacyMediaProduct{ID: "second", Image: "/assets/new.png"},
	)
	migration, assets, files := legacyHarness(t, store)
	write(t, assets, "new.png", largePNG(t, 1))
	write(t, assets, "older.png", largePNG(t, 2))
	older, err := media.Normalize(largePNG(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	olderID, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if err := files.Put(olderID, older.Data); err != nil {
		t.Fatal(err)
	}
	store.byDigest[older.SHA256] = domain.StoredMedia{ID: olderID, ContentType: older.ContentType, SizeBytes: int64(len(older.Data)), SHA256: older.SHA256}
	report, err := migration.Run(context.Background(), true, staffActor(), "req-p21")
	if err != nil {
		t.Fatal(err)
	}
	first, second := entryByID(t, report, "first"), entryByID(t, report, "second")
	if first.Status != LegacyMigrated || second.Status != LegacyMigrated || first.Primary.AssetID != second.Primary.AssetID || first.Gallery[0].AssetID != olderID || !first.Gallery[0].Reused {
		t.Fatalf("first %+v second %+v", first, second)
	}
	if len(store.assets) != 1 || len(store.thumbnails) != 1 || store.thumbnails[0].Width != 640 || store.thumbnails[0].Height != 480 {
		t.Fatalf("assets %d thumbnails %+v", len(store.assets), store.thumbnails)
	}
}

func legacyHarness(t *testing.T, store *fakeLegacyStore) (*LegacyMediaMigration, string, *media.FileStore) {
	t.Helper()
	web := t.TempDir()
	assets := filepath.Join(web, "assets")
	if err := os.Mkdir(assets, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte(`<img src="/assets/static.webp">`), 0o640); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	files, err := media.OpenFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewLegacyMediaMigration(store, NewMedia(store, files, logger), assets, logger), assets, files
}

func write(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func staffActor() domain.StaffUser {
	return domain.StaffUser{ID: "stf_" + "0123456789abcdef0123456789abcdef"}
}

func entryByID(t *testing.T, report LegacyMediaReport, id string) LegacyProductReport {
	t.Helper()
	for _, entry := range report.Products {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("no entry for %s", id)
	return LegacyProductReport{}
}

func newLegacyStore(products ...domain.LegacyMediaProduct) *fakeLegacyStore {
	return &fakeLegacyStore{products: products, refs: map[string]*domain.LegacyPathReferences{},
		byDigest: map[string]domain.StoredMedia{}, migrated: map[string]domain.ProductMedia{}}
}

func TestLegacyDryRunClassifiesWithoutChanges(t *testing.T) {
	store := newLegacyStore(
		domain.LegacyMediaProduct{ID: "legacy", Image: "/assets/a.png", Gallery: []string{"/assets/b.png", "/assets/a.png"}},
		domain.LegacyMediaProduct{ID: "managed", Image: "/assets/a.png", ManagedPrimary: true},
		domain.LegacyMediaProduct{ID: "none"},
		domain.LegacyMediaProduct{ID: "partial", Image: "/assets/a.png", ManagedGallery: 2},
		domain.LegacyMediaProduct{ID: "missing", Image: "/assets/gone.png"},
		domain.LegacyMediaProduct{ID: "corrupt", Image: "/assets/corrupt.png"},
		domain.LegacyMediaProduct{ID: "odd", Image: "https://cdn.example/x.png"},
		domain.LegacyMediaProduct{ID: "migrated", Image: "/assets/a.png", Retired: true},
	)
	migration, assets, _ := legacyHarness(t, store)
	write(t, assets, "a.png", samplePNG(t, 1))
	write(t, assets, "b.png", samplePNG(t, 2))
	write(t, assets, "corrupt.png", []byte("\x89PNG\r\n\x1a\ntruncated"))
	report, err := migration.Run(context.Background(), false, domain.StaffUser{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if store.locks != 0 || len(store.assets) != 0 || len(store.migrated) != 0 || len(store.retired) != 0 {
		t.Fatalf("dry run mutated: %+v", store)
	}
	want := map[string]LegacyMediaStatus{"legacy": LegacyNeedsMigration, "managed": LegacyAlreadyManaged, "none": LegacyNoMedia,
		"partial": LegacyConflict, "missing": LegacyFailed, "corrupt": LegacyFailed, "odd": LegacyFailed, "migrated": LegacyAlreadyMigrated}
	for id, status := range want {
		if entry := entryByID(t, report, id); entry.Status != status {
			t.Fatalf("%s: %+v", id, entry)
		}
	}
	entry := entryByID(t, report, "legacy")
	if entry.Primary == nil || entry.Primary.Source != "/assets/a.png" || len(entry.Gallery) != 1 || entry.Gallery[0].Source != "/assets/b.png" {
		t.Fatalf("mapping %+v", entry)
	}
	for id, kind := range map[string]string{"missing": LegacyMissingFile, "corrupt": LegacyInvalidImage, "odd": LegacyUnsupportedPath} {
		if problems := entryByID(t, report, id).Problems; len(problems) != 1 || problems[0].Kind != kind {
			t.Fatalf("%s: %+v", id, problems)
		}
	}
	if report.Pending() != true || !reflect.DeepEqual(report.ActiveLegacyProducts, []string{"legacy", "partial", "missing", "corrupt", "odd"}) {
		t.Fatalf("active %+v", report.ActiveLegacyProducts)
	}
}

func TestLegacyApplyDeduplicatesAndRetiresOnly(t *testing.T) {
	same := samplePNG(t, 7)
	store := newLegacyStore(
		domain.LegacyMediaProduct{ID: "first", Image: "/assets/a.png", Gallery: []string{"/assets/same.png", "/assets/b.png"}},
		domain.LegacyMediaProduct{ID: "second", Image: "/assets/a.png"},
		domain.LegacyMediaProduct{ID: "kept", Image: "/assets/a.png", ManagedPrimary: true},
	)
	migration, assets, _ := legacyHarness(t, store)
	write(t, assets, "a.png", same)
	write(t, assets, "same.png", same)
	write(t, assets, "b.png", samplePNG(t, 9))
	report, err := migration.Run(context.Background(), true, staffActor(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if store.locks != 1 || len(store.assets) != 2 {
		t.Fatalf("assets %+v locks %d", store.assets, store.locks)
	}
	first, second := store.migrated["first"], store.migrated["second"]
	if first.PrimaryID == "" || len(first.GalleryIDs) != 1 || first.GalleryIDs[0] == first.PrimaryID {
		t.Fatalf("first %+v", first)
	}
	if second.PrimaryID != first.PrimaryID || len(second.GalleryIDs) != 0 {
		t.Fatalf("shared file imported twice: %+v %+v", first, second)
	}
	if !reflect.DeepEqual(store.retired, []string{"kept"}) || entryByID(t, report, "kept").Retired != true {
		t.Fatalf("retired %+v", store.retired)
	}
	if entry := entryByID(t, report, "first"); len(entry.Duplicates) != 1 || entry.Duplicates[0] != "/assets/same.png" {
		t.Fatalf("duplicates %+v", entry)
	}
	if report.Pending() {
		t.Fatalf("pending %+v", report.Summary)
	}
}

func TestLegacyApplyLeavesAFailedProductUntouched(t *testing.T) {
	store := newLegacyStore(
		domain.LegacyMediaProduct{ID: "bad", Image: "/assets/a.png", Gallery: []string{"/assets/gone.png"}},
		domain.LegacyMediaProduct{ID: "stale", Image: "/assets/a.png"},
	)
	migration, assets, _ := legacyHarness(t, store)
	write(t, assets, "a.png", samplePNG(t, 3))
	store.attachE = domain.ErrConflict
	report, err := migration.Run(context.Background(), true, staffActor(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.migrated) != 0 {
		t.Fatalf("attached %+v", store.migrated)
	}
	if entry := entryByID(t, report, "bad"); entry.Status != LegacyFailed || entry.Problems[0].Kind != LegacyMissingFile || entry.Primary != nil {
		t.Fatalf("bad %+v", entry)
	}
	if entry := entryByID(t, report, "stale"); entry.Status != LegacyConflict || entry.Retired {
		t.Fatalf("stale %+v", entry)
	}
}

func TestLegacyApplyRefusesAMissingReusableObject(t *testing.T) {
	data := samplePNG(t, 5)
	normalized, err := media.Normalize(data)
	if err != nil {
		t.Fatal(err)
	}
	store := newLegacyStore(domain.LegacyMediaProduct{ID: "p", Image: "/assets/a.png"})
	store.byDigest[normalized.SHA256] = domain.StoredMedia{ID: "med_" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SizeBytes: 10, SHA256: normalized.SHA256}
	migration, assets, _ := legacyHarness(t, store)
	write(t, assets, "a.png", data)
	report, err := migration.Run(context.Background(), true, staffActor(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if entry := entryByID(t, report, "p"); entry.Status != LegacyFailed || entry.Problems[0].Kind != LegacyStorageError {
		t.Fatalf("entry %+v", entry)
	}
	if len(store.migrated) != 0 {
		t.Fatalf("attached %+v", store.migrated)
	}
}

func TestLegacyRetentionKeepsEveryConsumedFile(t *testing.T) {
	store := newLegacyStore()
	store.refs = map[string]*domain.LegacyPathReferences{
		"/assets/product.png": {Active: 1},
		"/assets/order.png":   {Orders: 2},
		"/assets/both.png":    {Active: 1, Orders: 1},
		"/assets/retired.png": {Compatibility: 3},
		"/assets/collect.png": {Other: 1},
		"/assets/lost.png":    {Orders: 1},
	}
	migration, assets, _ := legacyHarness(t, store)
	for _, name := range []string{"product.png", "order.png", "both.png", "retired.png", "collect.png", "static.webp", "unused.png"} {
		write(t, assets, name, []byte("x"))
	}
	report, err := migration.Run(context.Background(), false, domain.StaffUser{}, "")
	if err != nil {
		t.Fatal(err)
	}
	categories := map[string]string{}
	for _, file := range report.Files {
		categories[file.Path] = file.Category
	}
	want := map[string]string{
		"/assets/product.png": LegacyFileProduct, "/assets/order.png": LegacyFileOrder, "/assets/both.png": LegacyFileBoth,
		"/assets/collect.png": LegacyFileOther, "/assets/static.webp": LegacyFileOther, "/assets/retired.png": LegacyFileUnreferenced,
		"/assets/unused.png": LegacyFileUnreferenced, "/assets/lost.png": LegacyFileOrder,
	}
	if !reflect.DeepEqual(categories, want) {
		t.Fatalf("categories %+v", categories)
	}
	if !report.StaticScanned || !reflect.DeepEqual(report.SafeToDelete, []string{"/assets/retired.png", "/assets/unused.png"}) {
		t.Fatalf("safe %+v scanned %v", report.SafeToDelete, report.StaticScanned)
	}
	if !reflect.DeepEqual(report.MissingFiles, []string{"/assets/lost.png"}) {
		t.Fatalf("missing %+v", report.MissingFiles)
	}
}

func TestLegacyRetentionOffersNothingWithoutTheWebRoot(t *testing.T) {
	store := newLegacyStore()
	migration, assets, _ := legacyHarness(t, store)
	write(t, assets, "unused.png", []byte("x"))
	if err := os.Remove(filepath.Join(filepath.Dir(assets), "index.html")); err != nil {
		t.Fatal(err)
	}
	report, err := migration.Run(context.Background(), false, domain.StaffUser{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.StaticScanned || len(report.SafeToDelete) != 0 {
		t.Fatalf("report %+v", report)
	}
}
