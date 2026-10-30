package application

import (
	"bytes"
	"context"
	"errors"
	"image"
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
	live    map[string]domain.StoredMedia
	deleted []string
	set     []domain.ProductMedia
}

func (f *fakeMediaStore) CreateMediaAsset(_ context.Context, _ domain.StaffMutation, asset domain.MediaAsset) (domain.MediaAsset, error) {
	f.live[asset.ID] = domain.StoredMedia{ID: asset.ID, ContentType: asset.ContentType, SizeBytes: asset.SizeBytes, SHA256: asset.SHA256}
	return asset, nil
}

func (f *fakeMediaStore) DeleteMediaAsset(_ context.Context, _ domain.StaffMutation, id string) error {
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

// fakeFiles is an in-memory media.Store whose removals can fail.
type fakeFiles struct {
	objects   map[string][]byte
	removeErr error
}

func (f *fakeFiles) Put(id string, data []byte) error { f.objects[id] = data; return nil }
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
	store := &fakeMediaStore{live: map[string]domain.StoredMedia{}}
	files := &fakeFiles{objects: map[string][]byte{}}
	return NewMedia(store, files, slog.New(slog.NewTextHandler(io.Discard, nil))), store, files
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
		"no revision":     {0, domain.ProductMedia{ProductID: "p01"}, "expectedRevision"},
		"bad primary":     {1, domain.ProductMedia{ProductID: "p01", PrimaryID: "/assets/x.webp"}, "primaryId"},
		"repeated":        {1, domain.ProductMedia{ProductID: "p01", GalleryIDs: []string{a, a}}, "galleryIds"},
		"too many images": {1, domain.ProductMedia{ProductID: "p01", GalleryIDs: manyIDs(MaxGalleryImages + 1)}, "galleryIds"},
	} {
		_, err := library.SetProductMedia(context.Background(), mutation, tc.revision, tc.value)
		if got := strings.Join(fieldNames(err), ","); got != tc.fields {
			t.Errorf("%s: %q", name, got)
		}
	}
	if len(store.set) != 0 {
		t.Fatal("invalid input reached the store")
	}
	got, err := library.SetProductMedia(context.Background(), mutation, 3, domain.ProductMedia{ProductID: "p01", PrimaryID: a, GalleryIDs: []string{b, a}})
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
