package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/auth"
)

func totpNow(t *testing.T, secret string, offset time.Duration) string {
	t.Helper()
	code, err := auth.TOTPCode(secret, time.Now().Add(offset))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func enableTOTP(t *testing.T, s *Server, cookie *http.Cookie) (string, *http.Cookie) {
	t.Helper()
	var setup totpSetupResponse
	decode(t, do(t, s, "POST", "/api/auth/totp/setup", nil, cookie), &setup)
	if setup.Secret == "" || setup.URI == "" {
		t.Fatalf("setup = %+v", setup)
	}

	rec := do(t, s, "POST", "/api/auth/totp/enable", map[string]string{
		"secret": setup.Secret, "code": totpNow(t, setup.Secret, 0), "password": "hunter2hunter2",
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: want 200, got %d: %s", rec.Code, rec.Body)
	}
	var user userResponse
	decode(t, rec, &user)
	if !user.TOTPEnabled {
		t.Fatal("enable must report totp_enabled")
	}
	return setup.Secret, sessionCookieFrom(t, rec)
}

func TestTOTPLogin(t *testing.T) {
	s := newTestServer(t)
	cookie := loggedIn(t, s)
	other := sessionCookieFrom(t, do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2",
	}))

	secret, fresh := enableTOTP(t, s, cookie)
	if rec := do(t, s, "GET", "/api/auth/me", nil, other); rec.Code != http.StatusUnauthorized {
		t.Fatal("turning on 2FA must sign out other sessions")
	}
	if rec := do(t, s, "GET", "/api/auth/me", nil, fresh); rec.Code != http.StatusOK {
		t.Fatal("the session that turned on 2FA must stay signed in")
	}

	login := func(code string) (*http.Cookie, apiError, int) {
		rec := do(t, s, "POST", "/api/auth/login", map[string]string{
			"email": "admin@example.com", "password": "hunter2hunter2", "code": code,
		})
		var e apiError
		if rec.Code != http.StatusOK {
			decode(t, rec, &e)
			return nil, e, rec.Code
		}
		return sessionCookieFrom(t, rec), e, rec.Code
	}

	if _, e, code := login(""); code != http.StatusUnauthorized || e.Error.Code != "totp_required" {
		t.Fatalf("no code: want 401 totp_required, got %d %s", code, e.Error.Code)
	}
	if _, e, code := login("000000"); code != http.StatusUnauthorized || e.Error.Code != "totp_invalid" {
		t.Fatalf("wrong code: want 401 totp_invalid, got %d %s", code, e.Error.Code)
	}
	if _, e, code := login(totpNow(t, secret, 0)); code != http.StatusUnauthorized || e.Error.Code != "totp_invalid" {
		t.Fatalf("the code used to enable 2FA must not work again, got %d %s", code, e.Error.Code)
	}

	next := totpNow(t, secret, 30*time.Second)
	if c, _, code := login(next); code != http.StatusOK || c == nil {
		t.Fatalf("next code: want 200, got %d", code)
	}
	if _, _, code := login(next); code != http.StatusUnauthorized {
		t.Fatal("a code must not be accepted twice")
	}
}

func TestTOTPEnableNeedsPasswordAndCode(t *testing.T) {
	s := newTestServer(t)
	cookie := loggedIn(t, s)

	var setup totpSetupResponse
	decode(t, do(t, s, "POST", "/api/auth/totp/setup", nil, cookie), &setup)

	for name, tc := range map[string]struct {
		body  map[string]string
		field string
	}{
		"wrong password": {map[string]string{"secret": setup.Secret, "code": totpNow(t, setup.Secret, 0), "password": "nope-nope"}, "password"},
		"wrong code":     {map[string]string{"secret": setup.Secret, "code": "12", "password": "hunter2hunter2"}, "code"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, s, "POST", "/api/auth/totp/enable", tc.body, cookie)
			var e apiError
			decode(t, rec, &e)
			if rec.Code != http.StatusUnprocessableEntity || e.Error.Fields[tc.field] == "" {
				t.Fatalf("want a %s field error, got %d: %s", tc.field, rec.Code, rec.Body)
			}
		})
	}

	rec := do(t, s, "POST", "/api/auth/totp/enable", map[string]string{
		"secret": "AAAA", "code": "123456", "password": "hunter2hunter2",
	}, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a short secret: want 400, got %d", rec.Code)
	}

	var me userResponse
	decode(t, do(t, s, "GET", "/api/auth/me", nil, cookie), &me)
	if me.TOTPEnabled {
		t.Fatal("failed attempts must leave 2FA off")
	}
}

func TestTOTPDisable(t *testing.T) {
	s := newTestServer(t)
	secret, cookie := enableTOTP(t, s, loggedIn(t, s))

	rec := do(t, s, "POST", "/api/auth/totp/disable", map[string]string{
		"password": "hunter2hunter2", "code": "000000",
	}, cookie)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong code: want 422, got %d", rec.Code)
	}

	rec = do(t, s, "POST", "/api/auth/totp/disable", map[string]string{
		"password": "hunter2hunter2", "code": totpNow(t, secret, 30*time.Second),
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: want 200, got %d: %s", rec.Code, rec.Body)
	}

	rec = do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login after disable must not ask for a code, got %d: %s", rec.Code, rec.Body)
	}
}
