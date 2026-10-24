package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// Staff product management (ADR 013). Every route is behind staffAuth (staff
// cookie, staff Origin and CSRF on unsafe methods, optional
// X-Expected-Staff-ID) and is authorized through StaffAuth; mutations take
// their actor from the session through staffMutation, never from the body.

// WithProductAdmin enables the staff product routes; they exist only when the
// staff API does.
func WithProductAdmin(products *application.ProductAdmin) Option {
	return func(s *Server) { s.products = products }
}

func (s *Server) registerStaffProductRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/admin/v1/products", s.staffAuth(http.HandlerFunc(s.staffListProducts)))
	mux.Handle("POST /api/admin/v1/products", s.staffAuth(http.HandlerFunc(s.staffCreateProduct)))
	mux.Handle("GET /api/admin/v1/products/{product}", s.staffAuth(http.HandlerFunc(s.staffProduct)))
	mux.Handle("PUT /api/admin/v1/products/{product}", s.staffAuth(http.HandlerFunc(s.staffUpdateProduct)))
	mux.Handle("POST /api/admin/v1/products/{product}/archive", s.staffAuth(http.HandlerFunc(s.staffArchiveProduct)))
	mux.Handle("POST /api/admin/v1/products/{product}/restore", s.staffAuth(http.HandlerFunc(s.staffRestoreProduct)))
	mux.Handle("GET /api/admin/v1/catalog-options", s.staffAuth(http.HandlerFunc(s.staffCatalogOptions)))
}

// authorizeStaffRead is the read counterpart of staffMutation.
func (s *Server) authorizeStaffRead(r *http.Request, action string) error {
	staff, ok := currentStaff(r)
	if !ok {
		return domain.ErrUnauthorized
	}
	return s.staff.Authorize(staff, action)
}

func (s *Server) staffListProducts(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeStaffRead(r, application.ProductRead); err != nil {
		s.writeError(w, r, err)
		return
	}
	query := r.URL.Query()
	filter := domain.AdminProductFilter{Status: domain.AdminProductStatus(query.Get("status")), Query: query.Get("q")}
	fields := []domain.FieldError{}
	for _, param := range []struct {
		name   string
		target *int
	}{{"page", &filter.Page}, {"limit", &filter.Limit}} {
		if raw := query.Get(param.name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 {
				fields = append(fields, domain.FieldError{Field: param.name, Message: "Ожидается целое число больше нуля"})
				continue
			}
			*param.target = value
		}
	}
	if len(fields) > 0 {
		s.writeError(w, r, &domain.ValidationError{Fields: fields})
		return
	}
	page, err := s.products.List(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// staffCatalogOptions serves the dictionaries of the product form. Reading
// them is part of reading products, so it needs the same authorization.
func (s *Server) staffCatalogOptions(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeStaffRead(r, application.ProductRead); err != nil {
		s.writeError(w, r, err)
		return
	}
	options, err := s.products.CatalogOptions(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) staffProduct(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeStaffRead(r, application.ProductRead); err != nil {
		s.writeError(w, r, err)
		return
	}
	product, err := s.products.Get(r.Context(), r.PathValue("product"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (s *Server) staffCreateProduct(w http.ResponseWriter, r *http.Request) {
	mutation, err := s.staffMutation(r, application.ProductCreate)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body productBody
	if err := decodeComplete(w, r, &body, productFields); err != nil {
		s.writeError(w, r, err)
		return
	}
	product, err := s.products.Create(r.Context(), mutation, body.input())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, product)
}

func (s *Server) staffUpdateProduct(w http.ResponseWriter, r *http.Request) {
	mutation, err := s.staffMutation(r, application.ProductUpdate)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		productBody
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decodeComplete(w, r, &body, append([]string{"expectedRevision"}, productFields...)); err != nil {
		s.writeError(w, r, err)
		return
	}
	product, err := s.products.Update(r.Context(), mutation, r.PathValue("product"), body.ExpectedRevision, body.input())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (s *Server) staffArchiveProduct(w http.ResponseWriter, r *http.Request) {
	s.staffSetProductArchived(w, r, application.ProductArchive, s.products.Archive)
}

func (s *Server) staffRestoreProduct(w http.ResponseWriter, r *http.Request) {
	s.staffSetProductArchived(w, r, application.ProductRestore, s.products.Restore)
}

func (s *Server) staffSetProductArchived(w http.ResponseWriter, r *http.Request, action domain.AuditAction,
	apply func(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64) (domain.AdminProduct, error)) {
	mutation, err := s.staffMutation(r, action)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decodeComplete(w, r, &body, []string{"expectedRevision"}); err != nil {
		s.writeError(w, r, err)
		return
	}
	product, err := apply(r.Context(), mutation, r.PathValue("product"), body.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

// productBody is the staff product document. Every member is required, so
// an omitted field can never silently reset stock, price or a rule to zero.
type productBody struct {
	Slug            string   `json:"slug"`
	SKU             string   `json:"sku"`
	Name            string   `json:"name"`
	ShortName       string   `json:"shortName"`
	Description     string   `json:"description"`
	PriceMinor      int64    `json:"priceMinor"`
	OldPriceMinor   int64    `json:"oldPriceMinor"`
	Unit            string   `json:"unit"`
	WeightGrams     int      `json:"weightGrams"`
	CategoryID      string   `json:"categoryId"`
	ManufacturerID  string   `json:"manufacturerId"`
	BrandID         string   `json:"brandId"`
	CollectionIDs   []string `json:"collectionIds"`
	Series          string   `json:"series"`
	Tags            []string `json:"tags"`
	Badges          []string `json:"badges"`
	Ingredients     string   `json:"ingredients"`
	Nutrition       string   `json:"nutrition"`
	ShelfLifeDays   int      `json:"shelfLifeDays"`
	Stock           int      `json:"stock"`
	MinimumQuantity int      `json:"minimumQuantity"`
	QuantityStep    int      `json:"quantityStep"`
	Featured        bool     `json:"featured"`
}

var productFields = []string{"slug", "sku", "name", "shortName", "description", "priceMinor", "oldPriceMinor", "unit",
	"weightGrams", "categoryId", "manufacturerId", "brandId", "collectionIds", "series", "tags", "badges", "ingredients",
	"nutrition", "shelfLifeDays", "stock", "minimumQuantity", "quantityStep", "featured"}

func (b productBody) input() domain.ProductInput {
	return domain.ProductInput{Slug: b.Slug, SKU: b.SKU, Name: b.Name, ShortName: b.ShortName, Description: b.Description,
		PriceMinor: b.PriceMinor, OldPriceMinor: b.OldPriceMinor, Unit: b.Unit, WeightGrams: b.WeightGrams,
		CategoryID: b.CategoryID, ManufacturerID: b.ManufacturerID, BrandID: b.BrandID, CollectionIDs: b.CollectionIDs,
		Series: b.Series, Tags: b.Tags, Badges: b.Badges, Ingredients: b.Ingredients, Nutrition: b.Nutrition,
		ShelfLifeDays: b.ShelfLifeDays, Stock: b.Stock, MinimumQuantity: b.MinimumQuantity, QuantityStep: b.QuantityStep,
		Featured: b.Featured}
}

// decodeComplete is decodeJSON for documents whose members are all required
// and non-null: one JSON object with exactly these members. Numbers that do
// not fit the target type, such as fractions or overflowing values, are
// validation errors.
func decodeComplete(w http.ResponseWriter, r *http.Request, target any, required []string) error {
	var raw map[string]json.RawMessage
	if err := decodeJSON(w, r, &raw); err != nil {
		return err
	}
	fields := []domain.FieldError{}
	allowed := make(map[string]bool, len(required))
	for _, name := range required {
		allowed[name] = true
		if value, ok := raw[name]; !ok || string(bytes.TrimSpace(value)) == "null" {
			fields = append(fields, domain.FieldError{Field: name, Message: "Обязательное поле"})
		}
	}
	unknown := []string{}
	for name := range raw {
		if !allowed[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	for _, name := range unknown {
		fields = append(fields, domain.FieldError{Field: name, Message: "Неизвестное поле"})
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "body", Message: "Некорректный JSON"}}}
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "body", Message: "Некорректный JSON: " + err.Error()}}}
	}
	return nil
}
