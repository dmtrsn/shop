package contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"

	"sweetshop/internal/delivery/httpapi"
)

// AdminTrustedOrigin is the staff origin of the contract handler; it is
// distinct from TrustedOrigin.
const AdminTrustedOrigin = "https://admin.shop.example"

const staffCookieName = "__Host-sweetshop_staff"

// StaffConfig is the staff policy matching SessionConfig.
func StaffConfig() httpapi.StaffConfig {
	return httpapi.StaffConfig{TrustedOrigins: []string{AdminTrustedOrigin}}
}

// RunAdmin exercises every staff operation against api/admin-openapi.yaml.
// The handler must have the staff API enabled with StaffConfig and a staff
// account with the given credentials.
func RunAdmin(t *testing.T, handler http.Handler, email, password string) {
	t.Helper()
	doc := AdminDocument(t)
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	send := func(method, body string, status int, headers map[string]string) (map[string]any, *httptest.ResponseRecorder) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/admin/v1/auth/session", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", "admin-contract")
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
				t.Fatalf("%s request contract: %v", method, err)
			}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s status=%d want=%d body=%s", method, rec.Code, status, rec.Body.String())
		}
		if rec.Header().Get("X-Request-ID") != "admin-contract" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: request ID or no-store missing", method)
		}
		validation := &openapi3filter.ResponseValidationInput{RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route}, Status: rec.Code, Header: rec.Header(), Options: &openapi3filter.Options{IncludeResponseStatus: true}}
		if err := openapi3filter.ValidateResponse(context.Background(), validation.SetBodyBytes(rec.Body.Bytes())); err != nil {
			t.Fatalf("%s response contract: %v", method, err)
		}
		if status < 300 {
			covered[route.Operation.OperationID] = true
		}
		if rec.Code == http.StatusNoContent {
			return nil, rec
		}
		var result map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result, rec
	}
	errorCode := func(result map[string]any) string {
		return result["error"].(map[string]any)["code"].(string)
	}
	credentials := `{"email":"` + email + `","password":"` + password + `"}`
	origin := map[string]string{"Origin": AdminTrustedOrigin}

	if state, _ := send("GET", "", 200, nil); state["authenticated"] != false {
		t.Fatal("anonymous staff state")
	}
	if result, _ := send("POST", credentials, 403, map[string]string{"Origin": TrustedOrigin}); errorCode(result) != "csrf_failed" {
		t.Fatal("customer origin accepted for staff login")
	}
	if result, _ := send("POST", `{"email":"`+email+`","password":"wrong password 1"}`, 401, origin); errorCode(result) != "unauthorized" {
		t.Fatal("wrong password")
	}
	if result, _ := send("POST", `{"email":"x"`, 422, origin); errorCode(result) != "validation_error" {
		t.Fatal("malformed body")
	}
	if result, _ := send("POST", credentials, 401, map[string]string{"Origin": AdminTrustedOrigin, "Authorization": "Bearer customer-token"}); errorCode(result) != "unauthorized" {
		t.Fatal("bearer accepted on staff login")
	}
	session, rec := send("POST", credentials, 200, origin)
	cookie := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == staffCookieName {
			cookie = c.Name + "=" + c.Value
			if strings.Contains(rec.Body.String(), c.Value) {
				t.Fatal("staff session token in the body")
			}
		}
	}
	if cookie == "" {
		t.Fatal("no staff cookie")
	}
	staff := session["staff"].(map[string]any)
	id, csrf := staff["id"].(string), session["csrfToken"].(string)

	if state, _ := send("GET", "", 200, map[string]string{"Cookie": cookie}); state["authenticated"] != true || state["csrfToken"] != csrf {
		t.Fatal("authenticated staff state")
	}
	if result, _ := send("GET", "", 401, map[string]string{"Cookie": cookie, "Authorization": "Bearer x"}); errorCode(result) != "unauthorized" {
		t.Fatal("bearer accepted on staff state")
	}
	if result, _ := send("DELETE", "", 403, map[string]string{"Cookie": cookie, "Origin": AdminTrustedOrigin}); errorCode(result) != "csrf_failed" {
		t.Fatal("staff logout without CSRF")
	}
	if result, _ := send("DELETE", "", 409, map[string]string{"Cookie": cookie, "Origin": AdminTrustedOrigin, "X-CSRF-Token": csrf, "X-Expected-Staff-ID": "stf_00000000000000000000000000000000"}); errorCode(result) != "authenticated_staff_changed" {
		t.Fatal("expected staff precondition")
	}
	send("DELETE", "", 204, map[string]string{"Cookie": cookie, "Origin": AdminTrustedOrigin, "X-CSRF-Token": csrf, "X-Expected-Staff-ID": id})
	if state, _ := send("GET", "", 200, map[string]string{"Cookie": cookie}); state["authenticated"] != false {
		t.Fatal("staff session survived logout")
	}
	for _, operation := range []string{"staffSessionState", "startStaffSession", "endStaffSession"} {
		if !covered[operation] {
			t.Errorf("operation %s not covered", operation)
		}
	}
}
