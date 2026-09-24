package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithDocker(t, fakeDocker{})
}

func newTestServerWithDocker(t *testing.T, dk Docker) *Server {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	return New(config.Config{SessionTTL: time.Hour}, st, dk)
}

func do(t *testing.T, s *Server, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func sessionCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatal("response carries no session cookie")
	return nil
}

func TestSetupAndLoginFlow(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/setup", nil)
	var status struct {
		SetupRequired bool `json:"setup_required"`
	}
	decode(t, rec, &status)
	if !status.SetupRequired {
		t.Fatal("a fresh panel must report setup_required")
	}

	creds := map[string]string{"email": "Admin@Example.com", "password": "hunter2hunter2"}
	signup := map[string]string{"name": "Kwa", "email": "Admin@Example.com", "password": "hunter2hunter2"}
	rec = do(t, s, "POST", "/api/setup", signup)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: want 201, got %d: %s", rec.Code, rec.Body)
	}

	var created userResponse
	decode(t, rec, &created)
	if created.Email != "admin@example.com" {
		t.Fatalf("email must be normalised, got %q", created.Email)
	}

	cookie := sessionCookieFrom(t, rec)
	if !cookie.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Error("session cookie must be SameSite=Lax")
	}

	rec = do(t, s, "GET", "/api/setup", nil)
	decode(t, rec, &status)
	if status.SetupRequired {
		t.Fatal("setup_required must be false once an admin exists")
	}

	rec = do(t, s, "POST", "/api/setup", map[string]string{
		"name": "Attacker", "email": "attacker@example.com", "password": "hunter2hunter2",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("second setup: want 409, got %d", rec.Code)
	}

	rec = do(t, s, "GET", "/api/auth/me", nil, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("me with session: want 200, got %d: %s", rec.Code, rec.Body)
	}

	rec = do(t, s, "POST", "/api/auth/logout", nil, cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: want 204, got %d", rec.Code)
	}

	rec = do(t, s, "GET", "/api/auth/me", nil, cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: want 401, got %d", rec.Code)
	}

	rec = do(t, s, "POST", "/api/auth/login", creds)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d: %s", rec.Code, rec.Body)
	}

	rec = do(t, s, "GET", "/api/auth/me", nil, sessionCookieFrom(t, rec))
	if rec.Code != http.StatusOK {
		t.Fatalf("me after login: want 200, got %d", rec.Code)
	}
}

func TestProtectedRoutesRejectAnonymous(t *testing.T) {
	s := newTestServer(t)

	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/auth/me"},
		{"POST", "/api/auth/logout"},
		{"GET", "/api/system/docker"},
		{"GET", "/api/something-unknown"},
	} {
		rec := do(t, s, tc.method, tc.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: want 401, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	s := newTestServer(t)
	do(t, s, "POST", "/api/setup", map[string]string{
		"name": "Kwa", "email": "admin@example.com", "password": "hunter2hunter2",
	})

	rec := do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "wrong-password",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}

	rec = do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "nobody@example.com", "password": "hunter2hunter2",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown account must answer 401 like a wrong password, got %d", rec.Code)
	}
}

func TestLoginThrottleBlocksBruteForce(t *testing.T) {
	s := newTestServer(t)
	do(t, s, "POST", "/api/setup", map[string]string{
		"name": "Kwa", "email": "admin@example.com", "password": "hunter2hunter2",
	})

	wrong := map[string]string{"email": "admin@example.com", "password": "wrong-password"}
	for i := range loginMaxAttempts {
		if rec := do(t, s, "POST", "/api/auth/login", wrong); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: want 401, got %d", i+1, rec.Code)
		}
	}

	rec := do(t, s, "POST", "/api/auth/login", wrong)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 after %d failures, got %d", loginMaxAttempts, rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 must carry a Retry-After header")
	}

	// The throttle must not be bypassable by suddenly using the right password.
	rec = do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2",
	})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("correct password during a block: want 429, got %d", rec.Code)
	}
}

func TestSetupValidatesInput(t *testing.T) {
	for name, creds := range map[string]map[string]string{
		"bad email":      {"email": "not-an-email", "password": "hunter2hunter2"},
		"empty email":    {"email": "", "password": "hunter2hunter2"},
		"short password": {"email": "admin@example.com", "password": "short"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t)
			rec := do(t, s, "POST", "/api/setup", creds)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("want 422, got %d: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/setup", map[string]string{
		"name": "Kwa", "email": "admin@example.com", "password": "hunter2hunter2", "role": "admin",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for an unknown field, got %d", rec.Code)
	}
}

func TestHealthIsPublic(t *testing.T) {
	s := newTestServer(t)
	if rec := do(t, s, "GET", "/api/health", nil); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

// Two people hitting setup at the same moment must not both become admin:
// this is the flaw that a count-then-insert would leave open.
func TestConcurrentSetupCreatesOneAdmin(t *testing.T) {
	s := newTestServer(t)

	const attempts = 8
	var wg sync.WaitGroup
	codes := make([]int, attempts)

	start := make(chan struct{})
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec := do(t, s, "POST", "/api/setup", map[string]string{
				"name":     fmt.Sprintf("Admin %d", i),
				"email":    fmt.Sprintf("admin%d@example.com", i),
				"password": "hunter2hunter2",
			})
			codes[i] = rec.Code
		}()
	}
	close(start)
	wg.Wait()

	created := 0
	for i, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("request %d: want 201 or 409, got %d", i, code)
		}
	}
	if created != 1 {
		t.Fatalf("want exactly one admin created, got %d", created)
	}

	n, err := s.store.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 user in the database, got %d", n)
	}
}

func TestSetupRequiresName(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/setup", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 without a name, got %d: %s", rec.Code, rec.Body)
	}
}
