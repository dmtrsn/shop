package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

type contextKey string

const (
	requestIDKey   contextKey = "request-id"
	userKey        contextKey = "user"
	requestInfoKey contextKey = "request-info"
)

type Server struct {
	service        *application.Service
	logger         *slog.Logger
	staticDir      string
	storage        string
	cookieName     string
	secureCookie   bool
	trustedOrigins map[string]bool

	// Staff identity is separate; nil staff means no staff routes exist.
	staff           *application.StaffAuth
	staffOrigins    map[string]bool
	staffCookieName string

	// Self-service registration and recovery; nil means their routes do not
	// exist.
	registrations *application.CustomerRegistrations
	resets        *application.PasswordResets
}

// Option configures optional parts of the server.
type Option func(*Server)

func New(service *application.Service, logger *slog.Logger, staticDir, storage string, sessions SessionConfig, options ...Option) http.Handler {
	s := newServer(service, logger, staticDir, storage, sessions, options...)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/session", s.startSession)
	mux.HandleFunc("GET /api/auth/session", s.sessionState)
	mux.HandleFunc("DELETE /api/auth/session", s.endSession)
	mux.Handle("GET /api/me", s.auth(http.HandlerFunc(s.me)))
	mux.HandleFunc("GET /api/categories", s.categories)
	mux.HandleFunc("GET /api/manufacturers", s.manufacturers)
	mux.HandleFunc("GET /api/brands", s.brands)
	mux.HandleFunc("GET /api/collections", s.collections)
	mux.HandleFunc("GET /api/catalog", s.catalog)
	mux.HandleFunc("GET /api/catalog/facets", s.facets)
	mux.HandleFunc("GET /api/products/{product}", s.product)
	mux.HandleFunc("GET /api/search/suggest", s.suggestions)
	mux.Handle("GET /api/cart", s.auth(http.HandlerFunc(s.cart)))
	mux.Handle("POST /api/cart/items", s.auth(http.HandlerFunc(s.addCartItem)))
	mux.Handle("PATCH /api/cart/items/{product}", s.auth(http.HandlerFunc(s.updateCartItem)))
	mux.Handle("DELETE /api/cart/items/{product}", s.auth(http.HandlerFunc(s.removeCartItem)))
	mux.Handle("DELETE /api/cart", s.auth(http.HandlerFunc(s.clearCart)))
	mux.Handle("PATCH /api/cart/selection", s.auth(http.HandlerFunc(s.selectCartItems)))
	mux.Handle("GET /api/favorites", s.auth(http.HandlerFunc(s.favorites)))
	mux.Handle("PUT /api/favorites/{product}", s.auth(http.HandlerFunc(s.enableFavorite)))
	mux.Handle("DELETE /api/favorites/{product}", s.auth(http.HandlerFunc(s.disableFavorite)))
	mux.Handle("GET /api/orders", s.auth(http.HandlerFunc(s.orders)))
	mux.Handle("POST /api/orders", s.auth(http.HandlerFunc(s.checkout)))
	mux.Handle("GET /api/orders/{order}", s.auth(http.HandlerFunc(s.order)))
	mux.Handle("POST /api/orders/{order}/repeat/preview", s.auth(http.HandlerFunc(s.repeatPreview)))
	mux.Handle("POST /api/orders/{order}/repeat/commit", s.auth(http.HandlerFunc(s.repeatCommit)))
	mux.Handle("POST /api/v2/orders/{order}/repeat/preview", s.auth(http.HandlerFunc(s.repeatPreviewV2)))
	mux.Handle("POST /api/v2/orders/{order}/repeat/commit", s.auth(http.HandlerFunc(s.repeatCommitV2)))
	if s.selfServiceEnabled() {
		s.registerSelfServiceRoutes(mux)
	}
	if s.staffEnabled() {
		s.registerStaffRoutes(mux)
	}
	mux.Handle("/", s.staticHandler())
	return s.middleware(mux)
}

// newServer builds the server state; New registers the routes.
func newServer(service *application.Service, logger *slog.Logger, staticDir, storage string, sessions SessionConfig, options ...Option) *Server {
	s := &Server{service: service, logger: logger, staticDir: staticDir, storage: storage, secureCookie: sessions.Secure, cookieName: insecureSessionCookieName, trustedOrigins: map[string]bool{}}
	if sessions.Secure {
		s.cookieName = secureSessionCookieName
	}
	for _, origin := range sessions.TrustedOrigins {
		if normalized, err := ParseOrigin(origin, false); err == nil && normalized == origin {
			s.trustedOrigins[origin] = true
		} else {
			logger.Error("ignoring trusted origin that is not normalized")
		}
	}
	for _, option := range options {
		option(s)
	}
	return s
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.service.Ready(ctx); err != nil {
		s.logger.Error("readiness check failed", "storage", s.storage, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "storage": s.storage, "time": time.Now().UTC()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "storage": s.storage, "time": time.Now().UTC()})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	result, err := s.service.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		s.logAuthFailure(r, err)
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"user": currentUser(r)})
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Categories(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) manufacturers(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Manufacturers(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) brands(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Brands(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Collections(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func parseFilter(r *http.Request) (domain.ProductFilter, error) {
	q := r.URL.Query()
	filter := domain.ProductFilter{Query: q.Get("q"), Category: q.Get("category"), Manufacturer: q.Get("manufacturer"), Brand: q.Get("brand"), Collection: q.Get("collection"), Series: q.Get("series"), Sort: q.Get("sort")}
	filter.Tags = q["tag"]
	if joined := q.Get("tags"); joined != "" {
		for _, tag := range strings.Split(joined, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				filter.Tags = append(filter.Tags, tag)
			}
		}
	}
	var err error
	if raw := firstNonEmpty(q.Get("minPriceMinor"), q.Get("minPrice")); raw != "" {
		filter.MinPriceMinor, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || filter.MinPriceMinor < 0 {
			return filter, &domain.ValidationError{Fields: []domain.FieldError{{Field: "minPriceMinor", Message: "Ожидается неотрицательное целое число копеек"}}}
		}
	}
	if raw := firstNonEmpty(q.Get("maxPriceMinor"), q.Get("maxPrice")); raw != "" {
		filter.MaxPriceMinor, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || filter.MaxPriceMinor < 0 {
			return filter, &domain.ValidationError{Fields: []domain.FieldError{{Field: "maxPriceMinor", Message: "Ожидается неотрицательное целое число копеек"}}}
		}
	}
	if raw := q.Get("inStock"); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return filter, &domain.ValidationError{Fields: []domain.FieldError{{Field: "inStock", Message: "Ожидается true или false"}}}
		}
		filter.InStock = &value
	}
	if raw := q.Get("sale"); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return filter, &domain.ValidationError{Fields: []domain.FieldError{{Field: "sale", Message: "Ожидается true или false"}}}
		}
		filter.OnSale = value
	}
	if raw := firstNonEmpty(q.Get("newOnly"), q.Get("new")); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return filter, &domain.ValidationError{Fields: []domain.FieldError{{Field: "newOnly", Message: "Ожидается true или false"}}}
		}
		filter.NewOnly = value
	}
	if raw := q.Get("page"); raw != "" {
		filter.Page, err = strconv.Atoi(raw)
		if err != nil {
			return filter, &domain.ValidationError{Fields: []domain.FieldError{{Field: "page", Message: "Ожидается целое число"}}}
		}
	}
	if raw := firstNonEmpty(q.Get("limit"), q.Get("size")); raw != "" {
		filter.Limit, err = strconv.Atoi(raw)
		if err != nil {
			return filter, &domain.ValidationError{Fields: []domain.FieldError{{Field: "limit", Message: "Ожидается целое число"}}}
		}
	}
	return filter, nil
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	page, err := s.service.Catalog(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (s *Server) facets(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	facets, err := s.service.Facets(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, facets)
}
func (s *Server) product(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.Product(r.Context(), r.PathValue("product"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) suggestions(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Suggestions(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) cart(w http.ResponseWriter, r *http.Request) {
	cart, err := s.service.Cart(r.Context(), currentUser(r).ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cart)
}

func (s *Server) addCartItem(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProductID string `json:"productId"`
		Quantity  int    `json:"quantity"`
		Selected  *bool  `json:"selected"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	selected := true
	if input.Selected != nil {
		selected = *input.Selected
	}
	cart, err := s.service.SetCartItem(r.Context(), currentUser(r).ID, input.ProductID, input.Quantity, selected)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, cart)
}

func (s *Server) updateCartItem(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Quantity *int  `json:"quantity"`
		Selected *bool `json:"selected"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	if input.Quantity == nil && input.Selected == nil {
		s.writeError(w, r, &domain.ValidationError{Fields: []domain.FieldError{{Field: "body", Message: "Передайте quantity или selected"}}})
		return
	}
	userID := currentUser(r).ID
	productID := r.PathValue("product")
	cart, err := s.service.Cart(r.Context(), userID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	quantity, selected, found := 0, true, false
	for _, item := range cart.Items {
		if item.Product.ID == productID || item.Product.Slug == productID || strings.EqualFold(item.Product.SKU, productID) {
			quantity = item.Quantity
			selected = item.Selected
			productID = item.Product.ID
			found = true
			break
		}
	}
	if !found {
		s.writeError(w, r, domain.ErrNotFound)
		return
	}
	if input.Quantity != nil {
		quantity = *input.Quantity
	}
	if input.Selected != nil {
		selected = *input.Selected
	}
	// Selection-only updates must remain possible when a product has gone out of
	// stock after it was put in the cart. Re-validating the stale quantity here
	// would otherwise make the item impossible to deselect and block checkout.
	if input.Quantity == nil && input.Selected != nil {
		cart, err = s.service.SetCartSelection(r.Context(), userID, []string{productID}, selected)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, cart)
		return
	}
	cart, err = s.service.SetCartItem(r.Context(), userID, productID, quantity, selected)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cart)
}

func (s *Server) removeCartItem(w http.ResponseWriter, r *http.Request) {
	cart, err := s.service.RemoveCartItem(r.Context(), currentUser(r).ID, r.PathValue("product"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cart)
}
func (s *Server) clearCart(w http.ResponseWriter, r *http.Request) {
	selectedOnly, _ := strconv.ParseBool(r.URL.Query().Get("selectedOnly"))
	cart, err := s.service.ClearCart(r.Context(), currentUser(r).ID, selectedOnly)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cart)
}
func (s *Server) selectCartItems(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProductIDs []string `json:"productIds"`
		Selected   bool     `json:"selected"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	cart, err := s.service.SetCartSelection(r.Context(), currentUser(r).ID, input.ProductIDs, input.Selected)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cart)
}

func (s *Server) favorites(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.Favorites(r.Context(), currentUser(r).ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) enableFavorite(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.SetFavorite(r.Context(), currentUser(r).ID, r.PathValue("product"), true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) disableFavorite(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.SetFavorite(r.Context(), currentUser(r).ID, r.PathValue("product"), false)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	orders, err := s.service.Orders(r.Context(), currentUser(r).ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": orders})
}
func (s *Server) order(w http.ResponseWriter, r *http.Request) {
	order, err := s.service.Order(r.Context(), currentUser(r).ID, r.PathValue("order"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}
func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	var input domain.CheckoutInput
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	// Optional; absent keeps the legacy non-idempotent checkout. A present value
	// is validated by the service; a repeated header is ambiguous.
	if keys := r.Header.Values(idempotencyKeyHeader); len(keys) > 1 {
		s.writeError(w, r, &domain.ValidationError{Fields: []domain.FieldError{{Field: idempotencyKeyHeader, Message: "Передайте ровно один Idempotency-Key"}}})
		return
	} else if len(keys) == 1 {
		input.IdempotencyKey = &keys[0]
	}
	result, err := s.service.Checkout(r.Context(), currentUser(r).ID, input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, result.Order)
}

const idempotencyKeyHeader = "Idempotency-Key"

func (s *Server) repeatPreview(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1024))
	}
	preview, err := s.service.RepeatPreview(r.Context(), currentUser(r).ID, r.PathValue("order"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}
func (s *Server) repeatCommit(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Items []domain.RepeatSelection `json:"items"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	result, err := s.service.CommitRepeat(r.Context(), currentUser(r).ID, r.PathValue("order"), input.Items)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func currentUser(r *http.Request) domain.User {
	user, _ := r.Context().Value(userKey).(domain.User)
	return user
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = randomID()
		}
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self' data:; connect-src 'self'")
		info := &requestInfo{auth: "none"}
		ctx := context.WithValue(context.WithValue(r.Context(), requestIDKey, requestID), requestInfoKey, info)
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic in HTTP handler", "requestId", requestID, "panic", recovered, "stack", string(debug.Stack()))
				s.writeError(w, r.WithContext(ctx), fmt.Errorf("panic: %v", recovered))
			}
			s.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started), "requestId", requestID, "auth", info.auth)
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type apiError struct {
	Error struct {
		Code      string              `json:"code"`
		Message   string              `json:"message"`
		Fields    []domain.FieldError `json:"fields,omitempty"`
		RequestID string              `json:"requestId"`
	} `json:"error"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "Не удалось выполнить запрос"
	switch {
	case errors.Is(err, domain.ErrLinkInvalid):
		status, code, message = http.StatusGone, "link_invalid", "Ссылка недействительна или устарела"
	case errors.Is(err, domain.ErrBusy):
		status, code, message = http.StatusServiceUnavailable, "service_unavailable", "Сервис перегружен, повторите попытку через несколько секунд"
		w.Header().Set("Retry-After", "2")
	case errors.Is(err, domain.ErrValidation):
		status, code, message = http.StatusUnprocessableEntity, "validation_error", "Проверьте заполненные поля"
	case errors.Is(err, domain.ErrUnauthorized):
		status, code, message = http.StatusUnauthorized, "unauthorized", "Неверные данные для входа или сессия истекла"
	case errors.Is(err, domain.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "Недостаточно прав"
	case errors.Is(err, errCSRF):
		status, code, message = http.StatusForbidden, "csrf_failed", "Запрос отклонён: обновите страницу и повторите действие"
	case errors.Is(err, domain.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "Ничего не найдено"
	case errors.Is(err, domain.ErrOutOfStock):
		status, code, message = http.StatusConflict, "out_of_stock", "Запрошенного количества уже нет в наличии"
	case errors.Is(err, domain.ErrCheckoutChanged):
		status, code, message = http.StatusConflict, "checkout_changed", "Корзина, цены или остатки изменились: проверьте заказ и подтвердите его снова"
	case errors.Is(err, domain.ErrRepeatChanged):
		status, code, message = http.StatusConflict, "repeat_changed", "Условия повтора изменились: проверьте их и подтвердите снова"
	case errors.Is(err, domain.ErrIdempotencyKeyReused):
		status, code, message = http.StatusConflict, "idempotency_key_reused", "Ключ идемпотентности уже использован для другого заказа"
	case errors.Is(err, errAuthenticatedUserChanged):
		status, code, message = http.StatusConflict, "authenticated_user_changed", "Сессия принадлежит другому пользователю: обновите страницу"
	case errors.Is(err, errAuthenticatedStaffChanged):
		status, code, message = http.StatusConflict, "authenticated_staff_changed", "Сессия принадлежит другому сотруднику: обновите страницу"
	case errors.Is(err, domain.ErrConflict):
		status, code, message = http.StatusConflict, "conflict", "Операцию нельзя выполнить в текущем состоянии"
	}
	if status >= 500 {
		s.logger.Error("request failed", "error", err, "requestId", r.Context().Value(requestIDKey))
	}
	response := apiError{}
	response.Error.Code = code
	response.Error.Message = message
	response.Error.RequestID = fmt.Sprint(r.Context().Value(requestIDKey))
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		response.Error.Fields = validation.Fields
	}
	writeJSON(w, status, response)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return &domain.ValidationError{Fields: []domain.FieldError{{Field: "body", Message: "Тело запроса не может быть пустым"}}}
		}
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "body", Message: "Некорректный JSON: " + err.Error()}}}
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "body", Message: "Ожидается один JSON-объект"}}}
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

func (s *Server) staticHandler() http.Handler {
	if s.staticDir == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	}
	root, err := filepath.Abs(s.staticDir)
	if err != nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			s.writeError(w, r, domain.ErrNotFound)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		target := filepath.Join(root, clean)
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) && target != root {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			if strings.Contains(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		index := filepath.Join(root, "index.html")
		if _, err := os.Stat(index); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}
