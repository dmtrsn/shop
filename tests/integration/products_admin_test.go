//go:build integration

package integration_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/tests/contract"
)

// Staff product management on PostgreSQL (ADR 013). The demo fixture gives
// the dictionaries, p01 (in the demo cart, a collection and the demo order)
// and the demo customer; every test owns its database.

// signedInStaff inserts the staff account loginStaff uses, hashed at the
// minimum cost, and signs it in.
func (d *database) signedInStaff(t *testing.T, handler http.Handler) staffLogin {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(staffPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	d.exec(t, "INSERT INTO staff_users(id,email,display_name,password_hash) VALUES ($1,$2,'Ops',$3)", "stf_"+strings.Repeat("e", 32), staffEmail, string(hash))
	login, ex := loginStaff(t, handler, staffPassword, nil)
	if ex.Code != 200 {
		t.Fatalf("staff login: %d", ex.Code)
	}
	return login
}

func adminRequest(handler http.Handler, login staffLogin, method, path, body string) exchange {
	return serve(handler, method, path, body, map[string]string{
		"Cookie": staffCookie + "=" + login.token, "Origin": contract.AdminTrustedOrigin, "X-CSRF-Token": login.csrf, "X-Request-ID": "req-b1"})
}

func mutationAs(t *testing.T, staff domain.StaffUser, action domain.AuditAction) domain.StaffMutation {
	t.Helper()
	mutation, err := application.NewStaffAuth(nil).AuthorizeMutation(staff, action, "")
	if err != nil {
		t.Fatal(err)
	}
	return mutation
}

func (d *database) adminProduct(t *testing.T, id string) domain.AdminProduct {
	t.Helper()
	product, err := d.store.AdminProduct(d.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return product
}

// inputOf is what an editor sends back after changing nothing.
func inputOf(p domain.AdminProduct) domain.ProductInput {
	collections := []string{}
	for _, c := range p.Collections {
		collections = append(collections, c.ID)
	}
	return domain.ProductInput{Slug: p.Slug, SKU: p.SKU, Name: p.Name, ShortName: p.ShortName, Description: p.Description,
		PriceMinor: p.PriceMinor, OldPriceMinor: p.OldPriceMinor, Unit: p.Unit, WeightGrams: p.WeightGrams,
		CategoryID: p.Category.ID, ManufacturerID: p.Manufacturer.ID, BrandID: p.Brand.ID, CollectionIDs: collections,
		Series: p.Series, Tags: p.Tags, Badges: p.Badges, Ingredients: p.Ingredients, Nutrition: p.Nutrition,
		ShelfLifeDays: p.ShelfLifeDays, Stock: p.Stock, MinimumQuantity: p.MinimumQuantity, QuantityStep: p.QuantityStep, Featured: p.Featured}
}

func productJSONOf(t *testing.T, in domain.ProductInput, expectedRevision int64) string {
	t.Helper()
	body := map[string]any{"slug": in.Slug, "sku": in.SKU, "name": in.Name, "shortName": in.ShortName, "description": in.Description,
		"priceMinor": in.PriceMinor, "oldPriceMinor": in.OldPriceMinor, "unit": in.Unit, "weightGrams": in.WeightGrams,
		"categoryId": in.CategoryID, "manufacturerId": in.ManufacturerID, "brandId": in.BrandID, "collectionIds": in.CollectionIDs,
		"series": in.Series, "tags": in.Tags, "badges": in.Badges, "ingredients": in.Ingredients, "nutrition": in.Nutrition,
		"shelfLifeDays": in.ShelfLifeDays, "stock": in.Stock, "minimumQuantity": in.MinimumQuantity, "quantityStep": in.QuantityStep,
		"featured": in.Featured, "expectedRevision": expectedRevision}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

type auditRow struct{ actor, action, entityType, entityID, requestID string }

func (d *database) auditTrail(t *testing.T) []auditRow {
	t.Helper()
	rows, err := d.pool.Query(d.ctx, `SELECT staff_user_id,action,entity_type,entity_id,COALESCE(request_id,'') FROM staff_audit_events ORDER BY created_at,id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := []auditRow{}
	for rows.Next() {
		var row auditRow
		if err := rows.Scan(&row.actor, &row.action, &row.entityType, &row.entityID, &row.requestID); err != nil {
			t.Fatal(err)
		}
		result = append(result, row)
	}
	return result
}

func TestStaffCreatesAProductWithOneAuditEvent(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	ex := adminRequest(handler, login, "POST", "/api/admin/v1/products", contract.AdminProductBody("new-bar", 10, 0))
	if ex.Code != 201 {
		t.Fatalf("create: %d %s", ex.Code, ex.Body.String())
	}
	id := ex.body["id"].(string)
	if !strings.HasPrefix(id, "prd_") || ex.body["revision"].(float64) != 1 || ex.body["image"] != "" {
		t.Fatalf("created: %v", ex.body)
	}
	if trail := d.auditTrail(t); len(trail) != 1 || trail[0] != (auditRow{login.id, "product.create", "product", id, "req-b1"}) {
		t.Fatalf("audit: %+v", trail)
	}
	if got := adminRequest(handler, login, "GET", "/api/admin/v1/products/"+id, ""); got.Code != 200 || got.body["revision"].(float64) != 1 || got.body["brand"].(map[string]any)["id"] != "b-atelier-noir" {
		t.Fatalf("detail: %d %v", got.Code, got.body)
	}
	// New memberships go to the end of the collection.
	if d.number(t, "SELECT sort_order FROM collection_products WHERE collection_id='col-gifts' AND product_id=$1", id) != 6 {
		t.Fatal("collection position")
	}
	customerView, err := d.service.Product(d.ctx, "new-bar")
	if err != nil || customerView.ID != id || customerView.PriceMinor != 15900 || customerView.MinimumQuantity != 2 {
		t.Fatalf("customer view: %+v %v", customerView, err)
	}
}

func TestProductMutationsRefuseCustomersAndInactiveStaff(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	customerCookie := cookieLogin(t, handler, "demo@sugar.ru", "Sweet2026!")
	products := d.number(t, "SELECT count(*) FROM products")
	for _, headers := range []map[string]string{
		{"Cookie": shopCookie + "=" + customerCookie, "Origin": contract.AdminTrustedOrigin},
		{"Cookie": shopCookie + "=" + customerCookie, "Origin": contract.TrustedOrigin},
		{"Authorization": "Bearer " + customerCookie, "Origin": contract.AdminTrustedOrigin},
	} {
		if ex := serve(handler, "POST", "/api/admin/v1/products", contract.AdminProductBody("customer-bar", 1, 0), headers); ex.Code != 401 {
			t.Fatalf("customer create: %d", ex.Code)
		}
	}
	login := d.signedInStaff(t, handler)
	if _, err := d.store.SetStaffDisabled(d.ctx, staffEmail, true); err != nil {
		t.Fatal(err)
	}
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/products", contract.AdminProductBody("disabled-bar", 1, 0)); ex.Code != 401 {
		t.Fatalf("disabled staff: %d", ex.Code)
	}
	// A mutation authorized before the disable cannot commit after it.
	staff := domain.StaffUser{ID: login.id}
	p01 := d.adminProduct(t, "p01")
	if _, err := application.NewProductAdmin(d.store).Update(d.ctx, mutationAs(t, staff, application.ProductUpdate), "p01", p01.Revision, inputOf(p01)); !errors.Is(err, domain.ErrAuditActorInactive) {
		t.Fatalf("disabled actor: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM products") != products || len(d.auditTrail(t)) != 0 || d.adminProduct(t, "p01").Revision != p01.Revision {
		t.Fatal("a refused mutation changed something")
	}
}

func TestProductCreateRollsBackWhenItsAuditFails(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	d.exec(t, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$`)
	d.exec(t, `CREATE TRIGGER fail_product_audit BEFORE INSERT ON staff_audit_events FOR EACH ROW WHEN (NEW.entity_type='product') EXECUTE FUNCTION fail_audit()`)
	products, memberships := d.number(t, "SELECT count(*) FROM products"), d.number(t, "SELECT count(*) FROM collection_products")
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/products", contract.AdminProductBody("doomed-bar", 10, 0)); ex.Code != 500 {
		t.Fatalf("create: %d", ex.Code)
	}
	p01 := d.adminProduct(t, "p01")
	if ex := adminRequest(handler, login, "PUT", "/api/admin/v1/products/p01", productJSONOf(t, inputOf(p01), p01.Revision)); ex.Code != 500 {
		t.Fatalf("update: %d", ex.Code)
	}
	if d.number(t, "SELECT count(*) FROM products") != products || d.number(t, "SELECT count(*) FROM collection_products") != memberships ||
		d.adminProduct(t, "p01").Revision != p01.Revision || len(d.auditTrail(t)) != 0 {
		t.Fatal("a product change committed without its audit event")
	}
}

// Staff A and B both read revision 7. A's update commits revision 8 with one
// audit event; B's stale update is 409 and changes nothing.
func TestStaleProductUpdateIsRefused(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	b := d.signedInStaff(t, handler)
	a := d.auditStaff(t, "a@example.com")
	d.exec(t, "UPDATE products SET revision=7 WHERE id='p01'")
	readByA, readByB := d.adminProduct(t, "p01"), d.adminProduct(t, "p01")
	if readByA.Revision != 7 {
		t.Fatal("setup")
	}
	editA := inputOf(readByA)
	editA.Stock = 50
	updated, err := application.NewProductAdmin(d.store).Update(d.ctx, mutationAs(t, a, application.ProductUpdate), "p01", 7, editA)
	if err != nil || updated.Revision != 8 || updated.Stock != 50 {
		t.Fatalf("A: %+v %v", updated, err)
	}
	editB := inputOf(readByB)
	editB.PriceMinor = 1
	ex := adminRequest(handler, b, "PUT", "/api/admin/v1/products/p01", productJSONOf(t, editB, readByB.Revision))
	if ex.Code != 409 || !strings.HasPrefix(ex.errorCode(), "stale_revision|") {
		t.Fatalf("B: %d %s", ex.Code, ex.errorCode())
	}
	after := d.adminProduct(t, "p01")
	if after.Revision != 8 || after.Stock != 50 || after.PriceMinor != readByA.PriceMinor {
		t.Fatalf("B overwrote A: %+v", after)
	}
	if trail := d.auditTrail(t); len(trail) != 1 || trail[0].actor != a.ID || trail[0].action != "product.update" || trail[0].entityID != "p01" {
		t.Fatalf("audit: %+v", trail)
	}
	if ex := adminRequest(handler, b, "PUT", "/api/admin/v1/products/missing", productJSONOf(t, editB, 1)); ex.Code != 404 || len(d.auditTrail(t)) != 1 {
		t.Fatalf("missing product: %d", ex.Code)
	}
}

func TestConcurrentProductEditsHaveOneWinner(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	admin := application.NewProductAdmin(d.store)
	base := d.adminProduct(t, "p01")
	const n = 8
	mutations := make([]domain.StaffMutation, n)
	for i := range mutations {
		mutations[i] = mutationAs(t, staff, application.ProductUpdate)
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			edit := inputOf(base)
			edit.Stock = 100 + i
			<-start
			_, errs[i] = admin.Update(d.ctx, mutations[i], "p01", base.Revision, edit)
		}(i)
	}
	close(start)
	wg.Wait()
	winner := -1
	for i, err := range errs {
		switch {
		case err == nil && winner < 0:
			winner = i
		case errors.Is(err, domain.ErrStaleRevision):
		default:
			t.Fatalf("edit %d: %v", i, err)
		}
	}
	after := d.adminProduct(t, "p01")
	if winner < 0 || after.Revision != base.Revision+1 || after.Stock != 100+winner || len(d.auditTrail(t)) != 1 {
		t.Fatalf("winner %d, %+v", winner, after)
	}
	// Edits that each use the current revision all succeed.
	for i := 0; i < 5; i++ {
		current := d.adminProduct(t, "p01")
		edit := inputOf(current)
		edit.Stock = i
		if updated, err := admin.Update(d.ctx, mutationAs(t, staff, application.ProductUpdate), "p01", current.Revision, edit); err != nil || updated.Revision != current.Revision+1 {
			t.Fatalf("chained edit %d: %v", i, err)
		}
	}
	if len(d.auditTrail(t)) != 6 {
		t.Fatal("one audit event per edit")
	}
}

func TestArchiveWithdrawsAProductFromSale(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	admin := application.NewProductAdmin(d.store)
	d.customer(t, "usr_b1")
	d.set(t, "usr_b1", "p01", 2, true)
	if _, err := d.service.SetFavorite(d.ctx, "usr_b1", "p01", true); err != nil {
		t.Fatal(err)
	}
	categories := func() int {
		list, err := d.service.Categories(d.ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range list {
			if c.ID == "cat-bars" {
				return c.ProductCount
			}
		}
		return -1
	}
	barsBefore, stock := categories(), d.stock(t, "p01")
	before := d.adminProduct(t, "p01")
	archived, err := admin.Archive(d.ctx, mutationAs(t, staff, application.ProductArchive), "p01", before.Revision)
	if err != nil || !archived.Archived || archived.ArchivedAt == nil || archived.Revision != before.Revision+1 {
		t.Fatalf("archive: %+v %v", archived, err)
	}
	// Absent from every catalog read.
	for _, ref := range []string{"p01", "dark-chocolate-sea-salt", "cha-701"} {
		if _, err := d.service.Product(d.ctx, ref); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("detail %s: %v", ref, err)
		}
	}
	page, err := d.service.Catalog(d.ctx, domain.ProductFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range page.Items {
		if p.ID == "p01" {
			t.Fatal("archived product listed")
		}
	}
	if categories() != barsBefore-1 {
		t.Fatal("category count includes the archived product")
	}
	collections, err := d.service.Collections(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range collections {
		for _, id := range c.ProductIDs {
			if id == "p01" {
				t.Fatalf("collection %s lists the archived product", c.ID)
			}
		}
	}
	suggestions, err := d.service.Suggestions(d.ctx, "морской")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range suggestions {
		if s.ID == "p01" {
			t.Fatal("archived product suggested")
		}
	}
	if favorites, err := d.service.Favorites(d.ctx, "usr_b1"); err != nil || favorites.Total != 0 || d.number(t, "SELECT count(*) FROM favorites WHERE user_id='usr_b1'") != 1 {
		t.Fatalf("favorites: %+v %v", favorites, err)
	}
	// The cart line stays, unavailable; checkout refuses it and changes nothing.
	cart := d.cart(t, "usr_b1")
	if len(cart.Items) != 1 || cart.Items[0].Issue != "Товар больше не продаётся" || cart.Items[0].Available != 0 || cart.Summary.UnavailableCount != 1 {
		t.Fatalf("cart: %+v", cart.Items)
	}
	if _, err := d.service.SetCartItem(d.ctx, "usr_b1", "p01", 4, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("add archived: %v", err)
	}
	if _, err := d.service.Checkout(d.ctx, "usr_b1", domain.CheckoutInput{DeliveryAddress: "Адрес"}); !errors.Is(err, domain.ErrOutOfStock) {
		t.Fatalf("checkout: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM orders WHERE user_id='usr_b1'") != 0 || d.stock(t, "p01") != stock {
		t.Fatal("archived product sold")
	}
	// History stays readable; repeat calls it discontinued.
	order, err := d.service.Order(d.ctx, "usr-demo", "ord-2026-002")
	if err != nil || order.Items[0].ProductID != "p01" || order.Items[0].Name == "" {
		t.Fatalf("order history: %+v %v", order, err)
	}
	preview, err := d.service.RepeatPreview(d.ctx, "usr-demo", "ord-2026-002")
	if err != nil || preview.Items[0].Status != "discontinued" {
		t.Fatalf("repeat: %+v %v", preview.Items, err)
	}
	// Staff still see it.
	if list, err := admin.List(d.ctx, domain.AdminProductFilter{Status: domain.AdminProductsArchived}); err != nil || list.Total != 1 || list.Items[0].ID != "p01" {
		t.Fatalf("archived list: %+v %v", list, err)
	}
	if list, err := admin.List(d.ctx, domain.AdminProductFilter{Query: "p01"}); err != nil || list.Total != 0 {
		t.Fatalf("active list: %+v %v", list, err)
	}
	if _, err := admin.Archive(d.ctx, mutationAs(t, staff, application.ProductArchive), "p01", archived.Revision); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("archive twice: %v", err)
	}
	restored, err := admin.Restore(d.ctx, mutationAs(t, staff, application.ProductRestore), "p01", archived.Revision)
	if err != nil || restored.Archived || restored.Revision != archived.Revision+1 {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	if _, err := admin.Restore(d.ctx, mutationAs(t, staff, application.ProductRestore), "p01", restored.Revision); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("restore twice: %v", err)
	}
	if favorites, _ := d.service.Favorites(d.ctx, "usr_b1"); favorites.Total != 1 || categories() != barsBefore {
		t.Fatal("restore did not return the product")
	}
	if _, err := d.service.Checkout(d.ctx, "usr_b1", domain.CheckoutInput{DeliveryAddress: "Адрес"}); err != nil {
		t.Fatalf("checkout after restore: %v", err)
	}
	trail := d.auditTrail(t)
	if len(trail) != 2 || trail[0].action != "product.archive" || trail[1].action != "product.restore" {
		t.Fatalf("audit: %+v", trail)
	}
}

func TestProductWritesEnforceReferencesAndInvariants(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := domain.StaffUser{ID: login.id}
	admin := application.NewProductAdmin(d.store)
	valid := inputOf(d.adminProduct(t, "p01"))
	valid.Slug, valid.SKU = "fresh-bar", "FRESH-1"
	products := d.number(t, "SELECT count(*) FROM products")
	for name, tc := range map[string]struct {
		change func(*domain.ProductInput)
		field  string
	}{
		"unknown category":      {func(p *domain.ProductInput) { p.CategoryID = "cat-missing" }, "categoryId"},
		"unknown manufacturer":  {func(p *domain.ProductInput) { p.ManufacturerID = "m-missing" }, "manufacturerId"},
		"unknown brand":         {func(p *domain.ProductInput) { p.BrandID = "b-missing" }, "brandId"},
		"foreign brand":         {func(p *domain.ProductInput) { p.BrandID = "b-caffarel" }, "brandId"},
		"unknown collection":    {func(p *domain.ProductInput) { p.CollectionIDs = []string{"col-missing"} }, "collectionIds"},
		"used slug":             {func(p *domain.ProductInput) { p.Slug = "dark-chocolate-sea-salt" }, "slug"},
		"slug equal to an ID":   {func(p *domain.ProductInput) { p.Slug = "p02" }, "slug"},
		"slug equal to a SKU":   {func(p *domain.ProductInput) { p.Slug = "cha-701" }, "slug"},
		"used SKU, other case":  {func(p *domain.ProductInput) { p.SKU = "cha-701" }, "sku"},
		"SKU equal to a slug":   {func(p *domain.ProductInput) { p.SKU = "Milk-Chocolate-Hazelnut" }, "sku"},
		"SKU equal to an ID":    {func(p *domain.ProductInput) { p.SKU = "P02" }, "sku"},
		"zero price":            {func(p *domain.ProductInput) { p.PriceMinor = 0 }, "priceMinor"},
		"old price below price": {func(p *domain.ProductInput) { p.OldPriceMinor = p.PriceMinor - 1 }, "oldPriceMinor"},
		"negative stock":        {func(p *domain.ProductInput) { p.Stock = -1 }, "stock"},
		"zero minimum":          {func(p *domain.ProductInput) { p.MinimumQuantity = 0 }, "minimumQuantity"},
		"zero step":             {func(p *domain.ProductInput) { p.QuantityStep = 0 }, "quantityStep"},
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			tc.change(&input)
			_, err := admin.Create(d.ctx, mutationAs(t, staff, application.ProductCreate), input)
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || len(validation.Fields) != 1 || validation.Fields[0].Field != tc.field {
				t.Fatalf("%v", err)
			}
			// The same rule holds through HTTP and on update.
			ex := adminRequest(handler, login, "PUT", "/api/admin/v1/products/p03", productJSONOf(t, input, d.adminProduct(t, "p03").Revision))
			if ex.Code != 422 {
				t.Fatalf("update: %d", ex.Code)
			}
		})
	}
	// p01 keeps its own identifiers on update.
	p01 := d.adminProduct(t, "p01")
	if _, err := admin.Update(d.ctx, mutationAs(t, staff, application.ProductUpdate), "p01", p01.Revision, inputOf(p01)); err != nil {
		t.Fatalf("unchanged update: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM products") != products || len(d.auditTrail(t)) != 1 {
		t.Fatal("a rejected write changed something")
	}
}

// Quantities valid when added are rechecked against the current rules on the
// locked rows at checkout, and nothing is rounded or consumed.
func TestCheckoutRevalidatesCurrentQuantityRules(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	admin := application.NewProductAdmin(d.store)
	d.customer(t, "usr_b1")
	d.set(t, "usr_b1", "p01", 3, true)
	setRules := func(minimum, step int) {
		t.Helper()
		current := d.adminProduct(t, "p01")
		edit := inputOf(current)
		edit.MinimumQuantity, edit.QuantityStep = minimum, step
		if _, err := admin.Update(d.ctx, mutationAs(t, staff, application.ProductUpdate), "p01", current.Revision, edit); err != nil {
			t.Fatal(err)
		}
	}
	stock := d.stock(t, "p01")
	for _, rule := range []struct{ minimum, step int }{{4, 1}, {2, 2}} {
		setRules(rule.minimum, rule.step)
		if cart := d.cart(t, "usr_b1"); cart.Items[0].Issue == "" || cart.Items[0].Quantity != 3 {
			t.Fatalf("cart under %+v: %+v", rule, cart.Items[0])
		}
		if _, err := d.service.Checkout(d.ctx, "usr_b1", domain.CheckoutInput{DeliveryAddress: "Адрес"}); !errors.Is(err, domain.ErrCheckoutChanged) {
			t.Fatalf("checkout under %+v: %v", rule, err)
		}
		if d.stock(t, "p01") != stock || quantity(d.cart(t, "usr_b1"), "p01") != 3 {
			t.Fatal("refused checkout changed state")
		}
	}
	d.set(t, "usr_b1", "p01", 4, true)
	revision := d.adminProduct(t, "p01").Revision
	result, err := d.service.Checkout(d.ctx, "usr_b1", domain.CheckoutInput{DeliveryAddress: "Адрес"})
	if err != nil || result.Order.Items[0].Quantity != 4 {
		t.Fatalf("valid checkout: %v", err)
	}
	if after := d.adminProduct(t, "p01"); int64(after.Stock) != stock-4 || after.Revision != revision+1 {
		t.Fatalf("checkout must decrement stock and bump the revision: %+v", after)
	}
}

// A staff stock edit racing a checkout never loses the sale: either the edit
// commits first and the sale comes off the new stock, or the sale commits
// first and the edit, now stale, is refused without an audit event.
func TestStaffStockEditRacingCheckout(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	admin := application.NewProductAdmin(d.store)
	// Deterministic order first: the sale lands between the read and the edit.
	d.customer(t, "usr_seq")
	d.set(t, "usr_seq", "p01", 1, true)
	read := d.adminProduct(t, "p01")
	if _, err := d.service.Checkout(d.ctx, "usr_seq", domain.CheckoutInput{DeliveryAddress: "Адрес"}); err != nil {
		t.Fatal(err)
	}
	edit := inputOf(read)
	edit.Stock = 500
	if _, err := admin.Update(d.ctx, mutationAs(t, staff, application.ProductUpdate), "p01", read.Revision, edit); !errors.Is(err, domain.ErrStaleRevision) {
		t.Fatalf("edit based on pre-sale stock: %v", err)
	}
	if d.stock(t, "p01") != int64(read.Stock-1) || len(d.auditTrail(t)) != 0 {
		t.Fatal("the sale was overwritten")
	}
	audits := 0
	for round := 0; round < 6; round++ {
		user := "usr_race_" + string(rune('a'+round))
		d.customer(t, user)
		d.set(t, user, "p01", 1, true)
		read := d.adminProduct(t, "p01")
		edit := inputOf(read)
		edit.Stock = 500 + round
		mutation := mutationAs(t, staff, application.ProductUpdate)
		var staffErr, checkoutErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, staffErr = admin.Update(d.ctx, mutation, "p01", read.Revision, edit)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, checkoutErr = d.service.Checkout(d.ctx, user, domain.CheckoutInput{DeliveryAddress: "Адрес"})
		}()
		close(start)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("staff edit and checkout deadlocked")
		}
		if checkoutErr != nil {
			t.Fatalf("round %d checkout: %v", round, checkoutErr)
		}
		after := d.adminProduct(t, "p01")
		switch {
		case staffErr == nil:
			audits++
			if after.Stock != 500+round-1 {
				t.Fatalf("round %d: edit then sale left stock %d", round, after.Stock)
			}
		case errors.Is(staffErr, domain.ErrStaleRevision):
			if after.Stock != read.Stock-1 {
				t.Fatalf("round %d: sale then refused edit left stock %d", round, after.Stock)
			}
		default:
			t.Fatalf("round %d staff: %v", round, staffErr)
		}
		if len(d.auditTrail(t)) != audits {
			t.Fatalf("round %d: audit events do not match committed edits", round)
		}
	}
}

// The B0 image (migrations 001–012) accepts the B1 schema and keeps serving;
// it simply has no product routes. ADR 013 covers what it does with archived
// products.
func TestPreviousBinaryToleratesProductRevisionSchema(t *testing.T) {
	d := productionDatabase(t)
	d.exec(t, `INSERT INTO categories(id,slug,name) VALUES ('cat-x','x','X')`)
	d.exec(t, `INSERT INTO manufacturers(id,slug,name) VALUES ('m-x','mx','MX')`)
	d.exec(t, `INSERT INTO brands(id,slug,name,manufacturer_id) VALUES ('b-x','bx','BX','m-x')`)
	staff := d.auditStaff(t, "a@example.com")
	input := domain.ProductInput{Slug: "x-bar", SKU: "X-1", Name: "X", Unit: "шт.", PriceMinor: 100, CategoryID: "cat-x", ManufacturerID: "m-x",
		BrandID: "b-x", CollectionIDs: []string{}, Tags: []string{}, Badges: []string{}, Stock: 5, MinimumQuantity: 1, QuantityStep: 1}
	if _, err := application.NewProductAdmin(d.store).Create(d.ctx, mutationAs(t, staff, application.ProductCreate), input); err != nil {
		t.Fatal(err)
	}
	previous := productionMigrator(migrationDir(t, []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_", "010_", "011_", "012_"}, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("B0 schema check on the B1 schema: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("B0 migrate on the B1 schema: %v", err)
	}
	handler := d.staffServer(t, nil)
	d.operatorCustomer(t, customer, customerPassword)
	if cookieLogin(t, handler, customer, customerPassword) == "" {
		t.Fatal("customer login")
	}
	if ex := serve(handler, "GET", "/api/catalog", "", nil); ex.Code != 200 || ex.body["total"].(float64) != 1 {
		t.Fatalf("catalog: %d", ex.Code)
	}
	d.signedInStaff(t, handler)
}

// The product form's dictionaries: every reference, readable only by an
// active staff session.
func TestCatalogOptionsForStaffOnly(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	customerCookie := cookieLogin(t, handler, "demo@sugar.ru", "Sweet2026!")
	for _, headers := range []map[string]string{nil, {"Cookie": shopCookie + "=" + customerCookie}, {"Authorization": "Bearer " + customerCookie}} {
		if ex := serve(handler, "GET", "/api/admin/v1/catalog-options", "", headers); ex.Code != 401 {
			t.Fatalf("non-staff read: %d", ex.Code)
		}
	}
	login := d.signedInStaff(t, handler)
	ex := adminRequest(handler, login, "GET", "/api/admin/v1/catalog-options", "")
	if ex.Code != 200 {
		t.Fatalf("staff read: %d", ex.Code)
	}
	var options domain.CatalogOptions
	if err := json.Unmarshal(ex.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{"categories": int64(len(options.Categories)), "manufacturers": int64(len(options.Manufacturers)), "brands": int64(len(options.Brands)), "collections": int64(len(options.Collections))}
	for table, got := range counts {
		if want := d.number(t, "SELECT count(*) FROM "+table); got != want {
			t.Errorf("%s: %d of %d", table, got, want)
		}
	}
	owner := map[string]string{}
	for _, brand := range options.Brands {
		owner[brand.ID] = brand.ManufacturerID
	}
	parents := map[string]string{}
	for _, category := range options.Categories {
		parents[category.ID] = category.ParentID
	}
	// Brands without products (hidden from the customer list) are offered too.
	if owner["b-atelier-noir"] != "m-choco" || owner["brand-unbranded"] != "m-choco" || parents["cat-bars"] != "cat-chocolate" || parents["cat-chocolate"] != "" {
		t.Fatalf("relationships: %v %v", owner, parents)
	}
	if _, err := d.store.SetStaffDisabled(d.ctx, staffEmail, true); err != nil {
		t.Fatal(err)
	}
	if ex := adminRequest(handler, login, "GET", "/api/admin/v1/catalog-options", ""); ex.Code != 401 {
		t.Fatalf("disabled staff read: %d", ex.Code)
	}
	if len(d.auditTrail(t)) != 0 {
		t.Fatal("a read wrote an audit event")
	}
}
