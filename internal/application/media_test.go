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
	"strings"
	"testing"

	"sweetshop/internal/domain"
	"sweetshop/internal/media"
)

// fakeMediaStore embeds the port, so a call this test does not expect panics.
type fakeMediaStore struct {
	MediaStore
	live       map[string]domain.StoredMedia
	thumbnails map[string]*domain.MediaThumbnail
	deleted    []string
	set        []domain.ProductMedia
	createErr  error
	deleteErr  error
	// Simulate a committed metadata/audit transaction whose acknowledgement is lost.
	createErrAfterPersist error
	audit                 []string
}

func (f *fakeMediaStore) CreateMediaAsset(_ context.Context, _ domain.StaffMutation, asset domain.MediaAsset, thumbnail *domain.MediaThumbnail) (domain.MediaAsset, error) {
	if f.createErr != nil {
		return domain.MediaAsset{}, f.createErr
	}
	f.live[asset.ID] = domain.StoredMedia{ID: asset.ID, ContentType: asset.ContentType, SizeBytes: asset.SizeBytes, SHA256: asset.SHA256}
	f.thumbnails[asset.ID] = thumbnail
	f.audit = append(f.audit, asset.ID)
	if f.createErrAfterPersist != nil {
		return domain.MediaAsset{}, f.createErrAfterPersist
	}
	return asset, nil
}

func (f *fakeMediaStore) DeleteMediaAsset(_ context.Context, _ domain.StaffMutation, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	delete(f.live, id)
	return nil
}

func (f *fakeMediaStore) LiveMedia(_ context.Context, id string) (domain.StoredMedia, error) {
	stored, ok := f.live[id]
	if !ok {
		return domain.StoredMedia{}, domain.ErrNotFound
	}
	return stored, nil
}

func (f *fakeMediaStore) SetProductMedia(_ context.Context, _ domain.StaffMutation, _ int64, value domain.ProductMedia) (domain.ProductMedia, error) {
	f.set = append(f.set, value)
	return value, nil
}

// fakeFiles is an in-memory media.Store whose writes and removals can fail.
type fakeFiles struct {
	objects                   map[string][]byte
	thumbnails                map[string]map[string][]byte
	putErr                    error
	putThumbnailErr           error
	removeErr                 error
	putErrAfterWrite          error
	putThumbnailErrAfterWrite error
}

func (f *fakeFiles) Put(id string, data []byte) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.objects[id] = data
	return f.putErrAfterWrite
}
func (f *fakeFiles) PutThumbnail(id, sum string, data []byte) error {
	if f.putThumbnailErr != nil {
		return f.putThumbnailErr
	}
	if f.thumbnails[id] == nil {
		f.thumbnails[id] = map[string][]byte{}
	}
	f.thumbnails[id][sum] = data
	return f.putThumbnailErrAfterWrite
}
func (f *fakeFiles) RemoveThumbnails(id string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	delete(f.thumbnails, id)
	return nil
}
func (f *fakeFiles) Open(id string) (io.ReadSeekCloser, int64, error) {
	data, ok := f.objects[id]
	if !ok {
		return nil, 0, media.ErrNotStored
	}
	return nopCloser{bytes.NewReader(data)}, int64(len(data)), nil
}
func (f *fakeFiles) Remove(id string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	delete(f.objects, id)
	return nil
}
func (f *fakeFiles) Objects() ([]media.Object, error) { return nil, nil }
func (f *fakeFiles) RemoveTemp(string) error          { return nil }

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

func newMediaFixture() (*Media, *fakeMediaStore, *fakeFiles) {
	store := &fakeMediaStore{live: map[string]domain.StoredMedia{}, thumbnails: map[string]*domain.MediaThumbnail{}}
	files := &fakeFiles{objects: map[string][]byte{}, thumbnails: map[string]map[string][]byte{}}
	return NewMedia(store, files, slog.New(slog.NewTextHandler(io.Discard, nil))), store, files
}

// wide is an opaque image larger than the thumbnail recipe's maximum side.
func wide(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 3, 3)))
	return buf.Bytes()
}

func TestMediaMutationsNeedTheirOwnAction(t *testing.T) {
	library, store, files := newMediaFixture()
	ctx := context.Background()
	if _, err := library.Upload(ctx, staffMutationFor(t, MediaDelete), pngBytes(t)); err == nil {
		t.Fatal("a delete mutation uploaded")
	}
	if err := library.Delete(ctx, staffMutationFor(t, MediaUpload), "med_"+strings.Repeat("0", 32)); err == nil {
		t.Fatal("an upload mutation deleted")
	}
	if _, err := library.SetProductMedia(ctx, staffMutationFor(t, ProductUpdate), 1, domain.ProductMedia{ProductID: "p01"}); err == nil {
		t.Fatal("a product update mutation changed product media")
	}
	if len(files.objects)+len(store.deleted)+len(store.set) != 0 {
		t.Fatal("a mismatched mutation reached a store")
	}
}

func TestUploadMapsImageRefusals(t *testing.T) {
	library, _, files := newMediaFixture()
	mutation := staffMutationFor(t, MediaUpload)
	for name, tc := range map[string]struct {
		data []byte
		want error
	}{
		"svg":       {[]byte("<svg/>"), domain.ErrUnsupportedMedia},
		"too large": {make([]byte, media.MaxUploadBytes+1), domain.ErrMediaTooLarge},
		"truncated": {pngBytes(t)[:20], domain.ErrValidation},
	} {
		if _, err := library.Upload(context.Background(), mutation, tc.data); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(files.objects) != 0 {
		t.Fatal("a refused upload was stored")
	}
}

func TestDeleteStandsWhenTheUnlinkFails(t *testing.T) {
	library, store, files := newMediaFixture()
	asset, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), pngBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	files.removeErr = errors.New("read-only file system")
	if err := library.Delete(context.Background(), staffMutationFor(t, MediaDelete), asset.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(store.deleted) != 1 || files.objects[asset.ID] == nil {
		t.Fatal("expected a committed tombstone with the bytes left for reconciliation")
	}
	if _, _, err := library.Open(context.Background(), asset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a deleted asset is still served: %v", err)
	}
}

func TestUploadStoresOneThumbnailDerivedFromTheStoredOriginal(t *testing.T) {
	library, store, files := newMediaFixture()
	asset, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), wide(t))
	if err != nil {
		t.Fatal(err)
	}
	expected, ok, err := media.DeriveThumbnail(files.objects[asset.ID])
	if err != nil || !ok {
		t.Fatalf("derive from the stored original: %v %v", ok, err)
	}
	stored := files.thumbnails[asset.ID]
	if len(stored) != 1 || !bytes.Equal(stored[expected.SHA256], expected.Data) {
		t.Fatalf("stored thumbnails are not the original's derivative: %d", len(stored))
	}
	want := domain.MediaThumbnail{Recipe: media.ThumbnailRecipe, ContentType: media.ContentTypeJPEG, SizeBytes: int64(len(expected.Data)), Width: 640, Height: 360, SHA256: expected.SHA256}
	if row := store.thumbnails[asset.ID]; row == nil || *row != want {
		t.Fatalf("registered %+v, want %+v", row, want)
	}
	if asset.Width != 1280 || asset.Height != 720 {
		t.Fatalf("the original's metadata changed: %+v", asset)
	}
}

func TestSmallUploadGetsNoThumbnail(t *testing.T) {
	library, store, files := newMediaFixture()
	asset, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), pngBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(files.thumbnails) != 0 || store.thumbnails[asset.ID] != nil || files.objects[asset.ID] == nil {
		t.Fatalf("thumbnails %d row %+v", len(files.thumbnails), store.thumbnails[asset.ID])
	}
}

func TestFailedRegistrationRetainsFilesWhenRollbackIsNotExposed(t *testing.T) {
	library, store, files := newMediaFixture()
	store.createErr = errors.New("database down")
	if _, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), wide(t)); !errors.Is(err, store.createErr) {
		t.Fatalf("upload: %v", err)
	}
	if len(files.objects) != 1 || len(files.thumbnails) != 1 || len(store.live) != 0 {
		t.Fatal("a registration error must retain files even when the fake did not persist rows")
	}
}

func TestPersistedThenErrorRegistrationRetainsOriginalAndThumbnail(t *testing.T) {
	library, store, files := newMediaFixture()
	store.createErrAfterPersist = errors.New("commit acknowledgement lost")
	if _, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), wide(t)); !errors.Is(err, store.createErrAfterPersist) {
		t.Fatalf("upload: %v", err)
	}
	if len(store.live) != 1 || len(store.audit) != 1 {
		t.Fatalf("fake must persist metadata and audit before returning an error: %+v, %v", store.live, store.audit)
	}
	id := store.audit[0]
	stored, row := store.live[id], store.thumbnails[id]
	if row == nil {
		t.Fatal("fake did not persist the thumbnail row")
	}
	if data := files.objects[id]; int64(len(data)) != stored.SizeBytes {
		t.Error("persisted original was removed after an uncertain registration")
	}
	if data := files.thumbnails[id][row.SHA256]; int64(len(data)) != row.SizeBytes {
		t.Error("persisted thumbnail was removed after an uncertain registration")
	}
}

func TestFailedThumbnailWriteRemovesOriginalAndRegistersNothing(t *testing.T) {
	library, store, files := newMediaFixture()
	files.putThumbnailErr = errors.New("disk full")
	if _, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), wide(t)); !errors.Is(err, domain.ErrStorageUnavailable) {
		t.Fatalf("upload: %v", err)
	}
	if len(files.objects)+len(files.thumbnails)+len(store.live) != 0 {
		t.Fatal("a failed thumbnail write left state behind")
	}
}

func TestFailedThumbnailSyncCleansFilesBeforeRegistration(t *testing.T) {
	library, store, files := newMediaFixture()
	files.putThumbnailErrAfterWrite = errors.New("directory sync failed after publication")
	if _, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), wide(t)); !errors.Is(err, domain.ErrStorageUnavailable) {
		t.Fatalf("upload: %v", err)
	}
	// Both files were published, but no metadata transaction was attempted.
	if len(files.objects)+len(files.thumbnails)+len(store.live)+len(store.audit) != 0 {
		t.Fatal("pre-registration thumbnail failure left partial work")
	}
}

func TestFailedOriginalSyncLeavesRecoverableFileAndRegistersNothing(t *testing.T) {
	library, store, files := newMediaFixture()
	files.putErrAfterWrite = errors.New("directory sync failed after publication")
	if _, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), wide(t)); !errors.Is(err, domain.ErrStorageUnavailable) {
		t.Fatalf("upload: %v", err)
	}
	if len(files.objects) != 1 || len(files.thumbnails)+len(store.live)+len(store.audit) != 0 {
		t.Fatal("original publication failure must retain its leftover without registering metadata")
	}
}

func TestFailedOriginalWriteWritesNoThumbnail(t *testing.T) {
	library, store, files := newMediaFixture()
	files.putErr = errors.New("disk full")
	if _, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), wide(t)); !errors.Is(err, domain.ErrStorageUnavailable) {
		t.Fatalf("upload: %v", err)
	}
	if len(files.objects)+len(files.thumbnails)+len(store.live) != 0 {
		t.Fatal("a failed write left state behind")
	}
}

func TestDeleteRemovesTheThumbnailOnlyAfterTheStoreAgrees(t *testing.T) {
	library, store, files := newMediaFixture()
	asset, err := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), wide(t))
	if err != nil {
		t.Fatal(err)
	}
	store.deleteErr = &domain.ReferenceConflictError{Products: 1}
	if err := library.Delete(context.Background(), staffMutationFor(t, MediaDelete), asset.ID); err == nil {
		t.Fatal("a referenced asset was deleted")
	}
	if len(files.thumbnails[asset.ID]) != 1 || files.objects[asset.ID] == nil {
		t.Fatal("a refused delete removed files")
	}
	store.deleteErr = nil
	if err := library.Delete(context.Background(), staffMutationFor(t, MediaDelete), asset.ID); err != nil {
		t.Fatal(err)
	}
	if files.thumbnails[asset.ID] != nil || files.objects[asset.ID] != nil {
		t.Fatal("a delete left files behind")
	}
}

func TestOpenRefusesMissingOrMismatchedBytes(t *testing.T) {
	library, store, files := newMediaFixture()
	asset, _ := library.Upload(context.Background(), staffMutationFor(t, MediaUpload), pngBytes(t))
	files.objects[asset.ID] = []byte("short")
	if _, _, err := library.Open(context.Background(), asset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("size mismatch served: %v", err)
	}
	delete(files.objects, asset.ID)
	if _, _, err := library.Open(context.Background(), asset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing file: %v", err)
	}
	if _, _, err := library.Open(context.Background(), "../etc/passwd"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("malformed id: %v", err)
	}
	_ = store
}

func TestSetProductMediaValidatesBeforeTheStore(t *testing.T) {
	library, store, _ := newMediaFixture()
	mutation := staffMutationFor(t, ProductMediaUpdate)
	a, b := "med_"+strings.Repeat("a", 32), "med_"+strings.Repeat("b", 32)
	for name, tc := range map[string]struct {
		revision int64
		value    domain.ProductMedia
		fields   string
	}{
		"no revision":      {0, domain.ProductMedia{ProductID: "p01"}, "expectedRevision"},
		"bad primary":      {1, domain.ProductMedia{ProductID: "p01", PrimaryID: "/assets/x.webp"}, "primaryId"},
		"primary repeated": {1, domain.ProductMedia{ProductID: "p01", PrimaryID: a, GalleryIDs: []string{a}}, "galleryIds"},
		"repeated":         {1, domain.ProductMedia{ProductID: "p01", GalleryIDs: []string{a, a}}, "galleryIds"},
		"too many images":  {1, domain.ProductMedia{ProductID: "p01", GalleryIDs: manyIDs(MaxGalleryImages + 1)}, "galleryIds"},
	} {
		_, err := library.SetProductMedia(context.Background(), mutation, tc.revision, tc.value)
		if got := strings.Join(fieldNames(err), ","); got != tc.fields {
			t.Errorf("%s: %q", name, got)
		}
	}
	if len(store.set) != 0 {
		t.Fatal("invalid input reached the store")
	}
	got, err := library.SetProductMedia(context.Background(), mutation, 3, domain.ProductMedia{ProductID: "p01", PrimaryID: a, GalleryIDs: []string{b}})
	if err != nil || got.PrimaryID != a || len(store.set) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

func manyIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "med_" + strings.Repeat("0", 30) + string(rune('a'+i/16)) + "0123456789abcdef"[i%16:i%16+1]
	}
	return ids
}
