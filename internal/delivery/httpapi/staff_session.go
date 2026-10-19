package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// Staff identity shares no cookie, CSRF namespace, origin list, context key or
// expected-principal header with customers. Staff routes exist only when a
// staff service and non-overlapping staff origins are configured.
const (
	secureStaffCookieName   = "__Host-sweetshop_staff"
	insecureStaffCookieName = "sweetshop_staff"
	// The customer prefix is "sweetshop/csrf/v1\x00", so neither token can
	// validate in the other namespace.
	staffCSRFDerivationPrefix = "sweetshop/staff-csrf/v1\x00"
	expectedStaffHeader       = "X-Expected-Staff-ID"
	authenticatedStaffHeader  = "X-Authenticated-Staff-ID"
	staffTransport            = "staff-cookie"

	staffKey contextKey = "staff"
)

// errAuthenticatedStaffChanged rejects a staff request whose
// X-Expected-Staff-ID does not name the authenticated staff member.
var errAuthenticatedStaffChanged = errors.New("authenticated staff differs from the expected staff")

// StaffConfig is the staff browser-session policy. TrustedOrigins must be
// normalized with ParseTrustedOrigins and must not overlap the customer
// origins; staff routes are not registered otherwise.
type StaffConfig struct {
	TrustedOrigins []string
}

// WithStaff enables the staff session API. The cookie's Secure attribute
// follows SessionConfig.Secure.
func WithStaff(auth *application.StaffAuth, config StaffConfig) Option {
	return func(s *Server) {
		s.staff = auth
		s.staffOrigins = map[string]bool{}
		for _, origin := range config.TrustedOrigins {
			if normalized, err := ParseOrigin(origin, false); err == nil && normalized == origin {
				s.staffOrigins[origin] = true
			} else {
				s.logger.Error("ignoring staff origin that is not normalized")
			}
		}
	}
}

// staffEnabled fails closed: no service, no origins or any origin shared
// with customers leaves the staff routes unregistered.
func (s *Server) staffEnabled() bool {
	if s.staff == nil || len(s.staffOrigins) == 0 {
		return false
	}
	for origin := range s.staffOrigins {
		if s.trustedOrigins[origin] {
			s.logger.Error("staff origins overlap customer origins; staff routes disabled")
			return false
		}
	}
	s.staffCookieName = insecureStaffCookieName
	if s.secureCookie {
		s.staffCookieName = secureStaffCookieName
	}
	return true
}

func (s *Server) registerStaffRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/v1/auth/session", s.staffStartSession)
	mux.HandleFunc("GET /api/admin/v1/auth/session", s.staffSessionState)
	mux.HandleFunc("DELETE /api/admin/v1/auth/session", s.staffEndSession)
	if s.products != nil {
		s.registerStaffProductRoutes(mux)
	}
}

func staffCSRFToken(sessionToken string) string {
	sum := sha256.Sum256([]byte(staffCSRFDerivationPrefix + sessionToken))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// staffCookie returns the staff cookie only; several are an error.
func (s *Server) staffCookie(r *http.Request) (string, bool, error) {
	value, found := "", 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == s.staffCookieName {
			value = cookie.Value
			found++
		}
	}
	if found > 1 {
		return "", true, domain.ErrUnauthorized
	}
	return value, found == 1, nil
}

func (s *Server) setStaffCookie(w http.ResponseWriter, token string, expires time.Time) {
	maxAge := max(1, int(time.Until(expires).Seconds()))
	http.SetCookie(w, &http.Cookie{Name: s.staffCookieName, Value: token, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
}

func (s *Server) clearStaffCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.staffCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
}

// checkStaffCSRF requires a staff Origin and the token derived from the
// presented staff cookie, before any session lookup.
func (s *Server) checkStaffCSRF(r *http.Request, sessionToken string) string {
	if reason := checkOriginIn(r, s.staffOrigins); reason != "" {
		return reason
	}
	values := r.Header.Values(csrfHeader)
	if len(values) != 1 || values[0] == "" {
		return "missing csrf token"
	}
	if subtle.ConstantTimeCompare([]byte(values[0]), []byte(staffCSRFToken(sessionToken))) != 1 {
		return "invalid csrf token"
	}
	return ""
}

// rejectBearer refuses any Authorization header: staff use only their cookie,
// and a customer bearer token must never reach staff code.
func (s *Server) rejectBearer(w http.ResponseWriter, r *http.Request) bool {
	if len(r.Header.Values("Authorization")) == 0 {
		return false
	}
	s.writeError(w, r, domain.ErrUnauthorized)
	return true
}

func (s *Server) logStaffAuthFailure(r *http.Request, err error) {
	if errors.Is(err, domain.ErrStaffDisabled) {
		s.logger.Warn("disabled staff refused", "path", r.URL.Path, "requestId", r.Context().Value(requestIDKey))
	}
}

// bindStaff is the staff counterpart of bindAuthenticatedUser with its own
// headers; X-Expected-Authenticated-User-ID has no meaning here.
func (s *Server) bindStaff(w http.ResponseWriter, r *http.Request, staffID string) bool {
	w.Header().Set(authenticatedStaffHeader, staffID)
	if expected := r.Header.Values(expectedStaffHeader); len(expected) > 0 &&
		(len(expected) != 1 || expected[0] == "" || expected[0] != staffID) {
		s.logger.Warn("expected staff precondition failed", "method", r.Method, "path", r.URL.Path, "requestId", r.Context().Value(requestIDKey))
		s.writeError(w, r, errAuthenticatedStaffChanged)
		return false
	}
	return true
}

type staffSessionResponse struct {
	Staff     domain.StaffUser `json:"staff"`
	ExpiresAt time.Time        `json:"expiresAt"`
	CSRFToken string           `json:"csrfToken"`
}

type staffSessionStateResponse struct {
	Authenticated bool              `json:"authenticated"`
	Staff         *domain.StaffUser `json:"staff,omitempty"`
	ExpiresAt     *time.Time        `json:"expiresAt,omitempty"`
	CSRFToken     string            `json:"csrfToken,omitempty"`
}

// staffStartSession is the staff login: a staff Origin is required (login
// CSRF), a fresh token is always minted, a presented staff session is
// replaced, and the token never appears in the body.
func (s *Server) staffStartSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if reason := checkOriginIn(r, s.staffOrigins); reason != "" {
		s.rejectCSRF(w, r, reason)
		return
	}
	if s.rejectBearer(w, r) {
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
	previous, _, err := s.staffCookie(r)
	if err != nil {
		previous = ""
	}
	result, err := s.staff.Login(r.Context(), input.Email, input.Password, previous)
	if err != nil {
		s.logStaffAuthFailure(r, err)
		s.writeError(w, r, err)
		return
	}
	s.setStaffCookie(w, result.Token, result.ExpiresAt)
	setAuthTransport(r, staffTransport)
	s.logger.Info("staff signed in", "staffId", result.Staff.ID, "requestId", r.Context().Value(requestIDKey))
	writeJSON(w, http.StatusOK, staffSessionResponse{Staff: result.Staff, ExpiresAt: result.ExpiresAt, CSRFToken: staffCSRFToken(result.Token)})
}

// staffSessionState reports the staff cookie session only.
func (s *Server) staffSessionState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.rejectBearer(w, r) {
		return
	}
	state := staffSessionStateResponse{}
	token, has, err := s.staffCookie(r)
	if err != nil || !has {
		writeJSON(w, http.StatusOK, state)
		return
	}
	record, err := s.staff.Authenticate(r.Context(), token)
	if err != nil {
		if !errors.Is(err, domain.ErrUnauthorized) {
			s.writeError(w, r, err)
			return
		}
		s.clearStaffCookie(w)
		writeJSON(w, http.StatusOK, state)
		return
	}
	setAuthTransport(r, staffTransport)
	w.Header().Set(authenticatedStaffHeader, record.Staff.ID)
	state.Authenticated, state.Staff, state.ExpiresAt, state.CSRFToken = true, &record.Staff, &record.ExpiresAt, staffCSRFToken(token)
	writeJSON(w, http.StatusOK, state)
}

// staffEndSession revokes the presented staff session after staff Origin and
// CSRF checks. A caller sending X-Expected-Staff-ID opts into authentication
// and owner binding, so a stale tab cannot sign out a different staff member.
func (s *Server) staffEndSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.rejectBearer(w, r) {
		return
	}
	token, has, err := s.staffCookie(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if has {
		if reason := s.checkStaffCSRF(r, token); reason != "" {
			s.rejectCSRF(w, r, reason)
			return
		}
	}
	if len(r.Header.Values(expectedStaffHeader)) > 0 {
		record, err := s.staff.Authenticate(r.Context(), token)
		if err != nil {
			s.logStaffAuthFailure(r, err)
			s.writeError(w, r, err)
			return
		}
		setAuthTransport(r, staffTransport)
		if !s.bindStaff(w, r, record.Staff.ID) {
			return
		}
	}
	if has {
		if err := s.staff.Logout(r.Context(), token); err != nil {
			s.writeError(w, r, err)
			return
		}
		setAuthTransport(r, staffTransport)
	}
	s.clearStaffCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// staffAuth resolves the staff principal for staff routes: the staff cookie
// only (never Authorization or the customer cookie), staff CSRF on unsafe
// methods, and an active, unexpired session.
func (s *Server) staffAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if s.rejectBearer(w, r) {
			return
		}
		token, has, err := s.staffCookie(r)
		if err != nil || !has {
			s.writeError(w, r, domain.ErrUnauthorized)
			return
		}
		if unsafeMethod(r.Method) {
			if reason := s.checkStaffCSRF(r, token); reason != "" {
				s.rejectCSRF(w, r, reason)
				return
			}
		}
		record, err := s.staff.Authenticate(r.Context(), token)
		if err != nil {
			s.logStaffAuthFailure(r, err)
			if errors.Is(err, domain.ErrUnauthorized) {
				s.clearStaffCookie(w)
			}
			s.writeError(w, r, err)
			return
		}
		setAuthTransport(r, staffTransport)
		if !s.bindStaff(w, r, record.Staff.ID) {
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), staffKey, record.Staff)))
	})
}

// currentStaff returns the principal staffAuth stored. It cannot return a
// customer: the key and the type differ from currentUser's.
func currentStaff(r *http.Request) (domain.StaffUser, bool) {
	staff, ok := r.Context().Value(staffKey).(domain.StaffUser)
	return staff, ok
}

// staffMutation starts every audited staff write behind staffAuth: the
// authenticated principal, authorized for action, with this request's ID.
// The actor never comes from the request body.
func (s *Server) staffMutation(r *http.Request, action domain.AuditAction) (domain.StaffMutation, error) {
	staff, ok := currentStaff(r)
	if !ok {
		return domain.StaffMutation{}, domain.ErrUnauthorized
	}
	requestID, _ := r.Context().Value(requestIDKey).(string)
	return s.staff.AuthorizeMutation(staff, action, requestID)
}
