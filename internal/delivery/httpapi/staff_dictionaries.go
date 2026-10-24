package httpapi

import (
	"context"
	"net/http"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// Staff catalog dictionary management (ADR 018), behind staffAuth like the
// product routes. Each dictionary has the same six routes; there is no
// DELETE, and archive and restore are the only way to withdraw a row.

// WithDictionaryAdmin enables the staff dictionary routes; they exist only
// when the staff API does.
func WithDictionaryAdmin(dictionaries *application.DictionaryAdmin) Option {
	return func(s *Server) { s.dictionaries = dictionaries }
}

// dictionaryBody is one dictionary's request document. Bodies carry
// expectedRevision; create does not list it, so decodeComplete rejects it there.
type dictionaryBody[I any] interface {
	input() I
	revision() int64
}

// dictionaryHandlers serves one dictionary; param is its path wildcard.
type dictionaryHandlers[T, I any, B dictionaryBody[I]] struct {
	s           *Server
	param       string
	fields      []string
	nullable    []string
	create      domain.AuditAction
	update      domain.AuditAction
	archive     domain.AuditAction
	restore     domain.AuditAction
	list        func(context.Context, domain.DictionaryStatus) ([]T, error)
	get         func(context.Context, string) (T, error)
	add         func(context.Context, domain.StaffMutation, I) (T, error)
	change      func(context.Context, domain.StaffMutation, string, int64, I) (T, error)
	setArchived func(context.Context, domain.StaffMutation, string, int64, bool) (T, error)
}

func (d dictionaryHandlers[T, I, B]) listItems(w http.ResponseWriter, r *http.Request) {
	if err := d.s.authorizeStaffRead(r, application.DictionaryRead); err != nil {
		d.s.writeError(w, r, err)
		return
	}
	status, err := application.ParseDictionaryStatus(r.URL.Query().Get("status"))
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	items, err := d.list(r.Context(), status)
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d dictionaryHandlers[T, I, B]) read(w http.ResponseWriter, r *http.Request) {
	if err := d.s.authorizeStaffRead(r, application.DictionaryRead); err != nil {
		d.s.writeError(w, r, err)
		return
	}
	result, err := d.get(r.Context(), r.PathValue(d.param))
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (d dictionaryHandlers[T, I, B]) createItem(w http.ResponseWriter, r *http.Request) {
	mutation, err := d.s.staffMutation(r, d.create)
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	var body B
	if err := decodeCompleteNullable(w, r, &body, d.fields, d.nullable); err != nil {
		d.s.writeError(w, r, err)
		return
	}
	result, err := d.add(r.Context(), mutation, body.input())
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (d dictionaryHandlers[T, I, B]) updateItem(w http.ResponseWriter, r *http.Request) {
	mutation, err := d.s.staffMutation(r, d.update)
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	var body B
	if err := decodeCompleteNullable(w, r, &body, append([]string{"expectedRevision"}, d.fields...), d.nullable); err != nil {
		d.s.writeError(w, r, err)
		return
	}
	result, err := d.change(r.Context(), mutation, r.PathValue(d.param), body.revision(), body.input())
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (d dictionaryHandlers[T, I, B]) archiveItem(w http.ResponseWriter, r *http.Request) {
	d.setArchivedItem(w, r, d.archive, true)
}

func (d dictionaryHandlers[T, I, B]) restoreItem(w http.ResponseWriter, r *http.Request) {
	d.setArchivedItem(w, r, d.restore, false)
}

func (d dictionaryHandlers[T, I, B]) setArchivedItem(w http.ResponseWriter, r *http.Request, action domain.AuditAction, archived bool) {
	mutation, err := d.s.staffMutation(r, action)
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	var body struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decodeComplete(w, r, &body, []string{"expectedRevision"}); err != nil {
		d.s.writeError(w, r, err)
		return
	}
	result, err := d.setArchived(r.Context(), mutation, r.PathValue(d.param), body.ExpectedRevision, archived)
	if err != nil {
		d.s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// registerStaffDictionaryRoutes spells every route out, so the OpenAPI
// inventory (tests/contract) sees each one.
func (s *Server) registerStaffDictionaryRoutes(mux *http.ServeMux) {
	a := s.dictionaries
	categories := dictionaryHandlers[domain.AdminCategory, domain.CategoryInput, categoryBody]{
		s: s, param: "category", fields: []string{"slug", "name", "description", "parentId", "icon", "sortOrder"},
		create: application.CategoryCreate, update: application.CategoryUpdate, archive: application.CategoryArchive, restore: application.CategoryRestore,
		list: a.Categories, get: a.Category, add: a.CreateCategory, change: a.UpdateCategory, setArchived: a.SetCategoryArchived,
	}
	manufacturers := dictionaryHandlers[domain.AdminManufacturer, domain.ManufacturerInput, manufacturerBody]{
		s: s, param: "manufacturer", fields: []string{"slug", "name", "country", "description", "sortOrder"},
		create: application.ManufacturerCreate, update: application.ManufacturerUpdate, archive: application.ManufacturerArchive, restore: application.ManufacturerRestore,
		list: a.Manufacturers, get: a.Manufacturer, add: a.CreateManufacturer, change: a.UpdateManufacturer, setArchived: a.SetManufacturerArchived,
	}
	brands := dictionaryHandlers[domain.AdminBrand, domain.BrandInput, brandBody]{
		s: s, param: "brand", fields: []string{"slug", "name", "manufacturerId", "description", "sortOrder"},
		create: application.BrandCreate, update: application.BrandUpdate, archive: application.BrandArchive, restore: application.BrandRestore,
		list: a.Brands, get: a.Brand, add: a.CreateBrand, change: a.UpdateBrand, setArchived: a.SetBrandArchived,
	}
	collections := dictionaryHandlers[domain.AdminCollection, domain.CollectionInput, collectionBody]{
		s: s, param: "collection", fields: []string{"slug", "name", "description", "accent", "startsAt", "endsAt", "sortOrder"},
		nullable: []string{"startsAt", "endsAt"},
		create:   application.CollectionCreate, update: application.CollectionUpdate, archive: application.CollectionArchive, restore: application.CollectionRestore,
		list: a.Collections, get: a.Collection, add: a.CreateCollection, change: a.UpdateCollection, setArchived: a.SetCollectionArchived,
	}
	auth := func(handler http.HandlerFunc) http.Handler { return s.staffAuth(handler) }
	mux.Handle("GET /api/admin/v1/categories", auth(categories.listItems))
	mux.Handle("POST /api/admin/v1/categories", auth(categories.createItem))
	mux.Handle("GET /api/admin/v1/categories/{category}", auth(categories.read))
	mux.Handle("PUT /api/admin/v1/categories/{category}", auth(categories.updateItem))
	mux.Handle("POST /api/admin/v1/categories/{category}/archive", auth(categories.archiveItem))
	mux.Handle("POST /api/admin/v1/categories/{category}/restore", auth(categories.restoreItem))
	mux.Handle("GET /api/admin/v1/manufacturers", auth(manufacturers.listItems))
	mux.Handle("POST /api/admin/v1/manufacturers", auth(manufacturers.createItem))
	mux.Handle("GET /api/admin/v1/manufacturers/{manufacturer}", auth(manufacturers.read))
	mux.Handle("PUT /api/admin/v1/manufacturers/{manufacturer}", auth(manufacturers.updateItem))
	mux.Handle("POST /api/admin/v1/manufacturers/{manufacturer}/archive", auth(manufacturers.archiveItem))
	mux.Handle("POST /api/admin/v1/manufacturers/{manufacturer}/restore", auth(manufacturers.restoreItem))
	mux.Handle("GET /api/admin/v1/brands", auth(brands.listItems))
	mux.Handle("POST /api/admin/v1/brands", auth(brands.createItem))
	mux.Handle("GET /api/admin/v1/brands/{brand}", auth(brands.read))
	mux.Handle("PUT /api/admin/v1/brands/{brand}", auth(brands.updateItem))
	mux.Handle("POST /api/admin/v1/brands/{brand}/archive", auth(brands.archiveItem))
	mux.Handle("POST /api/admin/v1/brands/{brand}/restore", auth(brands.restoreItem))
	mux.Handle("GET /api/admin/v1/collections", auth(collections.listItems))
	mux.Handle("POST /api/admin/v1/collections", auth(collections.createItem))
	mux.Handle("GET /api/admin/v1/collections/{collection}", auth(collections.read))
	mux.Handle("PUT /api/admin/v1/collections/{collection}", auth(collections.updateItem))
	mux.Handle("POST /api/admin/v1/collections/{collection}/archive", auth(collections.archiveItem))
	mux.Handle("POST /api/admin/v1/collections/{collection}/restore", auth(collections.restoreItem))
}

// The dictionary documents: every member is required (parentId is "" for a
// top-level category; a collection's dates are null when open-ended), so an
// omitted field never silently clears a value.

type categoryBody struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	ParentID         string `json:"parentId"`
	Icon             string `json:"icon"`
	SortOrder        int    `json:"sortOrder"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func (b categoryBody) revision() int64 { return b.ExpectedRevision }
func (b categoryBody) input() domain.CategoryInput {
	return domain.CategoryInput{Slug: b.Slug, Name: b.Name, Description: b.Description, ParentID: b.ParentID, Icon: b.Icon, SortOrder: b.SortOrder}
}

type manufacturerBody struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Country          string `json:"country"`
	Description      string `json:"description"`
	SortOrder        int    `json:"sortOrder"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func (b manufacturerBody) revision() int64 { return b.ExpectedRevision }
func (b manufacturerBody) input() domain.ManufacturerInput {
	return domain.ManufacturerInput{Slug: b.Slug, Name: b.Name, Country: b.Country, Description: b.Description, SortOrder: b.SortOrder}
}

type brandBody struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	ManufacturerID   string `json:"manufacturerId"`
	Description      string `json:"description"`
	SortOrder        int    `json:"sortOrder"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func (b brandBody) revision() int64 { return b.ExpectedRevision }
func (b brandBody) input() domain.BrandInput {
	return domain.BrandInput{Slug: b.Slug, Name: b.Name, ManufacturerID: b.ManufacturerID, Description: b.Description, SortOrder: b.SortOrder}
}

type collectionBody struct {
	Slug             string     `json:"slug"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	Accent           string     `json:"accent"`
	StartsAt         *time.Time `json:"startsAt"`
	EndsAt           *time.Time `json:"endsAt"`
	SortOrder        int        `json:"sortOrder"`
	ExpectedRevision int64      `json:"expectedRevision"`
}

func (b collectionBody) revision() int64 { return b.ExpectedRevision }
func (b collectionBody) input() domain.CollectionInput {
	return domain.CollectionInput{Slug: b.Slug, Name: b.Name, Description: b.Description, Accent: b.Accent, StartsAt: b.StartsAt, EndsAt: b.EndsAt, SortOrder: b.SortOrder}
}
