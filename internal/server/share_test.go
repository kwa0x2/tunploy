package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type shareJSON struct {
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type sharedDeviceJSON struct {
	Name        string `json:"name"`
	Country     string `json:"country"`
	Status      string `json:"status"`
	DataLimit   int64  `json:"data_limit"`
	SpeedLimit  int64  `json:"speed_limit"`
	Config      string `json:"config"`
	FullTunnel  bool   `json:"full_tunnel"`
	PeriodUsage struct {
		TotalBytes int64 `json:"total_bytes"`
	} `json:"period_usage"`
}

// anon is a request from the device's owner, who has no session.
func (p *panel) anon(path string) *httptest.ResponseRecorder {
	p.t.Helper()
	return do(p.t, p.s, "GET", path, nil)
}

func tokenOf(t *testing.T, link shareJSON) string {
	t.Helper()
	_, token, ok := strings.Cut(link.URL, "/share/")
	if !ok || len(token) < 26 || !strings.HasPrefix(link.URL, "http://example.com/share/") {
		t.Fatalf("share url = %q", link.URL)
	}
	return token
}

func TestShareLink(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt", "country": "DE"})
	var peer peerJSON
	p.want(p.do("POST", fmt.Sprintf("/api/instances/%d/peers", in.ID), map[string]any{
		"name": "phone", "data_limit": 10 << 30, "speed_limit": 20000,
	}), http.StatusCreated, &peer)
	path := fmt.Sprintf("/api/instances/%d/peers/%d/share", in.ID, peer.ID)

	p.wantError(p.do("GET", path, nil), http.StatusNotFound, "not_found")

	var link shareJSON
	p.want(p.do("POST", path, nil), http.StatusCreated, &link)
	token := tokenOf(t, link)
	if link.ExpiresAt != nil {
		t.Fatalf("a link without an end date: %+v", link)
	}
	var again shareJSON
	p.want(p.do("GET", path, nil), http.StatusOK, &again)
	if again != link {
		t.Fatalf("get = %+v, want %+v", again, link)
	}

	res := p.anon("/api/share/" + token)
	if res.Code != http.StatusOK || res.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(res.Header().Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("share page: %d %v", res.Code, res.Header())
	}
	var d sharedDeviceJSON
	decode(t, res, &d)
	if d.Name != "phone" || d.Country != "DE" || d.Status != "active" || d.DataLimit != 10<<30 ||
		d.SpeedLimit != 20000 || !d.FullTunnel || !strings.Contains(d.Config, "PrivateKey = ") {
		t.Fatalf("shared device = %+v", d)
	}
	res = p.anon("/api/share/" + token + "/config")
	if res.Code != http.StatusOK || !strings.Contains(res.Header().Get("Content-Disposition"), `filename="phone.conf"`) {
		t.Fatalf("config: %d %v", res.Code, res.Header())
	}
	if res := p.anon("/share/" + token); res.Code != http.StatusOK || res.Header().Get("X-Robots-Tag") == "" {
		t.Fatalf("share page shell: %d %v", res.Code, res.Header())
	}

	// A new link is how a leaked one is replaced.
	until := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	p.want(p.do("POST", path, map[string]any{"expires_at": until}), http.StatusCreated, &link)
	if tokenOf(t, link) == token || link.ExpiresAt == nil || !link.ExpiresAt.Equal(until) {
		t.Fatalf("new link = %+v", link)
	}
	if res := p.anon("/api/share/" + token); res.Code != http.StatusNotFound {
		t.Fatalf("the old link still works: %d", res.Code)
	}
	token = tokenOf(t, link)

	e := p.wantError(p.do("POST", path, map[string]any{"expires_at": time.Now().Add(-time.Minute)}),
		http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["expires_at"] == "" {
		t.Fatalf("fields = %v", e.Error.Fields)
	}

	p.want(p.do("DELETE", path, nil), http.StatusNoContent, nil)
	p.want(p.do("DELETE", path, nil), http.StatusNoContent, nil)
	if res := p.anon("/api/share/" + token); res.Code != http.StatusNotFound {
		t.Fatalf("a removed link still works: %d", res.Code)
	}

	events, err := p.s.store.Events(t.Context(), store.EventFilter{Limit: 10, Kinds: []string{"device.shared", "device.unshared"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Kind != "device.unshared" || !strings.HasPrefix(events[1].Detail, "until ") {
		t.Fatalf("events = %+v", events)
	}
}

func TestExpiredShareLink(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt"})
	peer := p.createPeer(in.ID, "phone")
	past := time.Now().Add(-time.Second)
	if _, err := p.s.store.SetPeerShare(t.Context(), peer.ID, "EXPIREDTOKEN", &past); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/share/EXPIREDTOKEN", "/api/share/EXPIREDTOKEN/config", "/api/share/NOSUCHTOKEN"} {
		if res := p.anon(path); res.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, res.Code)
		}
	}
}

// The owner's page shows where the device is, how it stands and what it may
// use, but never the admin's name for the server.
func TestSharedDeviceView(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "customer-pool-3", "country": "NL", "client_allowed_ips": []string{"10.8.0.0/24"}})
	token := p.apiKey("billing", "devices:read", "devices:write")
	pub := wg.GeneratePrivateKey().PublicKey().String()

	var dev deviceJSON
	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{
		"server_id": in.ID, "name": "iPhone", "public_key": pub, "expires_at": time.Now().Add(-time.Hour),
	}), http.StatusCreated, &dev)

	var link shareJSON
	p.want(p.api(token, "POST", fmt.Sprintf("/api/v1/devices/%d/share", dev.ID), nil, "Idempotency-Key", "k1"),
		http.StatusCreated, &link)
	var replay shareJSON
	p.want(p.api(token, "POST", fmt.Sprintf("/api/v1/devices/%d/share", dev.ID), nil, "Idempotency-Key", "k1"),
		http.StatusCreated, &replay)
	if replay != link {
		t.Fatalf("a retried create made another link: %+v, %+v", replay, link)
	}
	p.want(p.api(token, "GET", fmt.Sprintf("/api/v1/devices/%d/share", dev.ID), nil), http.StatusOK, &replay)
	if replay != link {
		t.Fatalf("get = %+v, want %+v", replay, link)
	}

	res := p.anon("/api/share/" + tokenOf(t, link))
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "customer-pool-3") {
		t.Fatalf("share page: %d %s", res.Code, res.Body)
	}
	var d sharedDeviceJSON
	decode(t, res, &d)
	if d.Status != "expired" || d.Config != "" || d.FullTunnel {
		t.Fatalf("client-key device = %+v", d)
	}

	reader := p.apiKey("reader", "devices:read")
	p.wantError(p.api(reader, "POST", fmt.Sprintf("/api/v1/devices/%d/share", dev.ID), nil), http.StatusForbidden, "missing_scope")
	p.want(p.api(token, "DELETE", fmt.Sprintf("/api/v1/devices/%d/share", dev.ID), nil), http.StatusNoContent, nil)
	p.wantError(p.api(token, "GET", fmt.Sprintf("/api/v1/devices/%d/share", dev.ID), nil), http.StatusNotFound, "not_found")
}

func TestLogPathHidesShareTokens(t *testing.T) {
	for path, want := range map[string]string{
		"/share/ABC":             "/share/…",
		"/api/share/ABC":         "/api/share/…",
		"/api/share/ABC/config":  "/api/share/…/config",
		"/api/instances/1/peers": "/api/instances/1/peers",
		"/share/":                "/share/",
	} {
		if got := logPath(path); got != want {
			t.Errorf("logPath(%q) = %q, want %q", path, got, want)
		}
	}
}
