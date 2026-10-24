//go:build integration

package integration_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// Staff catalog dictionary management on PostgreSQL (ADR 018). The demo
// fixture supplies the dictionaries and products; every test owns its
// database.

func jsonOf(t *testing.T, body map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func categoryJSON(t *testing.T, slug, name, parent string, revision int64) string {
	body := map[string]any{"slug": slug, "name": name, "description": "", "parentId": parent, "icon": "", "sortOrder": 70}
	if revision > 0 {
		body["expectedRevision"] = revision
	}
	return jsonOf(t, body)
}

func (d *database) revisionOf(t *testing.T, table, id string) int64 {
	t.Helper()
	return d.number(t, "SELECT revision FROM "+table+" WHERE id=$1", id)
}

func (d *database) archivedIn(t *testing.T, table, id string) bool {
	t.Helper()
	return d.number(t, "SELECT count(*) FROM "+table+" WHERE id=$1 AND archived_at IS NOT NULL", id) == 1
}

func categoryInput(c domain.AdminCategory) domain.CategoryInput {
	return domain.CategoryInput{Slug: c.Slug, Name: c.Name, Description: c.Description, ParentID: c.ParentID, Icon: c.Icon, SortOrder: c.SortOrder}
}

func brandInput(b domain.AdminBrand) domain.BrandInput {
	return domain.BrandInput{Slug: b.Slug, Name: b.Name, ManufacturerID: b.Manufacturer.ID, Description: b.Description, SortOrder: b.SortOrder}
}

func (d *database) newCategory(t *testing.T, staff domain.StaffUser, slug, parent string) domain.AdminCategory {
	t.Helper()
	c, err := application.NewDictionaryAdmin(d.store).CreateCategory(d.ctx, mutationAs(t, staff, application.CategoryCreate),
		domain.CategoryInput{Slug: slug, Name: "Category " + slug, ParentID: parent})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func codeOf(err error) string {
	var conflict *domain.ReferenceConflictError
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, domain.ErrStaleRevision):
		return "stale"
	case errors.Is(err, domain.ErrInvalidHierarchy):
		return "hierarchy"
	case errors.As(err, &conflict):
		return "reference"
	case errors.Is(err, domain.ErrValidation):
		return "validation"
	case errors.Is(err, domain.ErrConflict):
		return "conflict"
	}
	return err.Error()
}

func TestStaffCreatesDictionariesWithOneAuditEventEach(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	created := map[string]string{}
	for _, tc := range []struct{ path, prefix, entity, action, body string }{
		{"categories", "cat_", "category", "category.create", categoryJSON(t, "honey", "Мёд", "", 0)},
		{"manufacturers", "mfr_", "manufacturer", "manufacturer.create", `{"slug":"apiary","name":"Apiary","country":"Россия","description":"","sortOrder":5}`},
		{"collections", "col_", "collection", "collection.create", `{"slug":"spring","name":"Весна","description":"","accent":"lime","startsAt":null,"endsAt":null,"sortOrder":5}`},
	} {
		ex := adminRequest(handler, login, "POST", "/api/admin/v1/"+tc.path, tc.body)
		if ex.Code != 201 {
			t.Fatalf("%s: %d %s", tc.path, ex.Code, ex.Body.String())
		}
		id := ex.body["id"].(string)
		if !strings.HasPrefix(id, tc.prefix) || ex.body["revision"].(float64) != 1 || ex.body["archived"] != false || ex.body["productCount"].(float64) != 0 {
			t.Fatalf("%s: %v", tc.path, ex.body)
		}
		created[tc.path] = id
		trail := d.auditTrail(t)
		if last := trail[len(trail)-1]; last != (auditRow{login.id, tc.action, tc.entity, id, "req-b1"}) {
			t.Fatalf("audit %+v", last)
		}
	}
	ex := adminRequest(handler, login, "POST", "/api/admin/v1/brands",
		`{"slug":"apiary-gold","name":"Apiary Gold","manufacturerId":"`+created["manufacturers"]+`","description":"","sortOrder":0}`)
	if ex.Code != 201 || ex.body["manufacturer"].(map[string]any)["name"] != "Apiary" {
		t.Fatalf("brand: %d %s", ex.Code, ex.Body.String())
	}
	if trail := d.auditTrail(t); len(trail) != 4 || trail[3].action != "brand.create" || trail[3].entityType != "brand" {
		t.Fatalf("audit: %+v", trail)
	}
	// The customer catalog lists them at once (brands only once they have products).
	categories, _ := d.service.Categories(d.ctx)
	collections, _ := d.service.Collections(d.ctx)
	manufacturers, _ := d.service.Manufacturers(d.ctx)
	if !containsID(categories, created["categories"]) || !containsID(collections, created["collections"]) || !containsID(manufacturers, created["manufacturers"]) {
		t.Fatal("customer catalog does not list the new dictionaries")
	}
	// A slug may equal no other row's slug or ID in its dictionary; nothing is written.
	for _, body := range []string{categoryJSON(t, "honey", "Ещё мёд", "", 0), categoryJSON(t, "cat-gifts", "ID как slug", "", 0)} {
		if ex := adminRequest(handler, login, "POST", "/api/admin/v1/categories", body); ex.Code != 422 || !strings.Contains(ex.Body.String(), `"field":"slug"`) {
			t.Fatalf("duplicate slug: %d %s", ex.Code, ex.Body.String())
		}
	}
	// The same slug in another dictionary is fine: filters are per dictionary.
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/collections", `{"slug":"honey","name":"Мёд","description":"","accent":"cocoa","startsAt":null,"endsAt":null,"sortOrder":0}`); ex.Code != 201 {
		t.Fatalf("cross-dictionary slug: %d", ex.Code)
	}
	if len(d.auditTrail(t)) != 5 {
		t.Fatal("a refused create wrote an audit event")
	}
	if ex := adminRequest(handler, login, "GET", "/api/admin/v1/categories/"+created["categories"], ""); ex.Code != 200 || ex.body["slug"] != "honey" {
		t.Fatalf("read back: %d", ex.Code)
	}
	if ex := adminRequest(handler, login, "GET", "/api/admin/v1/categories?status=all", ""); ex.Code != 200 || len(ex.body["items"].([]any)) != int(d.number(t, "SELECT count(*) FROM categories")) {
		t.Fatalf("list: %d", ex.Code)
	}
}

// containsID reports whether a customer list carries the ID.
func containsID(items any, id string) bool {
	encoded, _ := json.Marshal(items)
	return strings.Contains(string(encoded), `"id":"`+id+`"`)
}

func TestDictionaryMutationsRefuseCustomersAndInactiveStaffAndRollBackWithTheirAudit(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	customerCookie := cookieLogin(t, handler, "demo@sugar.ru", "Sweet2026!")
	if ex := serve(handler, "POST", "/api/admin/v1/categories", categoryJSON(t, "x", "X", "", 0), map[string]string{"Cookie": shopCookie + "=" + customerCookie}); ex.Code != 401 {
		t.Fatalf("customer: %d", ex.Code)
	}
	login := d.signedInStaff(t, handler)
	d.exec(t, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$`)
	d.exec(t, `CREATE TRIGGER fail_dictionary_audit BEFORE INSERT ON staff_audit_events FOR EACH ROW WHEN (NEW.entity_type IN ('category','brand','collection')) EXECUTE FUNCTION fail_audit()`)
	categories := d.number(t, "SELECT count(*) FROM categories")
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/categories", categoryJSON(t, "doomed", "X", "", 0)); ex.Code != 500 {
		t.Fatalf("create: %d", ex.Code)
	}
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/collections/col-new/archive", `{"expectedRevision":1}`); ex.Code != 500 {
		t.Fatalf("archive: %d", ex.Code)
	}
	if d.number(t, "SELECT count(*) FROM categories") != categories || d.archivedIn(t, "collections", "col-new") || d.revisionOf(t, "collections", "col-new") != 1 || len(d.auditTrail(t)) != 0 {
		t.Fatal("a dictionary change committed without its audit event")
	}
	d.exec(t, `DROP TRIGGER fail_dictionary_audit ON staff_audit_events`)
	staff := domain.StaffUser{ID: login.id}
	mutation := mutationAs(t, staff, application.CollectionArchive)
	if _, err := d.store.SetStaffDisabled(d.ctx, staffEmail, true); err != nil {
		t.Fatal(err)
	}
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/collections/col-new/archive", `{"expectedRevision":1}`); ex.Code != 401 {
		t.Fatalf("disabled staff: %d", ex.Code)
	}
	if _, err := application.NewDictionaryAdmin(d.store).SetCollectionArchived(d.ctx, mutation, "col-new", 1, true); !errors.Is(err, domain.ErrAuditActorInactive) {
		t.Fatalf("disabled actor: %v", err)
	}
	if d.archivedIn(t, "collections", "col-new") || len(d.auditTrail(t)) != 0 {
		t.Fatal("a refused mutation changed something")
	}
}

func TestStaleDictionaryUpdatesAreRefusedAndConcurrentOnesHaveOneWinner(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := d.auditStaff(t, "a@example.com")
	admin := application.NewDictionaryAdmin(d.store)
	m, err := d.store.AdminManufacturer(d.ctx, "m-kolomna")
	if err != nil || m.Revision != 1 {
		t.Fatalf("setup %+v %v", m, err)
	}
	rename := domain.ManufacturerInput{Slug: m.Slug, Name: "Коломна 1", Country: m.Country, Description: m.Description, SortOrder: m.SortOrder}
	if updated, err := admin.UpdateManufacturer(d.ctx, mutationAs(t, staff, application.ManufacturerUpdate), "m-kolomna", 1, rename); err != nil || updated.Revision != 2 {
		t.Fatalf("A: %+v %v", updated, err)
	}
	stale := `{"slug":"kolomna","name":"Коломна B","country":"","description":"","sortOrder":0,"expectedRevision":1}`
	if ex := adminRequest(handler, login, "PUT", "/api/admin/v1/manufacturers/m-kolomna", stale); ex.Code != 409 || !strings.HasPrefix(ex.errorCode(), "stale_revision|") {
		t.Fatalf("B: %d %s", ex.Code, ex.errorCode())
	}
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/manufacturers/m-kolomna/archive", `{"expectedRevision":1}`); ex.Code != 409 {
		t.Fatalf("stale archive: %d", ex.Code)
	}
	if after, _ := d.store.AdminManufacturer(d.ctx, "m-kolomna"); after.Name != "Коломна 1" || after.Revision != 2 || len(d.auditTrail(t)) != 1 {
		t.Fatalf("B overwrote A: %+v", after)
	}
	if ex := adminRequest(handler, login, "PUT", "/api/admin/v1/manufacturers/missing", stale); ex.Code != 404 {
		t.Fatalf("missing: %d", ex.Code)
	}

	const n = 8
	base, _ := d.store.AdminCollection(d.ctx, "col-new")
	mutations := make([]domain.StaffMutation, n)
	for i := range mutations {
		mutations[i] = mutationAs(t, staff, application.CollectionUpdate)
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = admin.UpdateCollection(d.ctx, mutations[i], "col-new", base.Revision, domain.CollectionInput{
				Slug: base.Slug, Name: fmt.Sprintf("Новинки %d", i), Description: base.Description, Accent: base.Accent, SortOrder: base.SortOrder})
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
	after, _ := d.store.AdminCollection(d.ctx, "col-new")
	if winner < 0 || after.Revision != base.Revision+1 || after.Name != fmt.Sprintf("Новинки %d", winner) || len(d.auditTrail(t)) != 2 {
		t.Fatalf("winner %d: %+v", winner, after)
	}
}

func TestCategoryTreeStaysTwoLevelsDeep(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := d.auditStaff(t, "a@example.com")
	for name, tc := range map[string]struct {
		method, path, body, code string
	}{
		"parent is a subcategory":      {"POST", "/api/admin/v1/categories", categoryJSON(t, "deep", "Deep", "cat-bars", 0), "invalid_hierarchy"},
		"category with children moves": {"PUT", "/api/admin/v1/categories/cat-chocolate", categoryJSON(t, "chocolate", "Шоколад", "cat-candy", 1), "invalid_hierarchy"},
		"own parent":                   {"PUT", "/api/admin/v1/categories/cat-gifts", categoryJSON(t, "gift-boxes", "Подарки", "cat-gifts", 1), "invalid_hierarchy"},
		"missing parent":               {"POST", "/api/admin/v1/categories", categoryJSON(t, "orphan", "Orphan", "cat-missing", 0), "validation_error"},
	} {
		ex := adminRequest(handler, login, tc.method, tc.path, tc.body)
		if !strings.HasPrefix(ex.errorCode(), tc.code+"|") || ex.Code != 422 {
			t.Errorf("%s: %d %s", name, ex.Code, ex.errorCode())
		}
	}
	if len(d.auditTrail(t)) != 0 || d.revisionOf(t, "categories", "cat-chocolate") != 1 {
		t.Fatal("a refused tree change was written")
	}
	// Moving a leaf under another top-level category is fine.
	if ex := adminRequest(handler, login, "PUT", "/api/admin/v1/categories/cat-truffles", categoryJSON(t, "truffles", "Трюфели", "cat-chocolate", 1)); ex.Code != 200 || ex.body["parentId"] != "cat-chocolate" {
		t.Fatalf("leaf move: %d %s", ex.Code, ex.Body.String())
	}
	// An archived parent is not offered for new placements.
	archived := d.newCategory(t, staff, "old-shelf", "")
	if _, err := application.NewDictionaryAdmin(d.store).SetCategoryArchived(d.ctx, mutationAs(t, staff, application.CategoryArchive), archived.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/categories", categoryJSON(t, "under-archived", "X", archived.ID, 0)); ex.Code != 422 || !strings.Contains(ex.Body.String(), `"field":"parentId"`) {
		t.Fatalf("archived parent: %d %s", ex.Code, ex.Body.String())
	}

	// Race: X under Y while Y goes under Z. Serialized, one of them must be
	// refused, and no category ever has a grandparent.
	admin := application.NewDictionaryAdmin(d.store)
	for round := 0; round < 6; round++ {
		x := d.newCategory(t, staff, fmt.Sprintf("x%d", round), "")
		y := d.newCategory(t, staff, fmt.Sprintf("y%d", round), "")
		z := d.newCategory(t, staff, fmt.Sprintf("z%d", round), "")
		xUnderY, yUnderZ := categoryInput(x), categoryInput(y)
		xUnderY.ParentID, yUnderZ.ParentID = y.ID, z.ID
		mutations := []domain.StaffMutation{mutationAs(t, staff, application.CategoryUpdate), mutationAs(t, staff, application.CategoryUpdate)}
		errs := make([]error, 2)
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, errs[0] = admin.UpdateCategory(d.ctx, mutations[0], x.ID, 1, xUnderY)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, errs[1] = admin.UpdateCategory(d.ctx, mutations[1], y.ID, 1, yUnderZ)
		}()
		close(start)
		wg.Wait()
		outcome := codeOf(errs[0]) + "," + codeOf(errs[1])
		if outcome != "ok,hierarchy" && outcome != "hierarchy,ok" {
			t.Fatalf("round %d: %s", round, outcome)
		}
	}
	if deep := d.number(t, `SELECT count(*) FROM categories c JOIN categories p ON p.id=c.parent_id WHERE p.parent_id IS NOT NULL`); deep != 0 {
		t.Fatalf("%d categories at a third level", deep)
	}
}

func TestUsedDictionariesCannotBeArchived(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	d.exec(t, `UPDATE products SET archived_at=now() WHERE id='p02'`)
	p01 := d.adminProduct(t, "p01")
	p02 := d.adminProduct(t, "p02")
	references := func(table, column, id string) int64 {
		return d.number(t, "SELECT count(*) FROM products WHERE "+column+"=$1", id)
	}
	for _, tc := range []struct {
		path, table, column, id string
		brands, children        int64
	}{
		{"categories", "categories", "category_id", p01.Category.ID, 0, 0},
		{"categories", "categories", "category_id", "cat-chocolate", 0, d.number(t, "SELECT count(*) FROM categories WHERE parent_id='cat-chocolate'")},
		{"manufacturers", "manufacturers", "manufacturer_id", p01.Manufacturer.ID, d.number(t, "SELECT count(*) FROM brands WHERE manufacturer_id=$1", p01.Manufacturer.ID), 0},
		{"brands", "brands", "brand_id", p01.Brand.ID, 0, 0},
		// p02 is archived; its references still count, since it can be restored.
		{"brands", "brands", "brand_id", p02.Brand.ID, 0, 0},
	} {
		ex := adminRequest(handler, login, "POST", "/api/admin/v1/"+tc.path+"/"+tc.id+"/archive", `{"expectedRevision":1}`)
		refs, _ := ex.body["error"].(map[string]any)["references"].(map[string]any)
		if ex.Code != 409 || !strings.HasPrefix(ex.errorCode(), "reference_conflict|Запись используется") ||
			int64(refs["products"].(float64)) != references(tc.table, tc.column, tc.id) || int64(refs["brands"].(float64)) != tc.brands || int64(refs["categories"].(float64)) != tc.children {
			t.Errorf("%s %s: %d %s", tc.path, tc.id, ex.Code, ex.Body.String())
		}
		if d.archivedIn(t, tc.table, tc.id) || d.revisionOf(t, tc.table, tc.id) != 1 {
			t.Errorf("%s %s changed", tc.path, tc.id)
		}
	}
	if len(d.auditTrail(t)) != 0 {
		t.Fatal("a refused archive wrote an audit event")
	}
	// A used brand keeps its manufacturer: products pair the two.
	b, _ := d.store.AdminBrand(d.ctx, p01.Brand.ID)
	move := brandInput(b)
	move.ManufacturerID = "m-lakrids"
	if _, err := application.NewDictionaryAdmin(d.store).UpdateBrand(d.ctx, mutationAs(t, d.auditStaff(t, "a@example.com"), application.BrandUpdate), b.ID, 1, move); codeOf(err) != "reference" {
		t.Fatalf("used brand moved: %v", err)
	}
}

func TestArchiveAndRestoreWithdrawAndReturnDictionaries(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := d.auditStaff(t, "a@example.com")
	admin := application.NewDictionaryAdmin(d.store)
	parent := d.newCategory(t, staff, "pantry", "")
	child := d.newCategory(t, staff, "pantry-jars", parent.ID)
	m, err := admin.CreateManufacturer(d.ctx, mutationAs(t, staff, application.ManufacturerCreate), domain.ManufacturerInput{Slug: "zephyr-lab", Name: "Zephyr Lab"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := admin.CreateBrand(d.ctx, mutationAs(t, staff, application.BrandCreate), domain.BrandInput{Slug: "zephyr-one", Name: "Zephyr One", ManufacturerID: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	audits := len(d.auditTrail(t))
	post := func(path string, revision int64) exchange {
		return adminRequest(handler, login, "POST", "/api/admin/v1/"+path, fmt.Sprintf(`{"expectedRevision":%d}`, revision))
	}
	// A parent with an active child, a manufacturer with an active brand: refused.
	if ex := post("categories/"+parent.ID+"/archive", 1); ex.Code != 409 || !strings.Contains(ex.errorCode(), "активных подкатегорий: 1") {
		t.Fatalf("parent: %d %s", ex.Code, ex.errorCode())
	}
	if ex := post("manufacturers/"+m.ID+"/archive", 1); ex.Code != 409 || !strings.Contains(ex.errorCode(), "активных брендов: 1") {
		t.Fatalf("manufacturer: %d %s", ex.Code, ex.errorCode())
	}
	// Bottom-up they go.
	for _, step := range []struct {
		path     string
		revision int64
	}{{"categories/" + child.ID + "/archive", 1}, {"categories/" + parent.ID + "/archive", 1}, {"brands/" + b.ID + "/archive", 1}, {"manufacturers/" + m.ID + "/archive", 1}} {
		if ex := post(step.path, step.revision); ex.Code != 200 || ex.body["archived"] != true || ex.body["archivedAt"] == nil || ex.body["revision"].(float64) != 2 {
			t.Fatalf("%s: %d %s", step.path, ex.Code, ex.Body.String())
		}
	}
	if ex := post("brands/"+b.ID+"/archive", 2); ex.Code != 409 || !strings.HasPrefix(ex.errorCode(), "conflict|") {
		t.Fatalf("repeated archive: %d %s", ex.Code, ex.errorCode())
	}
	categories, _ := d.service.Categories(d.ctx)
	manufacturers, _ := d.service.Manufacturers(d.ctx)
	suggestions, _ := d.service.Suggestions(d.ctx, "zephyr")
	if containsID(categories, parent.ID) || containsID(categories, child.ID) || containsID(manufacturers, m.ID) || len(suggestions) != 0 {
		t.Fatalf("archived dictionaries still shown: %v", suggestions)
	}
	if ex := adminRequest(handler, login, "GET", "/api/admin/v1/categories?status=archived", ""); ex.Code != 200 || len(ex.body["items"].([]any)) != 2 {
		t.Fatalf("archived list: %d %s", ex.Code, ex.Body.String())
	}
	if ex := adminRequest(handler, login, "GET", "/api/admin/v1/categories", ""); strings.Contains(ex.Body.String(), parent.ID) {
		t.Fatal("the default list shows archived rows")
	}
	// Restore needs an active parent; top-down it works.
	if ex := post("categories/"+child.ID+"/restore", 2); ex.Code != 422 || !strings.Contains(ex.Body.String(), `"field":"parentId"`) {
		t.Fatalf("child before parent: %d %s", ex.Code, ex.Body.String())
	}
	if ex := post("brands/"+b.ID+"/restore", 2); ex.Code != 422 || !strings.Contains(ex.Body.String(), `"field":"manufacturerId"`) {
		t.Fatalf("brand before manufacturer: %d %s", ex.Code, ex.Body.String())
	}
	for _, path := range []string{"categories/" + parent.ID, "categories/" + child.ID, "manufacturers/" + m.ID, "brands/" + b.ID} {
		if ex := post(path+"/restore", 2); ex.Code != 200 || ex.body["archived"] != false || ex.body["revision"].(float64) != 3 {
			t.Fatalf("%s restore: %d %s", path, ex.Code, ex.Body.String())
		}
	}
	categories, _ = d.service.Categories(d.ctx)
	if !containsID(categories, child.ID) {
		t.Fatal("restored category not shown")
	}
	trail := d.auditTrail(t)[audits:]
	actions := []string{}
	for _, row := range trail {
		actions = append(actions, row.action)
	}
	if strings.Join(actions, ",") != "category.archive,category.archive,brand.archive,manufacturer.archive,category.restore,category.restore,manufacturer.restore,brand.restore" {
		t.Fatalf("audit: %v", actions)
	}
}

func TestArchivedCollectionKeepsMembershipsButLeavesTheStorefront(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	members := d.number(t, "SELECT count(*) FROM collection_products WHERE collection_id='col-gifts'")
	before, err := d.service.Catalog(d.ctx, domain.ProductFilter{Collection: "gifts-under-2000", Page: 1, Limit: 50})
	if err != nil || before.Total == 0 {
		t.Fatalf("setup %v", err)
	}
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/collections/col-gifts/archive", `{"expectedRevision":1}`); ex.Code != 200 || ex.body["productCount"].(float64) != float64(members) {
		t.Fatalf("archive: %d %s", ex.Code, ex.Body.String())
	}
	collections, _ := d.service.Collections(d.ctx)
	after, _ := d.service.Catalog(d.ctx, domain.ProductFilter{Collection: "gifts-under-2000", Page: 1, Limit: 50})
	if containsID(collections, "col-gifts") || after.Total != 0 || d.number(t, "SELECT count(*) FROM collection_products WHERE collection_id='col-gifts'") != members {
		t.Fatal("archived collection still listed, or memberships lost")
	}
	// A member keeps the archived collection on an unrelated edit; adding it anew is refused.
	p04 := d.adminProduct(t, "p04")
	edit := inputOf(p04)
	edit.Stock++
	if ex := adminRequest(handler, login, "PUT", "/api/admin/v1/products/p04", productJSONOf(t, edit, p04.Revision)); ex.Code != 200 {
		t.Fatalf("member edit: %d %s", ex.Code, ex.Body.String())
	}
	p01 := d.adminProduct(t, "p01")
	add := inputOf(p01)
	add.CollectionIDs = append(add.CollectionIDs, "col-gifts")
	if ex := adminRequest(handler, login, "PUT", "/api/admin/v1/products/p01", productJSONOf(t, add, p01.Revision)); ex.Code != 422 || !strings.Contains(ex.Body.String(), "Подборка в архиве") {
		t.Fatalf("archived collection added: %d %s", ex.Code, ex.Body.String())
	}
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/collections/col-gifts/restore", `{"expectedRevision":2}`); ex.Code != 200 {
		t.Fatalf("restore: %d", ex.Code)
	}
	if restored, _ := d.service.Catalog(d.ctx, domain.ProductFilter{Collection: "gifts-under-2000", Page: 1, Limit: 50}); restored.Total != before.Total {
		t.Fatal("restored collection lost products")
	}
}

func TestProductWritesUseOnlyActiveDictionaries(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	staff := d.auditStaff(t, "a@example.com")
	admin := application.NewDictionaryAdmin(d.store)
	shelf := d.newCategory(t, staff, "shelf", "")
	if _, err := admin.SetCategoryArchived(d.ctx, mutationAs(t, staff, application.CategoryArchive), shelf.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	m, _ := admin.CreateManufacturer(d.ctx, mutationAs(t, staff, application.ManufacturerCreate), domain.ManufacturerInput{Slug: "idle", Name: "Idle"})
	b, _ := admin.CreateBrand(d.ctx, mutationAs(t, staff, application.BrandCreate), domain.BrandInput{Slug: "idle-brand", Name: "Idle brand", ManufacturerID: m.ID})
	if _, err := admin.SetBrandArchived(d.ctx, mutationAs(t, staff, application.BrandArchive), b.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	p01 := d.adminProduct(t, "p01")
	for field, change := range map[string]func(*domain.ProductInput){
		"categoryId": func(in *domain.ProductInput) { in.CategoryID = shelf.ID },
		"brandId":    func(in *domain.ProductInput) { in.ManufacturerID, in.BrandID = m.ID, b.ID },
	} {
		edit := inputOf(p01)
		change(&edit)
		ex := adminRequest(handler, login, "PUT", "/api/admin/v1/products/p01", productJSONOf(t, edit, p01.Revision))
		if ex.Code != 422 || !strings.Contains(ex.Body.String(), `"field":"`+field+`"`) || !strings.Contains(ex.Body.String(), "в архиве") {
			t.Errorf("%s: %d %s", field, ex.Code, ex.Body.String())
		}
	}
	if ex := adminRequest(handler, login, "POST", "/api/admin/v1/products", strings.Replace(productJSONOf(t, func() domain.ProductInput {
		in := inputOf(p01)
		in.Slug, in.SKU, in.CategoryID = "new-shelf-bar", "SHELF-1", shelf.ID
		return in
	}(), 1), `"expectedRevision":1,`, "", 1)); ex.Code != 422 || !strings.Contains(ex.Body.String(), "Категория в архиве") {
		t.Fatalf("create in archived category: %d %s", ex.Code, ex.Body.String())
	}
	// A reference the product already has stays usable even if it was archived
	// meanwhile (rows archived directly, as an older binary or operator might).
	d.exec(t, `UPDATE categories SET archived_at=now() WHERE id=$1`, p01.Category.ID)
	edit := inputOf(p01)
	edit.Stock++
	if ex := adminRequest(handler, login, "PUT", "/api/admin/v1/products/p01", productJSONOf(t, edit, p01.Revision)); ex.Code != 200 {
		t.Fatalf("unchanged archived reference: %d %s", ex.Code, ex.Body.String())
	}
	// The product form sees which entries are archived.
	ex := adminRequest(handler, login, "GET", "/api/admin/v1/catalog-options", "")
	var options domain.CatalogOptions
	if err := json.Unmarshal(ex.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	archived := map[string]bool{}
	for _, c := range options.Categories {
		archived[c.ID] = c.Archived
	}
	for _, br := range options.Brands {
		archived[br.ID] = br.Archived
	}
	if !archived[shelf.ID] || !archived[p01.Category.ID] || !archived[b.ID] || archived["cat-gifts"] || archived[p01.Brand.ID] {
		t.Fatalf("catalog options archived flags: %v", archived)
	}
}

// Archive versus a product write that assigns the row: whichever commits
// first decides, and both never succeed.
func TestArchiveRacingAProductAssignment(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	admin := application.NewDictionaryAdmin(d.store)
	products := application.NewProductAdmin(d.store)
	seen := map[string]bool{}
	for round := 0; round < 8; round++ {
		c := d.newCategory(t, staff, fmt.Sprintf("race-%d", round), "")
		p := d.adminProduct(t, "p01")
		edit := inputOf(p)
		edit.CategoryID = c.ID
		archive, update := mutationAs(t, staff, application.CategoryArchive), mutationAs(t, staff, application.ProductUpdate)
		var archiveErr, updateErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, archiveErr = admin.SetCategoryArchived(d.ctx, archive, c.ID, 1, true)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, updateErr = products.Update(d.ctx, update, "p01", p.Revision, edit)
		}()
		close(start)
		wg.Wait()
		outcome := codeOf(archiveErr) + "," + codeOf(updateErr)
		if outcome != "ok,validation" && outcome != "reference,ok" {
			t.Fatalf("round %d: archive %v, update %v", round, archiveErr, updateErr)
		}
		seen[outcome] = true
		if d.number(t, `SELECT count(*) FROM products p JOIN categories c ON c.id=p.category_id WHERE c.archived_at IS NOT NULL AND c.id=$1`, c.ID) != 0 {
			t.Fatalf("round %d: a product sits in an archived category", round)
		}
		// Move p01 back so the next round's category is free again.
		if updateErr == nil {
			back := inputOf(d.adminProduct(t, "p01"))
			back.CategoryID = p.Category.ID
			if _, err := products.Update(d.ctx, mutationAs(t, staff, application.ProductUpdate), "p01", p.Revision+1, back); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("outcomes seen: %v", seen)
}

// A brand moving to another manufacturer versus a product adopting it: the
// product's brand/manufacturer pair is always consistent at commit.
func TestBrandMoveRacingAProductAssignment(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	admin := application.NewDictionaryAdmin(d.store)
	products := application.NewProductAdmin(d.store)
	for round := 0; round < 8; round++ {
		b, err := admin.CreateBrand(d.ctx, mutationAs(t, staff, application.BrandCreate),
			domain.BrandInput{Slug: fmt.Sprintf("roaming-%d", round), Name: "Roaming", ManufacturerID: "m-choco"})
		if err != nil {
			t.Fatal(err)
		}
		p := d.adminProduct(t, "p01")
		edit := inputOf(p)
		edit.ManufacturerID, edit.BrandID = "m-choco", b.ID
		move := brandInput(b)
		move.ManufacturerID = "m-lakrids"
		moveMutation, update := mutationAs(t, staff, application.BrandUpdate), mutationAs(t, staff, application.ProductUpdate)
		var moveErr, updateErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, moveErr = admin.UpdateBrand(d.ctx, moveMutation, b.ID, 1, move) }()
		go func() {
			defer wg.Done()
			<-start
			_, updateErr = products.Update(d.ctx, update, "p01", p.Revision, edit)
		}()
		close(start)
		wg.Wait()
		outcome := codeOf(moveErr) + "," + codeOf(updateErr)
		if outcome != "ok,validation" && outcome != "reference,ok" {
			t.Fatalf("round %d: move %v, update %v", round, moveErr, updateErr)
		}
		if mismatched := d.number(t, `SELECT count(*) FROM products p JOIN brands b ON b.id=p.brand_id WHERE b.manufacturer_id<>p.manufacturer_id`); mismatched != 0 {
			t.Fatalf("round %d: %d products disagree with their brand", round, mismatched)
		}
	}
}

func TestDictionaryChangesLeaveOrderSnapshotsAlone(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "a@example.com")
	order := d.placeOrder(t, "usr-demo", line{"p01", 1})
	var name string
	if err := d.pool.QueryRow(d.ctx, `SELECT manufacturer FROM order_items WHERE order_id=$1`, order.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	p01 := d.adminProduct(t, "p01")
	m, _ := d.store.AdminManufacturer(d.ctx, p01.Manufacturer.ID)
	if _, err := application.NewDictionaryAdmin(d.store).UpdateManufacturer(d.ctx, mutationAs(t, staff, application.ManufacturerUpdate), m.ID, 1,
		domain.ManufacturerInput{Slug: m.Slug, Name: "Renamed", Country: m.Country, Description: m.Description, SortOrder: m.SortOrder}); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := d.pool.QueryRow(d.ctx, `SELECT manufacturer FROM order_items WHERE order_id=$1`, order.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != name || after == "Renamed" {
		t.Fatalf("order line manufacturer %q -> %q", name, after)
	}
}

// The B5 image (migrations 001–015) accepts migration 016; its dictionary
// inserts, which do not know the new columns, still work.
func TestPreviousBinaryToleratesDictionarySchema(t *testing.T) {
	d := newDatabase(t, true)
	previous := migrator(migrationDir(t, []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_", "010_", "011_", "012_", "013_", "014_", "015_"}, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("B5 schema check on the B6 schema: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("B5 migrate on the B6 schema: %v", err)
	}
	d.exec(t, `INSERT INTO categories(id,slug,name) VALUES ('cat-b5','b5','B5')`)
	d.exec(t, `INSERT INTO manufacturers(id,slug,name) VALUES ('m-b5','mb5','MB5')`)
	d.exec(t, `INSERT INTO brands(id,slug,name,manufacturer_id) VALUES ('b-b5','bb5','BB5','m-b5')`)
	d.exec(t, `INSERT INTO collections(id,slug,name) VALUES ('col-b5','cb5','CB5')`)
	for _, table := range []string{"categories", "manufacturers", "brands", "collections"} {
		if d.number(t, "SELECT count(*) FROM "+table+" WHERE archived_at IS NOT NULL OR revision<>1") != 0 {
			t.Fatalf("%s: rows not active at revision 1", table)
		}
	}
	if _, err := d.store.AdminCategory(d.ctx, "cat-b5"); err != nil {
		t.Fatal(err)
	}
}
