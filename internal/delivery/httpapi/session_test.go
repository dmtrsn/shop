package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

const (
	trustedOrigin = "https://shop.example"
	secureCookie  = "__Host-sweetshop_session"
	demoLogin     = `{"email":"demo@sugar.ru","password":"Sweet2026!"}`
)

var testSessions = httpapi.SessionConfig{Secure: true, TrustedOrigins: []string{trustedOrigin}}

type harness struct {
	t       *testing.T
	handler http.Handler
	logs    *bytes.Buffer
	cookie  string
}

func newHarness(t *testing.T, repo domain.Repository, options application.Options, config httpapi.SessionConfig) *harness {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	cookie := "sweetshop_session"
	if config.Secure {
		cookie = secureCookie
	}
	return &harness{t: t, handler: httpapi.New(application.New(repo, options), logger, "", "memory", config), logs: logs, cookie: cookie}
}

func demoHarness(t *testing.T) *harness {
	return newHarness(t, memory.NewDemo(), application.Options{AllowDemoAccount: true}, testSessions)
}

func (h *harness) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	return response
}

func (h *harness) sessionCookie(response *httptest.ResponseRecorder) *http.Cookie {
	h.t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == h.cookie {
			return cookie
		}
	}
	return nil
}

// cookieLogin returns the raw session token and the CSRF token.
func (h *harness) cookieLogin(extra map[string]string) (string, string) {
	h.t.Helper()
	headers := map[string]string{"Origin": trustedOrigin}
	for key, value := range extra {
		headers[key] = value
	}
	response := h.do(http.MethodPost, "/api/auth/session", demoLogin, headers)
	if response.Code != http.StatusOK {
		h.t.Fatalf("cookie login status=%d body=%s", response.Code, response.Body.String())
	}
	cookie := h.sessionCookie(response)
	if cookie == nil {
		h.t.Fatal("cookie login did not set the session cookie")
	}
	var body struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		h.t.Fatal(err)
	}
	return cookie.Value, body.CSRFToken
}

func (h *harness) cookieHeader(token string) map[string]string {
	return map[string]string{"Cookie": h.cookie + "=" + token}
}

func (h *harness) mutation(token, csrf string) map[string]string {
	return map[string]string{"Cookie": h.cookie + "=" + token, "Origin": trustedOrigin, "X-CSRF-Token": csrf}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func errorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body: %v (%s)", err, response.Body.String())
	}
	return body.Error.Code
}

func expectStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
}

func expectCleared(t *testing.T, h *harness, response *httptest.ResponseRecorder) {
	t.Helper()
	cookie := h.sessionCookie(response)
	if cookie == nil || cookie.Value != "" || cookie.MaxAge >= 0 || cookie.Path != "/" || !cookie.HttpOnly {
		t.Fatalf("session cookie not cleared: %#v", cookie)
	}
}

func TestCookieLoginSetsHardenedCookieWithoutExposingToken(t *testing.T) {
	h := demoHarness(t)
	response := h.do(http.MethodPost, "/api/auth/session", demoLogin, map[string]string{"Origin": trustedOrigin})
	expectStatus(t, response, http.StatusOK)
	raw := response.Header().Get("Set-Cookie")
	for _, attribute := range []string{secureCookie + "=", "Path=/", "HttpOnly", "Secure", "SameSite=Lax", "Max-Age="} {
		if !strings.Contains(raw, attribute) {
			t.Errorf("Set-Cookie %q lacks %s", raw, attribute)
		}
	}
	if strings.Contains(raw, "Domain=") {
		t.Error("session cookie must be host-only")
	}
	cookie := h.sessionCookie(response)
	if cookie.MaxAge > int((30*24*time.Hour).Seconds()) || cookie.MaxAge < int((30*24*time.Hour-2*time.Minute).Seconds()) {
		t.Fatalf("Max-Age=%d is not the remaining 30-day lifetime", cookie.MaxAge)
	}
	if strings.Contains(response.Body.String(), cookie.Value) || strings.Contains(response.Body.String(), hashToken(cookie.Value)) {
		t.Fatal("response body exposes the session credential")
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["token"]; ok || body["csrfToken"] == "" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cookie login body/headers: %s", response.Body.String())
	}
}

func TestInsecureDevelopmentCookieName(t *testing.T) {
	h := newHarness(t, memory.NewDemo(), application.Options{AllowDemoAccount: true}, httpapi.SessionConfig{TrustedOrigins: []string{trustedOrigin}})
	response := h.do(http.MethodPost, "/api/auth/session", demoLogin, map[string]string{"Origin": trustedOrigin})
	expectStatus(t, response, http.StatusOK)
	raw := response.Header().Get("Set-Cookie")
	if !strings.HasPrefix(raw, "sweetshop_session=") || strings.Contains(raw, "Secure") || !strings.Contains(raw, "HttpOnly") || !strings.Contains(raw, "SameSite=Lax") {
		t.Fatalf("development cookie: %q", raw)
	}
	token := h.sessionCookie(response).Value
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(token)), http.StatusOK)
	// The Secure-mode name is not an alias in insecure mode.
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", map[string]string{"Cookie": secureCookie + "=" + token}), http.StatusUnauthorized)
}

func TestSessionTokenEntropyAndUniqueness(t *testing.T) {
	h := demoHarness(t)
	first, _ := h.cookieLogin(nil)
	second, _ := h.cookieLogin(nil)
	for _, token := range []string{first, second} {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("token is not 32 random bytes: %v", err)
		}
	}
	if first == second {
		t.Fatal("two logins produced the same token")
	}
}

func TestCookieAuthenticatesSafeRequestsWithoutCSRF(t *testing.T) {
	h := demoHarness(t)
	token, _ := h.cookieLogin(nil)
	for _, path := range []string{"/api/me", "/api/cart", "/api/favorites", "/api/orders"} {
		response := h.do(http.MethodGet, path, "", h.cookieHeader(token))
		expectStatus(t, response, http.StatusOK)
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s missing no-store", path)
		}
	}
	expectStatus(t, h.do(http.MethodHead, "/api/cart", "", h.cookieHeader(token)), http.StatusOK)
}

func TestExpiredCookieSessionIsRejectedAndCleared(t *testing.T) {
	store := memory.NewDemo()
	past := time.Now().Add(-time.Hour)
	if err := store.CreateSession(context.Background(), domain.NewSession{UserID: domain.DemoUserID, TokenHash: hashToken("expired-token"), Transport: domain.SessionCookie, ExpiresAt: past}, past.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, store, application.Options{AllowDemoAccount: true}, testSessions)
	response := h.do(http.MethodGet, "/api/cart", "", h.cookieHeader("expired-token"))
	expectStatus(t, response, http.StatusUnauthorized)
	expectCleared(t, h, response)
}

func TestCSRFMatrixForCookieMutations(t *testing.T) {
	h := demoHarness(t)
	token, csrf := h.cookieLogin(nil)
	otherToken, otherCSRF := h.cookieLogin(nil)
	if otherToken == token || otherCSRF == csrf {
		t.Fatal("sessions must have distinct CSRF tokens")
	}
	cookie := h.cookie + "=" + token
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{"no origin", map[string]string{"Cookie": cookie, "X-CSRF-Token": csrf}},
		{"null origin", map[string]string{"Cookie": cookie, "Origin": "null", "X-CSRF-Token": csrf}},
		{"foreign origin", map[string]string{"Cookie": cookie, "Origin": "https://evil.example", "X-CSRF-Token": csrf}},
		{"sibling subdomain", map[string]string{"Cookie": cookie, "Origin": "https://admin.shop.example", "X-CSRF-Token": csrf}},
		{"http downgrade", map[string]string{"Cookie": cookie, "Origin": "http://shop.example", "X-CSRF-Token": csrf}},
		{"other port", map[string]string{"Cookie": cookie, "Origin": "https://shop.example:8443", "X-CSRF-Token": csrf}},
		{"origin with path", map[string]string{"Cookie": cookie, "Origin": "https://shop.example/x", "X-CSRF-Token": csrf}},
		{"missing token", map[string]string{"Cookie": cookie, "Origin": trustedOrigin}},
		{"wrong token", map[string]string{"Cookie": cookie, "Origin": trustedOrigin, "X-CSRF-Token": "not-the-token"}},
		{"token of another session", map[string]string{"Cookie": cookie, "Origin": trustedOrigin, "X-CSRF-Token": otherCSRF}},
		{"session token as csrf", map[string]string{"Cookie": cookie, "Origin": trustedOrigin, "X-CSRF-Token": token}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{http.MethodPut, http.MethodDelete} {
				response := h.do(method, "/api/favorites/p02", "", tc.headers)
				expectStatus(t, response, http.StatusForbidden)
				if errorCode(t, response) != "csrf_failed" {
					t.Fatal("expected csrf_failed")
				}
			}
			response := h.do(http.MethodPost, "/api/cart/items", `{"productId":"p02","quantity":1}`, tc.headers)
			expectStatus(t, response, http.StatusForbidden)
		})
	}
	expectStatus(t, h.do(http.MethodPut, "/api/favorites/p02", "", h.mutation(token, csrf)), http.StatusOK)
	// Case and an explicit default port normalize to the trusted origin.
	headers := h.mutation(token, csrf)
	headers["Origin"] = "HTTPS://SHOP.EXAMPLE:443"
	expectStatus(t, h.do(http.MethodDelete, "/api/favorites/p02", "", headers), http.StatusOK)
	// The pinned derivation: base64url(SHA-256("sweetshop/csrf/v1\x00" + token)).
	sum := sha256.Sum256([]byte("sweetshop/csrf/v1\x00" + token))
	if csrf != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatal("csrf derivation changed")
	}
	if !strings.Contains(h.logs.String(), `"msg":"csrf rejected"`) {
		t.Fatal("csrf rejection was not logged")
	}
}

func TestBearerRemainsCompatibleAndExemptFromCSRF(t *testing.T) {
	h := demoHarness(t)
	bearer := loginHTTP(t, h.handler)
	for _, scheme := range []string{"Bearer ", "bEaReR "} {
		expectStatus(t, h.do(http.MethodPut, "/api/favorites/p02", "", map[string]string{"Authorization": scheme + bearer}), http.StatusOK)
	}
	// A foreign Origin does not matter to bearer requests: the header itself
	// cannot be sent cross-origin without a (failing) preflight.
	expectStatus(t, h.do(http.MethodDelete, "/api/favorites/p02", "", map[string]string{"Authorization": "Bearer " + bearer, "Origin": "https://evil.example"}), http.StatusOK)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", map[string]string{"Authorization": "Token " + bearer}), http.StatusUnauthorized)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", map[string]string{"Authorization": "Bearer"}), http.StatusUnauthorized)
}

func TestTransportBinding(t *testing.T) {
	h := demoHarness(t)
	bearer := loginHTTP(t, h.handler)
	cookie, _ := h.cookieLogin(nil)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(bearer)), http.StatusUnauthorized)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", map[string]string{"Authorization": "Bearer " + cookie}), http.StatusUnauthorized)
}

// otherUserSessions adds one bearer session for a second principal; the memory
// demo store has only the demo user.
type otherUserSessions struct {
	*memory.Store
	token string
}

func (r otherUserSessions) SessionByTokenHash(ctx context.Context, hash string, transport domain.SessionTransport, now time.Time) (domain.SessionRecord, error) {
	if transport == domain.SessionBearer && hash == hashToken(r.token) {
		return domain.SessionRecord{User: domain.User{ID: "usr-other", Name: "Other", Email: "other@example.test"}, ExpiresAt: now.Add(time.Hour), Transport: transport}, nil
	}
	return r.Store.SessionByTokenHash(ctx, hash, transport, now)
}

func TestConflictingCredentials(t *testing.T) {
	h := newHarness(t, otherUserSessions{Store: memory.NewDemo(), token: "other-user-token"}, application.Options{AllowDemoAccount: true}, testSessions)
	cookie, _ := h.cookieLogin(nil)
	bearer := loginHTTP(t, h.handler)

	same := map[string]string{"Cookie": h.cookie + "=" + cookie, "Authorization": "Bearer " + bearer}
	response := h.do(http.MethodPut, "/api/favorites/p02", "", same)
	expectStatus(t, response, http.StatusOK)
	if h.sessionCookie(response) != nil {
		t.Fatal("matching credentials must not touch the cookie")
	}

	for name, headers := range map[string]map[string]string{
		"different users":              {"Cookie": h.cookie + "=" + cookie, "Authorization": "Bearer other-user-token"},
		"valid cookie, invalid bearer": {"Cookie": h.cookie + "=" + cookie, "Authorization": "Bearer invalid"},
		"invalid cookie, valid bearer": {"Cookie": h.cookie + "=invalid", "Authorization": "Bearer " + bearer},
	} {
		t.Run(name, func(t *testing.T) {
			response := h.do(http.MethodGet, "/api/cart", "", headers)
			expectStatus(t, response, http.StatusUnauthorized)
			expectCleared(t, h, response)
		})
	}
	// Rejection clears the browser cookie but revokes nothing server-side.
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(cookie)), http.StatusOK)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", map[string]string{"Authorization": "Bearer other-user-token"}), http.StatusOK)

	duplicate := h.do(http.MethodGet, "/api/cart", "", map[string]string{"Cookie": h.cookie + "=" + cookie + "; " + h.cookie + "=" + cookie})
	expectStatus(t, duplicate, http.StatusUnauthorized)
	multipleAuthorization := httptest.NewRequest(http.MethodGet, "/api/cart", nil)
	multipleAuthorization.Header.Add("Authorization", "Bearer "+bearer)
	multipleAuthorization.Header.Add("Authorization", "Bearer "+bearer)
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, multipleAuthorization)
	expectStatus(t, recorder, http.StatusUnauthorized)
}

func TestLogoutRevokesServerSide(t *testing.T) {
	h := demoHarness(t)
	token, csrf := h.cookieLogin(nil)

	response := h.do(http.MethodDelete, "/api/auth/session", "", h.cookieHeader(token))
	expectStatus(t, response, http.StatusForbidden)
	if h.sessionCookie(response) != nil {
		t.Fatal("rejected logout must not clear the cookie")
	}
	withoutToken := h.cookieHeader(token)
	withoutToken["Origin"] = trustedOrigin
	expectStatus(t, h.do(http.MethodDelete, "/api/auth/session", "", withoutToken), http.StatusForbidden)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(token)), http.StatusOK)

	response = h.do(http.MethodDelete, "/api/auth/session", "", h.mutation(token, csrf))
	expectStatus(t, response, http.StatusNoContent)
	expectCleared(t, h, response)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(token)), http.StatusUnauthorized)
	// Idempotent: the same (now revoked) cookie logs out again.
	expectStatus(t, h.do(http.MethodDelete, "/api/auth/session", "", h.mutation(token, csrf)), http.StatusNoContent)

	response = h.do(http.MethodDelete, "/api/auth/session", "", nil)
	expectStatus(t, response, http.StatusNoContent)
	expectCleared(t, h, response)
}

func TestBearerLogoutLeavesCookieSession(t *testing.T) {
	h := demoHarness(t)
	bearer := loginHTTP(t, h.handler)
	cookie, _ := h.cookieLogin(nil)
	response := h.do(http.MethodDelete, "/api/auth/session", "", map[string]string{"Authorization": "Bearer " + bearer, "Cookie": h.cookie + "=" + cookie})
	expectStatus(t, response, http.StatusNoContent)
	if h.sessionCookie(response) != nil {
		t.Fatal("bearer logout must not touch the cookie")
	}
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", map[string]string{"Authorization": "Bearer " + bearer}), http.StatusUnauthorized)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(cookie)), http.StatusOK)
	expectStatus(t, h.do(http.MethodDelete, "/api/auth/session", "", map[string]string{"Authorization": "Basic x"}), http.StatusUnauthorized)
}

func TestCookieLoginRequiresTrustedOrigin(t *testing.T) {
	h := demoHarness(t)
	for _, origin := range []string{"", "null", "https://evil.example", "https://shop.example.evil.example", "https://shop.example/login"} {
		headers := map[string]string{}
		if origin != "" {
			headers["Origin"] = origin
		}
		response := h.do(http.MethodPost, "/api/auth/session", demoLogin, headers)
		expectStatus(t, response, http.StatusForbidden)
		if errorCode(t, response) != "csrf_failed" || h.sessionCookie(response) != nil {
			t.Fatalf("origin %q: login CSRF not blocked", origin)
		}
	}
	response := h.do(http.MethodPost, "/api/auth/session", `{"email":"demo@sugar.ru","password":"wrong"}`, map[string]string{"Origin": trustedOrigin})
	expectStatus(t, response, http.StatusUnauthorized)
	expectStatus(t, h.do(http.MethodPost, "/api/auth/session", `{"email":""}`, map[string]string{"Origin": trustedOrigin}), http.StatusUnprocessableEntity)
}

func TestCookieLoginRotatesPresentedSession(t *testing.T) {
	h := demoHarness(t)
	first, _ := h.cookieLogin(nil)
	second, _ := h.cookieLogin(h.cookieHeader(first))
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(first)), http.StatusUnauthorized)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(second)), http.StatusOK)

	failed := map[string]string{"Origin": trustedOrigin, "Cookie": h.cookie + "=" + second}
	expectStatus(t, h.do(http.MethodPost, "/api/auth/session", `{"email":"demo@sugar.ru","password":"wrong"}`, failed), http.StatusUnauthorized)
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(second)), http.StatusOK)

	bearerLogin := h.do(http.MethodPost, "/api/auth/login", demoLogin, h.cookieHeader(second))
	expectStatus(t, bearerLogin, http.StatusOK)
	if h.sessionCookie(bearerLogin) != nil {
		t.Fatal("legacy login must not set a cookie")
	}
	expectStatus(t, h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(second)), http.StatusOK)
}

func TestSessionStateEndpoint(t *testing.T) {
	h := demoHarness(t)
	var state map[string]any
	response := h.do(http.MethodGet, "/api/auth/session", "", nil)
	expectStatus(t, response, http.StatusOK)
	_ = json.Unmarshal(response.Body.Bytes(), &state)
	if state["authenticated"] != false || state["demoLogin"] != true || len(state) != 2 {
		t.Fatalf("anonymous state: %v", state)
	}
	token, csrf := h.cookieLogin(nil)
	response = h.do(http.MethodGet, "/api/auth/session", "", h.cookieHeader(token))
	state = nil
	_ = json.Unmarshal(response.Body.Bytes(), &state)
	if state["authenticated"] != true || state["csrfToken"] != csrf || state["user"] == nil || strings.Contains(response.Body.String(), token) {
		t.Fatalf("authenticated state: %s", response.Body.String())
	}
	// Authorization is ignored by this endpoint.
	bearer := loginHTTP(t, h.handler)
	response = h.do(http.MethodGet, "/api/auth/session", "", map[string]string{"Authorization": "Bearer " + bearer})
	state = nil
	_ = json.Unmarshal(response.Body.Bytes(), &state)
	if state["authenticated"] != false {
		t.Fatal("bearer must not report a cookie session")
	}
	response = h.do(http.MethodGet, "/api/auth/session", "", h.cookieHeader("invalid"))
	expectStatus(t, response, http.StatusOK)
	expectCleared(t, h, response)

	production := newHarness(t, memory.NewDemo(), application.Options{}, testSessions)
	response = production.do(http.MethodGet, "/api/auth/session", "", nil)
	state = nil
	_ = json.Unmarshal(response.Body.Bytes(), &state)
	if state["demoLogin"] != false {
		t.Fatal("demoLogin must follow the service options")
	}
}

func TestProductionRefusesDemoPrincipal(t *testing.T) {
	store := memory.NewDemo()
	demo := newHarness(t, store, application.Options{AllowDemoAccount: true}, testSessions)
	existingCookie, _ := demo.cookieLogin(nil)
	existingBearer := loginHTTP(t, demo.handler)

	production := newHarness(t, store, application.Options{}, testSessions)
	wrongPassword := production.do(http.MethodPost, "/api/auth/login", `{"email":"demo@sugar.ru","password":"wrong"}`, nil)
	refused := production.do(http.MethodPost, "/api/auth/login", demoLogin, nil)
	expectStatus(t, refused, http.StatusUnauthorized)
	var a, b map[string]map[string]any
	_ = json.Unmarshal(wrongPassword.Body.Bytes(), &a)
	_ = json.Unmarshal(refused.Body.Bytes(), &b)
	if a["error"]["code"] != b["error"]["code"] || a["error"]["message"] != b["error"]["message"] {
		t.Fatal("demo refusal must be indistinguishable from a wrong password")
	}
	expectStatus(t, production.do(http.MethodPost, "/api/auth/session", demoLogin, map[string]string{"Origin": trustedOrigin}), http.StatusUnauthorized)
	response := production.do(http.MethodGet, "/api/cart", "", production.cookieHeader(existingCookie))
	expectStatus(t, response, http.StatusUnauthorized)
	expectCleared(t, production, response)
	expectStatus(t, production.do(http.MethodGet, "/api/cart", "", map[string]string{"Authorization": "Bearer " + existingBearer}), http.StatusUnauthorized)
	logs := production.logs.String()
	if !strings.Contains(logs, "demo account refused") || strings.Contains(logs, "demo@sugar.ru") {
		t.Fatalf("demo refusal logging: %s", logs)
	}
}

func TestRequestLogRecordsTransportWithoutCredentials(t *testing.T) {
	h := demoHarness(t)
	token, csrf := h.cookieLogin(nil)
	bearer := loginHTTP(t, h.handler)
	h.do(http.MethodGet, "/api/cart", "", h.cookieHeader(token))
	h.do(http.MethodGet, "/api/cart", "", map[string]string{"Authorization": "Bearer " + bearer})
	h.do(http.MethodGet, "/api/catalog", "", nil)
	h.do(http.MethodPut, "/api/favorites/p02", "", h.cookieHeader(token))
	logs := h.logs.String()
	for _, field := range []string{`"auth":"cookie"`, `"auth":"bearer"`, `"auth":"none"`} {
		if !strings.Contains(logs, field) {
			t.Errorf("request log lacks %s", field)
		}
	}
	for _, secret := range []string{token, hashToken(token), csrf, bearer, hashToken(bearer)} {
		if strings.Contains(logs, secret) {
			t.Fatal("log contains credential material")
		}
	}
}

func TestCORSRemainsClosed(t *testing.T) {
	h := demoHarness(t)
	headers := map[string]string{"Origin": "https://evil.example", "Access-Control-Request-Method": "DELETE", "Access-Control-Request-Headers": "authorization, x-csrf-token"}
	for _, response := range []*httptest.ResponseRecorder{
		h.do(http.MethodOptions, "/api/cart", "", headers),
		h.do(http.MethodOptions, "/api/auth/session", "", headers),
		h.do(http.MethodGet, "/api/catalog", "", map[string]string{"Origin": "https://evil.example"}),
		h.do(http.MethodGet, "/api/auth/session", "", map[string]string{"Origin": trustedOrigin}),
	} {
		for name := range response.Header() {
			if strings.HasPrefix(strings.ToLower(name), "access-control-") {
				t.Fatalf("CORS header %s present", name)
			}
		}
	}
}

// Cookie-authenticated GET/HEAD skip CSRF checks, so every GET route must stay
// free of side effects. Adding one here requires reviewing that invariant.
func TestGETRoutesAreReviewedAsReadOnly(t *testing.T) {
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	var routes []string
	for _, match := range regexp.MustCompile(`mux\.Handle(?:Func)?\("GET ([^"]+)"`).FindAllStringSubmatch(string(source), -1) {
		routes = append(routes, match[1])
	}
	sort.Strings(routes)
	reviewed := []string{"/api/auth/session", "/api/brands", "/api/cart", "/api/catalog", "/api/catalog/facets", "/api/categories", "/api/collections", "/api/favorites", "/api/health", "/api/manufacturers", "/api/me", "/api/orders", "/api/orders/{order}", "/api/products/{product}", "/api/search/suggest"}
	if strings.Join(routes, ",") != strings.Join(reviewed, ",") {
		t.Fatalf("GET routes changed; confirm they are read-only, then update the list: %v", routes)
	}
}

func TestParseOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"https://shop.example":         "https://shop.example",
		"HTTPS://Shop.Example":         "https://shop.example",
		"https://shop.example/":        "https://shop.example",
		"https://shop.example:443":     "https://shop.example",
		"http://host:80/":              "http://host",
		"http://127.0.0.1:8080":        "http://127.0.0.1:8080",
		"https://shop.example:0443":    "https://shop.example",
		"https://[::1]:8443":           "https://[::1]:8443",
		"https://[::1]:443/":           "https://[::1]",
		"https://xn--80ak6aa92e.com":   "https://xn--80ak6aa92e.com",
		"https://shop.example:8443":    "https://shop.example:8443",
		"http://localhost:3000":        "http://localhost:3000",
		"https://sub.shop-example.com": "https://sub.shop-example.com",
	} {
		got, err := httpapi.ParseOrigin(raw, false)
		if err != nil || got != want {
			t.Errorf("ParseOrigin(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"", "shop.example", "/relative", "//shop.example", "null", "ftp://shop.example", "https:shop.example", "https://",
		"https://user@shop.example", "https://user:pass@shop.example", "https://shop.example/path", "https://shop.example//",
		"https://shop.example?", "https://shop.example?a=1", "https://shop.example#", "https://shop.example#x",
		"https://*.shop.example", "*", "https://shop.example:*", "https://shop.example:", "https://shop.example:0",
		"https://shop.example:70000", "https://shop.example:-1", "https://shop.example:https", "https://магазин.рф",
		"https://shop.example ", " https://shop.example", "https://shop .example", "https://shop.example\t",
		"https://shop%2eexample", `https://shop.example\evil`, "https://shop_example", "https://.shop.example",
		"https://shop..example", "https://shop.example.", "https://[::ffff:127.0.0.1]", "https://[not-ip]",
		"javascript://shop.example",
	} {
		if got, err := httpapi.ParseOrigin(raw, false); err == nil {
			t.Errorf("ParseOrigin(%q) accepted as %q", raw, got)
		}
	}
	if _, err := httpapi.ParseOrigin("http://shop.example", true); err == nil {
		t.Error("https-only parsing accepted http")
	}
}

func TestParseTrustedOrigins(t *testing.T) {
	origins, err := httpapi.ParseTrustedOrigins(" https://shop.example , HTTPS://SHOP.EXAMPLE:443,https://admin.example ", true)
	if err != nil || strings.Join(origins, ",") != "https://shop.example,https://admin.example" {
		t.Fatalf("origins=%v err=%v", origins, err)
	}
	for _, raw := range []string{"", ",", "https://shop.example,,https://admin.example", "https://shop.example,", "http://shop.example", "https://secret-token@shop.example"} {
		_, err := httpapi.ParseTrustedOrigins(raw, true)
		if err == nil {
			t.Errorf("accepted %q", raw)
			continue
		}
		if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "shop.example") {
			t.Errorf("error exposes configured value: %v", err)
		}
	}
}
