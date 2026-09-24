package server

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

const sessionCookie = "tunploy_session"

type credentials struct {
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
			// Same work as a real check, so timing can't reveal the account.
			auth.VerifyPassword(auth.DummyHash, req.Password)
			s.loginThrottle.Fail(key)
			s.record(r.Context(), store.Event{Kind: "auth.login_failed", IP: clientIP(r), Detail: email})
			return httpx.Unauthorized("email or password is incorrect")
		}
		return err
	}

	if err := auth.VerifyPassword(user.PasswordHash, req.Password); err != nil {
		if errors.Is(err, auth.ErrPasswordMismatch) {
			s.loginThrottle.Fail(key)
			s.record(r.Context(), store.Event{Kind: "auth.login_failed", IP: clientIP(r), Detail: email})
			return httpx.Unauthorized("email or password is incorrect")
		}
		return err
	}

	s.loginThrottle.Reset(key)
	if err := s.startSession(w, r, user); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "auth.login", IP: clientIP(r)})
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

type passwordChange struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// A fresh token, so a stolen cookie dies with the old password.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) error {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		return httpx.Unauthorized("not signed in")
	}
	var req passwordChange
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if msg := auth.CheckPassword(req.NewPassword); msg != "" {
		return httpx.Invalid(map[string]string{"new_password": msg})
	}

	// Shares the login budget so a hijacked session can't guess freely.
	key := id.Email + "|" + clientIP(r)
	if ok, retryIn := s.loginThrottle.Allowed(key); !ok {
		w.Header().Set("Retry-After", retryAfterSeconds(retryIn))
		return httpx.Errorf(http.StatusTooManyRequests, "too_many_attempts",
			"too many failed attempts, try again in %s", retryIn.Round(time.Second))
	}

	user, err := s.store.UserByID(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	if err := auth.VerifyPassword(user.PasswordHash, req.CurrentPassword); err != nil {
		if errors.Is(err, auth.ErrPasswordMismatch) {
			s.loginThrottle.Fail(key)
			return httpx.Invalid(map[string]string{"current_password": "current password is incorrect"})
		}
		return err
	}
	s.loginThrottle.Reset(key)

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	if err := s.store.UpdateUserPassword(r.Context(), user.ID, hash); err != nil {
		return err
	}
	if err := s.store.DeleteUserSessions(r.Context(), user.ID); err != nil {
		return err
	}
	if err := s.startSession(w, r, user); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "auth.password_changed", IP: clientIP(r)})
	return httpx.NoContent(w)
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
