package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

func seedLogoutSession(t *testing.T, repo domain.Repository, token string, transport domain.SessionTransport, expires time.Time) {
	t.Helper()
	err := repo.CreateSession(context.Background(), domain.NewSession{
		UserID: domain.DemoUserID, TokenHash: hashToken(token), Transport: transport, ExpiresAt: expires,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
}

func assertLogoutSession(t *testing.T, repo domain.Repository, token string, transport domain.SessionTransport, valid bool) {
	t.Helper()
	_, err := repo.SessionByTokenHash(context.Background(), hashToken(token), transport, time.Now())
	if valid && err != nil || !valid && !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("session valid=%v, error=%v", valid, err)
	}
}

func TestGuardedLogoutPreconditions(t *testing.T) {
	for _, transport := range []domain.SessionTransport{domain.SessionCookie, domain.SessionBearer} {
		for name, expected := range map[string][]string{
			"different": {"usr-other"}, "empty": {""}, "repeated": {domain.DemoUserID, domain.DemoUserID},
			"mixed": {domain.DemoUserID, "usr-other"}, "case changed": {"USR-DEMO"},
		} {
			t.Run(string(transport)+"/"+name, func(t *testing.T) {
				repo := memory.NewDemo()
				seedLogoutSession(t, repo, "selected", transport, time.Now().Add(time.Hour))
				h := newHarness(t, repo, application.Options{AllowDemoAccount: true}, testSessions)
				headers := h.mutation("selected", hashToken("sweetshop/csrf/v1\x00selected"))
				if transport == domain.SessionBearer {
					headers = map[string]string{"Authorization": "Bearer selected"}
				}
				headers["X-Authenticated-User-ID"] = "usr-forged"
				response := send(h, "DELETE", "/api/auth/session", "", headers, expected...)
				expectStatus(t, response, 409)
				if errorCode(t, response) != "authenticated_user_changed" || response.Header().Get("X-Authenticated-User-ID") != domain.DemoUserID {
					t.Fatal("mismatch must bind the actual principal")
				}
				if response.Header().Get("Cache-Control") != "no-store" || h.sessionCookie(response) != nil {
					t.Fatal("rejection must be no-store and leave the cookie untouched")
				}
				assertLogoutSession(t, repo, "selected", transport, true)
			})
		}
	}
}

func TestGuardedLogoutCookieCSRFFirst(t *testing.T) {
	for _, failure := range []string{"missing origin", "untrusted origin", "missing csrf", "invalid csrf"} {
		t.Run(failure, func(t *testing.T) {
			repo := memory.NewDemo()
			seedLogoutSession(t, repo, "selected", domain.SessionCookie, time.Now().Add(time.Hour))
			h := newHarness(t, repo, application.Options{AllowDemoAccount: true}, testSessions)
			headers := h.mutation("selected", hashToken("sweetshop/csrf/v1\x00selected"))
			switch failure {
			case "missing origin":
				delete(headers, "Origin")
			case "untrusted origin":
				headers["Origin"] = "https://evil.example"
			case "missing csrf":
				delete(headers, "X-CSRF-Token")
			case "invalid csrf":
				headers["X-CSRF-Token"] = "invalid"
			}
			response := send(h, "DELETE", "/api/auth/session", "", headers, "usr-other")
			expectStatus(t, response, 403)
			if errorCode(t, response) != "csrf_failed" || response.Header().Get("X-Authenticated-User-ID") != "" || h.sessionCookie(response) != nil {
				t.Fatal("CSRF must reject before owner binding or cookie clearing")
			}
			assertLogoutSession(t, repo, "selected", domain.SessionCookie, true)
		})
	}
}

func TestGuardedLogoutAuthentication(t *testing.T) {
	for _, transport := range []domain.SessionTransport{domain.SessionCookie, domain.SessionBearer} {
		for _, state := range []string{"missing", "invalid", "expired", "revoked", "demo disabled", "malformed"} {
			t.Run(string(transport)+"/"+state, func(t *testing.T) {
				repo := memory.NewDemo()
				seedLogoutSession(t, repo, "survivor", transport, time.Now().Add(time.Hour))
				expires := time.Now().Add(time.Hour)
				if state == "expired" {
					expires = time.Now().Add(-time.Hour)
				}
				seedLogoutSession(t, repo, "selected", transport, expires)
				if state == "revoked" {
					if err := repo.DeleteSession(context.Background(), hashToken("selected"), transport); err != nil {
						t.Fatal(err)
					}
				}
				h := newHarness(t, repo, application.Options{AllowDemoAccount: state != "demo disabled"}, testSessions)
				token := "selected"
				if state == "invalid" {
					token = "invalid"
				}
				headers := h.mutation(token, hashToken("sweetshop/csrf/v1\x00"+token))
				if transport == domain.SessionBearer {
					headers = map[string]string{"Authorization": "Bearer " + token}
				}
				if state == "missing" {
					headers = map[string]string{}
				} else if state == "malformed" {
					if transport == domain.SessionBearer {
						headers["Authorization"] = "Basic invalid"
					} else {
						headers["Cookie"] += "; " + h.cookie + "=selected"
					}
				}
				headers["X-Authenticated-User-ID"] = domain.DemoUserID
				response := send(h, "DELETE", "/api/auth/session", "", headers, domain.DemoUserID)
				expectStatus(t, response, 401)
				if errorCode(t, response) != "unauthorized" || response.Header().Get("X-Authenticated-User-ID") != "" || h.sessionCookie(response) != nil {
					t.Fatal("authentication failure must not forge an owner or clear cookies")
				}
				assertLogoutSession(t, repo, "survivor", transport, true)
				assertLogoutSession(t, repo, "selected", transport, state != "expired" && state != "revoked")
				if state == "expired" {
					// Inspect at a time before expiry to distinguish expiration from deletion.
					if _, err := repo.SessionByTokenHash(context.Background(), hashToken("selected"), transport, time.Now().Add(-2*time.Hour)); err != nil {
						t.Fatal("rejected expired session was deleted")
					}
				}
			})
		}
	}
}

func TestGuardedLogoutRevokesOnlySelectedSession(t *testing.T) {
	for _, transport := range []domain.SessionTransport{domain.SessionCookie, domain.SessionBearer} {
		t.Run(string(transport), func(t *testing.T) {
			repo := memory.NewDemo()
			for _, token := range []string{"selected", "same-user-other-device"} {
				seedLogoutSession(t, repo, token, transport, time.Now().Add(time.Hour))
			}
			seedLogoutSession(t, repo, "other-cookie", domain.SessionCookie, time.Now().Add(time.Hour))
			h := newHarness(t, repo, application.Options{AllowDemoAccount: true}, testSessions)
			headers := h.mutation("selected", hashToken("sweetshop/csrf/v1\x00selected"))
			if transport == domain.SessionBearer {
				// No Origin/CSRF, and even ambiguous cookies must not affect bearer precedence.
				headers = map[string]string{"Authorization": "Bearer selected", "Cookie": h.cookie + "=other-cookie; " + h.cookie + "=invalid"}
			}
			response := send(h, "DELETE", "/api/auth/session", "", headers, domain.DemoUserID)
			expectStatus(t, response, 204)
			if response.Body.Len() != 0 || response.Header().Get("X-Authenticated-User-ID") != domain.DemoUserID || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("guarded 204 must be empty, owner-bound and retain security headers")
			}
			if transport == domain.SessionCookie {
				expectCleared(t, h, response)
			} else if h.sessionCookie(response) != nil {
				t.Fatal("bearer logout touched the cookie")
			}
			assertLogoutSession(t, repo, "selected", transport, false)
			assertLogoutSession(t, repo, "same-user-other-device", transport, true)
			assertLogoutSession(t, repo, "other-cookie", domain.SessionCookie, true)
			expectStatus(t, send(h, "DELETE", "/api/auth/session", "", headers, domain.DemoUserID), 401)
			// The same revoked credential retains legacy idempotent success when unguarded.
			expectStatus(t, send(h, "DELETE", "/api/auth/session", "", headers), 204)
		})
	}
}

type logoutFailureRepository struct {
	domain.Repository
	lookupError, deleteError error
}

func (r logoutFailureRepository) SessionByTokenHash(ctx context.Context, hash string, transport domain.SessionTransport, now time.Time) (domain.SessionRecord, error) {
	if r.lookupError != nil {
		return domain.SessionRecord{}, r.lookupError
	}
	return r.Repository.SessionByTokenHash(ctx, hash, transport, now)
}

func (r logoutFailureRepository) DeleteSession(ctx context.Context, hash string, transport domain.SessionTransport) error {
	if r.deleteError != nil {
		return r.deleteError
	}
	return r.Repository.DeleteSession(ctx, hash, transport)
}

func TestLegacyLogoutDoesNotAuthenticate(t *testing.T) {
	for _, transport := range []domain.SessionTransport{domain.SessionCookie, domain.SessionBearer} {
		for _, state := range []string{"valid", "expired", "revoked", "invalid", "missing"} {
			t.Run(string(transport)+"/"+state, func(t *testing.T) {
				repo := memory.NewDemo()
				seedLogoutSession(t, repo, "survivor", transport, time.Now().Add(time.Hour))
				expires := time.Now().Add(time.Hour)
				if state == "expired" {
					expires = time.Now().Add(-time.Hour)
				}
				seedLogoutSession(t, repo, "selected", transport, expires)
				if state == "revoked" {
					if err := repo.DeleteSession(context.Background(), hashToken("selected"), transport); err != nil {
						t.Fatal(err)
					}
				}
				// Even disabled demo principals and a failing authentication lookup
				// must not change the unguarded revocation contract.
				h := newHarness(t, logoutFailureRepository{Repository: repo, lookupError: errors.New("lookup must not run")}, application.Options{}, testSessions)
				token := "selected"
				if state == "invalid" {
					token = "unknown"
				}
				headers := h.mutation(token, hashToken("sweetshop/csrf/v1\x00"+token))
				if transport == domain.SessionBearer {
					headers = map[string]string{"Authorization": "Bearer " + token, "Cookie": h.cookie + "=unrelated"}
				}
				if state == "missing" {
					headers = nil
				}
				response := send(h, "DELETE", "/api/auth/session", "", headers)
				expectStatus(t, response, 204)
				if response.Body.Len() != 0 || response.Header().Get("X-Authenticated-User-ID") != "" || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("legacy logout response changed")
				}
				if transport == domain.SessionCookie || state == "missing" {
					expectCleared(t, h, response)
				} else if h.sessionCookie(response) != nil {
					t.Fatal("legacy bearer logout changed cookies")
				}
				assertLogoutSession(t, repo, "survivor", transport, true)
				assertLogoutSession(t, repo, "selected", transport, state == "invalid" || state == "missing")
			})
		}
	}
}

func TestGuardedLogoutStorageFailureOwner(t *testing.T) {
	for _, transport := range []domain.SessionTransport{domain.SessionCookie, domain.SessionBearer} {
		for _, stage := range []string{"authentication", "revocation"} {
			t.Run(string(transport)+"/"+stage, func(t *testing.T) {
				repo := memory.NewDemo()
				seedLogoutSession(t, repo, "selected-secret", transport, time.Now().Add(time.Hour))
				fault := logoutFailureRepository{Repository: repo}
				owner := ""
				if stage == "authentication" {
					fault.lookupError = errors.New("session storage unavailable")
				} else {
					fault.deleteError = errors.New("session storage unavailable")
					owner = domain.DemoUserID
				}
				h := newHarness(t, fault, application.Options{AllowDemoAccount: true}, testSessions)
				csrf := hashToken("sweetshop/csrf/v1\x00selected-secret")
				headers := h.mutation("selected-secret", csrf)
				if transport == domain.SessionBearer {
					headers = map[string]string{"Authorization": "Bearer selected-secret"}
				}
				response := send(h, "DELETE", "/api/auth/session", "", headers, domain.DemoUserID)
				expectStatus(t, response, http.StatusInternalServerError)
				if errorCode(t, response) != "internal_error" || response.Header().Get("X-Authenticated-User-ID") != owner || h.sessionCookie(response) != nil || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("storage failure lost its authentication boundary")
				}
				assertLogoutSession(t, repo, "selected-secret", transport, true)
				for _, secret := range []string{"selected-secret", hashToken("selected-secret"), csrf} {
					if strings.Contains(h.logs.String()+response.Body.String(), secret) {
						t.Fatal("credentials leaked in logs or error")
					}
				}
			})
		}
	}
}
