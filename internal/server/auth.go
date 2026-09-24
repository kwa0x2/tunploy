package server

import (
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	sessionCookie     = "tunploy_session"
	minPasswordLength = 8
	maxPasswordLength = 256
	maxNameLength     = 80
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registration struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func newUserResponse(u *store.User) userResponse {
	return userResponse{ID: u.ID, Name: u.Name, Email: u.Email, CreatedAt: u.CreatedAt}
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) error {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]bool{"setup_required": n == 0})
}

// handleSetup creates the first administrator. It stays open only while the
// panel has no users at all.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) error {
	var req registration
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := validateRegistration(req); err != nil {
		return err
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return err
	}

	user, err := s.store.CreateFirstUser(r.Context(), strings.TrimSpace(req.Name), req.Email, hash)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return httpx.Conflict("setup has already been completed")
		}
		return err
	}

	if err := s.startSession(w, r, user); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, newUserResponse(user))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var req credentials
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	email := store.NormalizeEmail(req.Email)
	key := email + "|" + clientIP(r)
	if ok, retryIn := s.loginThrottle.Allowed(key); !ok {
		w.Header().Set("Retry-After", retryAfterSeconds(retryIn))
		return httpx.Errorf(http.StatusTooManyRequests, "too_many_attempts",
			"too many failed attempts, try again in %s", retryIn.Round(time.Second))
	}

	user, err := s.store.UserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Spend the same work as a real check so timing does not reveal
			// whether the account exists.
			auth.VerifyPassword(auth.DummyHash, req.Password)
			s.loginThrottle.Fail(key)
			return httpx.Unauthorized("email or password is incorrect")
		}
		return err
	}

	if err := auth.VerifyPassword(user.PasswordHash, req.Password); err != nil {
		if errors.Is(err, auth.ErrPasswordMismatch) {
			s.loginThrottle.Fail(key)
			return httpx.Unauthorized("email or password is incorrect")
		}
		return err
	}

	s.loginThrottle.Reset(key)
	if err := s.startSession(w, r, user); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, newUserResponse(user))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if id, ok := auth.IdentityFrom(r.Context()); ok {
		if err := s.store.DeleteSession(r.Context(), id.TokenHash); err != nil {
			return err
		}
	}
	s.clearSessionCookie(w)
	return httpx.NoContent(w)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) error {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		return httpx.Unauthorized("not signed in")
	}
	user, err := s.store.UserByID(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, newUserResponse(user))
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *store.User) error {
	token, hash, err := auth.NewSessionToken()
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(s.cfg.SessionTTL)
	userAgent := truncate(r.UserAgent(), 255)
	if err := s.store.CreateSession(r.Context(), hash, user.ID, userAgent, expiresAt); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(s.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// requireAuth rejects requests without a live session and attaches the caller
// to the request context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			httpx.WriteError(w, r, httpx.Unauthorized("not signed in"))
			return
		}

		hash := auth.HashToken(cookie.Value)
		user, err := s.store.UserBySessionToken(r.Context(), hash)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.clearSessionCookie(w)
				httpx.WriteError(w, r, httpx.Unauthorized("session is no longer valid"))
				return
			}
			httpx.WriteError(w, r, err)
			return
		}

		ctx := auth.WithIdentity(r.Context(), auth.Identity{
			UserID:    user.ID,
			Email:     user.Email,
			TokenHash: hash,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validateRegistration(r registration) error {
	fields := validateCredentialFields(credentials{Email: r.Email, Password: r.Password})

	switch name := strings.TrimSpace(r.Name); {
	case name == "":
		fields["name"] = "name is required"
	case len(name) > maxNameLength:
		fields["name"] = "name must be at most 80 characters"
	}

	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	return nil
}

func validateCredentialFields(c credentials) map[string]string {
	fields := map[string]string{}

	email := store.NormalizeEmail(c.Email)
	if email == "" {
		fields["email"] = "email is required"
	} else if _, err := mail.ParseAddress(email); err != nil {
		fields["email"] = "email is not a valid address"
	}

	switch {
	case c.Password == "":
		fields["password"] = "password is required"
	case len(c.Password) < minPasswordLength:
		fields["password"] = "password must be at least 8 characters"
	case len(c.Password) > maxPasswordLength:
		fields["password"] = "password must be at most 256 characters"
	}

	return fields
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func retryAfterSeconds(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
