package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/media"
)

// testMediaStore keeps media rows in memory and records audited mutations.
type testMediaStore struct {
	mu         sync.Mutex
	rows       map[string]domain.MediaAsset
	deleted    map[string]bool
	references map[string]int
	thumbnails map[string]*domain.MediaThumbnail
	createErr  error
	mutations  []domain.StaffMutation
}

func newTestMediaStore() *testMediaStore {
	return &testMediaStore{rows: map[string]domain.MediaAsset{}, deleted: map[string]bool{}, references: map[string]int{}, thumbnails: map[string]*domain.MediaThumbnail{}}
}

func (s *testMediaStore) CreateMediaAsset(_ context.Context, m domain.StaffMutation, asset domain.MediaAsset, thumbnail *domain.MediaThumbnail) (domain.MediaAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return domain.MediaAsset{}, s.createErr
	}
	asset.CreatedAt, asset.URL = time.Now().UTC(), "/media/"+asset.ID
	if thumbnail != nil {
		s.thumbnails[asset.ID] = thumbnail
		asset.ThumbnailURL = "/media/" + asset.ID + "/thumbnail/" + thumbnail.SHA256
	}
	s.rows[asset.ID] = asset
	s.mutations = append(s.mutations, m)
	return asset, nil
}

func (s *testMediaStore) MediaAssets(context.Context, int, int) (domain.MediaPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page := domain.MediaPage{Items: []domain.MediaAsset{}, Page: 1, Limit: 50, TotalPages: 1}
	for id, asset := range s.rows {
		if !s.deleted[id] {
			page.Items = append(page.Items, asset)
		}
	}
	page.Total = len(page.Items)
	return page, nil
}

func (s *testMediaStore) MediaAsset(_ context.Context, id string) (domain.MediaAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	asset, ok := s.rows[id]
	if !ok || s.deleted[id] {
		return domain.MediaAsset{}, domain.ErrNotFound
	}
	return asset, nil
}

func (s *testMediaStore) LiveMedia(ctx context.Context, id string) (domain.StoredMedia, error) {
	asset, err := s.MediaAsset(ctx, id)
	return domain.StoredMedia{ID: asset.ID, ContentType: asset.ContentType, SizeBytes: asset.SizeBytes, SHA256: asset.SHA256}, err
}

func (s *testMediaStore) LiveMediaThumbnail(_ context.Context, id string) (domain.MediaThumbnail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thumbnail := s.thumbnails[id]
	if _, ok := s.rows[id]; !ok || s.deleted[id] || thumbnail == nil {
		return domain.MediaThumbnail{}, domain.ErrNotFound
	}
	return *thumbnail, nil
}

func (s *testMediaStore) MediaThumbnailStates(context.Context) ([]domain.MediaThumbnailState, error) {
	return nil, nil
}

func (s *testMediaStore) LiveMediaThumbnailStates(context.Context, string, int) ([]domain.MediaThumbnailState, error) {
	return nil, nil
}

func (s *testMediaStore) SaveMediaThumbnail(context.Context, string, domain.MediaThumbnail) error {
	return nil
}

func (s *testMediaStore) DeleteMediaAsset(_ context.Context, m domain.StaffMutation, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok || s.deleted[id] {
		return domain.ErrNotFound
	}
	if s.references[id] > 0 {
		return &domain.ReferenceConflictError{Products: s.references[id]}
	}
	s.deleted[id] = true
	s.mutations = append(s.mutations, m)
	return nil
}

func (s *testMediaStore) MediaRows(context.Context) (map[string]bool, error) { return nil, nil }
func (s *testMediaStore) ProductMedia(context.Context, string) (domain.ProductMedia, error) {
	return domain.ProductMedia{}, nil
}
func (s *testMediaStore) SetProductMedia(context.Context, domain.StaffMutation, int64, domain.ProductMedia) (domain.ProductMedia, error) {
	return domain.ProductMedia{}, nil
}

func mediaHandler(t *testing.T, store *testMediaStore) (http.Handler, *testStaffStore, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "media")
	_ = os.Mkdir(root, 0o750)
	files, err := media.OpenFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	web := t.TempDir()
	_ = os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>legacy</html>"), 0o644)
	staff := newTestStaffStore(t)
	handler := New(customerService(), discard(), web, "memory", SessionConfig{Secure: true, TrustedOrigins: []string{shopOrigin}},
		WithStaff(application.NewStaffAuth(staff), StaffConfig{TrustedOrigins: []string{adminOrigin}}),
		WithMedia(application.NewMedia(store, files, discard())))
	return handler, staff, root
}

func testPNG(t *testing.T, w, h int, alpha uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 7), uint8(y * 5), 90, alpha})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// multipartBody builds an upload with the given parts: field name, declared
// file name and type, bytes.
type uploadPart struct {
	field, filename, contentType string
	data                         []byte
}

func multipartBody(t *testing.T, parts ...uploadPart) (string, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="`+part.field+`"; filename="`+part.filename+`"`)
		header.Set("Content-Type", part.contentType)
		w, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(part.data)
	}
	_ = writer.Close()
	return body.String(), writer.FormDataContentType()
}

func upload(handler http.Handler, headers map[string]string, body, contentType string) *httptest.ResponseRecorder {
	h := map[string]string{"Content-Type": contentType}
	for k, v := range headers {
		h[k] = v
	}
	return call(handler, "POST", "/api/admin/v1/media", body, h)
}

func TestMediaRoutesAcceptOnlyTheStaffSession(t *testing.T) {
	store := newTestMediaStore()
	handler, _, _ := mediaHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	customer := call(handler, "POST", "/api/auth/session", `{"email":"demo@sugar.ru","password":"Sweet2026!"}`, map[string]string{"Origin": shopOrigin})
	customerCookie := "__Host-sweetshop_session=" + cookieNamed(customer, "__Host-sweetshop_session").Value
	body, contentType := multipartBody(t, uploadPart{"file", "a.png", "image/png", testPNG(t, 4, 4, 255)})
	id := "med_" + strings.Repeat("0", 32)
	for name, tc := range map[string]struct {
		method, path string
		headers      map[string]string
		status       int
	}{
		"anonymous list":       {"GET", "/api/admin/v1/media", nil, 401},
		"anonymous assign":     {"PUT", "/api/admin/v1/products/p01/media", nil, 401},
		"customer assign":      {"PUT", "/api/admin/v1/products/p01/media", map[string]string{"Cookie": customerCookie}, 401},
		"assign without CSRF":  {"PUT", "/api/admin/v1/products/p01/media", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin}, 403},
		"assign shop origin":   {"PUT", "/api/admin/v1/products/p01/media", map[string]string{"Cookie": staffCookie, "Origin": shopOrigin, "X-CSRF-Token": csrf}, 403},
		"assign changed staff": {"PUT", "/api/admin/v1/products/p01/media", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, expectedStaffHeader: "stf_" + strings.Repeat("0", 32)}, 409},
		"customer list":        {"GET", "/api/admin/v1/media", map[string]string{"Cookie": customerCookie}, 401},
		"customer upload":      {"POST", "/api/admin/v1/media", map[string]string{"Cookie": customerCookie, "Origin": adminOrigin, "Content-Type": contentType}, 401},
		"bearer":               {"GET", "/api/admin/v1/media", map[string]string{"Cookie": staffCookie, "Authorization": "Bearer x"}, 401},
		"upload without CSRF":  {"POST", "/api/admin/v1/media", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "Content-Type": contentType}, 403},
		"upload from shop":     {"POST", "/api/admin/v1/media", map[string]string{"Cookie": staffCookie, "Origin": shopOrigin, "X-CSRF-Token": csrf, "Content-Type": contentType}, 403},
		"delete without CSRF":  {"DELETE", "/api/admin/v1/media/" + id, map[string]string{"Cookie": staffCookie, "Origin": adminOrigin}, 403},
		"other expected staff": {"DELETE", "/api/admin/v1/media/" + id, map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, expectedStaffHeader: "stf_" + strings.Repeat("0", 32)}, 409},
		// Bytes never change under an ID: there is no PUT (the API fallback answers 404).
		"no replacing PUT":      {"PUT", "/api/admin/v1/media/" + id, map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}, 404},
		"staff list":            {"GET", "/api/admin/v1/media", map[string]string{"Cookie": staffCookie}, 200},
		"staff unknown asset":   {"GET", "/api/admin/v1/media/" + id, map[string]string{"Cookie": staffCookie}, 404},
		"staff malformed asset": {"GET", "/api/admin/v1/media/..%2Fetc", map[string]string{"Cookie": staffCookie}, 404},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := call(handler, tc.method, tc.path, body, tc.headers); rec.Code != tc.status {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		})
	}
	if len(store.mutations) != 0 || len(store.rows) != 0 {
		t.Fatal("a rejected request reached the store")
	}
}

func TestUploadedMediaIsNormalizedAndServedImmutably(t *testing.T) {
	store := newTestMediaStore()
	handler, staff, root := mediaHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, "X-Request-ID": "req-media"}
	// The declared name and type are ignored: the bytes are a PNG.
	body, contentType := multipartBody(t, uploadPart{"file", "../../evil.svg", "text/html", testPNG(t, 30, 20, 255)})
	rec := upload(handler, headers, body, contentType)
	if rec.Code != 201 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var asset domain.MediaAsset
	_ = json.Unmarshal(rec.Body.Bytes(), &asset)
	if !media.IDPattern.MatchString(asset.ID) || asset.ContentType != "image/jpeg" || asset.Width != 30 || asset.Height != 20 || asset.URL != "/media/"+asset.ID {
		t.Fatalf("asset %+v", asset)
	}
	if strings.Contains(rec.Body.String(), "evil") || strings.Contains(rec.Body.String(), root) {
		t.Fatal("the response echoes the file name or the storage path")
	}
	if len(store.mutations) != 1 || store.mutations[0].Action() != application.MediaUpload || store.mutations[0].Actor() != staff.credential.Staff.ID || store.mutations[0].RequestID() != "req-media" {
		t.Fatalf("mutations %+v", store.mutations)
	}
	stored, err := os.ReadFile(filepath.Join(root, "objects", asset.ID[4:6], asset.ID))
	if err != nil || int64(len(stored)) != asset.SizeBytes {
		t.Fatalf("stored file: %v", err)
	}

	get := call(handler, "GET", asset.URL, "", nil)
	h := get.Header()
	if get.Code != 200 || !bytes.Equal(get.Body.Bytes(), stored) || h.Get("Content-Type") != "image/jpeg" || h.Get("ETag") != `"`+asset.SHA256+`"` ||
		h.Get("Cache-Control") != "public, max-age=31536000, immutable" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Length") == "" ||
		!strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") || h.Get("Content-Disposition") != "" {
		t.Fatalf("GET %d %v", get.Code, h)
	}
	if _, err := jpeg.Decode(bytes.NewReader(get.Body.Bytes())); err != nil {
		t.Fatal("served bytes do not decode as JPEG")
	}
	if head := call(handler, "HEAD", asset.URL, "", nil); head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("ETag") != h.Get("ETag") {
		t.Fatalf("HEAD %d", head.Code)
	}
	if again := call(handler, "GET", asset.URL, "", map[string]string{"If-None-Match": h.Get("ETag")}); again.Code != 304 || again.Body.Len() != 0 {
		t.Fatalf("conditional GET %d", again.Code)
	}
	if post := call(handler, "POST", asset.URL, "", nil); post.Code != 405 {
		t.Fatalf("POST %d", post.Code)
	}

	// Delete: 204, then the public route refuses it and the bytes are gone.
	if del := call(handler, "DELETE", "/api/admin/v1/media/"+asset.ID, "", headers); del.Code != 204 {
		t.Fatalf("delete %d %s", del.Code, del.Body.String())
	}
	if gone := call(handler, "GET", asset.URL, "", nil); gone.Code != 404 || gone.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("after delete %d", gone.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "objects", asset.ID[4:6], asset.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted bytes remain")
	}
	if again := call(handler, "DELETE", "/api/admin/v1/media/"+asset.ID, "", headers); again.Code != 404 {
		t.Fatalf("second delete %d", again.Code)
	}
	if store.mutations[1].Action() != application.MediaDelete || len(store.mutations) != 2 {
		t.Fatalf("mutations %+v", store.mutations)
	}
}

func TestUploadRefusals(t *testing.T) {
	store := newTestMediaStore()
	handler, _, root := mediaHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}
	png4 := testPNG(t, 4, 4, 255)
	part := func(field string, data []byte) uploadPart { return uploadPart{field, "x.png", "image/png", data} }
	for name, tc := range map[string]struct {
		parts       []uploadPart
		contentType string
		status      int
		code        string
	}{
		"json body":      {nil, "application/json", 422, "validation_error"},
		"no file":        {[]uploadPart{}, "", 422, "validation_error"},
		"other field":    {[]uploadPart{part("image", png4)}, "", 422, "validation_error"},
		"two files":      {[]uploadPart{part("file", png4), part("file", png4)}, "", 422, "validation_error"},
		"svg":            {[]uploadPart{{"file", "x.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)}}, "", 415, "unsupported_media_type"},
		"html as png":    {[]uploadPart{{"file", "x.png", "image/png", []byte("<html><script>alert(1)</script>")}}, "", 415, "unsupported_media_type"},
		"truncated":      {[]uploadPart{part("file", png4[:len(png4)/2])}, "", 422, "validation_error"},
		"too large file": {[]uploadPart{part("file", make([]byte, media.MaxUploadBytes+1))}, "", 413, "media_too_large"},
		"too large body": {[]uploadPart{part("file", png4), part("pad", make([]byte, media.MaxUploadBytes+multipartOverhead))}, "", 422, "validation_error"},
	} {
		t.Run(name, func(t *testing.T) {
			body, contentType := multipartBody(t, tc.parts...)
			if tc.contentType != "" {
				body, contentType = `{"file":"x"}`, tc.contentType
			}
			rec := upload(handler, headers, body, contentType)
			code, _ := errorBody(t, rec.Body.String())
			if rec.Code != tc.status || code != tc.code {
				t.Fatalf("%d %s %s", rec.Code, code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), root) {
				t.Fatal("storage path leaked")
			}
		})
	}
	// An oversized body is refused while reading, whatever its parts.
	big, contentType := multipartBody(t, part("file", make([]byte, media.MaxUploadBytes+multipartOverhead+1)))
	if rec := upload(handler, headers, big, contentType); rec.Code != 413 {
		t.Fatalf("oversized body: %d", rec.Code)
	}
	if len(store.mutations) != 0 || len(store.rows) != 0 {
		t.Fatal("a refused upload was registered")
	}
	if objects, _ := os.ReadDir(filepath.Join(root, "objects")); len(objects) != 0 {
		t.Fatal("a refused upload left bytes")
	}
}

func TestFailedRegistrationRetainsTheNewObjectWithoutExposingTheError(t *testing.T) {
	store := newTestMediaStore()
	store.createErr = errors.New("database down")
	handler, _, root := mediaHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	body, contentType := multipartBody(t, uploadPart{"file", "a.png", "image/png", testPNG(t, 4, 4, 255)})
	rec := upload(handler, map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}, body, contentType)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "database down") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	files, err := media.OpenFileStoreReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := files.Objects()
	if err != nil || len(objects) != 1 || objects[0].Temp {
		t.Fatalf("registration outcome is not exposed; keep the object: %+v, %v", objects, err)
	}
	if len(store.rows) != 0 || len(store.mutations) != 0 {
		t.Fatal("fake registered an asset on failure")
	}
}

func TestMediaDeleteRefusesReferencedAssetsAndMissingFilesAre404(t *testing.T) {
	store := newTestMediaStore()
	handler, _, root := mediaHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}
	body, contentType := multipartBody(t, uploadPart{"file", "a.png", "image/png", testPNG(t, 4, 4, 128)})
	var asset domain.MediaAsset
	_ = json.Unmarshal(upload(handler, headers, body, contentType).Body.Bytes(), &asset)
	if asset.ContentType != "image/png" {
		t.Fatalf("translucent upload stored as %q", asset.ContentType)
	}
	store.references[asset.ID] = 2
	rec := call(handler, "DELETE", "/api/admin/v1/media/"+asset.ID, "", headers)
	var conflict struct {
		Error struct {
			Code       string
			References map[string]int
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &conflict)
	if rec.Code != 409 || conflict.Error.Code != "reference_conflict" || conflict.Error.References["products"] != 2 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if call(handler, "GET", asset.URL, "", nil).Code != 200 {
		t.Fatal("a refused delete removed the asset")
	}
	// A live row whose file vanished is 404, not 500, and leaks nothing.
	_ = os.Remove(filepath.Join(root, "objects", asset.ID[4:6], asset.ID))
	if missing := call(handler, "GET", asset.URL, "", nil); missing.Code != 404 || strings.Contains(missing.Body.String(), root) {
		t.Fatalf("missing file: %d", missing.Code)
	}
}

func TestMediaRouteShapesNeverReachTheStaticFallback(t *testing.T) {
	handler, _, _ := mediaHandler(t, newTestMediaStore())
	for _, path := range []string{"/media", "/media/", "/media/unknown", "/media/med_" + strings.Repeat("0", 32), "/media/med_" + strings.Repeat("0", 32) + "/x", "/media/MED_" + strings.Repeat("0", 32),
		"/media/med_" + strings.Repeat("0", 32) + "/thumbnail/" + strings.Repeat("0", 64), "/media/med_" + strings.Repeat("0", 32) + "/thumbnail", "/media/med_" + strings.Repeat("0", 32) + "/thumbnail/" + strings.Repeat("0", 64) + "/x"} {
		rec := call(handler, "GET", path, "", nil)
		if rec.Code != 404 || strings.Contains(rec.Body.String(), "legacy") {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	// Without managed media, /media is still 404 rather than the legacy shell.
	web := t.TempDir()
	_ = os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>legacy</html>"), 0o644)
	plain := New(customerService(), discard(), web, "memory", SessionConfig{TrustedOrigins: []string{shopOrigin}})
	if rec := call(plain, "GET", "/media/med_"+strings.Repeat("0", 32), "", nil); rec.Code != 404 || strings.Contains(rec.Body.String(), "legacy") {
		t.Fatalf("media disabled: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(plain, "GET", "/catalog", "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "legacy") {
		t.Fatal("the legacy fallback changed")
	}
}

func TestThumbnailRouteServesOnlyTheCurrentDigestOfALiveParent(t *testing.T) {
	store := newTestMediaStore()
	handler, _, root := mediaHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}
	body, contentType := multipartBody(t, uploadPart{"file", "a.png", "image/png", testPNG(t, 900, 600, 255)})
	rec := upload(handler, headers, body, contentType)
	if rec.Code != 201 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var asset domain.MediaAsset
	_ = json.Unmarshal(rec.Body.Bytes(), &asset)
	row := store.thumbnails[asset.ID]
	if row == nil || asset.ThumbnailURL != "/media/"+asset.ID+"/thumbnail/"+row.SHA256 {
		t.Fatalf("asset %+v row %+v", asset, row)
	}
	thumbnailPath := filepath.Join(root, "thumbnails", asset.ID[4:6], asset.ID, row.SHA256)
	stored, err := os.ReadFile(thumbnailPath)
	if err != nil {
		t.Fatal(err)
	}

	get := call(handler, "GET", asset.ThumbnailURL, "", nil)
	h := get.Header()
	if get.Code != 200 || !bytes.Equal(get.Body.Bytes(), stored) || h.Get("Content-Type") != row.ContentType || h.Get("Content-Length") != strconv.Itoa(len(stored)) ||
		h.Get("ETag") != `"`+row.SHA256+`"` || h.Get("Cache-Control") != "public, max-age=31536000, immutable" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Fatalf("GET %d %v", get.Code, h)
	}
	if head := call(handler, "HEAD", asset.ThumbnailURL, "", nil); head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("ETag") != h.Get("ETag") ||
		head.Header().Get("Content-Length") != h.Get("Content-Length") || head.Header().Get("Cache-Control") != h.Get("Cache-Control") {
		t.Fatalf("HEAD %d %v", head.Code, head.Header())
	}
	if again := call(handler, "GET", asset.ThumbnailURL, "", map[string]string{"If-None-Match": h.Get("ETag")}); again.Code != 304 || again.Body.Len() != 0 {
		t.Fatalf("conditional GET %d", again.Code)
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if rec := call(handler, method, asset.ThumbnailURL, "", nil); rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %d %v", method, rec.Code, rec.Header())
		}
	}

	refused := func(name, path string) {
		t.Helper()
		rec := call(handler, "GET", path, "", nil)
		if rec.Code != 404 || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Location") != "" || strings.Contains(rec.Body.String(), root) || strings.Contains(rec.Body.String(), "legacy") {
			t.Fatalf("%s: %d %v %s", name, rec.Code, rec.Header(), rec.Body.String())
		}
	}
	unknown := "med_" + strings.Repeat("1", 32)
	refused("malformed id", "/media/not-an-id/thumbnail/"+row.SHA256)
	refused("malformed digest", "/media/"+asset.ID+"/thumbnail/xyz")
	refused("uppercase digest", "/media/"+asset.ID+"/thumbnail/"+strings.ToUpper(row.SHA256))
	refused("traversal digest", "/media/"+asset.ID+"/thumbnail/..%2F..%2Fobjects")
	refused("unknown asset", "/media/"+unknown+"/thumbnail/"+row.SHA256)
	refused("wrong digest", "/media/"+asset.ID+"/thumbnail/"+strings.Repeat("a", 64))

	// A live asset with no row: the file alone authorizes nothing.
	saved := store.thumbnails[asset.ID]
	delete(store.thumbnails, asset.ID)
	refused("missing row", asset.ThumbnailURL)
	store.thumbnails[asset.ID] = saved

	// A stale digest is not redirected to the current one, even with its file.
	stale := strings.Repeat("b", 64)
	if err := os.WriteFile(filepath.Join(filepath.Dir(thumbnailPath), stale), []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	refused("stale digest with a file", "/media/"+asset.ID+"/thumbnail/"+stale)

	// A row whose file vanished or shrank is 404 and names no path.
	if err := os.Truncate(thumbnailPath, 1); err != nil {
		t.Fatal(err)
	}
	refused("wrong size", asset.ThumbnailURL)
	if err := os.Remove(thumbnailPath); err != nil {
		t.Fatal(err)
	}
	refused("missing file", asset.ThumbnailURL)
	if err := os.WriteFile(thumbnailPath, stored, 0o640); err != nil {
		t.Fatal(err)
	}
	if rec := call(handler, "GET", asset.ThumbnailURL, "", nil); rec.Code != 200 {
		t.Fatalf("restored %d", rec.Code)
	}

	// An older binary's delete leaves the row and file; the tombstone decides.
	store.deleted[asset.ID] = true
	refused("tombstoned parent", asset.ThumbnailURL)
	if !store.deleted[asset.ID] || store.thumbnails[asset.ID] == nil {
		t.Fatal("the route changed state")
	}
}

// The original is not derived from, and does not depend on, its thumbnail.
func TestOriginalMediaIsIndependentOfItsThumbnail(t *testing.T) {
	store := newTestMediaStore()
	handler, _, root := mediaHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	body, contentType := multipartBody(t, uploadPart{"file", "a.png", "image/png", testPNG(t, 900, 600, 255)})
	var asset domain.MediaAsset
	_ = json.Unmarshal(upload(handler, map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}, body, contentType).Body.Bytes(), &asset)
	if err := os.RemoveAll(filepath.Join(root, "thumbnails", asset.ID[4:6], asset.ID)); err != nil {
		t.Fatal(err)
	}
	original := call(handler, "GET", asset.URL, "", nil)
	if original.Code != 200 || original.Header().Get("ETag") != `"`+asset.SHA256+`"` || int64(original.Body.Len()) != asset.SizeBytes {
		t.Fatalf("original %d %v", original.Code, original.Header())
	}
	if rec := call(handler, "HEAD", asset.URL, "", nil); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD %d", rec.Code)
	}
	if rec := call(handler, "GET", asset.ThumbnailURL, "", nil); rec.Code != 404 {
		t.Fatalf("thumbnail %d", rec.Code)
	}
}
