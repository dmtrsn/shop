//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/media"
	"sweetshop/internal/repository/postgres"
)

// Thumbnail delivery, metadata, backfill and reconciliation on PostgreSQL and
// a real temporary media root (ADR 022).

func (d *database) uploadLarge(t *testing.T, staff domain.StaffUser) domain.MediaAsset {
	t.Helper()
	asset, err := d.mediaLibrary(t).Upload(d.ctx, mutationAs(t, staff, application.MediaUpload), pngImage(t, 700, 400))
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func (d *database) attachPrimary(t *testing.T, staff domain.StaffUser, product, id string, gallery ...string) {
	t.Helper()
	if gallery == nil {
		gallery = []string{}
	}
	if _, err := d.mediaLibrary(t).SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, product),
		domain.ProductMedia{ProductID: product, PrimaryID: id, GalleryIDs: gallery}); err != nil {
		t.Fatal(err)
	}
}

func (d *database) thumbnailDigest(t *testing.T, id string) string {
	t.Helper()
	return d.text(t, `SELECT sha256 FROM media_thumbnails WHERE media_id=$1`, id)
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A regenerated thumbnail's old URL is 404 even with its file on disk, and a
// tombstoned parent's thumbnail is 404 even with its row and file left behind.
func TestThumbnailDeliveryFollowsTheCurrentRowOfALiveParent(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	library := d.mediaLibrary(t)
	ex := uploadAs(t, handler, login, pngImage(t, 700, 400))
	if ex.Code != 201 {
		t.Fatalf("upload: %d %s", ex.Code, ex.Body.String())
	}
	id := ex.body["id"].(string)
	current := d.thumbnailDigest(t, id)
	url := "/media/" + id + "/thumbnail/" + current
	if ex.body["thumbnailUrl"] != url {
		t.Fatalf("thumbnail url %v", ex.body["thumbnailUrl"])
	}
	get := serve(handler, "GET", url, "", nil)
	stored, _ := os.ReadFile(filepath.Join(d.thumbnailDir(id), current))
	if get.Code != 200 || !bytes.Equal(get.Body.Bytes(), stored) || get.Header().Get("ETag") != `"`+current+`"` ||
		get.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || get.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("GET %d %v", get.Code, get.Header())
	}
	if head := serve(handler, "HEAD", url, "", nil); head.Code != 200 || head.Body.Len() != 0 {
		t.Fatalf("HEAD %d", head.Code)
	}
	if wrong := serve(handler, "GET", "/media/"+id+"/thumbnail/"+strings.Repeat("a", 64), "", nil); wrong.Code != 404 || wrong.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("wrong digest %d", wrong.Code)
	}

	// An older recipe's output is the current row until it is replaced.
	old := []byte("the output of an older recipe")
	oldSum := sha256.Sum256(old)
	oldDigest := hex.EncodeToString(oldSum[:])
	if err := os.WriteFile(filepath.Join(d.thumbnailDir(id), oldDigest), old, 0o640); err != nil {
		t.Fatal(err)
	}
	d.exec(t, `UPDATE media_thumbnails SET recipe='fit640-q80-v0',sha256=$2,size_bytes=$3 WHERE media_id=$1`, id, oldDigest, len(old))
	oldURL := "/media/" + id + "/thumbnail/" + oldDigest
	if before := serve(handler, "GET", oldURL, "", nil); before.Code != 200 || !bytes.Equal(before.Body.Bytes(), old) {
		t.Fatalf("current row's URL %d", before.Code)
	}
	report, err := library.BackfillThumbnails(d.ctx, true, 0)
	if err != nil || report.Regenerated != 1 {
		t.Fatalf("backfill %+v %v", report, err)
	}
	if d.thumbnailDigest(t, id) != current || d.text(t, `SELECT recipe FROM media_thumbnails WHERE media_id=$1`, id) != media.ThumbnailRecipe {
		t.Fatal("row not replaced")
	}
	if after := serve(handler, "GET", oldURL, "", nil); after.Code != 404 || after.Header().Get("Location") != "" {
		t.Fatalf("old digest %d %v", after.Code, after.Header())
	}
	if _, err := os.Stat(filepath.Join(d.thumbnailDir(id), oldDigest)); err != nil {
		t.Fatal("the stale file is for reconciliation to remove")
	}
	if fresh := serve(handler, "GET", url, "", nil); fresh.Code != 200 {
		t.Fatalf("new digest %d", fresh.Code)
	}

	// Reconciliation: young, then old enough, then gone; the current file stays.
	stale := id + "/" + oldDigest
	verified, err := library.Reconcile(d.ctx, 24*time.Hour, false)
	if err != nil || len(verified.YoungStaleThumbnails) != 1 || verified.YoungStaleThumbnails[0] != stale || verified.HealthyThumbnails != 1 || verified.Problems() {
		t.Fatalf("young %+v %v", verified, err)
	}
	long := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(d.thumbnailDir(id), oldDigest), long, long); err != nil {
		t.Fatal(err)
	}
	verified, err = library.Reconcile(d.ctx, 24*time.Hour, true)
	if err != nil || len(verified.Removed) != 1 || verified.Removed[0] != stale {
		t.Fatalf("apply %+v %v", verified, err)
	}
	if entries, _ := os.ReadDir(d.thumbnailDir(id)); len(entries) != 1 || entries[0].Name() != current {
		t.Fatalf("thumbnail files %v", entries)
	}

	// An older binary's delete sets the tombstone and nothing else.
	d.exec(t, `UPDATE media_assets SET deleted_at=now() WHERE id=$1`, id)
	if gone := serve(handler, "GET", url, "", nil); gone.Code != 404 || gone.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("tombstoned parent %d", gone.Code)
	}
	if gone := serve(handler, "GET", "/media/"+id, "", nil); gone.Code != 404 {
		t.Fatalf("tombstoned original %d", gone.Code)
	}
	if d.number(t, `SELECT count(*) FROM media_thumbnails WHERE media_id=$1`, id) != 1 {
		t.Fatal("the row should still be there")
	}
	verified, err = library.Reconcile(d.ctx, 24*time.Hour, true)
	if err != nil || len(verified.DeletedThumbnails) != 1 {
		t.Fatalf("deleted %+v %v", verified, err)
	}
	if _, err := os.Stat(d.thumbnailDir(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted asset's directory: %v", err)
	}
}

type queryCounter struct{ queries atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.queries.Add(1)
	return ctx
}

func (*queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Partly backfilled states stay valid: each image reports only what exists.
func TestThumbnailMetadataIsOptionalAndCostsNoExtraQuery(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := domain.StaffUser{ID: login.id}
	large, small := d.uploadLarge(t, staff), d.uploadMedia(t, staff)
	if large.ThumbnailURL == "" || small.ThumbnailURL != "" {
		t.Fatalf("upload: %q %q", large.ThumbnailURL, small.ThumbnailURL)
	}
	d.attachPrimary(t, staff, "p01", large.ID, small.ID)
	d.attachPrimary(t, staff, "p02", small.ID)
	expected := "/media/" + large.ID + "/thumbnail/" + d.thumbnailDigest(t, large.ID)

	product, err := d.service.Product(d.ctx, "p01")
	if err != nil {
		t.Fatal(err)
	}
	primary, gallery := product.ManagedMedia.Primary, product.ManagedMedia.Gallery
	if product.Image != large.URL || len(product.Gallery) != 1 || product.Gallery[0] != small.URL || primary.URL != large.URL || primary.ThumbnailURL != expected || gallery[0].URL != small.URL || gallery[0].ThumbnailURL != "" {
		t.Fatalf("product %+v %+v", product.Image, product.ManagedMedia)
	}
	serialized, _ := json.Marshal(gallery[0])
	if strings.Contains(string(serialized), "thumbnailUrl") {
		t.Fatalf("absent thumbnail serialized: %s", serialized)
	}
	if admin := d.adminProduct(t, "p01"); admin.ManagedMedia == nil || admin.ManagedMedia.Primary.ThumbnailURL != expected {
		t.Fatalf("admin product %+v", admin.ManagedMedia)
	}
	for id, want := range map[string]string{large.ID: expected, small.ID: ""} {
		asset, err := d.mediaLibrary(t).Get(d.ctx, id)
		if err != nil || asset.ThumbnailURL != want {
			t.Fatalf("asset %+v %v", asset, err)
		}
	}
	page, err := d.mediaLibrary(t).List(d.ctx, 1, 10)
	if err != nil || page.Total != 2 {
		t.Fatalf("page %+v %v", page, err)
	}
	for _, item := range page.Items {
		if (item.ID == large.ID) != (item.ThumbnailURL == expected) {
			t.Fatalf("list item %+v", item)
		}
	}

	suggestion := func(id string) domain.SearchSuggestion {
		t.Helper()
		items, err := d.store.Suggestions(d.ctx, d.text(t, `SELECT name FROM products WHERE id=$1`, id), 8)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Type == "product" && item.ID == id {
				return item
			}
		}
		t.Fatalf("no suggestion for %s", id)
		return domain.SearchSuggestion{}
	}
	if with := suggestion("p01"); with.Thumbnail != expected || with.Image != large.URL {
		t.Fatalf("managed with thumbnail %+v", with)
	}
	if without := suggestion("p02"); without.Thumbnail != "" || without.Image != small.URL {
		t.Fatalf("managed without thumbnail %+v", without)
	}
	if legacy := suggestion("p03"); legacy.Thumbnail != "" || !strings.HasPrefix(legacy.Image, "/assets/") {
		t.Fatalf("legacy %+v", legacy)
	}

	config := d.pool.Config()
	counter := &queryCounter{}
	config.ConnConfig.Tracer = counter
	traced, err := pgxpool.NewWithConfig(d.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	store := postgres.New(traced)
	count := func(limit int) int64 {
		t.Helper()
		counter.queries.Store(0)
		page, err := store.ListProducts(d.ctx, domain.ProductFilter{Page: 1, Limit: limit})
		if err != nil || len(page.Items) != limit {
			t.Fatalf("list %+v %v", len(page.Items), err)
		}
		return counter.queries.Load()
	}
	withThumbnails := count(20)
	if single := count(1); single != withThumbnails {
		t.Fatalf("query count depends on page size: %d vs %d", single, withThumbnails)
	}

	// No thumbnail rows at all: the state of a deployment before any backfill.
	d.exec(t, `DELETE FROM media_thumbnails`)
	if without := count(20); without != withThumbnails {
		t.Fatalf("query count depends on thumbnails: %d vs %d", without, withThumbnails)
	}
	product, err = d.service.Product(d.ctx, "p01")
	if err != nil || product.Image != large.URL || product.ManagedMedia.Primary.ThumbnailURL != "" {
		t.Fatalf("without a row: %+v %v", product.ManagedMedia, err)
	}
	if original := serve(handler, "GET", large.URL, "", nil); original.Code != 200 {
		t.Fatalf("original %d", original.Code)
	}
	if without := suggestion("p01"); without.Thumbnail != "" || without.Image != large.URL {
		t.Fatalf("suggestion without a row %+v", without)
	}
}

func TestThumbnailBackfillOnPostgresIsNotAStaffMutation(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	library := d.mediaLibrary(t)
	large, small := d.uploadLarge(t, staff), d.uploadMedia(t, staff)
	// The state of an asset from before P2: no row, no file.
	d.exec(t, `DELETE FROM media_thumbnails`)
	if err := os.RemoveAll(d.thumbnailDir(large.ID)); err != nil {
		t.Fatal(err)
	}
	d.attachPrimary(t, staff, "p01", large.ID, small.ID)
	audits, revision := len(d.auditTrail(t)), d.productRevision(t, "p01")
	updatedAt := d.text(t, `SELECT updated_at::text FROM products WHERE id='p01'`)
	verified, err := library.Reconcile(d.ctx, 24*time.Hour, false)
	if err != nil || len(verified.PendingThumbnails) != 1 || verified.PendingThumbnails[0] != large.ID || verified.Problems() {
		t.Fatalf("pending %+v %v", verified, err)
	}

	dry, err := library.BackfillThumbnails(d.ctx, false, 0)
	if err != nil || dry.Mode != "dry-run" || dry.Inspected != 2 || dry.NotApplicable != 1 || dry.NeedsGeneration != 1 {
		t.Fatalf("dry run %+v %v", dry, err)
	}
	if d.number(t, `SELECT count(*) FROM media_thumbnails`) != 0 {
		t.Fatal("a dry run wrote a row")
	}
	if _, err := os.Stat(d.thumbnailDir(large.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a dry run wrote a file: %v", err)
	}

	applied, err := library.BackfillThumbnails(d.ctx, true, 0)
	if err != nil || applied.Generated != 1 || applied.NotApplicable != 1 || applied.Failed != 0 || applied.ThumbnailBytesGenerated <= 0 || applied.OriginalBytesInspected != large.SizeBytes {
		t.Fatalf("apply %+v %v", applied, err)
	}
	digest := d.thumbnailDigest(t, large.ID)
	if _, err := os.Stat(filepath.Join(d.thumbnailDir(large.ID), digest)); err != nil {
		t.Fatal(err)
	}
	if d.number(t, `SELECT count(*) FROM media_thumbnails WHERE media_id=$1`, small.ID) != 0 {
		t.Fatal("a small image got a row")
	}
	product, err := d.service.Product(d.ctx, "p01")
	if err != nil || product.ManagedMedia.Primary.ThumbnailURL != "/media/"+large.ID+"/thumbnail/"+digest {
		t.Fatalf("product %+v %v", product.ManagedMedia, err)
	}
	if len(d.auditTrail(t)) != audits || d.productRevision(t, "p01") != revision || d.text(t, `SELECT updated_at::text FROM products WHERE id='p01'`) != updatedAt {
		t.Fatal("the backfill changed staff or product state")
	}

	second, err := library.BackfillThumbnails(d.ctx, true, 0)
	if err != nil || second.Complete != 1 || second.Generated != 0 || second.Regenerated != 0 {
		t.Fatalf("second run %+v %v", second, err)
	}
	verified, err = library.Reconcile(d.ctx, 24*time.Hour, false)
	if err != nil || verified.HealthyThumbnails != 1 || len(verified.PendingThumbnails) != 0 || verified.Problems() {
		t.Fatalf("settled %+v %v", verified, err)
	}

	// Missing and damaged files are verify problems the backfill repairs.
	path := filepath.Join(d.thumbnailDir(large.ID), digest)
	if err := os.WriteFile(path, []byte("damaged"), 0o640); err != nil {
		t.Fatal(err)
	}
	if verified, err = library.Reconcile(d.ctx, 24*time.Hour, true); err != nil || len(verified.CorruptThumbnails) != 1 || len(verified.Removed) != 0 {
		t.Fatalf("corrupt %+v %v", verified, err)
	}
	if repaired, err := library.BackfillThumbnails(d.ctx, true, 0); err != nil || repaired.Regenerated != 1 {
		t.Fatalf("repair %+v %v", repaired, err)
	}
	if verified, err = library.Reconcile(d.ctx, 24*time.Hour, false); err != nil || verified.Problems() {
		t.Fatalf("repaired %+v %v", verified, err)
	}
}

// tombstoningFiles deletes the parent just after its thumbnail file is written.
type tombstoningFiles struct {
	media.Store
	t *testing.T
	d *database
}

func (f tombstoningFiles) ReplaceThumbnail(id, sum string, data []byte) error {
	if err := f.Store.ReplaceThumbnail(id, sum, data); err != nil {
		return err
	}
	f.d.exec(f.t, `UPDATE media_assets SET deleted_at=now() WHERE id=$1`, id)
	return nil
}

func TestThumbnailBackfillDiscardsItsFileWhenTheParentIsDeletedMeanwhile(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	asset := d.uploadLarge(t, staff)
	d.exec(t, `DELETE FROM media_thumbnails`)
	if err := os.RemoveAll(d.thumbnailDir(asset.ID)); err != nil {
		t.Fatal(err)
	}
	files, err := media.OpenFileStore(d.mediaRoot)
	if err != nil {
		t.Fatal(err)
	}
	report, err := application.NewMedia(d.store, tombstoningFiles{files, t, d}, discardLogger()).BackfillThumbnails(d.ctx, true, 0)
	if err != nil || report.ParentDeleted != 1 || report.Generated != 0 || report.Failed != 0 {
		t.Fatalf("report %+v %v", report, err)
	}
	if d.number(t, `SELECT count(*) FROM media_thumbnails`) != 0 {
		t.Fatal("a row names a deleted asset")
	}
	if _, err := os.Stat(d.thumbnailDir(asset.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file of a deleted asset: %v", err)
	}
}

// The upsert waits for a delete in progress and then obeys its outcome.
func TestThumbnailSaveWaitsForAnUncommittedDelete(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	d.mediaLibrary(t) // Its temporary root must outlive the subtests.
	row := domain.MediaThumbnail{Recipe: media.ThumbnailRecipe, ContentType: "image/jpeg", SizeBytes: 1, Width: 1, Height: 1, SHA256: strings.Repeat("a", 64)}
	for name, finish := range map[string]func(context.Context, pgx.Tx) error{
		"committed delete":   func(ctx context.Context, tx pgx.Tx) error { return tx.Commit(ctx) },
		"rolled back delete": func(ctx context.Context, tx pgx.Tx) error { return tx.Rollback(ctx) },
	} {
		t.Run(name, func(t *testing.T) {
			asset := d.uploadMedia(t, staff)
			tx, err := d.pool.Begin(d.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(d.ctx)
			if _, err := tx.Exec(d.ctx, `SELECT 1 FROM media_assets WHERE id=$1 FOR UPDATE`, asset.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(d.ctx, `UPDATE media_assets SET deleted_at=now() WHERE id=$1`, asset.ID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- d.store.SaveMediaThumbnail(d.ctx, asset.ID, row) }()
			deadline := time.Now().Add(20 * time.Second)
			for d.number(t, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'`) == 0 {
				if time.Now().After(deadline) {
					t.Fatal("the save did not wait for the delete")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := finish(d.ctx, tx); err != nil {
				t.Fatal(err)
			}
			err = <-done
			count := d.number(t, `SELECT count(*) FROM media_thumbnails WHERE media_id=$1`, asset.ID)
			if name == "committed delete" {
				if !errors.Is(err, domain.ErrNotFound) || count != 0 {
					t.Fatalf("save %v rows %d", err, count)
				}
			} else if err != nil || count != 1 {
				t.Fatalf("save %v rows %d", err, count)
			}
		})
	}
}

// The order snapshot keeps the original, and it survives a later backfill.
func TestCheckoutSnapshotsTheOriginalNeverTheThumbnail(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := domain.StaffUser{ID: login.id}
	asset := d.uploadLarge(t, staff)
	d.attachPrimary(t, staff, "p01", asset.ID)
	if product, err := d.service.Product(d.ctx, "p01"); err != nil || product.ManagedMedia.Primary.ThumbnailURL == "" {
		t.Fatalf("product %+v %v", product.ManagedMedia, err)
	}
	items, total := d.reviewOf(t, "usr-demo")
	result, err := d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test", ExpectedItems: items, ExpectedTotalMinor: total})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range result.Order.Items {
		if item.ProductID == "p01" {
			found = item.Image == asset.URL
		}
		if strings.Contains(item.Image, "/thumbnail/") {
			t.Fatalf("order item %+v", item)
		}
	}
	if !found {
		t.Fatalf("order items %+v", result.Order.Items)
	}
	assertSnapshot := func() {
		t.Helper()
		if d.text(t, `SELECT image FROM order_items WHERE order_id=$1 AND product_id='p01'`, result.Order.ID) != asset.URL ||
			d.number(t, `SELECT count(*) FROM order_items WHERE image LIKE '%/thumbnail/%'`) != 0 {
			t.Fatal("the order snapshot changed")
		}
	}
	assertSnapshot()
	d.exec(t, `UPDATE media_thumbnails SET recipe='fit640-q80-v0' WHERE media_id=$1`, asset.ID)
	if report, err := d.mediaLibrary(t).BackfillThumbnails(d.ctx, true, 0); err != nil || report.Regenerated != 1 {
		t.Fatalf("backfill %+v %v", report, err)
	}
	assertSnapshot()
	// The historical reference still protects the original from deletion.
	d.attachPrimary(t, staff, "p01", "")
	if refused := adminRequest(handler, login, "DELETE", "/api/admin/v1/media/"+asset.ID, ""); refused.Code != 409 {
		t.Fatalf("delete %d %s", refused.Code, refused.Body.String())
	}
	if original := serve(handler, "GET", asset.URL, "", nil); original.Code != 200 || original.Header().Get("ETag") != `"`+asset.SHA256+`"` {
		t.Fatalf("original %d", original.Code)
	}
}
