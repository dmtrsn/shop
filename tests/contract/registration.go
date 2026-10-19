package contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"

	"sweetshop/internal/email"
	"sweetshop/internal/email/emailtest"
)

// selfServiceOperation reports an operation marked x-self-service: it
// exists only with PostgreSQL and configured email.
func selfServiceOperation(operation *openapi3.Operation) bool {
	return operation.Extensions["x-self-service"] == true
}

func selfServicePaths(t *testing.T, doc *openapi3.T) []string {
	t.Helper()
	paths := []string{}
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if selfServiceOperation(operation) {
				if method != http.MethodPost {
					t.Fatalf("self-service %s %s: only POST is expected", method, path)
				}
				paths = append(paths, path)
			}
		}
	}
	if len(paths) != 5 {
		t.Fatalf("self-service operations %v", paths)
	}
	return paths
}

// assertSelfServiceAbsent requires selfService=false and no self-service
// route: each answers like any unknown /api path and creates nothing.
func assertSelfServiceAbsent(t *testing.T, doc *openapi3.T, handler http.Handler) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/auth/session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var state map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil || state["selfService"] != false {
		t.Fatalf("selfService must be false without self-registration: %s", rec.Body.String())
	}
	body := `{"email":"absent@example.com","password":"absent password 1","name":"Absent","token":"` + strings.Repeat("A", 43) + `"}`
	for _, path := range selfServicePaths(t, doc) {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Origin", TrustedOrigin)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || len(rec.Result().Cookies()) != 0 {
			t.Fatalf("%s exists without self-registration: %d", path, rec.Code)
		}
	}
}

// RunSelfService exercises every x-self-service operation against the
// contract. The handler must have self-registration enabled with outbox as
// its email outbox, and registrant@example.com must be unused.
func RunSelfService(t *testing.T, handler http.Handler, outbox *emailtest.Outbox) {
	t.Helper()
	doc := document(t)
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	send := func(path, body string, status int, headers map[string]string) (map[string]any, *httptest.ResponseRecorder) {
		t.Helper()
		method := "POST"
		if path == "/api/auth/session" {
			method = "GET"
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", "self-service-contract")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		route, params, err := router.FindRoute(req)
		if err != nil {
			t.Fatal(err)
		}
		if status < 300 {
			input := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route,
				Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, SkipSettingDefaults: true}}
			if err := openapi3filter.ValidateRequest(context.Background(), input); err != nil {
				t.Fatalf("%s request contract: %v", path, err)
			}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s status=%d want=%d body=%s", path, rec.Code, status, rec.Body.String())
		}
		if rec.Header().Get("X-Request-ID") != "self-service-contract" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: request ID or no-store missing", path)
		}
		validation := &openapi3filter.ResponseValidationInput{RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route}, Status: rec.Code, Header: rec.Header(), Options: &openapi3filter.Options{IncludeResponseStatus: true}}
		if err := openapi3filter.ValidateResponse(context.Background(), validation.SetBodyBytes(rec.Body.Bytes())); err != nil {
			t.Fatalf("%s response contract: %v", path, err)
		}
		if status < 300 {
			covered[route.Operation.OperationID] = true
		}
		if rec.Code == http.StatusNoContent {
			if rec.Body.Len() != 0 {
				t.Fatalf("%s: 204 with a body", path)
			}
			return nil, rec
		}
		var result map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result, rec
	}
	origin := map[string]string{"Origin": TrustedOrigin}
	const (
		address  = "registrant@example.com"
		password = "registrant password 1"
	)

	state, _ := send("/api/auth/session", "", 200, nil)
	if state["selfService"] != true {
		t.Fatal("selfService must be true with self-registration")
	}
	registration := `{"email":" Registrant@Example.com ","password":"` + password + `","name":"Регистрант","phone":"+7 900 000-00-00"}`
	send("/api/auth/registration", registration, 403, nil)
	send("/api/auth/registration", registration, 403, map[string]string{"Origin": "https://evil.example"})
	invalid, _ := send("/api/auth/registration", `{"email":"nope","password":"short","name":""}`, 422, origin)
	if len(invalid["error"].(map[string]any)["fields"].([]any)) != 3 {
		t.Fatal("field errors")
	}
	send("/api/auth/registration", `{"email":"a@b.co","password":"`+password+`","name":"X","extra":1}`, 422, origin)
	accepted, rec := send("/api/auth/registration", registration, 202, origin)
	if accepted["status"] != "pending" || len(accepted) != 1 || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("registration answer %v", accepted)
	}
	messages := outbox.To(address)
	if len(messages) != 1 {
		t.Fatalf("confirmation emails: %d", len(messages))
	}
	token := emailtest.Token(messages[0])

	send("/api/auth/registration/resend", `{"email":"`+address+`"}`, 403, nil)
	send("/api/auth/registration/resend", `{"email":"nope"}`, 422, origin)
	// Within the cooldown: the same 202 and no new email, so the link stays valid.
	resent, _ := send("/api/auth/registration/resend", `{"email":"`+address+`"}`, 202, origin)
	if resent["status"] != "pending" || len(outbox.To(address)) != 1 {
		t.Fatal("resend inside the cooldown")
	}
	send("/api/auth/registration/resend", `{"email":"unknown@example.com"}`, 202, origin)

	confirm := func(token, password string) string {
		return `{"token":"` + token + `","password":"` + password + `"}`
	}
	send("/api/auth/registration/confirm", confirm(token, password), 403, nil)
	send("/api/auth/registration/confirm", `{"token":"`+token+`"}`, 422, origin)
	if bad, _ := send("/api/auth/registration/confirm", confirm(strings.Repeat("A", 43), password), 410, origin); bad["error"].(map[string]any)["code"] != "link_invalid" {
		t.Fatal("unknown token code")
	}
	send("/api/auth/registration/confirm", confirm("not a token", password), 410, origin)
	send("/api/auth/registration/confirm", confirm(token, "wrong password 1"), 401, origin)
	session, rec := send("/api/auth/registration/confirm", confirm(token, password), 200, origin)
	user := session["user"].(map[string]any)
	if user["email"] != address || user["name"] != "Регистрант" || session["csrfToken"] == "" {
		t.Fatalf("confirmation session %v", session)
	}
	cookie := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.HttpOnly && c.Secure && c.SameSite == http.SameSiteLaxMode && c.Path == "/" && c.Value != "" {
			cookie = c.Value
		}
	}
	if cookie == "" || strings.Contains(rec.Body.String(), cookie) || cookie == token {
		t.Fatal("confirmation did not set the normal session cookie")
	}
	signedIn, _ := send("/api/auth/session", "", 200, map[string]string{"Cookie": sessionCookieName + "=" + cookie})
	if signedIn["authenticated"] != true || signedIn["user"].(map[string]any)["id"] != user["id"] {
		t.Fatal("the new session does not authenticate")
	}
	send("/api/auth/registration/confirm", confirm(token, password), 410, origin)
	// The address is now an account: registering it again changes nothing
	// visible and answers the same.
	again, _ := send("/api/auth/registration", registration, 202, origin)
	if again["status"] != "pending" {
		t.Fatal("registration of an existing account")
	}

	runPasswordReset(t, handler, outbox, send, token)

	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if selfServiceOperation(operation) && !covered[operation.OperationID] {
				t.Errorf("no success response checked for %s %s", method, path)
			}
		}
	}
}

type selfServiceSender func(path, body string, status int, headers map[string]string) (map[string]any, *httptest.ResponseRecorder)

// cookieLogin signs in outside the self-service coverage and returns the
// session cookie, or "" when the login fails with want.
func cookieLogin(t *testing.T, handler http.Handler, address, password string, want int) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(`{"email":"`+address+`","password":"`+password+`"}`))
	req.Header.Set("Origin", TrustedOrigin)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("login %s: %d, want %d", address, rec.Code, want)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// runPasswordReset recovers the seeded demo account, an ordinary enabled
// customer here. registrationToken is an already used confirmation token.
func runPasswordReset(t *testing.T, handler http.Handler, outbox *emailtest.Outbox, send selfServiceSender, registrationToken string) {
	t.Helper()
	origin := map[string]string{"Origin": TrustedOrigin}
	const (
		address  = "demo@sugar.ru"
		old      = "Sweet2026!"
		password = "recovered password 1"
	)
	cookie := cookieLogin(t, handler, address, old, 200)
	send("/api/auth/password-reset", `{"email":"`+address+`"}`, 403, nil)
	send("/api/auth/password-reset", `{"email":"nope"}`, 422, origin)
	accepted, rec := send("/api/auth/password-reset", `{"email":" DEMO@sugar.ru "}`, 202, origin)
	if accepted["status"] != "pending" || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("reset request answer %v", accepted)
	}
	send("/api/auth/password-reset", `{"email":"nobody@example.com"}`, 202, origin)
	token := ""
	for _, message := range outbox.To(address) {
		if message.Kind == email.KindPasswordReset {
			token = emailtest.Token(message)
		}
	}
	if token == "" {
		t.Fatal("no reset email")
	}
	if len(outbox.To("nobody@example.com")) != 0 {
		t.Fatal("email to an unknown address")
	}
	confirm := func(token, password string) string {
		return `{"token":"` + token + `","password":"` + password + `"}`
	}
	send("/api/auth/password-reset/confirm", confirm(token, password), 403, nil)
	send("/api/auth/password-reset/confirm", confirm(strings.Repeat("A", 43), password), 410, origin)
	// Purposes never cross: a confirmation token is no reset token, and back.
	send("/api/auth/password-reset/confirm", confirm(registrationToken, password), 410, origin)
	send("/api/auth/registration/confirm", confirm(token, old), 410, origin)
	if invalid, _ := send("/api/auth/password-reset/confirm", confirm(token, "short"), 422, origin); invalid["error"].(map[string]any)["fields"] == nil {
		t.Fatal("password policy field error")
	}
	_, rec = send("/api/auth/password-reset/confirm", confirm(token, password), 204, map[string]string{"Origin": TrustedOrigin, "Cookie": sessionCookieName + "=" + cookie})
	cleared := false
	for _, c := range rec.Result().Cookies() {
		cleared = cleared || (c.Name == sessionCookieName && c.MaxAge < 0)
		if c.Name == sessionCookieName && c.Value != "" {
			t.Fatal("password reset signed in")
		}
	}
	if !cleared {
		t.Fatal("password reset did not clear the session cookie")
	}
	send("/api/auth/password-reset/confirm", confirm(token, password), 410, origin)
	state, _ := send("/api/auth/session", "", 200, map[string]string{"Cookie": sessionCookieName + "=" + cookie})
	if state["authenticated"] != false {
		t.Fatal("an old session survived the reset")
	}
	cookieLogin(t, handler, address, old, 401)
	cookieLogin(t, handler, address, password, 200)
}
