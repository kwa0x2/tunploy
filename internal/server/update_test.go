package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/update"
)

type fakeSelf struct {
	mu      sync.Mutex
	pulled  []string
	updater []string
}

func (f *fakeSelf) FindSelf(ctx context.Context) (docker.Self, error) {
	return docker.Self{ID: "abc123", Name: "tunploy", Image: "ghcr.io/kwa0x2/tunploy:latest"}, nil
}

func (f *fakeSelf) PullImage(ctx context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulled = append(f.pulled, ref)
	return nil
}

func (f *fakeSelf) StartUpdater(ctx context.Context, self docker.Self, image string, cmd []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updater = cmd
	return nil
}

func (f *fakeSelf) Running(ctx context.Context, id string) (bool, int, error) { return true, 0, nil }
func (f *fakeSelf) RunHelper(ctx context.Context, name, image string, cmd, binds []string) error {
	return nil
}

type updateJSON struct {
	Current   string `json:"current"`
	Available bool   `json:"available"`
	Latest    *struct {
		Version string `json:"version"`
		URL     string `json:"url"`
	} `json:"latest"`
	Unsupported string `json:"unsupported"`
	Updating    string `json:"updating"`
}

func TestUpdateFlow(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/kwa0x2/tunploy/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.1.0","html_url":"https://github.com/kwa0x2/tunploy/releases/tag/v1.1.0"}`))
	}))
	defer gh.Close()

	p := newPanel(t)
	fk := &fakeSelf{}
	p.s.updates = newUpdateService(t, fk, gh.URL)

	var st updateJSON
	p.want(p.do("GET", "/api/system/update", nil), http.StatusOK, &st)
	if st.Current != "1.0.0" || st.Available || st.Latest != nil || st.Unsupported != "" {
		t.Fatalf("status before a check = %+v", st)
	}
	p.wantError(p.do("POST", "/api/system/update", nil), http.StatusConflict, "conflict")

	p.want(p.do("POST", "/api/system/update/check", nil), http.StatusOK, &st)
	if !st.Available || st.Latest == nil || st.Latest.Version != "1.1.0" {
		t.Fatalf("status after a check = %+v", st)
	}

	p.want(p.do("POST", "/api/system/update", nil), http.StatusAccepted, &st)
	if st.Updating != "1.1.0" {
		t.Fatalf("status once started = %+v", st)
	}
	p.wantError(p.do("POST", "/api/system/update", nil), http.StatusConflict, "conflict")

	deadline := time.Now().Add(5 * time.Second)
	for {
		fk.mu.Lock()
		cmd, pulled := fk.updater, slices.Clone(fk.pulled)
		fk.mu.Unlock()
		if cmd != nil {
			if !slices.Equal(pulled, []string{"ghcr.io/kwa0x2/tunploy:1.1.0"}) {
				t.Fatalf("pulled %v", pulled)
			}
			want := []string{"self-update", "--container", "abc123", "--image", "ghcr.io/kwa0x2/tunploy:1.1.0", "--from", "1.0.0", "--to", "1.1.0"}
			if !slices.Equal(cmd, want) {
				t.Fatalf("updater command = %v", cmd)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the updater was never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpdateNeedsSignIn(t *testing.T) {
	s := newTestServer(t)
	if rec := do(t, s, "POST", "/api/system/update", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func newUpdateService(t *testing.T, dk update.Docker, api string) *update.Service {
	t.Helper()
	up := update.New("1.0.0", t.TempDir(), dk, false)
	up.API = api
	return up
}
