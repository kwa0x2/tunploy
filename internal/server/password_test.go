package server

import (
	"net/http"
	"testing"
)

func TestChangePassword(t *testing.T) {
	s := newTestServer(t)
	cookie := loggedIn(t, s)

	other := do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2",
	})
	otherCookie := sessionCookieFrom(t, other)

	rec := do(t, s, "POST", "/api/auth/password", map[string]string{
		"current_password": "hunter2hunter2", "new_password": "correct horse battery",
	}, cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change password: want 204, got %d: %s", rec.Code, rec.Body)
	}
	fresh := sessionCookieFrom(t, rec)

	for name, c := range map[string]*http.Cookie{"old": cookie, "other": otherCookie} {
		if rec := do(t, s, "GET", "/api/auth/me", nil, c); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s session must be signed out, got %d", name, rec.Code)
		}
	}
	if rec := do(t, s, "GET", "/api/auth/me", nil, fresh); rec.Code != http.StatusOK {
		t.Fatalf("the session that changed the password must continue, got %d", rec.Code)
	}

	for pw, want := range map[string]int{
		"hunter2hunter2":        http.StatusUnauthorized,
		"correct horse battery": http.StatusOK,
	} {
		rec := do(t, s, "POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": pw})
		if rec.Code != want {
			t.Errorf("login with %q: want %d, got %d", pw, want, rec.Code)
		}
	}
}

func TestChangePasswordRejectsWrongCurrent(t *testing.T) {
	s := newTestServer(t)
	cookie := loggedIn(t, s)

	rec := do(t, s, "POST", "/api/auth/password", map[string]string{
		"current_password": "wrong password", "new_password": "correct horse battery",
	}, cookie)
	var e apiError
	decode(t, rec, &e)
	if rec.Code != http.StatusUnprocessableEntity || e.Error.Fields["current_password"] == "" {
		t.Fatalf("want a current_password field error, got %d: %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "GET", "/api/auth/me", nil, cookie); rec.Code != http.StatusOK {
		t.Fatal("a failed change must not sign the user out")
	}

	rec = do(t, s, "POST", "/api/auth/password", map[string]string{
		"current_password": "hunter2hunter2", "new_password": "short",
	}, cookie)
	decode(t, rec, &e)
	if e.Error.Fields["new_password"] == "" {
		t.Fatalf("want a new_password field error, got %s", rec.Body)
	}
}

func TestChangePasswordIsThrottled(t *testing.T) {
	s := newTestServer(t)
	cookie := loggedIn(t, s)

	for range loginMaxAttempts {
		do(t, s, "POST", "/api/auth/password", map[string]string{
			"current_password": "wrong password", "new_password": "correct horse battery",
		}, cookie)
	}
	rec := do(t, s, "POST", "/api/auth/password", map[string]string{
		"current_password": "hunter2hunter2", "new_password": "correct horse battery",
	}, cookie)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 after repeated failures, got %d", rec.Code)
	}
}
