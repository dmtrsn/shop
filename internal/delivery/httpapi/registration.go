package httpapi

import (
	"net/http"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// WithSelfService enables customer self-service: registration with email
// confirmation and password recovery. The server passes it only with
// PostgreSQL storage and a running email dispatcher, so the routes, and
// selfService=true in GET /api/auth/session, exist only when their email can
// actually be sent. Both parts are required; one flag covers both.
func WithSelfService(registrations *application.CustomerRegistrations, resets *application.PasswordResets) Option {
	return func(s *Server) {
		if registrations != nil && resets != nil {
			s.registrations, s.resets = registrations, resets
		}
	}
}

func (s *Server) selfServiceEnabled() bool { return s.registrations != nil && s.resets != nil }

func (s *Server) registerSelfServiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/registration", s.startRegistration)
	mux.HandleFunc("POST /api/auth/registration/resend", s.resendRegistration)
	mux.HandleFunc("POST /api/auth/registration/confirm", s.confirmRegistration)
	mux.HandleFunc("POST /api/auth/password-reset", s.requestPasswordReset)
	mux.HandleFunc("POST /api/auth/password-reset/confirm", s.confirmPasswordReset)
}

// registrationAccepted is the only success answer of registration, resend
// and reset request, whatever happened to the address.
type registrationAccepted struct {
	Status string `json:"status"`
}

// startRegistration requires a trusted Origin, like the cookie login; there
// is no session yet, so no CSRF token. A valid request always gets 202.
func (s *Server) startRegistration(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if reason := s.checkOrigin(r); reason != "" {
		s.rejectCSRF(w, r, reason)
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
		Phone    string `json:"phone"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.registrations.Register(r.Context(), application.NewCustomerInput{Email: input.Email, Password: input.Password, Name: input.Name, Phone: input.Phone}); err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, registrationAccepted{Status: "pending"})
}

func (s *Server) resendRegistration(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if reason := s.checkOrigin(r); reason != "" {
		s.rejectCSRF(w, r, reason)
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.registrations.Resend(r.Context(), input.Email); err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, registrationAccepted{Status: "pending"})
}

// confirmRegistration creates the customer and signs it in with the same
// cookie, answer and session replacement as POST /api/auth/session.
func (s *Server) confirmRegistration(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if reason := s.checkOrigin(r); reason != "" {
		s.rejectCSRF(w, r, reason)
		return
	}
	var input struct {
		Token    string `json:"token"`
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
	result, err := s.registrations.Confirm(r.Context(), input.Token, input.Password, previous)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeCookieSession(w, r, result)
}

// writeCookieSession sets the session cookie and answers like a login; the
// token itself never appears in the body.
func (s *Server) writeCookieSession(w http.ResponseWriter, r *http.Request, result application.LoginResult) {
	s.setSessionCookie(w, result.Token, result.ExpiresAt)
	setAuthTransport(r, string(domain.SessionCookie))
	writeJSON(w, http.StatusOK, cookieSessionResponse{User: result.User, ExpiresAt: result.ExpiresAt, CSRFToken: csrfToken(result.Token)})
}

// requestPasswordReset answers every valid address with the same 202 as
// registration, whatever the account state.
func (s *Server) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if reason := s.checkOrigin(r); reason != "" {
		s.rejectCSRF(w, r, reason)
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.resets.Request(r.Context(), input.Email); err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, registrationAccepted{Status: "pending"})
}

// confirmPasswordReset sets the new password and revokes every session of
// the customer. It never signs in: on success it also ends the session this
// browser presented, whoever it belonged to, and clears the cookie, so the
// browser is signed out and the customer signs in again with the new
// password.
func (s *Server) confirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if reason := s.checkOrigin(r); reason != "" {
		s.rejectCSRF(w, r, reason)
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.resets.Reset(r.Context(), input.Token, input.Password); err != nil {
		s.writeError(w, r, err)
		return
	}
	if presented, found, err := s.sessionCookie(r); err == nil && found {
		if err := s.service.Logout(r.Context(), presented, domain.SessionCookie); err != nil {
			s.logger.Warn("could not end the presenting session after a password reset", "error", err, "requestId", r.Context().Value(requestIDKey))
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
