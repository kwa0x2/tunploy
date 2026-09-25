package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

const sessionCookie = "tunploy_session"

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

type userResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	TOTPEnabled bool      `json:"totp_enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func newUserResponse(u *store.User) userResponse {
	return userResponse{ID: u.ID, Name: u.Name, Email: u.Email, TOTPEnabled: u.TOTPSecret != "", CreatedAt: u.CreatedAt}
}

// Container names the panel's own, which differs under Compose or Dokploy,
// for the docker exec command the UI shows; only until an admin exists.
type setupStatus struct {
	SetupRequired bool   `json:"setup_required"`
	Container     string `json:"container,omitempty"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) error {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		return err
	}
	st := setupStatus{SetupRequired: n == 0}
	if st.SetupRequired {
		st.Container = s.updates.ContainerName(r.Context())
	}
	return httpx.JSON(w, http.StatusOK, st)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var req credentials
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	email := store.NormalizeEmail(req.Email)
	key := email + "|" + clientIP(r)
	if err := s.checkThrottle(w, key); err != nil {
		return err
	}
	failed := func(detail string) {
		s.loginThrottle.Fail(key)
		s.record(r.Context(), store.Event{Kind: "auth.login_failed", IP: clientIP(r), Detail: detail})
	}
	badPassword := httpx.Unauthorized("email or password is incorrect")

	user, err := s.store.UserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Same work as a real check, so timing can't reveal the account.
			auth.VerifyPassword(auth.DummyHash, req.Password)
			failed(email)
			return badPassword
		}
		return err
	}

	if err := auth.VerifyPassword(user.PasswordHash, req.Password); err != nil {
		if errors.Is(err, auth.ErrPasswordMismatch) {
			failed(email)
			return badPassword
		}
		return err
	}

	// Asked for only after the password, so it reveals nothing to a guesser.
	if user.TOTPSecret != "" {
		if strings.TrimSpace(req.Code) == "" {
			return httpx.Errorf(http.StatusUnauthorized, "totp_required", "enter the code from your authenticator app")
		}
		ok, err := s.checkTOTP(r.Context(), user, req.Code)
		if err != nil {
			return err
		}
		if !ok {
			failed(email + ", wrong authentication code")
			return httpx.Errorf(http.StatusUnauthorized, "totp_invalid", "the authentication code is incorrect")
		}
	}

	s.loginThrottle.Reset(key)
	if err := s.startSession(w, r, user); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "auth.login", IP: clientIP(r)})
	return httpx.JSON(w, http.StatusOK, newUserResponse(user))
}

func (s *Server) checkTOTP(ctx context.Context, user *store.User, code string) (bool, error) {
	step, ok := auth.VerifyTOTP(user.TOTPSecret, code, time.Now(), user.TOTPLastStep)
	if !ok {
		return false, nil
	}
	return s.store.ClaimTOTPStep(ctx, user.ID, step)
}

func (s *Server) checkThrottle(w http.ResponseWriter, key string) error {
	if ok, retryIn := s.loginThrottle.Allowed(key); !ok {
		w.Header().Set("Retry-After", retryAfterSeconds(retryIn))
		return httpx.Errorf(http.StatusTooManyRequests, "too_many_attempts",
			"too many failed attempts, try again in %s", retryIn.Round(time.Second))
	}
	return nil
}

// Shares the login budget, so a hijacked session can't guess the password freely.
func (s *Server) reauthenticate(w http.ResponseWriter, r *http.Request, field, password string) (*store.User, string, error) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		return nil, "", httpx.Unauthorized("not signed in")
	}
	key := id.Email + "|" + clientIP(r)
	if err := s.checkThrottle(w, key); err != nil {
		return nil, "", err
	}

	user, err := s.store.UserByID(r.Context(), id.UserID)
	if err != nil {
		return nil, "", err
	}
	if err := auth.VerifyPassword(user.PasswordHash, password); err != nil {
		if errors.Is(err, auth.ErrPasswordMismatch) {
			s.loginThrottle.Fail(key)
			return nil, "", httpx.Invalid(map[string]string{field: "current password is incorrect"})
		}
		return nil, "", err
	}
	return user, key, nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if id, ok := auth.IdentityFrom(r.Context()); ok {
		if err := s.store.DeleteSession(r.Context(), id.TokenHash); err != nil {
			return err
		}
	}
	s.clearSessionCookie(w, r)
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
	var req passwordChange
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if msg := auth.CheckPassword(req.NewPassword); msg != "" {
		return httpx.Invalid(map[string]string{"new_password": msg})
	}

	user, key, err := s.reauthenticate(w, r, "current_password", req.CurrentPassword)
	if err != nil {
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
	if err := s.restartSessions(w, r, user); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "auth.password_changed", IP: clientIP(r)})
	return httpx.NoContent(w)
}

// Signs out every other device and hands this one a fresh token.
func (s *Server) restartSessions(w http.ResponseWriter, r *http.Request, user *store.User) error {
	if err := s.store.DeleteUserSessions(r.Context(), user.ID); err != nil {
		return err
	}
	return s.startSession(w, r, user)
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
		Secure:   s.secureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies(r),
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
				s.clearSessionCookie(w, r)
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
