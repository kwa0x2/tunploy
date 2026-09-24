package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker/dockertest"
	"github.com/kwa0x2/tunploy/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithDocker(t, fakeDocker{})
}

func newTestServerWithDocker(t *testing.T, dk Docker) *Server {
	t.Helper()
	s, _ := newTestServerWithDeploy(t, dk, dockertest.New())
	return s
}

func newTestServerWithDeploy(t *testing.T, dk Docker, fk *dockertest.Fake) (*Server, *deploy.Manager) {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	mgr, err := deploy.New(st, fk, t.TempDir())
	if err != nil {
		t.Fatalf("new deploy manager: %v", err)
	}
	cfg := config.Config{SessionTTL: time.Hour, PublicHost: "vpn.example.com"}
	return New(cfg, st, dk, mgr), mgr
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

func TestSetupStatusAndLoginFlow(t *testing.T) {
	s := newTestServer(t)

	var status struct {
		SetupRequired bool `json:"setup_required"`
	}
	decode(t, do(t, s, "GET", "/api/setup", nil), &status)
	if !status.SetupRequired {
		t.Fatal("a panel without an admin must report setup_required")
	}

	createAdmin(t, s)
	decode(t, do(t, s, "GET", "/api/setup", nil), &status)
	if status.SetupRequired {
		t.Fatal("setup_required must be false once an admin exists")
	}

	rec := do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "ADMIN@example.com", "password": "hunter2hunter2",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d: %s", rec.Code, rec.Body)
	}
	var user userResponse
	decode(t, rec, &user)
	if user.Email != "admin@example.com" || user.Name != "Kwa" {
		t.Fatalf("user = %+v", user)
	}

	cookie := sessionCookieFrom(t, rec)
	if !cookie.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Error("session cookie must be SameSite=Lax")
	}

	if rec := do(t, s, "GET", "/api/auth/me", nil, cookie); rec.Code != http.StatusOK {
		t.Fatalf("me with session: want 200, got %d: %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", "/api/auth/logout", nil, cookie); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: want 204, got %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/auth/me", nil, cookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: want 401, got %d", rec.Code)
	}
}

// The admin is created on the server with the CLI. A sign-up form on an
// internet-facing panel would hand it to whoever finds it first.
func TestNoAccountCanBeCreatedOverHTTP(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/setup", map[string]string{
		"name": "Attacker", "email": "attacker@example.com", "password": "hunter2hunter2",
	})
	if rec.Code < 400 {
		t.Fatalf("POST /api/setup: want a rejection, got %d", rec.Code)
	}
	if n, _ := s.store.CountUsers(context.Background()); n != 0 {
		t.Fatalf("want no users, got %d", n)
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
	createAdmin(t, s)

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
	createAdmin(t, s)

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

func TestUnknownFieldsAreRejected(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2", "remember": "yes",
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
