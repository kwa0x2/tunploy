package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/docker"
)

type fakeDocker struct {
	info docker.Info
	err  error
}

func (f fakeDocker) Ping(context.Context) (docker.Info, error) { return f.info, f.err }

func (f fakeDocker) Daemon(context.Context) (docker.Daemon, error) {
	return docker.Daemon{ID: "local", Version: f.info.Version}, f.err
}

func loggedIn(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	createAdmin(t, s)
	rec := do(t, s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2",
	})
	return sessionCookieFrom(t, rec)
}

// createAdmin stands in for `tunploy admin create`, the only way to make one.
func createAdmin(t *testing.T, s *Server) {
	t.Helper()
	hash, err := auth.HashPassword("hunter2hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateFirstUser(context.Background(), "Kwa", "Admin@Example.com", hash); err != nil {
		t.Fatalf("create admin: %v", err)
	}
}

func TestDockerStatus(t *testing.T) {
	s := newTestServerWithDocker(t, fakeDocker{info: docker.Info{Version: "29.4.0", APIVersion: "1.54"}})

	rec := do(t, s, "GET", "/api/system/docker", nil, loggedIn(t, s))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
	}
	var got dockerStatusResponse
	decode(t, rec, &got)
	if !got.Available || got.Info == nil || got.Info.Version != "29.4.0" || got.Error != "" {
		t.Fatalf("unexpected status: %+v", got)
	}
}

func TestDockerStatusUnavailable(t *testing.T) {
	s := newTestServerWithDocker(t, fakeDocker{
		err: fmt.Errorf("docker ping: %w: dial unix /var/run/docker.sock: permission denied", docker.ErrUnavailable),
	})

	rec := do(t, s, "GET", "/api/system/docker", nil, loggedIn(t, s))
	if rec.Code != http.StatusOK {
		t.Fatalf("an unreachable daemon is a state, not a failed request: got %d", rec.Code)
	}
	var got dockerStatusResponse
	decode(t, rec, &got)
	if got.Available || got.Info != nil || got.Error == "" {
		t.Fatalf("unexpected status: %+v", got)
	}
}
