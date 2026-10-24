package httpapi

import (
	"net/http"
	"strconv"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// Staff order management (ADR 016), behind staffAuth like the product routes.
// There is no route that deletes an order or sets an arbitrary status.

// WithOrderAdmin enables the staff order routes; they exist only when the
// staff API does.
func WithOrderAdmin(orders *application.OrderAdmin) Option {
	return func(s *Server) { s.orderAdmin = orders }
}

func (s *Server) registerStaffOrderRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/admin/v1/orders", s.staffAuth(http.HandlerFunc(s.staffListOrders)))
	mux.Handle("GET /api/admin/v1/orders/{order}", s.staffAuth(http.HandlerFunc(s.staffOrder)))
	mux.Handle("POST /api/admin/v1/orders/{order}/status", s.staffAuth(http.HandlerFunc(s.staffChangeOrderStatus)))
	mux.Handle("POST /api/admin/v1/orders/{order}/cancel", s.staffAuth(http.HandlerFunc(s.staffCancelOrder)))
}

func (s *Server) staffListOrders(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeStaffRead(r, application.OrderRead); err != nil {
		s.writeError(w, r, err)
		return
	}
	query := r.URL.Query()
	filter := domain.AdminOrderFilter{Status: domain.OrderStatus(query.Get("status")), Query: query.Get("q")}
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
	page, err := s.orderAdmin.List(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) staffOrder(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeStaffRead(r, application.OrderRead); err != nil {
		s.writeError(w, r, err)
		return
	}
	order, err := s.orderAdmin.Get(r.Context(), r.PathValue("order"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (s *Server) staffChangeOrderStatus(w http.ResponseWriter, r *http.Request) {
	mutation, err := s.staffMutation(r, application.OrderStatusChange)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		Status           domain.OrderStatus `json:"status"`
		ExpectedRevision int64              `json:"expectedRevision"`
	}
	if err := decodeComplete(w, r, &body, []string{"status", "expectedRevision"}); err != nil {
		s.writeError(w, r, err)
		return
	}
	order, err := s.orderAdmin.ChangeStatus(r.Context(), mutation, r.PathValue("order"), body.ExpectedRevision, body.Status)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (s *Server) staffCancelOrder(w http.ResponseWriter, r *http.Request) {
	mutation, err := s.staffMutation(r, application.OrderCancel)
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
	order, err := s.orderAdmin.Cancel(r.Context(), mutation, r.PathValue("order"), body.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}
