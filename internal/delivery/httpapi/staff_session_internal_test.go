package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

const (
	shopOrigin  = "https://shop.example"
	adminOrigin = "https://admin.example"
	staffEmail  = "ops@example.com"
	staffSecret = "staff password 1"
)

// testStaffStore is an in-test staff store; production staff persistence is
// PostgreSQL only.
type testStaffStore struct {
	mu         sync.Mutex
	credential domain.StaffCredential
	sessions   map[string]domain.NewStaffSession
}

func newTestStaffStore(t *testing.T) *testStaffStore {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(staffSecret), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	staff := domain.StaffUser{ID: "stf_" + strings.Repeat("1", 32), Email: staffEmail, DisplayName: "Ops"}
	return &testStaffStore{credential: domain.StaffCredential{Staff: staff, PasswordHash: string(hash)}, sessions: map[string]domain.NewStaffSession{}}
}

func (s *testStaffStore) FindStaffByEmail(_ context.Context, email string) (domain.StaffCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if email != s.credential.Staff.Email {
		return domain.StaffCredential{}, domain.ErrNotFound
	}
	return s.credential, nil
}

func (s *testStaffStore) CreateStaffSession(_ context.Context, session domain.NewStaffSession, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.PasswordHash != s.credential.PasswordHash || s.credential.Disabled {
		return domain.ErrUnauthorized
	}
	delete(s.sessions, session.ReplacesTokenHash)
	s.sessions[session.TokenHash] = session
	return nil
}

func (s *testStaffStore) StaffSessionByTokenHash(_ context.Context, hash string, now time.Time) (domain.StaffSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[hash]
	if !ok || !session.ExpiresAt.After(now) || s.credential.Disabled {
		return domain.StaffSessionRecord{}, domain.ErrUnauthorized
	}
	return domain.StaffSessionRecord{Staff: s.credential.Staff, ExpiresAt: session.ExpiresAt}, nil
}

func (s *testStaffStore) DeleteStaffSession(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, hash)
	return nil
}

func (s *testStaffStore) CreateStaff(context.Context, domain.NewStaff) (domain.StaffAccount, error) {
	return domain.StaffAccount{}, domain.ErrForbidden
}

func (s *testStaffStore) SetStaffPassword(context.Context, string, string) (domain.StaffChange, error) {
	return domain.StaffChange{}, domain.ErrForbidden
}

func (s *testStaffStore) SetStaffDisabled(context.Context, string, bool) (domain.StaffChange, error) {
	return domain.StaffChange{}, domain.ErrForbidden
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func customerService() *application.Service {
	return application.New(memory.NewDemo(), application.Options{AllowDemoAccount: true})
}

// staffHandler is New plus a staffAuth-protected probe route that reports the
// principals it can see.
func staffHandler(t *testing.T, store *testStaffStore, secure bool) http.Handler {
	t.Helper()
	s := newServer(customerService(), discard(), "", "memory", SessionConfig{Secure: secure, TrustedOrigins: []string{shopOrigin}},
		WithStaff(application.NewStaffAuth(store), StaffConfig{TrustedOrigins: []string{adminOrigin}}))
	if !s.staffEnabled() {
		t.Fatal("staff not enabled")
	}
	mux := http.NewServeMux()
	s.registerStaffRoutes(mux)
	probe := func(w http.ResponseWriter, r *http.Request) {
		staff, ok := currentStaff(r)
		writeJSON(w, http.StatusOK, map[string]any{"staff": staff.ID, "staffOK": ok, "customer": currentUser(r).ID})
	}
	mux.Handle("GET /probe", s.staffAuth(http.HandlerFunc(probe)))
	mux.Handle("POST /probe", s.staffAuth(http.HandlerFunc(probe)))
	mux.HandleFunc("POST /api/auth/session", s.startSession)
	mux.Handle("GET /api/me", s.auth(http.HandlerFunc(s.me)))
	return s.middleware(mux)
}

func call(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func staffLogin(t *testing.T, handler http.Handler, secure bool) (cookie, csrf string) {
	t.Helper()
	rec := call(handler, "POST", "/api/admin/v1/auth/session", `{"email":"`+staffEmail+`","password":"`+staffSecret+`"}`, map[string]string{"Origin": adminOrigin})
	if rec.Code != 200 {
		t.Fatalf("staff login: %d %s", rec.Code, rec.Body.String())
	}
	name := insecureStaffCookieName
	if secure {
		name = secureStaffCookieName
	}
	c := cookieNamed(rec, name)
	if c == nil {
		t.Fatal("no staff cookie")
	}
	var body struct{ CSRFToken string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return c.Name + "=" + c.Value, body.CSRFToken
}

func TestStaffRoutesAbsentUnlessConfigured(t *testing.T) {
	store := newTestStaffStore(t)
	auth := application.NewStaffAuth(store)
	sessions := SessionConfig{Secure: true, TrustedOrigins: []string{shopOrigin}}
	for name, options := range map[string][]Option{
		"not configured":           nil,
		"no staff service":         {WithStaff(nil, StaffConfig{TrustedOrigins: []string{adminOrigin}})},
		"no staff origins":         {WithStaff(auth, StaffConfig{})},
		"overlapping origins":      {WithStaff(auth, StaffConfig{TrustedOrigins: []string{adminOrigin, shopOrigin}})},
		"only unnormalized origin": {WithStaff(auth, StaffConfig{TrustedOrigins: []string{"HTTPS://ADMIN.example/"}})},
	} {
		t.Run(name, func(t *testing.T) {
			handler := New(customerService(), discard(), "", "memory", sessions, options...)
			for _, method := range []string{"GET", "POST", "DELETE"} {
				rec := call(handler, method, "/api/admin/v1/auth/session", `{"email":"`+staffEmail+`","password":"`+staffSecret+`"}`, map[string]string{"Origin": adminOrigin})
				if rec.Code != http.StatusNotFound || strings.Contains(rec.Header().Get("Set-Cookie"), "staff") {
					t.Fatalf("%s: %d", method, rec.Code)
				}
			}
		})
	}
}

func TestStaffCookieAttributes(t *testing.T) {
	for _, secure := range []bool{true, false} {
		handler := staffHandler(t, newTestStaffStore(t), secure)
		rec := call(handler, "POST", "/api/admin/v1/auth/session", `{"email":"`+staffEmail+`","password":"`+staffSecret+`"}`, map[string]string{"Origin": adminOrigin})
		name := map[bool]string{true: "__Host-sweetshop_staff", false: "sweetshop_staff"}[secure]
		c := cookieNamed(rec, name)
		if rec.Code != 200 || c == nil || !c.HttpOnly || c.Secure != secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" || c.MaxAge < int((11*time.Hour).Seconds()) || c.MaxAge > int((12*time.Hour).Seconds()) {
			t.Fatalf("secure=%v cookie=%+v", secure, c)
		}
		if strings.Contains(rec.Body.String(), c.Value) || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("token in body or cacheable")
		}
	}
	// The customer cookie is unchanged: its own name and SameSite=Lax.
	handler := staffHandler(t, newTestStaffStore(t), true)
	rec := call(handler, "POST", "/api/auth/session", `{"email":"demo@sugar.ru","password":"Sweet2026!"}`, map[string]string{"Origin": shopOrigin})
	c := cookieNamed(rec, "__Host-sweetshop_session")
	if rec.Code != 200 || c == nil || c.SameSite != http.SameSiteLaxMode || !c.Secure || !c.HttpOnly || cookieNamed(rec, "__Host-sweetshop_staff") != nil {
		t.Fatalf("customer cookie: %+v", c)
	}
}

func TestCSRFNamespacesAreSeparate(t *testing.T) {
	for _, token := range []string{"a", "same-token", strings.Repeat("x", 43)} {
		if csrfToken(token) == staffCSRFToken(token) {
			t.Fatal("customer and staff CSRF tokens coincide")
		}
	}
	if secureStaffCookieName == secureSessionCookieName || insecureStaffCookieName == insecureSessionCookieName || expectedStaffHeader == expectedUserHeader || staffKey == userKey {
		t.Fatal("staff and customer names must differ")
	}
}

func TestStaffAuthMiddlewareSeesOnlyTheStaffPrincipal(t *testing.T) {
	handler := staffHandler(t, newTestStaffStore(t), true)
	staffCookie, staffCSRF := staffLogin(t, handler, true)
	customer := call(handler, "POST", "/api/auth/session", `{"email":"demo@sugar.ru","password":"Sweet2026!"}`, map[string]string{"Origin": shopOrigin})
	customerCookie := "__Host-sweetshop_session=" + cookieNamed(customer, "__Host-sweetshop_session").Value
	var customerBody struct{ CSRFToken string }
	_ = json.Unmarshal(customer.Body.Bytes(), &customerBody)
	staffValue := strings.SplitN(staffCookie, "=", 2)[1]

	ok := call(handler, "GET", "/probe", "", map[string]string{"Cookie": staffCookie + "; " + customerCookie})
	if ok.Code != 200 || !strings.Contains(ok.Body.String(), `"customer":""`) || !strings.Contains(ok.Body.String(), `"staffOK":true`) || ok.Header().Get(authenticatedStaffHeader) == "" || ok.Header().Get("X-Authenticated-User-ID") != "" {
		t.Fatalf("staff probe: %d %s", ok.Code, ok.Body.String())
	}
	for name, tc := range map[string]struct {
		method  string
		headers map[string]string
		status  int
	}{
		"customer cookie only":             {"GET", map[string]string{"Cookie": customerCookie}, 401},
		"customer token under staff name":  {"GET", map[string]string{"Cookie": "__Host-sweetshop_staff=" + strings.SplitN(customerCookie, "=", 2)[1]}, 401},
		"bearer customer token":            {"GET", map[string]string{"Authorization": "Bearer " + strings.SplitN(customerCookie, "=", 2)[1]}, 401},
		"bearer beside staff cookie":       {"GET", map[string]string{"Cookie": staffCookie, "Authorization": "Bearer x"}, 401},
		"two staff cookies":                {"GET", map[string]string{"Cookie": staffCookie + "; " + staffCookie}, 401},
		"unsafe without CSRF":              {"POST", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin}, 403},
		"customer-derived CSRF":            {"POST", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrfToken(staffValue)}, 403},
		"customer CSRF token":              {"POST", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": customerBody.CSRFToken}, 403},
		"customer origin":                  {"POST", map[string]string{"Cookie": staffCookie, "Origin": shopOrigin, "X-CSRF-Token": staffCSRF}, 403},
		"wrong expected staff":             {"POST", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": staffCSRF, expectedStaffHeader: "stf_other"}, 409},
		"staff CSRF ok":                    {"POST", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": staffCSRF}, 200},
		"customer expected header ignored": {"POST", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": staffCSRF, expectedUserHeader: "usr-demo"}, 200},
	} {
		t.Run(name, func(t *testing.T) {
			rec := call(handler, tc.method, "/probe", "", tc.headers)
			if rec.Code != tc.status {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			if tc.status >= 400 && rec.Header().Get(authenticatedStaffHeader) != "" && tc.status != 409 {
				t.Fatal("rejected request exposes the staff ID")
			}
		})
	}
	// A staff cookie never authenticates a customer route.
	if rec := call(handler, "GET", "/api/me", "", map[string]string{"Cookie": staffCookie}); rec.Code != 401 {
		t.Fatalf("staff cookie on /api/me: %d", rec.Code)
	}
	if rec := call(handler, "GET", "/api/me", "", map[string]string{"Cookie": "__Host-sweetshop_session=" + staffValue}); rec.Code != 401 {
		t.Fatalf("staff token under customer name: %d", rec.Code)
	}
}
