package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// testDictionaryStore records every mutation that reaches it.
type testDictionaryStore struct {
	mu        sync.Mutex
	err       error
	mutations []domain.StaffMutation
	ids       []string
	revisions []int64
	inputs    []any
}

func (s *testDictionaryStore) record(mutation domain.StaffMutation, id string, revision int64, input any) (domain.DictionaryState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return domain.DictionaryState{}, s.err
	}
	s.mutations, s.ids, s.revisions, s.inputs = append(s.mutations, mutation), append(s.ids, id), append(s.revisions, revision), append(s.inputs, input)
	return domain.DictionaryState{ID: id, Revision: revision + 1}, nil
}

func (s *testDictionaryStore) AdminCategories(context.Context, domain.DictionaryStatus) ([]domain.AdminCategory, error) {
	return []domain.AdminCategory{}, s.err
}
func (s *testDictionaryStore) AdminCategory(context.Context, string) (domain.AdminCategory, error) {
	return domain.AdminCategory{}, domain.ErrNotFound
}
func (s *testDictionaryStore) CreateCategory(_ context.Context, m domain.StaffMutation, id string, in domain.CategoryInput) (domain.AdminCategory, error) {
	state, err := s.record(m, id, 0, in)
	return domain.AdminCategory{DictionaryState: state}, err
}
func (s *testDictionaryStore) UpdateCategory(_ context.Context, m domain.StaffMutation, id string, rev int64, in domain.CategoryInput) (domain.AdminCategory, error) {
	state, err := s.record(m, id, rev, in)
	return domain.AdminCategory{DictionaryState: state}, err
}
func (s *testDictionaryStore) SetCategoryArchived(_ context.Context, m domain.StaffMutation, id string, rev int64, archived bool) (domain.AdminCategory, error) {
	state, err := s.record(m, id, rev, archived)
	return domain.AdminCategory{DictionaryState: state}, err
}
func (s *testDictionaryStore) AdminManufacturers(context.Context, domain.DictionaryStatus) ([]domain.AdminManufacturer, error) {
	return []domain.AdminManufacturer{}, s.err
}
func (s *testDictionaryStore) AdminManufacturer(context.Context, string) (domain.AdminManufacturer, error) {
	return domain.AdminManufacturer{}, domain.ErrNotFound
}
func (s *testDictionaryStore) CreateManufacturer(_ context.Context, m domain.StaffMutation, id string, in domain.ManufacturerInput) (domain.AdminManufacturer, error) {
	state, err := s.record(m, id, 0, in)
	return domain.AdminManufacturer{DictionaryState: state}, err
}
func (s *testDictionaryStore) UpdateManufacturer(_ context.Context, m domain.StaffMutation, id string, rev int64, in domain.ManufacturerInput) (domain.AdminManufacturer, error) {
	state, err := s.record(m, id, rev, in)
	return domain.AdminManufacturer{DictionaryState: state}, err
}
func (s *testDictionaryStore) SetManufacturerArchived(_ context.Context, m domain.StaffMutation, id string, rev int64, archived bool) (domain.AdminManufacturer, error) {
	state, err := s.record(m, id, rev, archived)
	return domain.AdminManufacturer{DictionaryState: state}, err
}
func (s *testDictionaryStore) AdminBrands(context.Context, domain.DictionaryStatus) ([]domain.AdminBrand, error) {
	return []domain.AdminBrand{}, s.err
}
func (s *testDictionaryStore) AdminBrand(context.Context, string) (domain.AdminBrand, error) {
	return domain.AdminBrand{}, domain.ErrNotFound
}
func (s *testDictionaryStore) CreateBrand(_ context.Context, m domain.StaffMutation, id string, in domain.BrandInput) (domain.AdminBrand, error) {
	state, err := s.record(m, id, 0, in)
	return domain.AdminBrand{DictionaryState: state}, err
}
func (s *testDictionaryStore) UpdateBrand(_ context.Context, m domain.StaffMutation, id string, rev int64, in domain.BrandInput) (domain.AdminBrand, error) {
	state, err := s.record(m, id, rev, in)
	return domain.AdminBrand{DictionaryState: state}, err
}
func (s *testDictionaryStore) SetBrandArchived(_ context.Context, m domain.StaffMutation, id string, rev int64, archived bool) (domain.AdminBrand, error) {
	state, err := s.record(m, id, rev, archived)
	return domain.AdminBrand{DictionaryState: state}, err
}
func (s *testDictionaryStore) AdminCollections(context.Context, domain.DictionaryStatus) ([]domain.AdminCollection, error) {
	return []domain.AdminCollection{}, s.err
}
func (s *testDictionaryStore) AdminCollection(context.Context, string) (domain.AdminCollection, error) {
	return domain.AdminCollection{}, domain.ErrNotFound
}
func (s *testDictionaryStore) CreateCollection(_ context.Context, m domain.StaffMutation, id string, in domain.CollectionInput) (domain.AdminCollection, error) {
	state, err := s.record(m, id, 0, in)
	return domain.AdminCollection{DictionaryState: state}, err
}
func (s *testDictionaryStore) UpdateCollection(_ context.Context, m domain.StaffMutation, id string, rev int64, in domain.CollectionInput) (domain.AdminCollection, error) {
	state, err := s.record(m, id, rev, in)
	return domain.AdminCollection{DictionaryState: state}, err
}
func (s *testDictionaryStore) SetCollectionArchived(_ context.Context, m domain.StaffMutation, id string, rev int64, archived bool) (domain.AdminCollection, error) {
	state, err := s.record(m, id, rev, archived)
	return domain.AdminCollection{DictionaryState: state}, err
}

func dictionaryHandler(t *testing.T, store *testDictionaryStore) (http.Handler, *testStaffStore) {
	t.Helper()
	staff := newTestStaffStore(t)
	s := newServer(customerService(), discard(), "", "memory", SessionConfig{Secure: true, TrustedOrigins: []string{shopOrigin}},
		WithStaff(application.NewStaffAuth(staff), StaffConfig{TrustedOrigins: []string{adminOrigin}}),
		WithDictionaryAdmin(application.NewDictionaryAdmin(store)))
	if !s.staffEnabled() {
		t.Fatal("staff not enabled")
	}
	mux := http.NewServeMux()
	s.registerStaffRoutes(mux)
	mux.HandleFunc("POST /api/auth/session", s.startSession)
	return s.middleware(mux), staff
}

// dictionaryDocuments are valid create bodies, keyed by route.
var dictionaryDocuments = map[string]string{
	"categories":    `{"slug":"jams","name":"Джемы","description":"","parentId":"","icon":"jar","sortOrder":5}`,
	"manufacturers": `{"slug":"north","name":"Север","country":"Россия","description":"","sortOrder":0}`,
	"brands":        `{"slug":"north-line","name":"Северная линия","manufacturerId":"mfr-1","description":"","sortOrder":0}`,
	"collections":   `{"slug":"winter","name":"Зима","description":"","accent":"sky","startsAt":null,"endsAt":"2027-03-01T00:00:00Z","sortOrder":0}`,
}

func withRevision(document string, revision string) string {
	return `{"expectedRevision":` + revision + `,` + strings.TrimPrefix(document, "{")
}

func TestDictionaryRoutesAcceptOnlyTheStaffSession(t *testing.T) {
	store := &testDictionaryStore{}
	handler, _ := dictionaryHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	customer := call(handler, "POST", "/api/auth/session", `{"email":"demo@sugar.ru","password":"Sweet2026!"}`, map[string]string{"Origin": shopOrigin})
	customerCookie := "__Host-sweetshop_session=" + cookieNamed(customer, "__Host-sweetshop_session").Value
	for path := range dictionaryDocuments {
		base := "/api/admin/v1/" + path
		for name, tc := range map[string]struct {
			method, path string
			headers      map[string]string
			status       int
		}{
			"anonymous list":       {"GET", base, nil, 401},
			"customer list":        {"GET", base, map[string]string{"Cookie": customerCookie}, 401},
			"customer create":      {"POST", base, map[string]string{"Cookie": customerCookie, "Origin": adminOrigin}, 401},
			"bearer":               {"GET", base, map[string]string{"Cookie": staffCookie, "Authorization": "Bearer x"}, 401},
			"create without CSRF":  {"POST", base, map[string]string{"Cookie": staffCookie, "Origin": adminOrigin}, 403},
			"archive from shop":    {"POST", base + "/x1/archive", map[string]string{"Cookie": staffCookie, "Origin": shopOrigin, "X-CSRF-Token": csrf}, 403},
			"other expected staff": {"POST", base + "/x1/restore", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, expectedStaffHeader: "stf_" + strings.Repeat("0", 32)}, 409},
			"no delete route":      {"DELETE", base + "/x1", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}, 405},
			"no PATCH route":       {"PATCH", base + "/x1", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}, 405},
			"staff list":           {"GET", base + "?status=all", map[string]string{"Cookie": staffCookie}, 200},
			"staff missing":        {"GET", base + "/x1", map[string]string{"Cookie": staffCookie}, 404},
		} {
			t.Run(path+" "+name, func(t *testing.T) {
				if rec := call(handler, tc.method, tc.path, dictionaryDocuments[path], tc.headers); rec.Code != tc.status {
					t.Fatalf("%d %s", rec.Code, rec.Body.String())
				}
			})
		}
	}
	if len(store.mutations) != 0 {
		t.Fatal("a rejected request reached the store")
	}
}

func TestDictionaryMutationsTakeTheActorFromTheSession(t *testing.T) {
	store := &testDictionaryStore{}
	handler, staff := dictionaryHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, "X-Request-ID": "req-7"}
	type step struct {
		method, path, body string
		status             int
		action             domain.AuditAction
		revision           int64
	}
	steps := []step{}
	for _, d := range []struct {
		path                             string
		create, update, archive, restore domain.AuditAction
	}{
		{"categories", application.CategoryCreate, application.CategoryUpdate, application.CategoryArchive, application.CategoryRestore},
		{"manufacturers", application.ManufacturerCreate, application.ManufacturerUpdate, application.ManufacturerArchive, application.ManufacturerRestore},
		{"brands", application.BrandCreate, application.BrandUpdate, application.BrandArchive, application.BrandRestore},
		{"collections", application.CollectionCreate, application.CollectionUpdate, application.CollectionArchive, application.CollectionRestore},
	} {
		base := "/api/admin/v1/" + d.path
		steps = append(steps,
			step{"POST", base, dictionaryDocuments[d.path], 201, d.create, 0},
			step{"PUT", base + "/x1", withRevision(dictionaryDocuments[d.path], "3"), 200, d.update, 3},
			step{"POST", base + "/x1/archive", `{"expectedRevision":4}`, 200, d.archive, 4},
			step{"POST", base + "/x1/restore", `{"expectedRevision":5}`, 200, d.restore, 5},
		)
	}
	for _, s := range steps {
		if rec := call(handler, s.method, s.path, s.body, headers); rec.Code != s.status {
			t.Fatalf("%s %s: %d %s", s.method, s.path, rec.Code, rec.Body.String())
		}
	}
	if len(store.mutations) != len(steps) {
		t.Fatalf("mutations = %d, want %d", len(store.mutations), len(steps))
	}
	for i, s := range steps {
		m := store.mutations[i]
		if m.Actor() != staff.credential.Staff.ID || m.Action() != s.action || m.RequestID() != "req-7" || store.revisions[i] != s.revision {
			t.Fatalf("step %d: %s %s %s rev %d", i, m.Actor(), m.Action(), m.RequestID(), store.revisions[i])
		}
	}
	// Server-generated IDs carry the dictionary's prefix; path IDs pass through.
	for i, prefix := range []string{"cat_", "mfr_", "brd_", "col_"} {
		if id := store.ids[i*4]; !strings.HasPrefix(id, prefix) || len(id) != len(prefix)+32 {
			t.Fatalf("created id %q", id)
		}
		if store.ids[i*4+1] != "x1" {
			t.Fatalf("updated id %q", store.ids[i*4+1])
		}
	}
	if archive, restore := store.inputs[2], store.inputs[3]; archive != true || restore != false {
		t.Fatalf("archive flags %v %v", archive, restore)
	}
	collection := store.inputs[12].(domain.CollectionInput)
	if collection.StartsAt != nil || collection.EndsAt == nil || collection.Accent != "sky" {
		t.Fatalf("collection input %+v", collection)
	}
}

func TestDictionaryBodiesAndErrors(t *testing.T) {
	store := &testDictionaryStore{}
	handler, _ := dictionaryHandler(t, store)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}
	for name, tc := range map[string]struct{ method, path, body, fields string }{
		"missing member":          {"POST", "/api/admin/v1/manufacturers", `{"slug":"a","name":"A","description":"","sortOrder":0}`, "country"},
		"null name":               {"POST", "/api/admin/v1/manufacturers", `{"slug":"a","name":null,"country":"","description":"","sortOrder":0}`, "name"},
		"read-only logo":          {"POST", "/api/admin/v1/brands", `{"slug":"a","name":"A","manufacturerId":"m","description":"","sortOrder":0,"logo":"x.png"}`, "logo"},
		"client id":               {"POST", "/api/admin/v1/categories", `{"id":"cat-x","slug":"a","name":"A","description":"","parentId":"","icon":"","sortOrder":0}`, "id"},
		"revision on create":      {"POST", "/api/admin/v1/categories", withRevision(dictionaryDocuments["categories"], "1"), "expectedRevision"},
		"actor in body":           {"POST", "/api/admin/v1/collections/x1/archive", `{"expectedRevision":1,"staffUserId":"stf_x"}`, "staffUserId"},
		"missing revision":        {"PUT", "/api/admin/v1/categories/x1", dictionaryDocuments["categories"], "expectedRevision"},
		"zero revision":           {"POST", "/api/admin/v1/brands/x1/archive", `{"expectedRevision":0}`, "expectedRevision"},
		"fractional sort":         {"POST", "/api/admin/v1/manufacturers", `{"slug":"a","name":"A","country":"","description":"","sortOrder":1.5}`, "body"},
		"collection dates absent": {"POST", "/api/admin/v1/collections", `{"slug":"a","name":"A","description":"","accent":"sky","sortOrder":0}`, "startsAt,endsAt"},
		"bad slug":                {"POST", "/api/admin/v1/categories", `{"slug":"Не slug","name":"A","description":"","parentId":"","icon":"","sortOrder":0}`, "slug"},
		"unknown accent":          {"POST", "/api/admin/v1/collections", `{"slug":"a","name":"A","description":"","accent":"neon","startsAt":null,"endsAt":null,"sortOrder":0}`, "accent"},
		"dates reversed":          {"POST", "/api/admin/v1/collections", `{"slug":"a","name":"A","description":"","accent":"sky","startsAt":"2027-02-01T00:00:00Z","endsAt":"2027-01-01T00:00:00Z","sortOrder":0}`, "endsAt"},
		"brand without maker":     {"POST", "/api/admin/v1/brands", `{"slug":"a","name":"A","manufacturerId":"","description":"","sortOrder":0}`, "manufacturerId"},
		"negative sort":           {"POST", "/api/admin/v1/manufacturers", `{"slug":"a","name":"A","country":"","description":"","sortOrder":-1}`, "sortOrder"},
		"bad status":              {"GET", "/api/admin/v1/brands?status=deleted", ``, "status"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := call(handler, tc.method, tc.path, tc.body, headers)
			code, fields := errorBody(t, rec.Body.String())
			if rec.Code != 422 || code != "validation_error" || strings.Join(fields, ",") != tc.fields {
				t.Fatalf("%d %s %v", rec.Code, code, fields)
			}
		})
	}
	if rec := call(handler, "PUT", "/api/admin/v1/categories/x1", withRevision(strings.Replace(dictionaryDocuments["categories"], `"parentId":""`, `"parentId":"x1"`, 1), "1"), headers); rec.Code != 422 {
		t.Fatalf("self parent: %d %s", rec.Code, rec.Body.String())
	} else if code, _ := errorBody(t, rec.Body.String()); code != "invalid_hierarchy" {
		t.Fatalf("self parent code %s", code)
	}
	if len(store.mutations) != 0 {
		t.Fatal("an invalid request reached the store")
	}
	for err, want := range map[error]struct {
		status int
		code   string
	}{
		domain.ErrStaleRevision:    {409, "stale_revision"},
		domain.ErrNotFound:         {404, "not_found"},
		domain.ErrConflict:         {409, "conflict"},
		domain.ErrInvalidHierarchy: {422, "invalid_hierarchy"},
		&domain.ReferenceConflictError{Products: 2, Categories: 1}: {409, "reference_conflict"},
	} {
		store.err = err
		rec := call(handler, "POST", "/api/admin/v1/categories/x1/archive", `{"expectedRevision":1}`, headers)
		if code, _ := errorBody(t, rec.Body.String()); rec.Code != want.status || code != want.code {
			t.Errorf("%v: %d %s", err, rec.Code, code)
		}
		if want.code == "reference_conflict" {
			var body struct {
				Error struct {
					Message    string
					References map[string]int
				}
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body.Error.References["products"] != 2 || body.Error.References["categories"] != 1 || body.Error.References["brands"] != 0 ||
				body.Error.Message != "Запись используется — товаров: 2, активных подкатегорий: 1" {
				t.Errorf("reference conflict body %s", rec.Body.String())
			}
		} else if strings.Contains(rec.Body.String(), "references") {
			t.Errorf("%v carries references: %s", err, rec.Body.String())
		}
	}
}
