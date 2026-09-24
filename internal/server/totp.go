package server

import (
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

const totpIssuer = "Tunploy"

type totpSetupResponse struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

type totpEnableRequest struct {
	Secret   string `json:"secret"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

type totpDisableRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

// Nothing is stored yet: the secret only sticks once a code from it comes back.
func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) error {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		return httpx.Unauthorized("not signed in")
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, totpSetupResponse{
		Secret: secret,
		URI:    auth.TOTPURI(totpIssuer, id.Email, secret),
	})
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) error {
	var req totpEnableRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	// The password stops a borrowed session from locking the owner out.
	user, key, err := s.reauthenticate(w, r, "password", req.Password)
	if err != nil {
		return err
	}
	if user.TOTPSecret != "" {
		return httpx.Conflict("two-factor authentication is already on")
	}

	if !auth.ValidTOTPSecret(req.Secret) {
		return httpx.BadRequest("secret is not one this panel generated")
	}
	step, ok := auth.VerifyTOTP(req.Secret, req.Code, time.Now(), 0)
	if !ok {
		s.loginThrottle.Fail(key)
		return httpx.Invalid(map[string]string{"code": "code is incorrect; check that your phone's clock is right"})
	}
	s.loginThrottle.Reset(key)

	if err := s.store.EnableTOTP(r.Context(), user.ID, req.Secret, step); err != nil {
		return err
	}
	user.TOTPSecret, user.TOTPLastStep = req.Secret, step
	if err := s.restartSessions(w, r, user); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "auth.totp_enabled", IP: clientIP(r)})
	return httpx.JSON(w, http.StatusOK, newUserResponse(user))
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) error {
	var req totpDisableRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	user, key, err := s.reauthenticate(w, r, "password", req.Password)
	if err != nil {
		return err
	}
	if user.TOTPSecret == "" {
		return httpx.Conflict("two-factor authentication is already off")
	}

	ok, err := s.checkTOTP(r.Context(), user, req.Code)
	if err != nil {
		return err
	}
	if !ok {
		s.loginThrottle.Fail(key)
		return httpx.Invalid(map[string]string{"code": "code is incorrect"})
	}
	s.loginThrottle.Reset(key)

	if err := s.store.DisableTOTP(r.Context(), user.ID); err != nil {
		return err
	}
	user.TOTPSecret, user.TOTPLastStep = "", 0
	s.record(r.Context(), store.Event{Kind: "auth.totp_disabled", IP: clientIP(r)})
	return httpx.JSON(w, http.StatusOK, newUserResponse(user))
}
