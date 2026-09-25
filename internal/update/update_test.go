package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/store"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"1.2.0", "1.1.9", true},
		{"v1.10.0", "1.9.0", true},
		{"2.0.0", "1.99.99", true},
		{"1.2.0", "1.2.0", false},
		{"1.1.0", "1.2.0", false},
		{"1.2.0", "1.2.0-rc.1", true},
		{"1.2.0-rc.2", "1.2.0-rc.1", true},
		{"1.2.0-rc.1", "1.2.0", false},
		{"1.2.0", "dev", false},
		{"1.2.0", "edge", false},
		{"latest", "1.0.0", false},
	} {
		if got := newer(tc.latest, tc.current); got != tc.want {
			t.Errorf("newer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestRepository(t *testing.T) {
	for in, want := range map[string]string{
		"ghcr.io/kwa0x2/tunploy:latest":       "ghcr.io/kwa0x2/tunploy",
		"ghcr.io/kwa0x2/tunploy":              "ghcr.io/kwa0x2/tunploy",
		"ghcr.io/kwa0x2/tunploy@sha256:abc":   "ghcr.io/kwa0x2/tunploy",
		"localhost:5000/tunploy:1.0.0":        "localhost:5000/tunploy",
		"localhost:5000/tunploy":              "localhost:5000/tunploy",
		"ghcr.io/kwa0x2/tunploy:1.0@sha256:a": "ghcr.io/kwa0x2/tunploy",
	} {
		if got := Repository(in); got != want {
			t.Errorf("Repository(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoOf(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/someone/tunploy":     "someone/tunploy",
		"https://github.com/someone/tunploy.git": "someone/tunploy",
		"https://gitlab.com/someone/tunploy":     DefaultRepo,
		"":                                       DefaultRepo,
	} {
		if got := repoOf(in); got != want {
			t.Errorf("repoOf(%q) = %q, want %q", in, got, want)
		}
	}
}

type fakeDocker struct {
	self    docker.Self
	selfErr error
	pullErr error
	// gone makes the updater vanish as soon as it starts.
	gone bool
}

func (f *fakeDocker) FindSelf(ctx context.Context) (docker.Self, error) { return f.self, f.selfErr }
func (f *fakeDocker) PullImage(ctx context.Context, ref string) error   { return f.pullErr }
func (f *fakeDocker) StartUpdater(ctx context.Context, self docker.Self, image string, cmd []string) error {
	return nil
}

func (f *fakeDocker) Running(ctx context.Context, id string) (bool, int, error) {
	if f.gone {
		return false, 0, docker.ErrNotFound
	}
	return true, 0, nil
}

func github(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestCheck(t *testing.T) {
	ctx := context.Background()

	s := New("1.0.0", t.TempDir(), &fakeDocker{}, false)
	s.API = github(t, http.StatusNotFound, `{"message":"Not Found"}`)
	st, err := s.Check(ctx)
	if err != nil || st.Latest != nil || st.Available || st.CheckedAt == nil {
		t.Fatalf("no releases yet: %+v, %v", st, err)
	}

	s.API = github(t, http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
	st, err = s.Check(ctx)
	if err == nil || !strings.Contains(st.CheckError, "rate limit") {
		t.Fatalf("rate limited: %+v, %v", st, err)
	}

	s.API = github(t, http.StatusOK, `{"tag_name":"v1.3.0","html_url":"https://example.com/r"}`)
	st, err = s.Check(ctx)
	if err != nil || !st.Available || st.Latest.Version != "1.3.0" || st.CheckError != "" {
		t.Fatalf("new release: %+v, %v", st, err)
	}
}

func TestUnsupported(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		current string
		dk      *fakeDocker
		want    string
	}{
		{"dev build", "dev", &fakeDocker{}, "not a release"},
		{"no container", "1.0.0", &fakeDocker{selfErr: docker.ErrNotFound}, "install command"},
		{"compose", "1.0.0", &fakeDocker{self: docker.Self{Compose: "tunploy"}}, "docker compose pull"},
		{"supported", "1.0.0", &fakeDocker{}, ""},
	} {
		st := New(tc.current, t.TempDir(), tc.dk, false).Status(ctx)
		if tc.want == "" && st.Unsupported != "" || !strings.Contains(st.Unsupported, tc.want) {
			t.Errorf("%s: unsupported = %q", tc.name, st.Unsupported)
		}
	}
}

func TestFailedPullIsReported(t *testing.T) {
	ctx := context.Background()
	s := New("1.0.0", t.TempDir(), &fakeDocker{pullErr: errors.New("manifest unknown")}, false)
	s.API = github(t, http.StatusOK, `{"tag_name":"v1.1.0"}`)
	var events []store.Event
	done := make(chan struct{})
	s.OnEvent(func(ctx context.Context, e store.Event) {
		events = append(events, e)
		close(done)
	})

	if _, err := s.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no failure event")
	}
	st := s.Status(ctx)
	if st.Updating != "" || !strings.Contains(st.Error, "manifest unknown") {
		t.Fatalf("status = %+v", st)
	}
	if events[0].Kind != "panel.update_failed" || events[0].Detail != "1.1.0" {
		t.Fatalf("events = %+v", events)
	}
}

func TestVanishedUpdaterIsReported(t *testing.T) {
	ctx := context.Background()
	s := New("1.0.0", t.TempDir(), &fakeDocker{gone: true}, false)
	s.API = github(t, http.StatusOK, `{"tag_name":"v1.1.0"}`)
	done := make(chan struct{})
	s.OnEvent(func(ctx context.Context, e store.Event) { close(done) })

	if _, err := s.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no failure event")
	}
	if st := s.Status(ctx); st.Updating != "" || !strings.Contains(st.Error, "updater stopped") {
		t.Fatalf("status = %+v", st)
	}
}

func TestResultIsReportedOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	var kinds []string
	record := func(ctx context.Context, e store.Event) { kinds = append(kinds, e.Kind) }

	if err := WriteResult(dir, Result{From: "1.0.0", To: "1.1.0"}); err != nil {
		t.Fatal(err)
	}
	updated := New("1.1.0", dir, &fakeDocker{}, false)
	updated.OnEvent(record)
	updated.ReportLast(ctx)
	updated.ReportLast(ctx)
	if len(kinds) != 1 || kinds[0] != "panel.updated" {
		t.Fatalf("after success: %v", kinds)
	}

	kinds = nil
	if err := WriteResult(dir, Result{From: "1.0.0", To: "1.1.0", Error: "the new version stopped"}); err != nil {
		t.Fatal(err)
	}
	old := New("1.0.0", dir, &fakeDocker{}, false)
	old.OnEvent(record)
	old.ReportLast(ctx)
	if len(kinds) != 1 || kinds[0] != "panel.update_failed" {
		t.Fatalf("after rollback: %v", kinds)
	}
	if st := old.Status(ctx); st.Error != "the new version stopped" {
		t.Fatalf("status = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, resultName)); !os.IsNotExist(err) {
		t.Fatalf("result file left behind: %v", err)
	}
}
