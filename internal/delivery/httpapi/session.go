package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sweetshop/internal/domain"
)

const (
	secureSessionCookieName   = "__Host-sweetshop_session"
	insecureSessionCookieName = "sweetshop_session"
	csrfHeader                = "X-CSRF-Token"
	csrfDerivationPrefix      = "sweetshop/csrf/v1\x00"
)

// SessionConfig is the browser-session policy. TrustedOrigins must already be
// normalized with ParseTrustedOrigins; entries that are not are ignored.
type SessionConfig struct {
	// Secure selects the __Host- cookie with the Secure attribute. It must be
	// true in production.
	Secure         bool
	TrustedOrigins []string
}

// errAuthenticatedUserChanged rejects a protected request whose optional
// X-Expected-Authenticated-User-ID precondition does not name the user that
// authentication resolved. It is never a credential.
var errAuthenticatedUserChanged = errors.New("authenticated user differs from the expected user")

const expectedUserHeader = "X-Expected-Authenticated-User-ID"

// errCSRF rejects a cookie-authenticated unsafe request or a cookie login that
// failed Origin or CSRF-token validation.
var errCSRF = errors.New("csrf validation failed")

// ParseOrigin validates and normalizes a single origin: an absolute http(s)
// URL with only a lowercase ASCII host, an optional numeric port and at most a
// "/" path. The result is scheme://host[:port] without a default port. The
// same function normalizes configuration and request Origin headers.
func ParseOrigin(raw string, httpsOnly bool) (string, error) {
	if raw == "" {
		return "", errors.New("origin is empty")
	}
	for _, r := range raw {
		if r > 0x7e || r <= 0x20 {
			return "", errors.New("origin contains whitespace, control or non-ASCII characters")
		}
	}
	if strings.Contains(raw, "*") {
		return "", errors.New("origin must not contain wildcards")
	}
	if strings.ContainsAny(raw, "?#") {
		return "", errors.New("origin must not contain a query or fragment")
	}
	if strings.ContainsAny(raw, `\%`) {
		return "", errors.New("origin must not contain escapes or backslashes")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Opaque != "" {
		return "", errors.New("origin must be an absolute URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("origin scheme must be http or https")
	}
	if httpsOnly && scheme != "https" {
		return "", errors.New("origin scheme must be https")
	}
	if u.User != nil || strings.Contains(u.Host, "@") {
		return "", errors.New("origin must not contain credentials")
	}
	if (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", errors.New("origin must not contain a path")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return "", errors.New("origin must not contain a query or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", errors.New("origin host is empty")
	}
	if strings.HasPrefix(u.Host, "[") {
		if ip := net.ParseIP(host); ip == nil || ip.To4() != nil {
			return "", errors.New("origin IPv6 host is invalid")
		}
		host = "[" + host + "]"
	} else {
		for _, r := range host {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
				return "", errors.New("origin host contains invalid characters")
			}
		}
		if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
			return "", errors.New("origin host is malformed")
		}
	}
	if rawPort := u.Port(); rawPort != "" || strings.HasSuffix(u.Host, ":") {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return "", errors.New("origin port must be between 1 and 65535")
		}
		if !(scheme == "https" && port == 443) && !(scheme == "http" && port == 80) {
			host += ":" + strconv.Itoa(port)
		}
	}
	return scheme + "://" + host, nil
}

// ParseTrustedOrigins parses a comma-separated TRUSTED_ORIGINS value. Errors
// identify an entry by position only, never by value.
func ParseTrustedOrigins(raw string, httpsOnly bool) ([]string, error) {
	seen := map[string]bool{}
	origins := []string{}
	for i, entry := range strings.Split(raw, ",") {
		origin, err := ParseOrigin(strings.TrimSpace(entry), httpsOnly)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_ORIGINS entry %d is invalid: %w", i+1, err)
		}
		if !seen[origin] {
			seen[origin] = true
			origins = append(origins, origin)
		}
	}
	return origins, nil
}

// csrfToken derives the synchronizer token from the raw session token with
// domain separation. It differs from the stored token hash and does not reveal
// the session token.
func csrfToken(sessionToken string) string {
	sum := sha256.Sum256([]byte(csrfDerivationPrefix + sessionToken))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type requestInfo struct{ auth string }

func setAuthTransport(r *http.Request, transport string) {
	if info, ok := r.Context().Value(requestInfoKey).(*requestInfo); ok {
		info.auth = transport
	}
}

func unsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead
}

// bearerToken returns the Authorization bearer token. A present but malformed
// or repeated header is an error.
func bearerToken(r *http.Request) (string, bool, error) {
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		return "", false, nil
	}
	if len(values) > 1 {
		return "", true, domain.ErrUnauthorized
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", true, domain.ErrUnauthorized
	}
	return parts[1], true, nil
}

// sessionCookie returns the session cookie value. Several cookies with the
// session name are an error rather than a guess.
func (s *Server) sessionCookie(r *http.Request) (string, bool, error) {
	value, found := "", 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == s.cookieName {
			value = cookie.Value
			found++
		}
	}
	if found > 1 {
		return "", true, domain.ErrUnauthorized
	}
	return value, found == 1, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: token, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteLaxMode})
}

// checkOrigin returns a rejection reason, or "" when Origin is trusted.
func (s *Server) checkOrigin(r *http.Request) string {
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return "missing origin"
	}
	if len(values) > 1 {
		return "multiple origin headers"
	}
	origin, err := ParseOrigin(values[0], false)
	if err != nil || !s.trustedOrigins[origin] {
		return "untrusted origin"
	}
	return ""
}

// checkCSRF requires a trusted Origin and the token derived from the presented
// session cookie. It runs before any session lookup.
func (s *Server) checkCSRF(r *http.Request, sessionToken string) string {
	if reason := s.checkOrigin(r); reason != "" {
		return reason
	}
	values := r.Header.Values(csrfHeader)
	if len(values) != 1 || values[0] == "" {
		return "missing csrf token"
	}
	if subtle.ConstantTimeCompare([]byte(values[0]), []byte(csrfToken(sessionToken))) != 1 {
		return "invalid csrf token"
	}
	return ""
}

func (s *Server) rejectCSRF(w http.ResponseWriter, r *http.Request, reason string) {
	s.logger.Warn("csrf rejected", "reason", reason, "method", r.Method, "path", r.URL.Path, "requestId", r.Context().Value(requestIDKey))
	s.writeError(w, r, errCSRF)
}

// logAuthFailure records refused demo and disabled principals; other failures
// are ordinary.
func (s *Server) logAuthFailure(r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrDemoAccountDisabled):
		s.logger.Warn("demo account refused", "path", r.URL.Path, "requestId", r.Context().Value(requestIDKey))
	case errors.Is(err, domain.ErrAccountDisabled):
		s.logger.Warn("disabled account refused", "path", r.URL.Path, "requestId", r.Context().Value(requestIDKey))
	}
}

// auth resolves exactly one identity. Bearer and cookie credentials are bound to
// their transports; when both are present they must resolve to the same user,
// otherwise the request is rejected and the cookie cleared.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		bearer, hasBearer, bearerErr := bearerToken(r)
		cookie, hasCookie, cookieErr := s.sessionCookie(r)
		if bearerErr != nil || cookieErr != nil {
			s.writeError(w, r, domain.ErrUnauthorized)
			return
		}
		var (
			user      domain.User
			transport domain.SessionTransport
		)
		switch {
		case hasBearer && hasCookie:
			bearerRecord, bearerAuthErr := s.service.AuthenticateSession(r.Context(), bearer, domain.SessionBearer)
			cookieRecord, cookieAuthErr := s.service.AuthenticateSession(r.Context(), cookie, domain.SessionCookie)
			for _, err := range []error{bearerAuthErr, cookieAuthErr} {
				if err != nil && !errors.Is(err, domain.ErrUnauthorized) {
					s.writeError(w, r, err)
					return
				}
			}
			if bearerAuthErr != nil || cookieAuthErr != nil || bearerRecord.User.ID != cookieRecord.User.ID {
				s.logAuthFailure(r, errors.Join(bearerAuthErr, cookieAuthErr))
				s.clearSessionCookie(w)
				s.writeError(w, r, domain.ErrUnauthorized)
				return
			}
			// The Authorization header proves a same-origin script, so no CSRF check.
			user, transport = bearerRecord.User, domain.SessionBearer
		case hasBearer:
			record, err := s.service.AuthenticateSession(r.Context(), bearer, domain.SessionBearer)
			if err != nil {
				s.logAuthFailure(r, err)
				s.writeError(w, r, err)
				return
			}
			user, transport = record.User, domain.SessionBearer
		case hasCookie:
			if unsafeMethod(r.Method) {
				if reason := s.checkCSRF(r, cookie); reason != "" {
					s.rejectCSRF(w, r, reason)
					return
				}
			}
			record, err := s.service.AuthenticateSession(r.Context(), cookie, domain.SessionCookie)
			if err != nil {
				s.logAuthFailure(r, err)
				if errors.Is(err, domain.ErrUnauthorized) {
					s.clearSessionCookie(w)
				}
				s.writeError(w, r, err)
				return
			}
			user, transport = record.User, domain.SessionCookie
		default:
			s.writeError(w, r, domain.ErrUnauthorized)
			return
		}
		setAuthTransport(r, string(transport))
		if !s.bindAuthenticatedUser(w, r, user.ID) {
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

// bindAuthenticatedUser must run only after authentication. The actual owner
// binds success and later errors; request headers never supply that identity.
// An absent expectation preserves compatibility; empty/repeated values fail closed.
func (s *Server) bindAuthenticatedUser(w http.ResponseWriter, r *http.Request, userID string) bool {
	w.Header().Set("X-Authenticated-User-ID", userID)
	if expected := r.Header.Values(expectedUserHeader); len(expected) > 0 &&
		(len(expected) != 1 || expected[0] == "" || expected[0] != userID) {
		s.logger.Warn("expected user precondition failed", "method", r.Method, "path", r.URL.Path, "requestId", r.Context().Value(requestIDKey))
		s.writeError(w, r, errAuthenticatedUserChanged)
		return false
	}
	return true
}

type cookieSessionResponse struct {
	User      domain.User `json:"user"`
	ExpiresAt time.Time   `json:"expiresAt"`
	CSRFToken string      `json:"csrfToken"`
}

type sessionStateResponse struct {
	Authenticated bool         `json:"authenticated"`
	User          *domain.User `json:"user,omitempty"`
	ExpiresAt     *time.Time   `json:"expiresAt,omitempty"`
	CSRFToken     string       `json:"csrfToken,omitempty"`
	DemoLogin     bool         `json:"demoLogin"`
}

// startSession is the browser login. It requires a trusted Origin (login CSRF)
// and never returns the session token in the body.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if reason := s.checkOrigin(r); reason != "" {
		s.rejectCSRF(w, r, reason)
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	previous, _, err := s.sessionCookie(r)
	if err != nil {
		previous = ""
	}
	result, err := s.service.StartCookieSession(r.Context(), input.Email, input.Password, previous)
	if err != nil {
		s.logAuthFailure(r, err)
		s.writeError(w, r, err)
		return
	}
	s.setSessionCookie(w, result.Token, result.ExpiresAt)
	setAuthTransport(r, string(domain.SessionCookie))
	writeJSON(w, http.StatusOK, cookieSessionResponse{User: result.User, ExpiresAt: result.ExpiresAt, CSRFToken: csrfToken(result.Token)})
}

// sessionState reports the cookie session only; Authorization is ignored.
func (s *Server) sessionState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	state := sessionStateResponse{DemoLogin: s.service.DemoLoginAllowed()}
	cookie, hasCookie, err := s.sessionCookie(r)
	if err != nil || !hasCookie {
		writeJSON(w, http.StatusOK, state)
		return
	}
	record, err := s.service.AuthenticateSession(r.Context(), cookie, domain.SessionCookie)
	if err != nil {
		if !errors.Is(err, domain.ErrUnauthorized) {
			s.writeError(w, r, err)
			return
		}
		s.logAuthFailure(r, err)
		s.clearSessionCookie(w)
		writeJSON(w, http.StatusOK, state)
		return
	}
	setAuthTransport(r, string(domain.SessionCookie))
	state.Authenticated, state.User, state.ExpiresAt, state.CSRFToken = true, &record.User, &record.ExpiresAt, csrfToken(cookie)
	writeJSON(w, http.StatusOK, state)
}

// endSession revokes the presented credential. A bearer header takes precedence
// and leaves the cookie untouched; a cookie requires Origin and CSRF checks.
// Only callers sending an expected user opt into authentication and owner binding.
// Authentication and deletion use the same captured token and transport.
func (s *Server) endSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	bearer, hasBearer, err := bearerToken(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	token, transport, hasCredential := bearer, domain.SessionBearer, hasBearer
	if !hasBearer {
		token, hasCredential, err = s.sessionCookie(r)
		transport = domain.SessionCookie
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if hasCredential {
			if reason := s.checkCSRF(r, token); reason != "" {
				s.rejectCSRF(w, r, reason)
				return
			}
		}
	}
	if len(r.Header.Values(expectedUserHeader)) > 0 {
		record, err := s.service.AuthenticateSession(r.Context(), token, transport)
		if err != nil {
			s.logAuthFailure(r, err)
			s.writeError(w, r, err)
			return
		}
		setAuthTransport(r, string(transport))
		if !s.bindAuthenticatedUser(w, r, record.User.ID) {
			return
		}
	}
	if hasCredential {
		if err := s.service.Logout(r.Context(), token, transport); err != nil {
			s.writeError(w, r, err)
			return
		}
		setAuthTransport(r, string(transport))
	}
	if !hasBearer {
		s.clearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}
