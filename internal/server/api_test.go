package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type deviceJSON struct {
	ID         int64           `json:"id"`
	ServerID   int64           `json:"server_id"`
	Name       string          `json:"name"`
	ExternalID string          `json:"external_id"`
	Metadata   json.RawMessage `json:"metadata"`
	PublicKey  string          `json:"public_key"`
	ClientKey  bool            `json:"client_key"`
	Enabled    bool            `json:"enabled"`
	Status     string          `json:"status"`
	DataLimit  int64           `json:"data_limit"`
	ExpiresAt  *time.Time      `json:"expires_at"`
	SpeedLimit int64           `json:"speed_limit"`
	MonthUsage apiTraffic      `json:"month_usage"`
	Config     string          `json:"config"`
}

type pageJSON[T any] struct {
	Data    []T  `json:"data"`
	HasMore bool `json:"has_more"`
}

func (p *panel) apiKey(name string, scopes ...string) string {
	p.t.Helper()
	var k struct {
		Token  string   `json:"token"`
		Prefix string   `json:"prefix"`
		Scopes []string `json:"scopes"`
	}
	p.want(p.do("POST", "/api/api-keys", map[string]any{"name": name, "scopes": scopes}), http.StatusCreated, &k)
	if !strings.HasPrefix(k.Token, "tp_") || !strings.HasPrefix(k.Token, k.Prefix) {
		p.t.Fatalf("token %q with prefix %q", k.Token, k.Prefix)
	}
	return k.Token
}

func (p *panel) api(token, method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	p.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			p.t.Fatalf("encode request: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	p.s.ServeHTTP(rec, req)
	return rec
}

var allScopes = []string{"devices:read", "devices:write", "servers:read", "events:read"}

func TestAPIKeyAuth(t *testing.T) {
	p := newPanel(t)
	p.createInstance(map[string]any{"name": "Frankfurt"})
	readOnly := p.apiKey("reader", "servers:read")

	p.wantError(p.api("", "GET", "/api/v1/servers", nil), http.StatusUnauthorized, "unauthorized")
	p.wantError(p.api("tp_nope", "GET", "/api/v1/servers", nil), http.StatusUnauthorized, "unauthorized")
	// The panel's cookie is not an API credential.
	p.wantError(p.do("GET", "/api/v1/servers", nil), http.StatusUnauthorized, "unauthorized")

	var servers pageJSON[struct {
		Name        string `json:"name"`
		Capacity    int    `json:"capacity"`
		DeviceCount int    `json:"device_count"`
		Status      string `json:"status"`
		Node        struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"node"`
	}]
	p.want(p.api(readOnly, "GET", "/api/v1/servers", nil), http.StatusOK, &servers)
	if len(servers.Data) != 1 || servers.Data[0].Capacity != 253 || servers.Data[0].Status != "running" ||
		servers.Data[0].Node.Name != "This server" {
		t.Fatalf("servers = %+v", servers)
	}
	p.wantError(p.api(readOnly, "GET", "/api/v1/devices", nil), http.StatusForbidden, "missing_scope")
	p.wantError(p.api(readOnly, "GET", "/api/v1/nope", nil), http.StatusNotFound, "not_found")

	var keys []struct {
		ID         int64      `json:"id"`
		Name       string     `json:"name"`
		Token      string     `json:"token"`
		LastUsedAt *time.Time `json:"last_used_at"`
	}
	p.want(p.do("GET", "/api/api-keys", nil), http.StatusOK, &keys)
	if len(keys) != 1 || keys[0].Token != "" || keys[0].LastUsedAt == nil {
		t.Fatalf("keys = %+v, want one without its token and with a last use", keys)
	}
	p.want(p.do("DELETE", fmt.Sprintf("/api/api-keys/%d", keys[0].ID), nil), http.StatusNoContent, nil)
	p.wantError(p.api(readOnly, "GET", "/api/v1/servers", nil), http.StatusUnauthorized, "unauthorized")
}

func TestAPIKeyValidation(t *testing.T) {
	p := newPanel(t)
	e := p.wantError(p.do("POST", "/api/api-keys", map[string]any{"name": " ", "scopes": []string{"root"}}),
		http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["name"] == "" || !strings.Contains(e.Error.Fields["scopes"], "root") {
		t.Fatalf("fields = %v", e.Error.Fields)
	}
	p.wantError(p.do("POST", "/api/api-keys", map[string]any{
		"name": "old", "scopes": allScopes, "expires_at": time.Now().Add(-time.Hour),
	}), http.StatusUnprocessableEntity, "validation_failed")

	p.apiKey("billing", "devices:read")
	e = p.wantError(p.do("POST", "/api/api-keys", map[string]any{"name": "Billing", "scopes": allScopes}),
		http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["name"] == "" {
		t.Fatalf("duplicate name: %v", e.Error.Fields)
	}
}

func TestAPIKeyExpires(t *testing.T) {
	p := newPanel(t)
	token := p.apiKey("soon", "servers:read")
	k, err := p.s.store.APIKeyByToken(t.Context(), auth.HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Second)
	k.ExpiresAt = &past
	if err := p.s.store.DeleteAPIKey(t.Context(), k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.s.store.CreateAPIKey(t.Context(), *k); err != nil {
		t.Fatal(err)
	}
	p.wantError(p.api(token, "GET", "/api/v1/servers", nil), http.StatusUnauthorized, "unauthorized")
}

func TestAPIDeviceLifecycle(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt"})
	token := p.apiKey("billing", allScopes...)

	var d deviceJSON
	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{
		"server_id": in.ID, "external_id": "user_123", "metadata": map[string]any{"plan": "pro"},
		"data_limit": 50 << 30, "expires_at": time.Now().Add(30 * 24 * time.Hour),
	}), http.StatusCreated, &d)
	if !strings.HasPrefix(d.Name, "user_123-") || d.ExternalID != "user_123" || string(d.Metadata) != `{"plan":"pro"}` {
		t.Fatalf("device = %+v", d)
	}
	if d.ClientKey || d.Status != "active" || !strings.Contains(d.Config, "PrivateKey = ") {
		t.Fatalf("server-made key: %+v", d)
	}

	// The panel's own list sees it too, with the same fields.
	var peers []struct {
		ExternalID string `json:"external_id"`
	}
	p.want(p.do("GET", fmt.Sprintf("/api/instances/%d/peers", in.ID), nil), http.StatusOK, &peers)
	if len(peers) != 1 || peers[0].ExternalID != "user_123" {
		t.Fatalf("panel peers = %+v", peers)
	}

	rec := p.api(token, "GET", fmt.Sprintf("/api/v1/devices/%d/config?format=qr", d.ID), nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("qr: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}

	var patched deviceJSON
	p.want(p.api(token, "PATCH", fmt.Sprintf("/api/v1/devices/%d", d.ID), map[string]any{
		"enabled": false, "metadata": nil, "expires_at": nil,
	}), http.StatusOK, &patched)
	if patched.Enabled || patched.Status != "disabled" || string(patched.Metadata) != "{}" || patched.ExpiresAt != nil {
		t.Fatalf("patched = %+v", patched)
	}
	e := p.wantError(p.api(token, "PATCH", fmt.Sprintf("/api/v1/devices/%d", d.ID), map[string]any{
		"server_id": in.ID + 1, "public_key": wg.GeneratePrivateKey().PublicKey().String(),
	}), http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["server_id"] == "" || e.Error.Fields["public_key"] == "" {
		t.Fatalf("fields = %v", e.Error.Fields)
	}

	var usage struct {
		DataLimit int64 `json:"data_limit"`
		Daily     []any `json:"daily"`
	}
	p.want(p.api(token, "GET", fmt.Sprintf("/api/v1/devices/%d/usage", d.ID), nil), http.StatusOK, &usage)
	if usage.DataLimit != 50<<30 || len(usage.Daily) != 30 {
		t.Fatalf("usage = %+v", usage)
	}

	p.want(p.api(token, "DELETE", fmt.Sprintf("/api/v1/devices/%d", d.ID), nil), http.StatusNoContent, nil)
	p.wantError(p.api(token, "GET", fmt.Sprintf("/api/v1/devices/%d", d.ID), nil), http.StatusNotFound, "not_found")

	// Every change names the key that made it.
	events, err := p.s.store.Events(t.Context(), store.EventFilter{Limit: 50, Families: []string{"device"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Actor != "api:billing" {
			t.Fatalf("event %s has actor %q", e.Kind, e.Actor)
		}
	}
	if len(events) < 3 {
		t.Fatalf("events = %+v", events)
	}
}

func TestAPIDeviceWithClientKey(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt"})
	token := p.apiKey("app", "devices:read", "devices:write")
	pub := wg.GeneratePrivateKey().PublicKey().String()

	var d deviceJSON
	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{
		"server_id": in.ID, "name": "iPhone", "public_key": pub,
	}), http.StatusCreated, &d)
	if !d.ClientKey || d.PublicKey != pub || strings.Contains(d.Config, "PrivateKey") ||
		!strings.Contains(d.Config, "PresharedKey = ") {
		t.Fatalf("client key device = %+v", d)
	}
	stored, err := p.s.store.PeerByID(t.Context(), d.ID)
	if err != nil || !stored.KeyOnClient() {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	server, err := p.s.store.Peers(t.Context(), in.ID)
	if err != nil || !strings.Contains(string(wg.ServerConfig(wg.Instance{}, server)), pub) {
		t.Fatal("the server config must carry the client's public key")
	}

	p.wantError(p.api(token, "GET", fmt.Sprintf("/api/v1/devices/%d/config?format=qr", d.ID), nil),
		http.StatusConflict, "client_key")
	e := p.wantError(p.api(token, "POST", "/api/v1/devices", map[string]any{
		"server_id": in.ID, "name": "iPad", "public_key": pub,
	}), http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["public_key"] == "" {
		t.Fatalf("duplicate key: %v", e.Error.Fields)
	}
	p.wantError(p.api(token, "POST", "/api/v1/devices", map[string]any{
		"server_id": in.ID, "public_key": "short",
	}), http.StatusUnprocessableEntity, "validation_failed")
	p.wantError(p.api(token, "POST", "/api/v1/devices", map[string]any{
		"server_id": in.ID, "metadata": []int{1},
	}), http.StatusUnprocessableEntity, "validation_failed")
	p.wantError(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": 99}),
		http.StatusUnprocessableEntity, "validation_failed")
}

func TestAPIListDevices(t *testing.T) {
	p := newPanel(t)
	a := p.createInstance(map[string]any{"name": "Frankfurt"})
	b := p.createInstance(map[string]any{"name": "Amsterdam"})
	token := p.apiKey("billing", "devices:read", "devices:write")

	create := func(server int64, ext string, extra map[string]any) deviceJSON {
		t.Helper()
		body := map[string]any{"server_id": server, "external_id": ext}
		for k, v := range extra {
			body[k] = v
		}
		var d deviceJSON
		p.want(p.api(token, "POST", "/api/v1/devices", body), http.StatusCreated, &d)
		return d
	}
	create(a.ID, "u1", nil)
	create(b.ID, "u1", nil)
	create(a.ID, "u2", map[string]any{"enabled": false})
	create(a.ID, "u3", map[string]any{"expires_at": time.Now().Add(-time.Minute)})

	list := func(query string) pageJSON[deviceJSON] {
		t.Helper()
		var pg pageJSON[deviceJSON]
		p.want(p.api(token, "GET", "/api/v1/devices"+query, nil), http.StatusOK, &pg)
		return pg
	}
	if pg := list("?external_id=u1"); len(pg.Data) != 2 || pg.HasMore {
		t.Fatalf("by external_id: %+v", pg)
	}
	if pg := list(fmt.Sprintf("?external_id=u1&server_id=%d", b.ID)); len(pg.Data) != 1 || pg.Data[0].ServerID != b.ID {
		t.Fatalf("by server: %+v", pg)
	}
	for status, ext := range map[string]string{"disabled": "u2", "expired": "u3"} {
		if pg := list("?status=" + status); len(pg.Data) != 1 || pg.Data[0].ExternalID != ext || pg.Data[0].Status != status {
			t.Fatalf("status %s: %+v", status, pg)
		}
	}
	if pg := list("?status=active"); len(pg.Data) != 2 {
		t.Fatalf("active: %+v", pg)
	}

	first := list("?limit=3")
	if len(first.Data) != 3 || !first.HasMore {
		t.Fatalf("first page: %+v", first)
	}
	rest := list(fmt.Sprintf("?limit=3&after=%d", first.Data[2].ID))
	if len(rest.Data) != 1 || rest.HasMore {
		t.Fatalf("second page: %+v", rest)
	}
	p.wantError(p.api(token, "GET", "/api/v1/devices?status=gone", nil), http.StatusBadRequest, "invalid_request")
	p.wantError(p.api(token, "GET", "/api/v1/devices?limit=1000", nil), http.StatusBadRequest, "invalid_request")
}

func TestAPIDeviceLimitReachedStatus(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt"})
	token := p.apiKey("billing", "devices:read", "devices:write")
	var d deviceJSON
	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": in.ID, "data_limit": 1000}),
		http.StatusCreated, &d)

	peer, err := p.s.store.PeerByID(t.Context(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	stats := map[wg.Key]wg.PeerStats{peer.PublicKey: {RxBytes: 0}}
	if err := p.s.store.RecordTraffic(t.Context(), in.ID, stats, now); err != nil {
		t.Fatal(err)
	}
	stats[peer.PublicKey] = wg.PeerStats{RxBytes: 1500}
	if err := p.s.store.RecordTraffic(t.Context(), in.ID, stats, now); err != nil {
		t.Fatal(err)
	}

	var pg pageJSON[deviceJSON]
	p.want(p.api(token, "GET", "/api/v1/devices?status=limit_reached", nil), http.StatusOK, &pg)
	if len(pg.Data) != 1 || pg.Data[0].Status != "limit_reached" {
		t.Fatalf("limit_reached: %+v", pg)
	}
	p.want(p.api(token, "GET", "/api/v1/devices?status=active", nil), http.StatusOK, &pg)
	if len(pg.Data) != 0 {
		t.Fatalf("active: %+v", pg)
	}
}

func TestAPIUsageReset(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt"})
	token := p.apiKey("billing", "devices:read", "devices:write", "events:read")
	var d struct {
		deviceJSON
		LimitPeriod  string     `json:"limit_period"`
		PeriodUsage  apiTraffic `json:"period_usage"`
		UsageResetAt *time.Time `json:"usage_reset_at"`
	}
	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{
		"server_id": in.ID, "data_limit": 1000, "limit_period": "total",
	}), http.StatusCreated, &d)
	if d.LimitPeriod != "total" || d.UsageResetAt != nil {
		t.Fatalf("created = %+v", d)
	}
	p.wantError(p.api(token, "PATCH", fmt.Sprintf("/api/v1/devices/%d", d.ID), map[string]any{"limit_period": "weekly"}),
		http.StatusUnprocessableEntity, "validation_failed")

	peer, err := p.s.store.PeerByID(t.Context(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, rx := range []int64{0, 1500} {
		stats := map[wg.Key]wg.PeerStats{peer.PublicKey: {RxBytes: rx}}
		if err := p.s.store.RecordTraffic(t.Context(), in.ID, stats, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	p.want(p.api(token, "GET", fmt.Sprintf("/api/v1/devices/%d", d.ID), nil), http.StatusOK, &d)
	if d.Status != "limit_reached" || d.PeriodUsage.TotalBytes != 1500 {
		t.Fatalf("before reset = %+v", d)
	}

	p.want(p.api(token, "POST", fmt.Sprintf("/api/v1/devices/%d/usage/reset", d.ID), nil), http.StatusOK, &d)
	if d.Status != "active" || d.PeriodUsage.TotalBytes != 0 || d.UsageResetAt == nil || d.MonthUsage.TotalBytes != 1500 {
		t.Fatalf("after reset = %+v", d)
	}

	var events pageJSON[struct {
		Kind   string `json:"kind"`
		Detail string `json:"detail"`
	}]
	p.want(p.api(token, "GET", "/api/v1/events?kind=device.usage_reset", nil), http.StatusOK, &events)
	if len(events.Data) != 1 || events.Data[0].Detail != "1.5 KB used in total" {
		t.Fatalf("events = %+v", events.Data)
	}
}

func TestAPIIdempotency(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt"})
	token := p.apiKey("billing", "devices:read", "devices:write")
	body := map[string]any{"server_id": in.ID, "external_id": "order_1"}

	var first, again deviceJSON
	p.want(p.api(token, "POST", "/api/v1/devices", body, "Idempotency-Key", "order_1"), http.StatusCreated, &first)
	rec := p.api(token, "POST", "/api/v1/devices", body, "Idempotency-Key", "order_1")
	p.want(rec, http.StatusCreated, &again)
	if again.ID != first.ID || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay = %+v (%v), want device %d", again, rec.Header(), first.ID)
	}

	body["external_id"] = "order_2"
	p.wantError(p.api(token, "POST", "/api/v1/devices", body, "Idempotency-Key", "order_1"),
		http.StatusUnprocessableEntity, "idempotency_key_reused")

	// Validation failures replay too; a fixed request needs a new key.
	bad := map[string]any{"server_id": in.ID, "data_limit": -1}
	p.wantError(p.api(token, "POST", "/api/v1/devices", bad, "Idempotency-Key", "bad"), http.StatusUnprocessableEntity, "validation_failed")
	p.wantError(p.api(token, "POST", "/api/v1/devices", bad, "Idempotency-Key", "bad"), http.StatusUnprocessableEntity, "validation_failed")

	// Keys belong to one API key each.
	other := p.apiKey("other", "devices:write")
	p.want(p.api(other, "POST", "/api/v1/devices", map[string]any{"server_id": in.ID}, "Idempotency-Key", "order_1"),
		http.StatusCreated, &again)
	if again.ID == first.ID {
		t.Fatal("another API key's request must not replay this one's reply")
	}

	peers, err := p.s.store.Peers(t.Context(), in.ID)
	if err != nil || len(peers) != 2 {
		t.Fatalf("peers = %d, %v; want 2", len(peers), err)
	}
}

func TestAPIEvents(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt"})
	p.createPeer(in.ID, "laptop")
	token := p.apiKey("monitor", "events:read", "devices:write")

	var pg pageJSON[struct {
		ID         int64  `json:"id"`
		Kind       string `json:"kind"`
		ServerName string `json:"server_name"`
		DeviceName string `json:"device_name"`
		Actor      string `json:"actor"`
	}]
	p.want(p.api(token, "GET", "/api/v1/events", nil), http.StatusOK, &pg)
	var kinds []string
	for _, e := range pg.Data {
		kinds = append(kinds, e.Kind)
	}
	// Oldest first, and the sign-in and API key events stay out.
	if strings.Join(kinds, " ") != "server.created device.created" {
		t.Fatalf("kinds = %v", kinds)
	}
	last := pg.Data[len(pg.Data)-1].ID

	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": in.ID, "name": "phone"}), http.StatusCreated, nil)
	p.want(p.api(token, "GET", fmt.Sprintf("/api/v1/events?after=%d", last), nil), http.StatusOK, &pg)
	if len(pg.Data) != 1 || pg.Data[0].DeviceName != "phone" || pg.Data[0].Actor != "api:monitor" {
		t.Fatalf("new events = %+v", pg.Data)
	}
	p.want(p.api(token, "GET", "/api/v1/events?kind=server.created&limit=1", nil), http.StatusOK, &pg)
	if len(pg.Data) != 1 || pg.HasMore {
		t.Fatalf("by kind = %+v", pg)
	}
}

func TestAPIRateLimit(t *testing.T) {
	p := newPanel(t)
	token := p.apiKey("busy", "servers:read")
	// The bucket refills on the wall clock while the loop runs, which on a
	// slow runner lets a few extra requests through; the exact counts are
	// checked in TestRateLimiter.
	var rec *httptest.ResponseRecorder
	for sent := 0; ; sent++ {
		rec = p.api(token, "GET", "/api/v1/servers", nil)
		if rec.Code != http.StatusOK {
			if sent < apiRateBurst {
				t.Fatalf("within the burst, request %d: %d", sent+1, rec.Code)
			}
			break
		}
		if sent > 2*apiRateBurst {
			t.Fatal("never rate limited")
		}
	}
	p.wantError(rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter()
	start := time.Now()
	for i := range apiRateBurst {
		if ok, _ := l.allow(1, start); !ok {
			t.Fatalf("request %d within the burst was refused", i+1)
		}
	}
	ok, wait := l.allow(1, start)
	if ok || wait != time.Second/apiRatePerSec {
		t.Fatalf("past the burst: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.allow(2, start); !ok {
		t.Fatal("another key shares the bucket")
	}
	if ok, _ := l.allow(1, start.Add(time.Second/apiRatePerSec)); !ok {
		t.Fatal("no token after the refill interval")
	}
	if ok, _ := l.allow(1, start.Add(time.Second/apiRatePerSec)); ok {
		t.Fatal("refill gave more than one token")
	}
}

func TestAPIServerLocation(t *testing.T) {
	p := newPanel(t)
	p.createInstance(map[string]any{"name": "Frankfurt", "country": " de", "city": "Frankfurt am Main"})
	p.createInstance(map[string]any{"name": "Amsterdam", "country": "NL"})
	p.wantError(p.do("POST", "/api/instances", map[string]any{"name": "Nowhere", "country": "Germany"}),
		http.StatusUnprocessableEntity, "validation_failed")
	token := p.apiKey("app", "servers:read")

	type server struct {
		Name    string `json:"name"`
		Country string `json:"country"`
		City    string `json:"city"`
	}
	var pg pageJSON[server]
	p.want(p.api(token, "GET", "/api/v1/servers?country=de", nil), http.StatusOK, &pg)
	if len(pg.Data) != 1 || pg.Data[0] != (server{"Frankfurt", "DE", "Frankfurt am Main"}) {
		t.Fatalf("servers in DE = %+v", pg.Data)
	}
	p.want(p.api(token, "GET", "/api/v1/servers", nil), http.StatusOK, &pg)
	if len(pg.Data) != 2 {
		t.Fatalf("all servers = %+v", pg.Data)
	}
}
