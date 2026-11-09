//go:build integration

package integration_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/postgres"
)

// The legacy media migration (ADR 021) over PostgreSQL, a real media root and
// a temporary copy of the catalog's own web assets.

const chocolatePath, giftboxPath, marshmallowPath = "/assets/product-chocolate.webp", "/assets/product-giftbox.webp", "/assets/product-marshmallow.webp"

func legacyAssets(t *testing.T) string {
	t.Helper()
	web := t.TempDir()
	assets := filepath.Join(web, "assets")
	if err := os.Mkdir(assets, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"product-chocolate.webp", "product-giftbox.webp", "product-marshmallow.webp"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "web", "assets", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(assets, name), data, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html></html>"), 0o640); err != nil {
		t.Fatal(err)
	}
	return assets
}

func (d *database) legacyMigration(t *testing.T, assets string) *application.LegacyMediaMigration {
	t.Helper()
	return application.NewLegacyMediaMigration(d.store, d.mediaLibrary(t), assets, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func legacyEntry(t *testing.T, report application.LegacyMediaReport, id string) application.LegacyProductReport {
	t.Helper()
	for _, entry := range report.Products {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("no report entry for %s", id)
	return application.LegacyProductReport{}
}

// legacyState is everything a run could change for the watched products.
func (d *database) legacyState(t *testing.T) map[string]string {
	t.Helper()
	state := map[string]string{}
	for key, query := range map[string]string{
		"assets":        `SELECT count(*)::text FROM media_assets`,
		"product_media": `SELECT COALESCE(string_agg(product_id||':'||role||':'||position||':'||media_id,',' ORDER BY product_id,role,position),'') FROM product_media`,
		"products":      `SELECT string_agg(id||':'||revision||':'||(legacy_media_retired_at IS NOT NULL)||':'||image||':'||array_to_string(gallery,'|'),',' ORDER BY id) FROM products`,
		"orders":        `SELECT string_agg(order_id||':'||position||':'||image,',' ORDER BY order_id,position) FROM order_items`,
		"audit":         `SELECT count(*)::text FROM staff_audit_events`,
	} {
		state[key] = d.text(t, query)
	}
	return state
}

func TestLegacyMediaMigrationScenario(t *testing.T) {
	d := newDatabase(t, true)
	login := d.signedInStaff(t, d.staffServer(t, nil))
	staff := domain.StaffUser{ID: login.id}
	assets := legacyAssets(t)
	migration := d.legacyMigration(t, assets)

	// A: legacy-only product with an overlapping, ordered gallery.
	d.exec(t, `UPDATE products SET image=$1,gallery=$2 WHERE id='p01'`, chocolatePath, []string{chocolatePath, giftboxPath, marshmallowPath})
	// B: product staff already configured in C2.
	asset := d.uploadMedia(t, staff)
	if _, err := d.mediaLibrary(t).SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p02"),
		domain.ProductMedia{ProductID: "p02", PrimaryID: asset.ID}); err != nil {
		t.Fatal(err)
	}
	// C: product without images.
	d.exec(t, `INSERT INTO products(id,slug,sku,name,price_minor,unit,category_id,manufacturer_id,brand_id,stock)
		SELECT 'no-image','no-image','NO-IMAGE',name,price_minor,unit,category_id,manufacturer_id,brand_id,stock FROM products WHERE id='p04'`)
	// D is the seeded order; E points one of its lines at a managed image.
	d.exec(t, `UPDATE order_items SET image=$1 WHERE order_id='ord-2026-002' AND position=2`, asset.URL)
	// F: a referenced file that does not exist.
	d.exec(t, `UPDATE products SET image='/assets/gone.webp',gallery='{}' WHERE id='p03'`)
	before := d.legacyState(t)
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()

	dry, err := migration.Run(ctx, false, domain.StaffUser{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if after := d.legacyState(t); !reflect.DeepEqual(before, after) {
		t.Fatalf("dry run changed state:\nbefore %v\nafter  %v", before, after)
	}
	want := map[string]application.LegacyMediaStatus{"p01": application.LegacyNeedsMigration, "p02": application.LegacyAlreadyManaged,
		"no-image": application.LegacyNoMedia, "p03": application.LegacyFailed}
	for id, status := range want {
		if entry := legacyEntry(t, dry, id); entry.Status != status {
			t.Fatalf("dry run %s: %+v", id, entry)
		}
	}
	plan := legacyEntry(t, dry, "p01")
	if plan.Primary == nil || plan.Primary.Source != chocolatePath || len(plan.Gallery) != 2 || plan.Gallery[0].Source != giftboxPath || plan.Gallery[1].Source != marshmallowPath {
		t.Fatalf("plan %+v", plan)
	}
	if problem := legacyEntry(t, dry, "p03").Problems[0]; problem.Kind != application.LegacyMissingFile || problem.Path != "/assets/gone.webp" {
		t.Fatalf("missing file %+v", problem)
	}
	if !reflect.DeepEqual(dry.MissingFiles, []string{"/assets/gone.webp"}) || len(dry.SafeToDelete) != 0 {
		t.Fatalf("retention %+v safe %+v", dry.MissingFiles, dry.SafeToDelete)
	}

	report, err := migration.Run(ctx, true, staff, "migrate-legacy:test")
	if err != nil {
		t.Fatal(err)
	}
	// 24 seeded products minus B and F, plus nothing else.
	if report.Summary.Migrated != 22 || report.Summary.AlreadyManaged != 1 || report.Summary.NoMedia != 1 || report.Summary.Failed != 1 {
		t.Fatalf("summary %+v", report.Summary)
	}
	if assets := d.number(t, `SELECT count(*) FROM media_assets`); assets != 4 {
		t.Fatalf("expected the 3 shared catalog files plus the staff upload, got %d", assets)
	}
	if !report.Pending() || !reflect.DeepEqual(report.ActiveLegacyProducts, []string{"p03"}) {
		t.Fatalf("pending %+v", report.ActiveLegacyProducts)
	}

	// A became managed, primary and ordered gallery, one revision, one event.
	migrated := legacyEntry(t, report, "p01")
	media, err := d.mediaLibrary(t).ProductMedia(d.ctx, "p01")
	if err != nil {
		t.Fatal(err)
	}
	if media.PrimaryID != migrated.Primary.AssetID || !reflect.DeepEqual(media.GalleryIDs, []string{migrated.Gallery[0].AssetID, migrated.Gallery[1].AssetID}) {
		t.Fatalf("attached %+v from %+v", media, migrated)
	}
	product, err := d.service.Product(d.ctx, "p01")
	if err != nil {
		t.Fatal(err)
	}
	if product.Image != "/media/"+media.PrimaryID || !reflect.DeepEqual(product.Gallery, []string{"/media/" + media.GalleryIDs[0], "/media/" + media.GalleryIDs[1]}) {
		t.Fatalf("customer view %s %+v", product.Image, product.Gallery)
	}
	if d.number(t, `SELECT count(*) FROM products WHERE id='p01' AND image=$1 AND gallery=$2`, chocolatePath, []string{chocolatePath, giftboxPath, marshmallowPath}) != 1 {
		t.Fatal("legacy columns of a migrated product changed")
	}
	if n := d.number(t, `SELECT count(*) FROM staff_audit_events WHERE action='product.media_update' AND entity_id='p01' AND request_id='migrate-legacy:test' AND staff_user_id=$1`, login.id); n != 1 {
		t.Fatalf("attach audit events %d", n)
	}
	stored, file, err := d.mediaLibrary(t).Open(d.ctx, media.PrimaryID)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if stored.ContentType != "image/jpeg" {
		t.Fatalf("stored %+v", stored)
	}

	// B keeps its references and revision; only the fallback is retired.
	keep := legacyEntry(t, report, "p02")
	if keep.Status != application.LegacyAlreadyManaged || !keep.Retired {
		t.Fatalf("already managed %+v", keep)
	}
	if got := d.number(t, `SELECT revision FROM products WHERE id='p02'`); got != 2 {
		t.Fatalf("already-managed revision changed to %d", got)
	}
	if n := d.number(t, `SELECT count(*) FROM product_media WHERE product_id='p02' AND media_id=$1 AND role='primary'`, asset.ID); n != 1 {
		t.Fatal("already-managed references changed")
	}
	if n := d.number(t, `SELECT count(*) FROM staff_audit_events WHERE action='product.legacy_media_retire' AND entity_id='p02' AND staff_user_id=$1`, login.id); n != 1 {
		t.Fatalf("retire audit events %d", n)
	}

	// C stays image-free and F keeps rendering its legacy path.
	empty, err := d.service.Product(d.ctx, "no-image")
	if err != nil || empty.Image != "" || len(empty.Gallery) != 0 {
		t.Fatalf("no-image %+v %v", empty, err)
	}
	failed, err := d.service.Product(d.ctx, "p03")
	if err != nil || failed.Image != "/assets/gone.webp" {
		t.Fatalf("failed product %+v %v", failed.Image, err)
	}

	// D and E: order snapshots are untouched.
	if d.text(t, `SELECT image FROM order_items WHERE order_id='ord-2026-002' AND position=1`) != d.text(t, `SELECT image FROM products WHERE id='p01'`) {
		t.Fatal("historical legacy snapshot changed")
	}
	if d.text(t, `SELECT image FROM order_items WHERE order_id='ord-2026-002' AND position=2`) != asset.URL {
		t.Fatal("historical managed snapshot changed")
	}
	if d.text(t, `SELECT string_agg(order_id||':'||position||':'||image,',' ORDER BY order_id,position) FROM order_items`) != before["orders"] {
		t.Fatal("order snapshots changed")
	}

	// Retention: files behind historical orders stay. Marshmallow is only in
	// migrated products' compatibility columns, and this web root has no
	// static reference to it, so it alone is offered for removal.
	if !reflect.DeepEqual(report.SafeToDelete, []string{marshmallowPath}) || !reflect.DeepEqual(report.MissingFiles, []string{"/assets/gone.webp"}) {
		t.Fatalf("retention safe %+v missing %+v", report.SafeToDelete, report.MissingFiles)
	}
	if reconcile, err := d.mediaLibrary(t).Reconcile(d.ctx, time.Hour, false); err != nil || reconcile.Problems() {
		t.Fatalf("reconciliation %+v %v", reconcile, err)
	}

	// The second apply changes nothing and still reports the broken product.
	settled := d.legacyState(t)
	second, err := migration.Run(ctx, true, staff, "migrate-legacy:second")
	if err != nil {
		t.Fatal(err)
	}
	if after := d.legacyState(t); !reflect.DeepEqual(settled, after) {
		t.Fatalf("second apply changed state:\nbefore %v\nafter  %v", settled, after)
	}
	if second.Summary.Migrated != 0 || second.Summary.AlreadyMigrated != 23 || second.Summary.Failed != 1 {
		t.Fatalf("second summary %+v", second.Summary)
	}

	// Restoring the file lets a later run converge.
	data, err := os.ReadFile(filepath.Join(assets, "product-giftbox.webp"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "gone.webp"), data, 0o640); err != nil {
		t.Fatal(err)
	}
	third, err := migration.Run(ctx, true, staff, "migrate-legacy:third")
	if err != nil {
		t.Fatal(err)
	}
	if third.Summary.Migrated != 1 || third.Summary.Failed != 0 || third.Pending() {
		t.Fatalf("third summary %+v", third.Summary)
	}
	if entry := legacyEntry(t, third, "p03"); entry.Status != application.LegacyMigrated || !entry.Primary.Reused {
		t.Fatalf("recovered entry %+v", entry)
	}
}

func TestLegacyMediaRetiredProductIgnoresLaterStaffChoices(t *testing.T) {
	d := newDatabase(t, true)
	login := d.signedInStaff(t, d.staffServer(t, nil))
	staff := domain.StaffUser{ID: login.id}
	migration := d.legacyMigration(t, legacyAssets(t))
	if _, err := migration.Run(d.ctx, true, staff, "migrate-legacy:run"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.mediaLibrary(t).SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p01"),
		domain.ProductMedia{ProductID: "p01"}); err != nil {
		t.Fatal(err)
	}
	product, err := d.service.Product(d.ctx, "p01")
	if err != nil || product.Image != "" || len(product.Gallery) != 0 {
		t.Fatalf("detached product %q %+v %v", product.Image, product.Gallery, err)
	}
	// A rerun must not bring back images staff deliberately removed.
	if _, err := migration.Run(d.ctx, true, staff, "migrate-legacy:rerun"); err != nil {
		t.Fatal(err)
	}
	if n := d.number(t, `SELECT count(*) FROM product_media WHERE product_id='p01'`); n != 0 {
		t.Fatalf("rerun restored %d references", n)
	}
}

func TestLegacyMediaPartialManagedGalleryIsAConflict(t *testing.T) {
	d := newDatabase(t, true)
	login := d.signedInStaff(t, d.staffServer(t, nil))
	staff := domain.StaffUser{ID: login.id}
	asset := d.uploadMedia(t, staff)
	if _, err := d.mediaLibrary(t).SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p01"),
		domain.ProductMedia{ProductID: "p01", GalleryIDs: []string{asset.ID}}); err != nil {
		t.Fatal(err)
	}
	revision := d.productRevision(t, "p01")
	report, err := d.legacyMigration(t, legacyAssets(t)).Run(d.ctx, true, staff, "migrate-legacy:run")
	if err != nil {
		t.Fatal(err)
	}
	entry := legacyEntry(t, report, "p01")
	if entry.Status != application.LegacyConflict || entry.Retired || d.productRevision(t, "p01") != revision {
		t.Fatalf("conflict %+v", entry)
	}
	if n := d.number(t, `SELECT count(*) FROM product_media WHERE product_id='p01' AND role='primary'`); n != 0 {
		t.Fatal("conflict product was changed")
	}
}

func TestLegacyMediaAttachRechecksTheProductUnderLock(t *testing.T) {
	d := newDatabase(t, true)
	login := d.signedInStaff(t, d.staffServer(t, nil))
	staff := domain.StaffUser{ID: login.id}
	asset := d.uploadMedia(t, staff)
	// Staff attach media after classification and before the migration commit.
	if _, err := d.mediaLibrary(t).SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p01"),
		domain.ProductMedia{ProductID: "p01", PrimaryID: asset.ID}); err != nil {
		t.Fatal(err)
	}
	expected := domain.LegacyMediaProduct{ID: "p01", Image: d.text(t, `SELECT image FROM products WHERE id='p01'`)}
	err := d.store.MigrateLegacyProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), expected,
		domain.ProductMedia{ProductID: "p01", GalleryIDs: []string{}})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("attach over staff media: %v", err)
	}
	if n := d.number(t, `SELECT count(*) FROM product_media WHERE product_id='p01' AND media_id=$1`, asset.ID); n != 1 {
		t.Fatal("staff media overwritten")
	}
}

func TestLegacyMediaApplyRunLockExcludesAConcurrentRun(t *testing.T) {
	d := newDatabase(t, true)
	release, err := d.store.LockLegacyMigration(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.LockLegacyMigration(d.ctx); err == nil || !errors.Is(err, postgres.ErrLegacyMigrationRunning) {
		t.Fatalf("second lock: %v", err)
	}
	release()
	second, err := d.store.LockLegacyMigration(d.ctx)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	second()
}

func TestLegacyMediaApplyRequiresAnActiveActor(t *testing.T) {
	d := newDatabase(t, true)
	login := d.signedInStaff(t, d.staffServer(t, nil))
	if _, err := d.store.SetStaffDisabled(d.ctx, staffEmail, true); err != nil {
		t.Fatal(err)
	}
	before := d.legacyState(t)
	report, err := d.legacyMigration(t, legacyAssets(t)).Run(d.ctx, true, domain.StaffUser{ID: login.id}, "migrate-legacy:run")
	if err != nil {
		t.Fatal(err)
	}
	if entry := legacyEntry(t, report, "p01"); entry.Status != application.LegacyFailed {
		t.Fatalf("disabled actor %+v", entry)
	}
	after := d.legacyState(t)
	if !reflect.DeepEqual(before["product_media"], after["product_media"]) || before["products"] != after["products"] || before["audit"] != after["audit"] {
		t.Fatalf("disabled actor changed products\nbefore %v\nafter  %v", before, after)
	}
}
