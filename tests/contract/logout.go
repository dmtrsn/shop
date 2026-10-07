package contract

import (
	"net/http/httptest"
	"testing"
)

// Runs unchanged against memory and PostgreSQL, including live authentication
// checks after every rejection and after deleting just one of several sessions.
func runGuardedLogout(t *testing.T, send sender) {
	t.Helper()
	const credentials = `{"email":"demo@sugar.ru","password":"Sweet2026!"}`
	const expected = "X-Expected-Authenticated-User-ID"
	check := func(rec *httptest.ResponseRecorder, owner string, cleared bool) {
		t.Helper()
		if rec.Header().Get("X-Authenticated-User-ID") != owner || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("logout owner/cache contract")
		}
		cookies := rec.Result().Cookies()
		if cleared {
			if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].Value != "" || cookies[0].MaxAge >= 0 {
				t.Fatal("cookie logout must clear its cookie")
			}
		} else if len(cookies) != 0 {
			t.Fatal("logout unexpectedly touched cookies")
		}
		if rec.Code == 204 && rec.Body.Len() != 0 {
			t.Fatal("204 must have no body")
		}
	}
	_, rec := send("DELETE", "/api/auth/session", "", 204, nil)
	check(rec, "", true)
	_, rec = send("DELETE", "/api/auth/session", "", 401, map[string]string{expected: "usr-demo"})
	check(rec, "", false)

	loginCookie := func() map[string]string {
		body, response := send("POST", "/api/auth/session", credentials, 200, map[string]string{"Origin": TrustedOrigin})
		return map[string]string{
			"Cookie": sessionCookieName + "=" + response.Result().Cookies()[0].Value,
			"Origin": TrustedOrigin, "X-CSRF-Token": body["csrfToken"].(string),
		}
	}
	cookie, otherDevice := loginCookie(), loginCookie()
	bearer, _ := send("POST", "/api/auth/login", credentials, 200, nil)
	token := bearer["token"].(string)
	for _, want := range []string{"usr-other", ""} {
		cookie[expected] = want
		body, rec := send("DELETE", "/api/auth/session", "", 409, cookie)
		expectCode(t, "authenticated_user_changed")(body, rec)
		check(rec, "usr-demo", false)
		delete(cookie, expected)
		send("GET", "/api/me", "", 200, cookie)
	}
	cookie[expected] = "usr-demo"
	// Bearer logout never consumes, authenticates or clears the accompanying cookie.
	both := map[string]string{"Authorization": "Bearer " + token, "Cookie": cookie["Cookie"], expected: "usr-other"}
	body, rec := send("DELETE", "/api/auth/session", "", 409, both)
	expectCode(t, "authenticated_user_changed")(body, rec)
	check(rec, "usr-demo", false)
	send("GET", "/api/me", "", 200, map[string]string{"Authorization": "Bearer " + token})
	both[expected] = "usr-demo"
	_, rec = send("DELETE", "/api/auth/session", "", 204, both)
	check(rec, "usr-demo", false)
	send("GET", "/api/me", "", 401, map[string]string{"Authorization": "Bearer " + token})
	send("GET", "/api/me", "", 200, cookie)
	send("GET", "/api/me", "", 200, otherDevice)

	_, rec = send("DELETE", "/api/auth/session", "", 204, cookie)
	check(rec, "usr-demo", true)
	_, rec = send("DELETE", "/api/auth/session", "", 401, cookie)
	check(rec, "", false)
	state, _ := send("GET", "/api/auth/session", "", 200, cookie)
	if state["authenticated"] != false {
		t.Fatal("guarded cookie logout did not revoke the session")
	}
	send("GET", "/api/me", "", 200, otherDevice)
	delete(cookie, expected)
	_, rec = send("DELETE", "/api/auth/session", "", 204, cookie)
	check(rec, "", true)
}
