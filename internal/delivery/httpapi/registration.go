package httpapi

import (
	"net/http"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// WithSelfService enables customer self-registration. The server passes it
// only with PostgreSQL storage and a running email dispatcher, so the routes,
// and selfService=true in GET /api/auth/session, exist only when a
// confirmation email can actually be sent.
func WithSelfService(registrations *application.CustomerRegistrations) Option {
	return func(s *Server) { s.registrations = registrations }
}

func (s *Server) selfServiceEnabled() bool { return s.registrations != nil }

func (s *Server) registerSelfServiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/registration", s.startRegistration)
	mux.HandleFunc("POST /api/auth/registration/resend", s.resendRegistration)
	mux.HandleFunc("POST /api/auth/registration/confirm", s.confirmRegistration)
}

// registrationAccepted is the only success answer of registration and
// resend, whatever happened to the address.
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
