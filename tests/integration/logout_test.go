//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/tests/contract"
)

func logoutHandler(repo domain.Repository) http.Handler {
	return httpapi.New(application.New(repo, application.Options{AllowDemoAccount: true}),
		slog.New(slog.NewTextHandler(io.Discard, nil)), "../../web", "postgres", contract.SessionConfig())
}

func logoutRequest(handler http.Handler, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("DELETE", "/api/auth/session", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func assertLogoutResponse(t *testing.T, response *httptest.ResponseRecorder, status int, owner string, clear bool) {
	t.Helper()
	if response.Code != status || response.Header().Get("X-Authenticated-User-ID") != owner || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("logout status=%d owner=%q, want %d/%q", response.Code, response.Header().Get("X-Authenticated-User-ID"), status, owner)
	}
	if clear {
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Value != "" || cookies[0].MaxAge >= 0 {
			t.Fatal("cookie was not cleared")
		}
	} else if len(response.Result().Cookies()) != 0 {
		t.Fatal("cookie unexpectedly changed")
	}
	if status == 204 && response.Body.Len() != 0 {
		t.Fatal("204 has a body")
	}
}

func TestPostgresGuardedLogoutIsolationAndPrecedence(t *testing.T) {
	d := newDatabase(t, true)
	d.customer(t, "usr-a")
	d.customer(t, "usr-b")
	for _, session := range []struct {
		token, user, transport string
	}{
		{"a-bearer", "usr-a", "bearer"}, {"a-other", "usr-a", "bearer"},
		{"b-cookie", "usr-b", "cookie"}, {"b-other", "usr-b", "cookie"},
	} {
		d.exec(t, "INSERT INTO sessions(token_hash,user_id,transport,expires_at) VALUES($1,$2,$3,now()+interval '1 day')",
			hashToken(session.token), session.user, session.transport)
	}
	handler := logoutHandler(d.store)
	const expected = "X-Expected-Authenticated-User-ID"
	cookie := "__Host-sweetshop_session=b-cookie"
	csrf := hashToken("sweetshop/csrf/v1\x00b-cookie")
	assertState := func(aBearer, bCookie int64) {
		t.Helper()
		for token, want := range map[string]int64{"a-bearer": aBearer, "b-cookie": bCookie, "a-other": 1, "b-other": 1} {
			if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", hashToken(token)) != want {
				t.Fatalf("wrong persisted state for %s", token)
			}
		}
		for _, other := range []struct {
			token     string
			transport domain.SessionTransport
		}{{"a-other", domain.SessionBearer}, {"b-other", domain.SessionCookie}} {
			if _, err := d.service.AuthenticateSession(d.ctx, other.token, other.transport); err != nil {
				t.Fatal("another device's session no longer authenticates")
			}
		}
	}
	// A bearer wins even though B's cookie is valid and expected=B. Cookie CSRF
	// is intentionally absent, proving the existing transport precedence.
	both := map[string]string{"Authorization": "Bearer a-bearer", "Cookie": cookie, expected: "usr-b", "X-Authenticated-User-ID": "usr-forged"}
	assertLogoutResponse(t, logoutRequest(handler, both), 409, "usr-a", false)
	assertState(1, 1)
	both["Authorization"] = "Bearer invalid"
	assertLogoutResponse(t, logoutRequest(handler, both), 401, "", false)
	assertState(1, 1)
	both["Authorization"] = "Bearer a-bearer"
	both[expected] = "usr-a"
	assertLogoutResponse(t, logoutRequest(handler, both), 204, "usr-a", false)
	assertState(0, 1)

	cookieHeaders := map[string]string{"Cookie": cookie, "Origin": contract.TrustedOrigin, "X-CSRF-Token": "invalid", expected: "usr-a"}
	assertLogoutResponse(t, logoutRequest(handler, cookieHeaders), 403, "", false)
	assertState(0, 1)
	cookieHeaders["X-CSRF-Token"] = csrf
	assertLogoutResponse(t, logoutRequest(handler, cookieHeaders), 409, "usr-b", false)
	assertState(0, 1)
	cookieHeaders[expected] = "usr-b"
	assertLogoutResponse(t, logoutRequest(handler, cookieHeaders), 204, "usr-b", true)
	assertState(0, 0)
	assertLogoutResponse(t, logoutRequest(handler, cookieHeaders), 401, "", false)
	assertState(0, 0)

	request := httptest.NewRequest("GET", "/api/auth/session", nil)
	request.Header.Set("Cookie", cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || body.Authenticated {
		t.Fatal("session GET must be anonymous after cookie logout")
	}
}

// The barrier coordinates real PostgreSQL reads before either DELETE. It
// changes neither the captured token nor the repository's revocation logic.
type logoutAuthenticationBarrier struct {
	domain.Repository
	arrived chan<- struct{}
	release <-chan struct{}
}

func (r logoutAuthenticationBarrier) SessionByTokenHash(ctx context.Context, hash string, transport domain.SessionTransport, now time.Time) (domain.SessionRecord, error) {
	record, err := r.Repository.SessionByTokenHash(ctx, hash, transport, now)
	if err != nil {
		return record, err
	}
	select {
	case r.arrived <- struct{}{}:
	case <-ctx.Done():
		return domain.SessionRecord{}, ctx.Err()
	}
	select {
	case <-r.release:
		return record, nil
	case <-ctx.Done():
		return domain.SessionRecord{}, ctx.Err()
	}
}

func TestPostgresConcurrentGuardedLogout(t *testing.T) {
	for _, transport := range []domain.SessionTransport{domain.SessionCookie, domain.SessionBearer} {
		t.Run(string(transport), func(t *testing.T) {
			d := newDatabase(t, true)
			for _, token := range []string{"selected", "survivor"} {
				d.exec(t, "INSERT INTO sessions(token_hash,user_id,transport,expires_at) VALUES($1,'usr-demo',$2,now()+interval '1 day')", hashToken(token), string(transport))
			}
			arrived, release := make(chan struct{}, 2), make(chan struct{})
			handler := logoutHandler(logoutAuthenticationBarrier{Repository: d.store, arrived: arrived, release: release})
			ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
			defer cancel()
			responses := make(chan *httptest.ResponseRecorder, 2)
			for i := 0; i < 2; i++ {
				go func() {
					request := httptest.NewRequest("DELETE", "/api/auth/session", nil).WithContext(ctx)
					request.Header.Set("X-Expected-Authenticated-User-ID", domain.DemoUserID)
					if transport == domain.SessionBearer {
						request.Header.Set("Authorization", "Bearer selected")
					} else {
						request.Header.Set("Cookie", "__Host-sweetshop_session=selected")
						request.Header.Set("Origin", contract.TrustedOrigin)
						request.Header.Set("X-CSRF-Token", hashToken("sweetshop/csrf/v1\x00selected"))
					}
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, request)
					responses <- response
				}()
			}
			for i := 0; i < 2; i++ {
				select {
				case <-arrived:
				case <-ctx.Done():
					t.Fatal("concurrent authentication did not reach barrier")
				}
			}
			close(release)
			for i := 0; i < 2; i++ {
				select {
				case response := <-responses:
					assertLogoutResponse(t, response, 204, domain.DemoUserID, transport == domain.SessionCookie)
				case <-ctx.Done():
					t.Fatal("concurrent logout did not finish")
				}
			}
			if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", hashToken("selected")) != 0 || d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", hashToken("survivor")) != 1 {
				t.Fatal("concurrent logout affected the wrong sessions")
			}
		})
	}
}
