//go:build integration

package integration_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/media"
	"sweetshop/tests/contract"
)

// Managed media on PostgreSQL and a real temporary media root (ADR 019).

func pngImage(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadBody(t *testing.T, data []byte) (string, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = writer.Close()
	return body.String(), writer.FormDataContentType()
}

func uploadAs(t *testing.T, handler http.Handler, login staffLogin, data []byte) exchange {
	t.Helper()
	body, contentType := uploadBody(t, data)
	return serve(handler, "POST", "/api/admin/v1/media", body, map[string]string{
		"Cookie": staffCookie + "=" + login.token, "Origin": contract.AdminTrustedOrigin, "X-CSRF-Token": login.csrf,
		"X-Request-ID": "req-c1", "Content-Type": contentType})
}

func (d *database) objectPath(id string) string {
	return filepath.Join(d.mediaRoot, "objects", id[4:6], id)
}

func (d *database) uploadMedia(t *testing.T, staff domain.StaffUser) domain.MediaAsset {
	t.Helper()
	asset, err := d.mediaLibrary(t).Upload(d.ctx, mutationAs(t, staff, application.MediaUpload), pngImage(t, 16, 12))
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestMediaUploadRegistersOneAssetAndAuditEventAndServesTheStoredBytes(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	ex := uploadAs(t, handler, login, pngImage(t, 40, 30))
	if ex.Code != 201 {
		t.Fatalf("upload: %d %s", ex.Code, ex.Body.String())
	}
	id := ex.body["id"].(string)
	if !media.IDPattern.MatchString(id) || ex.body["contentType"] != "image/jpeg" || ex.body["width"].(float64) != 40 || ex.body["height"].(float64) != 30 {
		t.Fatalf("asset %v", ex.body)
	}
	if trail := d.auditTrail(t); len(trail) != 1 || trail[0] != (auditRow{login.id, "media.upload", "media", id, "req-c1"}) {
		t.Fatalf("audit %+v", trail)
	}
	stored, err := os.ReadFile(d.objectPath(id))
	if err != nil {
		t.Fatal(err)
	}
	var size int64
	var digest, contentType string
	if err := d.pool.QueryRow(d.ctx, `SELECT size_bytes,sha256,content_type FROM media_assets WHERE id=$1`, id).Scan(&size, &digest, &contentType); err != nil {
		t.Fatal(err)
	}
	if size != int64(len(stored)) || digest != ex.body["sha256"] || contentType != "image/jpeg" {
		t.Fatal("metadata does not describe the stored file")
	}
	public := serve(handler, "GET", "/media/"+id, "", nil)
	if public.Code != 200 || !bytes.Equal(public.Body.Bytes(), stored) || public.Header().Get("ETag") != `"`+digest+`"` ||
		public.Header().Get("Content-Type") != "image/jpeg" || public.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(public.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("public GET %d %v", public.Code, public.Header())
	}
	if decoded, err := jpeg.Decode(bytes.NewReader(public.Body.Bytes())); err != nil || decoded.Bounds().Dx() != 40 {
		t.Fatal("served bytes are not the normalized image")
	}
	// The audit row carries a reference only: no bytes, names or metadata.
	var auditText string
	_ = d.pool.QueryRow(d.ctx, `SELECT row_to_json(e)::text FROM staff_audit_events e`).Scan(&auditText)
	if strings.Contains(auditText, "photo.png") || strings.Contains(auditText, digest) || strings.Contains(auditText, d.mediaRoot) {
		t.Fatalf("audit row carries more than a reference: %s", auditText)
	}
	if list := adminRequest(handler, login, "GET", "/api/admin/v1/media", ""); list.Code != 200 || list.body["total"].(float64) != 1 {
		t.Fatalf("list %d", list.Code)
	}
}

func TestRefusedUploadsWriteNothing(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	valid := pngImage(t, 8, 8)
	for name, data := range map[string][]byte{
		"svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "garbage": []byte("not an image"),
		"truncated": valid[:len(valid)/2], "bomb header": valid[:33],
	} {
		if ex := uploadAs(t, handler, login, data); ex.Code < 400 || ex.Code >= 500 {
			t.Errorf("%s: %d", name, ex.Code)
		}
	}
	customerCookie := cookieLogin(t, handler, "demo@sugar.ru", "Sweet2026!")
	body, contentType := uploadBody(t, valid)
	if ex := serve(handler, "POST", "/api/admin/v1/media", body, map[string]string{"Cookie": shopCookie + "=" + customerCookie, "Origin": contract.AdminTrustedOrigin, "Content-Type": contentType}); ex.Code != 401 {
		t.Fatalf("customer upload: %d", ex.Code)
	}
	if _, err := d.store.SetStaffDisabled(d.ctx, staffEmail, true); err != nil {
		t.Fatal(err)
	}
	if ex := uploadAs(t, handler, login, valid); ex.Code != 401 {
		t.Fatalf("disabled staff upload: %d", ex.Code)
	}
	if _, err := d.mediaLibrary(t).Upload(d.ctx, mutationAs(t, domain.StaffUser{ID: login.id}, application.MediaUpload), valid); !errors.Is(err, domain.ErrAuditActorInactive) {
		t.Fatalf("disabled actor: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM media_assets") != 0 || len(d.auditTrail(t)) != 0 {
		t.Fatal("a refused upload was registered")
	}
	assertNoObjects(t, d)
}

func assertNoObjects(t *testing.T, d *database) {
	t.Helper()
	shards, _ := os.ReadDir(filepath.Join(d.mediaRoot, "objects"))
	for _, shard := range shards {
		if entries, _ := os.ReadDir(filepath.Join(d.mediaRoot, "objects", shard.Name())); len(entries) != 0 {
			t.Fatalf("objects left in shard %s", shard.Name())
		}
	}
}

func TestUploadWhoseAuditFailsLeavesNoLiveAssetAndNoBytes(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	d.exec(t, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$`)
	d.exec(t, `CREATE TRIGGER fail_media_audit BEFORE INSERT ON staff_audit_events FOR EACH ROW WHEN (NEW.entity_type='media') EXECUTE FUNCTION fail_audit()`)
	if ex := uploadAs(t, handler, login, pngImage(t, 8, 8)); ex.Code != 500 || strings.Contains(ex.Body.String(), "injected") {
		t.Fatalf("upload: %d %s", ex.Code, ex.Body.String())
	}
	if d.number(t, "SELECT count(*) FROM media_assets") != 0 || len(d.auditTrail(t)) != 0 {
		t.Fatal("an asset committed without its audit event")
	}
	assertNoObjects(t, d)
}

func (d *database) thumbnailDir(id string) string {
	return filepath.Join(d.mediaRoot, "thumbnails", id[4:6], id)
}

func assertNoThumbnails(t *testing.T, d *database) {
	t.Helper()
	if d.number(t, "SELECT count(*) FROM media_thumbnails") != 0 {
		t.Fatal("thumbnail rows left")
	}
	shards, _ := os.ReadDir(filepath.Join(d.mediaRoot, "thumbnails"))
	for _, shard := range shards {
		if entries, _ := os.ReadDir(filepath.Join(d.mediaRoot, "thumbnails", shard.Name())); len(entries) != 0 {
			t.Fatalf("thumbnails left in shard %s", shard.Name())
		}
	}
}

func TestUploadPersistsExactlyOneThumbnailWithItsMetadata(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	ex := uploadAs(t, handler, login, pngImage(t, 1280, 720))
	if ex.Code != 201 {
		t.Fatalf("upload: %d %s", ex.Code, ex.Body.String())
	}
	id := ex.body["id"].(string)
	if trail := d.auditTrail(t); len(trail) != 1 || trail[0].action != "media.upload" {
		t.Fatalf("audit %+v", trail)
	}
	original, err := os.ReadFile(d.objectPath(id))
	if err != nil {
		t.Fatal(err)
	}
	expected, ok, err := media.DeriveThumbnail(original)
	if err != nil || !ok {
		t.Fatalf("derive: %v %v", ok, err)
	}
	var recipe, contentType, digest string
	var size int64
	var width, height int
	if err := d.pool.QueryRow(d.ctx, `SELECT recipe,content_type,size_bytes,width,height,sha256 FROM media_thumbnails WHERE media_id=$1`, id).
		Scan(&recipe, &contentType, &size, &width, &height, &digest); err != nil {
		t.Fatal(err)
	}
	if recipe != media.ThumbnailRecipe || contentType != "image/jpeg" || size != int64(len(expected.Data)) || width != 640 || height != 360 || digest != expected.SHA256 {
		t.Fatalf("row %s %s %d %dx%d %s", recipe, contentType, size, width, height, digest)
	}
	entries, err := os.ReadDir(d.thumbnailDir(id))
	if err != nil || len(entries) != 1 || entries[0].Name() != digest {
		t.Fatalf("thumbnail files %v %v", entries, err)
	}
	stored, _ := os.ReadFile(filepath.Join(d.thumbnailDir(id), digest))
	if !bytes.Equal(stored, expected.Data) {
		t.Fatal("the stored thumbnail is not the original's derivative")
	}
	if d.number(t, "SELECT count(*) FROM media_thumbnails") != 1 {
		t.Fatal("expected exactly one thumbnail row")
	}
	if ex.body["thumbnailUrl"] != "/media/"+id+"/thumbnail/"+digest {
		t.Fatalf("thumbnail url %v", ex.body["thumbnailUrl"])
	}
	if public := serve(handler, "GET", "/media/"+id, "", nil); public.Code != 200 || !bytes.Equal(public.Body.Bytes(), original) {
		t.Fatalf("public original: %d", public.Code)
	}
}

func TestUploadNoLargerThanTheRecipeGetsNoThumbnail(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	asset, err := d.mediaLibrary(t).Upload(d.ctx, mutationAs(t, staff, application.MediaUpload), pngImage(t, 640, 200))
	if err != nil {
		t.Fatal(err)
	}
	assertNoThumbnails(t, d)
	if _, err := os.Stat(d.thumbnailDir(asset.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("thumbnail directory: %v", err)
	}
	if _, err := os.Stat(d.objectPath(asset.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestUploadWhoseThumbnailRowFailsLeavesNoPartialState(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	d.exec(t, `CREATE FUNCTION fail_thumbnail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected thumbnail failure'; END $$`)
	d.exec(t, `CREATE TRIGGER fail_media_thumbnail BEFORE INSERT ON media_thumbnails FOR EACH ROW EXECUTE FUNCTION fail_thumbnail()`)
	if _, err := d.mediaLibrary(t).Upload(d.ctx, mutationAs(t, staff, application.MediaUpload), pngImage(t, 1280, 720)); err == nil {
		t.Fatal("upload succeeded")
	}
	if d.number(t, "SELECT count(*) FROM media_assets") != 0 || len(d.auditTrail(t)) != 0 {
		t.Fatal("a partial asset committed")
	}
	assertNoObjects(t, d)
	assertNoThumbnails(t, d)
}

func TestUploadWhoseAuditFailsRemovesItsThumbnail(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	d.exec(t, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$`)
	d.exec(t, `CREATE TRIGGER fail_media_audit BEFORE INSERT ON staff_audit_events FOR EACH ROW WHEN (NEW.entity_type='media') EXECUTE FUNCTION fail_audit()`)
	if _, err := d.mediaLibrary(t).Upload(d.ctx, mutationAs(t, staff, application.MediaUpload), pngImage(t, 1280, 720)); err == nil {
		t.Fatal("upload succeeded")
	}
	if d.number(t, "SELECT count(*) FROM media_assets") != 0 {
		t.Fatal("an asset committed without its audit event")
	}
	assertNoObjects(t, d)
	assertNoThumbnails(t, d)
}

func TestUploadWhoseThumbnailCannotBeWrittenRegistersNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not bind root")
	}
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	library := d.mediaLibrary(t)
	thumbnails := filepath.Join(d.mediaRoot, "thumbnails")
	if err := os.Chmod(thumbnails, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(thumbnails, 0o750) })
	if _, err := library.Upload(d.ctx, mutationAs(t, staff, application.MediaUpload), pngImage(t, 1280, 720)); !errors.Is(err, domain.ErrStorageUnavailable) {
		t.Fatalf("upload: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM media_assets") != 0 || len(d.auditTrail(t)) != 0 {
		t.Fatal("a failed thumbnail write registered an asset")
	}
	assertNoObjects(t, d)
	// A small image needs no thumbnail, so it still uploads.
	if _, err := library.Upload(d.ctx, mutationAs(t, staff, application.MediaUpload), pngImage(t, 16, 12)); err != nil {
		t.Fatalf("small upload: %v", err)
	}
}

// One asset shared by two products keeps one thumbnail, which lives as long
// as the asset does.
func TestDeleteRemovesTheThumbnailOnlyAfterReferencesAreGone(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	library := d.mediaLibrary(t)
	asset, err := library.Upload(d.ctx, mutationAs(t, staff, application.MediaUpload), pngImage(t, 1280, 720))
	if err != nil {
		t.Fatal(err)
	}
	for _, product := range []string{"p01", "p02"} {
		if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, product),
			domain.ProductMedia{ProductID: product, PrimaryID: asset.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if d.number(t, "SELECT count(*) FROM media_thumbnails") != 1 {
		t.Fatal("sharing an asset changed its thumbnails")
	}
	var conflict *domain.ReferenceConflictError
	if err := library.Delete(d.ctx, mutationAs(t, staff, application.MediaDelete), asset.ID); !errors.As(err, &conflict) || conflict.Products != 2 {
		t.Fatalf("delete of a shared asset: %v", err)
	}
	if entries, _ := os.ReadDir(d.thumbnailDir(asset.ID)); len(entries) != 1 || d.number(t, "SELECT count(*) FROM media_thumbnails") != 1 {
		t.Fatal("a refused delete touched the thumbnail")
	}
	for _, product := range []string{"p01", "p02"} {
		if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, product),
			domain.ProductMedia{ProductID: product}); err != nil {
			t.Fatal(err)
		}
	}
	if err := library.Delete(d.ctx, mutationAs(t, staff, application.MediaDelete), asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.thumbnailDir(asset.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("thumbnail directory: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM media_thumbnails") != 0 {
		t.Fatal("thumbnail row outlived its file")
	}
	if d.number(t, "SELECT count(*) FROM media_assets WHERE deleted_at IS NOT NULL") != 1 {
		t.Fatal("expected a tombstone")
	}
}

func TestMediaThumbnailSchemaConstraints(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	asset := d.uploadMedia(t, staff)
	digest := strings.Repeat("a", 64)
	for name, row := range map[string][]any{
		"recipe format": {asset.ID, "Bad Recipe", "image/jpeg", 1, 1, 1, digest},
		"content type":  {asset.ID, "r1", "image/webp", 1, 1, 1, digest},
		"empty":         {asset.ID, "r1", "image/jpeg", 0, 1, 1, digest},
		"width":         {asset.ID, "r1", "image/jpeg", 1, 0, 1, digest},
		"height":        {asset.ID, "r1", "image/jpeg", 1, 1, 0, digest},
		"digest":        {asset.ID, "r1", "image/jpeg", 1, 1, 1, "abc"},
		"unknown asset": {"med_" + strings.Repeat("9", 32), "r1", "image/jpeg", 1, 1, 1, digest},
	} {
		if _, err := d.pool.Exec(d.ctx, `INSERT INTO media_thumbnails(media_id,recipe,content_type,size_bytes,width,height,sha256) VALUES ($1,$2,$3,$4,$5,$6,$7)`, row...); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// A binary from before migration 020 tolerates it, leaves thumbnails alone and
// keeps serving originals.
func TestPreviousBinaryIgnoresThumbnails(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	ex := uploadAs(t, handler, login, pngImage(t, 1280, 720))
	if ex.Code != 201 {
		t.Fatalf("upload: %d", ex.Code)
	}
	id := ex.body["id"].(string)
	var prefixes []string
	for number := 1; number <= 19; number++ {
		prefixes = append(prefixes, fmt.Sprintf("%03d_", number))
	}
	previous := migrator(migrationDir(t, prefixes, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("previous schema check: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("previous migrate: %v", err)
	}
	report, err := d.mediaLibrary(t).Reconcile(d.ctx, time.Hour, true)
	if err != nil || report.Healthy != 1 || len(report.Removed) != 0 {
		t.Fatalf("reconcile %+v %v", report, err)
	}
	if entries, _ := os.ReadDir(d.thumbnailDir(id)); len(entries) != 1 {
		t.Fatal("reconciliation touched the thumbnail")
	}
	if public := serve(handler, "GET", "/media/"+id, "", nil); public.Code != 200 {
		t.Fatalf("original: %d", public.Code)
	}
}

func TestMediaSchemaConstraints(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	asset := d.uploadMedia(t, staff)
	second := d.uploadMedia(t, staff)
	digest := strings.Repeat("a", 64)
	for name, tc := range map[string]struct{ sql, code string }{
		"duplicate id":        {`INSERT INTO media_assets(id,content_type,size_bytes,width,height,sha256) VALUES ('` + asset.ID + `','image/png',1,1,1,'` + digest + `')`, "23505"},
		"id format":           {`INSERT INTO media_assets(id,content_type,size_bytes,width,height,sha256) VALUES ('../x','image/png',1,1,1,'` + digest + `')`, "23514"},
		"svg content type":    {`INSERT INTO media_assets(id,content_type,size_bytes,width,height,sha256) VALUES ('med_` + strings.Repeat("1", 32) + `','image/svg+xml',1,1,1,'` + digest + `')`, "23514"},
		"empty file":          {`INSERT INTO media_assets(id,content_type,size_bytes,width,height,sha256) VALUES ('med_` + strings.Repeat("1", 32) + `','image/png',0,1,1,'` + digest + `')`, "23514"},
		"digest format":       {`INSERT INTO media_assets(id,content_type,size_bytes,width,height,sha256) VALUES ('med_` + strings.Repeat("1", 32) + `','image/png',1,1,1,'x')`, "23514"},
		"unknown media":       {`INSERT INTO product_media VALUES ('p01','med_` + strings.Repeat("9", 32) + `','gallery',0)`, "23503"},
		"unknown product":     {`INSERT INTO product_media VALUES ('nope','` + asset.ID + `','gallery',0)`, "23503"},
		"role":                {`INSERT INTO product_media VALUES ('p01','` + asset.ID + `','hero',0)`, "23514"},
		"negative position":   {`INSERT INTO product_media VALUES ('p01','` + asset.ID + `','gallery',-1)`, "23514"},
		"primary not first":   {`INSERT INTO product_media VALUES ('p01','` + asset.ID + `','primary',1)`, "23514"},
		"two primaries":       {`INSERT INTO product_media VALUES ('p01','` + asset.ID + `','primary',0),('p01','` + second.ID + `','primary',0)`, "23505"},
		"same gallery slot":   {`INSERT INTO product_media VALUES ('p01','` + asset.ID + `','gallery',0),('p01','` + second.ID + `','gallery',0)`, "23505"},
		"asset twice in role": {`INSERT INTO product_media VALUES ('p01','` + asset.ID + `','gallery',0),('p01','` + asset.ID + `','gallery',1)`, "23505"},
	} {
		_, err := d.pool.Exec(d.ctx, tc.sql)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestProductMediaReferencesBlockDeletionAndDeletedMediaCannotBeAttached(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := domain.StaffUser{ID: login.id}
	library := d.mediaLibrary(t)
	primary, gallery, spare := d.uploadMedia(t, staff), d.uploadMedia(t, staff), d.uploadMedia(t, staff)
	legacyImage, legacyGallery := d.adminProduct(t, "p01").Image, d.adminProduct(t, "p01").Gallery
	revision := d.productRevision(t, "p01")
	set, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), revision,
		domain.ProductMedia{ProductID: "p01", PrimaryID: primary.ID, GalleryIDs: []string{gallery.ID}})
	if err != nil || set.PrimaryID != primary.ID || strings.Join(set.GalleryIDs, ",") != gallery.ID {
		t.Fatalf("attach: %+v %v", set, err)
	}
	if d.productRevision(t, "p01") != revision+1 {
		t.Fatal("attaching media did not bump the product revision")
	}
	if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), revision, domain.ProductMedia{ProductID: "p01"}); !errors.Is(err, domain.ErrStaleRevision) {
		t.Fatalf("stale attach: %v", err)
	}
	// Legacy columns remain untouched; customer reads resolve managed media.
	after := d.adminProduct(t, "p01")
	customerView, _ := d.service.Product(d.ctx, "p01")
	if after.Image != legacyImage || strings.Join(after.Gallery, ",") != strings.Join(legacyGallery, ",") || customerView.Image != primary.URL {
		t.Fatal("managed media changed legacy product images")
	}

	audits := len(d.auditTrail(t))
	for _, id := range []string{primary.ID, gallery.ID} {
		ex := adminRequest(handler, login, "DELETE", "/api/admin/v1/media/"+id, "")
		refs, _ := ex.body["error"].(map[string]any)["references"].(map[string]any)
		if ex.Code != 409 || !strings.HasPrefix(ex.errorCode(), "reference_conflict|") || refs["products"].(float64) != 1 {
			t.Fatalf("referenced delete: %d %s", ex.Code, ex.Body.String())
		}
	}
	if ex := adminRequest(handler, login, "GET", "/api/admin/v1/media/"+primary.ID, ""); ex.body["productCount"].(float64) != 1 {
		t.Fatalf("productCount %v", ex.body)
	}
	if len(d.auditTrail(t)) != audits || serve(handler, "GET", "/media/"+primary.ID, "", nil).Code != 200 {
		t.Fatal("a refused delete changed something")
	}

	// An unreferenced asset is tombstoned, audited, unpublished and unlinked.
	if ex := adminRequest(handler, login, "DELETE", "/api/admin/v1/media/"+spare.ID, ""); ex.Code != 204 {
		t.Fatalf("delete: %d %s", ex.Code, ex.Body.String())
	}
	trail := d.auditTrail(t)
	if last := trail[len(trail)-1]; last != (auditRow{login.id, "media.delete", "media", spare.ID, "req-b1"}) {
		t.Fatalf("audit %+v", last)
	}
	if d.number(t, "SELECT count(*) FROM media_assets WHERE id=$1 AND deleted_at IS NOT NULL", spare.ID) != 1 {
		t.Fatal("no tombstone")
	}
	if serve(handler, "GET", "/media/"+spare.ID, "", nil).Code != 404 || adminRequest(handler, login, "GET", "/api/admin/v1/media/"+spare.ID, "").Code != 404 {
		t.Fatal("a deleted asset is still served")
	}
	if _, err := os.Stat(d.objectPath(spare.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted bytes remain")
	}
	// A deleted asset can never be referenced again.
	_, err = library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), revision+1,
		domain.ProductMedia{ProductID: "p01", PrimaryID: spare.ID})
	if !strings.Contains(strings.Join(fieldNamesOf(err), ","), "primaryId") || d.productRevision(t, "p01") != revision+1 {
		t.Fatalf("deleted asset attached: %v", err)
	}
	// Detaching everything frees the assets for deletion.
	if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), revision+1, domain.ProductMedia{ProductID: "p01"}); err != nil {
		t.Fatal(err)
	}
	if ex := adminRequest(handler, login, "DELETE", "/api/admin/v1/media/"+primary.ID, ""); ex.Code != 204 {
		t.Fatalf("detached delete: %d", ex.Code)
	}
}

func fieldNamesOf(err error) []string {
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		return nil
	}
	names := []string{}
	for _, field := range validation.Fields {
		names = append(names, field.Field)
	}
	return names
}

// Attaching an asset while it is being deleted: one of them wins, and no
// product ever references a deleted asset.
func TestAttachRacingDeleteNeverLeavesADanglingReference(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	library := d.mediaLibrary(t)
	seen := map[string]bool{}
	for round := 0; round < 10; round++ {
		asset := d.uploadMedia(t, staff)
		revision := d.productRevision(t, "p01")
		attach, remove := mutationAs(t, staff, application.ProductMediaUpdate), mutationAs(t, staff, application.MediaDelete)
		var attachErr, deleteErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, attachErr = library.SetProductMedia(d.ctx, attach, revision, domain.ProductMedia{ProductID: "p01", GalleryIDs: []string{asset.ID}})
		}()
		go func() { defer wg.Done(); <-start; deleteErr = library.Delete(d.ctx, remove, asset.ID) }()
		close(start)
		wg.Wait()
		var conflict *domain.ReferenceConflictError
		outcome := ""
		switch {
		case attachErr == nil && errors.As(deleteErr, &conflict):
			outcome = "attach first"
		case deleteErr == nil && errors.Is(attachErr, domain.ErrValidation):
			outcome = "delete first"
		default:
			t.Fatalf("round %d: attach %v, delete %v", round, attachErr, deleteErr)
		}
		seen[outcome] = true
		if dangling := d.number(t, `SELECT count(*) FROM product_media pm JOIN media_assets m ON m.id=pm.media_id WHERE m.deleted_at IS NOT NULL`); dangling != 0 {
			t.Fatalf("round %d: %d references to deleted media", round, dangling)
		}
		if attachErr == nil {
			if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), revision+1, domain.ProductMedia{ProductID: "p01"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("outcomes seen: %v", seen)
}

func TestReconciliationClassifiesFilesystemAndDatabaseDivergence(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	library := d.mediaLibrary(t)
	healthy := d.uploadMedia(t, staff) // A: row and file
	missing := d.uploadMedia(t, staff) // B: row, file gone
	_ = os.Remove(d.objectPath(missing.ID))
	orphan, err := media.NewID() // C: file, no row
	if err != nil {
		t.Fatal(err)
	}
	files, _ := media.OpenFileStore(d.mediaRoot)
	_ = files.Put(orphan, []byte("unregistered"))
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(d.objectPath(orphan), old, old)
	leftover := d.uploadMedia(t, staff) // D: deleted row, file remains
	d.exec(t, `UPDATE media_assets SET deleted_at=now() WHERE id=$1`, leftover.ID)

	report, err := library.Reconcile(d.ctx, 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Healthy != 1 || fmt.Sprint(report.Missing) != fmt.Sprint([]string{missing.ID}) || fmt.Sprint(report.Orphans) != fmt.Sprint([]string{orphan}) ||
		fmt.Sprint(report.Deleted) != fmt.Sprint([]string{leftover.ID}) || len(report.Removed) != 0 {
		t.Fatalf("dry run %+v", report)
	}
	report, err = library.Reconcile(d.ctx, 24*time.Hour, true)
	if err != nil || len(report.Removed) != 2 {
		t.Fatalf("apply %+v %v", report, err)
	}
	for id, present := range map[string]bool{healthy.ID: true, orphan: false, leftover.ID: false} {
		if _, err := os.Stat(d.objectPath(id)); (err == nil) != present {
			t.Errorf("%s present=%v", id, err == nil)
		}
	}
	if d.number(t, "SELECT count(*) FROM media_assets") != 3 {
		t.Fatal("reconciliation changed metadata")
	}
}

// The C1 migration runner (001–017) tolerates C2's additive index.
// Legacy columns and ordinary product edits remain compatible.
func TestPreviousBinaryToleratesMediaSchema(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	asset := d.uploadMedia(t, staff)
	if _, err := d.mediaLibrary(t).SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p01"),
		domain.ProductMedia{ProductID: "p01", PrimaryID: asset.ID}); err != nil {
		t.Fatal(err)
	}
	previous := migrator(migrationDir(t, []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_", "010_", "011_", "012_", "013_", "014_", "015_", "016_", "017_"}, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("C1 schema check on the C2 schema: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("C1 migrate on the C2 schema: %v", err)
	}
	// B6's product write (no media awareness) still works on a product with managed media.
	p01 := d.adminProduct(t, "p01")
	edit := inputOf(p01)
	edit.Stock++
	if _, err := application.NewProductAdmin(d.store).Update(d.ctx, mutationAs(t, staff, application.ProductUpdate), "p01", p01.Revision, edit); err != nil {
		t.Fatal(err)
	}
	product, err := d.service.Product(d.ctx, "p01")
	if err != nil || product.Image != asset.URL || !strings.HasPrefix(p01.Image, "/assets/") {
		t.Fatalf("customer product: %+v %v", product.Image, err)
	}
}
