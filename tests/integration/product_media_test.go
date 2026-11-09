//go:build integration

package integration_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

func TestProductMediaAPIAtomicRevisionAuditAndCoexistence(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := domain.StaffUser{ID: login.id}
	a, b, c := d.uploadMedia(t, staff), d.uploadMedia(t, staff), d.uploadMedia(t, staff)
	legacy := d.adminProduct(t, "p01")
	path := "/api/admin/v1/products/p01/media"
	body := func(rev int64, primary string, gallery ...string) string {
		if gallery == nil {
			gallery = []string{}
		}
		var p *string
		if primary != "" {
			p = &primary
		}
		data, _ := json.Marshal(map[string]any{"expectedRevision": rev, "primaryId": p, "galleryIds": gallery})
		return string(data)
	}
	before := len(d.auditTrail(t))
	for i, state := range []struct {
		primary string
		gallery []string
	}{{a.ID, []string{b.ID, c.ID}}, {a.ID, []string{c.ID, b.ID}}, {a.ID, []string{}}, {"", []string{}}} {
		rev := legacy.Revision + int64(i)
		ex := adminRequest(handler, login, "PUT", path, body(rev, state.primary, state.gallery...))
		if ex.Code != 200 || ex.body["revision"].(float64) != float64(rev+1) {
			t.Fatalf("set: %d %s", ex.Code, ex.Body.String())
		}
		current := d.adminProduct(t, "p01")
		if current.Image != legacy.Image || !reflect.DeepEqual(current.Gallery, legacy.Gallery) {
			t.Fatal("legacy rows changed")
		}
		public, err := d.service.Product(d.ctx, "p01")
		if err != nil {
			t.Fatal(err)
		}
		if i < 3 && public.Image != a.URL {
			t.Fatalf("primary %+v", public)
		}
		if i == 1 && !reflect.DeepEqual(public.Gallery, []string{c.URL, b.URL}) {
			t.Fatalf("reorder %v", public.Gallery)
		}
		if i == 2 && len(public.Gallery) != 0 {
			t.Fatal("managed primary mixed legacy gallery")
		}
		if i == 3 && (public.Image != legacy.Image || !reflect.DeepEqual(public.Gallery, legacy.Gallery)) {
			t.Fatal("detach did not restore fallback")
		}
		serialized, _ := json.Marshal(current.ManagedMedia)
		if strings.Contains(string(serialized), "sha256") || strings.Contains(string(serialized), d.mediaRoot) {
			t.Fatal("private metadata")
		}
		if stale := adminRequest(handler, login, "PUT", path, body(rev, b.ID)); stale.Code != 409 {
			t.Fatal("stale accepted")
		}
		if after := d.adminProduct(t, "p01"); !reflect.DeepEqual(after.ManagedMedia, current.ManagedMedia) {
			t.Fatalf("stale save changed references: %+v", after.ManagedMedia)
		}
		if d.productRevision(t, "p01") != rev+1 || len(d.auditTrail(t)) != before+i+1 {
			t.Fatal("revision/audit count")
		}
		trail := d.auditTrail(t)
		last := trail[len(trail)-1]
		if last != (auditRow{login.id, "product.media_update", "product", "p01", "req-b1"}) {
			t.Fatalf("audit %+v", last)
		}
	}
	rev := d.productRevision(t, "p01")
	for _, invalid := range []string{body(rev, a.ID, a.ID), body(rev, "", b.ID, b.ID), `{"expectedRevision":1}`, body(rev, "med_"+strings.Repeat("0", 32))} {
		if ex := adminRequest(handler, login, "PUT", path, invalid); ex.Code != 422 {
			t.Fatalf("invalid: %d %s", ex.Code, ex.Body.String())
		}
	}
	if len(d.auditTrail(t)) != before+4 {
		t.Fatal("invalid writes audited")
	}
	// Other products keep their references, including archived products.
	library := d.mediaLibrary(t)
	if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p03"), domain.ProductMedia{ProductID: "p03", PrimaryID: a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.NewProductAdmin(d.store).Archive(d.ctx, mutationAs(t, staff, application.ProductArchive), "p03", d.productRevision(t, "p03")); err != nil {
		t.Fatal(err)
	}
	if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p03"), domain.ProductMedia{ProductID: "p03", PrimaryID: b.ID}); err != nil {
		t.Fatal("archived edit", err)
	}
	if ex := adminRequest(handler, login, "DELETE", "/api/admin/v1/media/"+b.ID, ""); ex.Code != 409 {
		t.Fatal("other archived product reference deleted")
	}
	// A product never given managed media keeps its legacy images exactly.
	untouched := d.adminProduct(t, "p02")
	public, err := d.service.Product(d.ctx, "p02")
	if err != nil || public.ManagedMedia != nil || public.Image != untouched.Image || !reflect.DeepEqual(public.Gallery, untouched.Gallery) || !strings.HasPrefix(public.Image, "/assets/") {
		t.Fatalf("legacy-only product: %+v %v", public, err)
	}
}

func TestManagedOrderSnapshotRetainsImageAfterPrimaryChange(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := domain.StaffUser{ID: login.id}
	library := d.mediaLibrary(t)
	a, b := d.uploadMedia(t, staff), d.uploadMedia(t, staff)
	set := func(id string) {
		t.Helper()
		if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p03"), domain.ProductMedia{ProductID: "p03", PrimaryID: id}); err != nil {
			t.Fatal(err)
		}
	}
	set(a.ID)
	first := d.placeOrder(t, "usr-demo", line{"p03", 1})
	if first.Items[0].Image != a.URL {
		t.Fatal("checkout did not snapshot managed URL")
	}
	set(b.ID)
	second := d.placeOrder(t, "usr-demo", line{"p03", 1})
	if second.Items[0].Image != b.URL {
		t.Fatal("new primary not used")
	}
	historical, err := d.service.Order(d.ctx, "usr-demo", first.ID)
	if err != nil || historical.Items[0].Image != a.URL {
		t.Fatal("historical snapshot changed")
	}
	count, err := library.Get(d.ctx, a.ID)
	if err != nil || count.ProductCount != 0 || count.OrderCount != 1 {
		t.Fatalf("references %+v %v", count, err)
	}
	before := len(d.auditTrail(t))
	ex := adminRequest(handler, login, "DELETE", "/api/admin/v1/media/"+a.ID, "")
	if ex.Code != 409 || ex.body["error"].(map[string]any)["references"].(map[string]any)["orders"].(float64) != 1 {
		t.Fatalf("order delete: %d %s", ex.Code, ex.Body.String())
	}
	if len(d.auditTrail(t)) != before || serve(handler, "GET", a.URL, "", nil).Code != 200 {
		t.Fatal("refused delete broke snapshot or audited")
	}
	// Existing historical legacy paths remain exactly as seeded.
	if d.number(t, "SELECT count(*) FROM order_items WHERE image LIKE '/assets/%'") == 0 {
		t.Fatal("legacy order images lost")
	}
}

func TestCheckoutRacingDetachAndDeletePreservesSnapshots(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "media-race@example.com")
	library := d.mediaLibrary(t)
	for round := 0; round < 8; round++ {
		a := d.uploadMedia(t, staff)
		if _, err := library.SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p03"), domain.ProductMedia{ProductID: "p03", PrimaryID: a.ID}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.service.ClearCart(d.ctx, "usr-demo", false); err != nil {
			t.Fatal(err)
		}
		d.set(t, "usr-demo", "p03", 1, true)
		rev := d.productRevision(t, "p03")
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var checkoutErr, detachErr, deleteErr error
		mutation := mutationAs(t, staff, application.ProductMediaUpdate)
		remove := mutationAs(t, staff, application.MediaDelete)
		go func() {
			defer wg.Done()
			<-start
			_, checkoutErr = d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Москва, Тестовая, 1"})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, detachErr = library.SetProductMedia(d.ctx, mutation, rev, domain.ProductMedia{ProductID: "p03"})
			deleteErr = library.Delete(d.ctx, remove, a.ID)
		}()
		close(start)
		wg.Wait()
		if checkoutErr != nil {
			t.Fatal(checkoutErr)
		}
		if detachErr != nil && !errors.Is(detachErr, domain.ErrStaleRevision) {
			t.Fatal(detachErr)
		}
		var conflict *domain.ReferenceConflictError
		if deleteErr != nil && !errors.As(deleteErr, &conflict) {
			t.Fatal(deleteErr)
		}
		if n := d.number(t, `SELECT count(*) FROM order_items oi JOIN media_assets m ON oi.image='/media/'||m.id WHERE m.deleted_at IS NOT NULL`); n != 0 {
			t.Fatal("broken order snapshot", fmt.Sprint(round))
		}
	}
}

// Exercise the changed public DTO against its actual schema for every cutover
// shape, including the nullable primary of a gallery-only assignment.
func TestManagedCustomerProductContract(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := domain.StaffUser{ID: login.id}
	asset := d.uploadMedia(t, staff)
	doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []domain.ProductMedia{
		{ProductID: "p01", PrimaryID: asset.ID},
		{ProductID: "p01", GalleryIDs: []string{asset.ID}},
		{ProductID: "p01"},
	} {
		if _, err := d.mediaLibrary(t).SetProductMedia(d.ctx, mutationAs(t, staff, application.ProductMediaUpdate), d.productRevision(t, "p01"), state); err != nil {
			t.Fatal(err)
		}
		response := serve(handler, "GET", "/api/products/p01", "", nil)
		if response.Code != 200 {
			t.Fatalf("public product: %d", response.Code)
		}
		if err := doc.Components.Schemas["Product"].Value.VisitJSON(response.body); err != nil {
			t.Fatalf("customer product contract: %v", err)
		}
	}
	before := len(d.auditTrail(t))
	if _, err := d.store.SetStaffDisabled(d.ctx, staffEmail, true); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(handler, login, "PUT", "/api/admin/v1/products/p01/media", fmt.Sprintf(`{"expectedRevision":%d,"primaryId":null,"galleryIds":[]}`, d.productRevision(t, "p01")))
	if response.Code != 401 || len(d.auditTrail(t)) != before {
		t.Fatal("disabled staff changed media")
	}
}
