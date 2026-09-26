package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type deviceWithAddress struct {
	deviceJSON
	Address string `json:"address"`
}

func TestAPIMoveDevice(t *testing.T) {
	p := newPanel(t)
	from := p.createInstance(map[string]any{"name": "Frankfurt", "country": "DE"})
	to := p.createInstance(map[string]any{"name": "Amsterdam", "country": "NL", "endpoint": "nl.example.com"})
	token := p.apiKey("billing", "devices:read", "devices:write")

	var d deviceJSON
	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{
		"server_id": from.ID, "name": "phone", "external_id": "u1", "data_limit": 5 << 30,
	}), http.StatusCreated, &d)

	var moved deviceWithAddress
	p.want(p.api(token, "POST", fmt.Sprintf("/api/v1/devices/%d/move", d.ID), map[string]any{"server_id": to.ID}),
		http.StatusOK, &moved)
	subnet := netip.MustParsePrefix(to.Address).Masked()
	if moved.ID != d.ID || moved.ServerID != to.ID || moved.PublicKey != d.PublicKey || moved.DataLimit != 5<<30 ||
		moved.ExternalID != "u1" || !subnet.Contains(netip.MustParseAddr(moved.Address)) {
		t.Fatalf("moved = %+v", moved)
	}
	if !strings.Contains(moved.Config, "Endpoint = nl.example.com:") || !strings.Contains(moved.Config, to.PublicKey) ||
		!strings.Contains(moved.Config, "PrivateKey = ") {
		t.Fatalf("config after move:\n%s", moved.Config)
	}
	for _, id := range []int64{from.ID, to.ID} {
		peers, err := p.s.store.Peers(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		conf := string(wg.ServerConfig(wg.Instance{}, peers))
		if strings.Contains(conf, d.PublicKey) != (id == to.ID) {
			t.Fatalf("server %d config:\n%s", id, conf)
		}
	}

	events, err := p.s.store.Events(t.Context(), store.EventFilter{Limit: 1, Kinds: []string{"device.moved"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].InstanceName != "Amsterdam" || events[0].Detail != "from Frankfurt" ||
		events[0].Actor != "api:billing" {
		t.Fatalf("moved events = %+v", events)
	}

	move := func(body map[string]any) *httptest.ResponseRecorder {
		return p.api(token, "POST", fmt.Sprintf("/api/v1/devices/%d/move", d.ID), body)
	}
	p.wantError(move(map[string]any{"server_id": to.ID}), http.StatusUnprocessableEntity, "validation_failed")
	p.wantError(move(map[string]any{"server_id": 99}), http.StatusUnprocessableEntity, "validation_failed")
	p.wantError(move(map[string]any{}), http.StatusUnprocessableEntity, "validation_failed")

	// "auto" never picks the server the device is leaving.
	p.want(move(map[string]any{"server_id": "auto"}), http.StatusOK, &moved)
	if moved.ServerID != from.ID {
		t.Fatalf("auto move went to %d", moved.ServerID)
	}
	p.wantError(move(map[string]any{"server_id": "auto", "country": "DE"}), http.StatusConflict, "no_server_available")

	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": to.ID, "name": "phone"}),
		http.StatusCreated, nil)
	e := p.wantError(move(map[string]any{"server_id": to.ID}), http.StatusUnprocessableEntity, "validation_failed")
	if !strings.Contains(e.Error.Fields["name"], "phone") {
		t.Fatalf("name clash: %v", e.Error.Fields)
	}

	e = p.wantError(p.api(token, "PATCH", fmt.Sprintf("/api/v1/devices/%d", d.ID), map[string]any{"server_id": "auto"}),
		http.StatusUnprocessableEntity, "validation_failed")
	if !strings.Contains(e.Error.Fields["server_id"], "/move") {
		t.Fatalf("patch server_id: %v", e.Error.Fields)
	}
}

func TestAPIMoveClientKeyDevice(t *testing.T) {
	p := newPanel(t)
	a := p.createInstance(map[string]any{"name": "Frankfurt"})
	b := p.createInstance(map[string]any{"name": "Amsterdam"})
	token := p.apiKey("app", "devices:read", "devices:write")
	pub := wg.GeneratePrivateKey().PublicKey().String()

	var d deviceJSON
	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": a.ID, "public_key": pub}),
		http.StatusCreated, &d)
	p.want(p.api(token, "POST", fmt.Sprintf("/api/v1/devices/%d/move", d.ID), map[string]any{"server_id": b.ID}),
		http.StatusOK, &d)
	if !d.ClientKey || d.PublicKey != pub || strings.Contains(d.Config, "PrivateKey") || !strings.Contains(d.Config, b.PublicKey) {
		t.Fatalf("moved client-key device = %+v", d)
	}
}

func TestAPIAutoServer(t *testing.T) {
	p := newPanel(t)
	fra := p.createInstance(map[string]any{"name": "Frankfurt", "country": "DE", "city": "Frankfurt"})
	ber := p.createInstance(map[string]any{"name": "Berlin", "country": "DE", "city": "Berlin"})
	ams := p.createInstance(map[string]any{"name": "Amsterdam", "country": "NL"})
	token := p.apiKey("app", "devices:read", "devices:write")

	create := func(body map[string]any) deviceJSON {
		t.Helper()
		var d deviceJSON
		p.want(p.api(token, "POST", "/api/v1/devices", body), http.StatusCreated, &d)
		return d
	}
	create(map[string]any{"server_id": fra.ID})
	create(map[string]any{"server_id": fra.ID})

	if d := create(map[string]any{"server_id": "auto", "country": "de"}); d.ServerID != ber.ID {
		t.Fatalf("auto in DE went to %d, want the emptier Berlin (%d)", d.ServerID, ber.ID)
	}
	if d := create(map[string]any{"server_id": "auto", "country": "DE", "city": "frankfurt"}); d.ServerID != fra.ID {
		t.Fatalf("auto in Frankfurt went to %d", d.ServerID)
	}
	if d := create(map[string]any{"server_id": "auto"}); d.ServerID != ams.ID {
		t.Fatalf("auto anywhere went to %d, want Amsterdam (%d)", d.ServerID, ams.ID)
	}

	p.want(p.do("POST", fmt.Sprintf("/api/instances/%d/stop", ams.ID), nil), http.StatusOK, nil)
	e := p.wantError(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": "auto", "country": "NL"}),
		http.StatusConflict, "no_server_available")
	if !strings.Contains(e.Error.Message, "NL") {
		t.Fatalf("message = %q", e.Error.Message)
	}

	p.wantError(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": fra.ID, "country": "DE"}),
		http.StatusUnprocessableEntity, "validation_failed")
	p.wantError(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": "anywhere"}),
		http.StatusBadRequest, "invalid_request")
}

type apiServerJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Address     string `json:"address"`
	Subnet      string `json:"subnet"`
	Country     string `json:"country"`
	City        string `json:"city"`
	MTU         int    `json:"mtu"`
	Capacity    int    `json:"capacity"`
	DeviceCount int    `json:"device_count"`
}

func TestAPIServerLifecycle(t *testing.T) {
	p := newPanel(t)
	p.createInstance(map[string]any{"name": "Frankfurt"})
	token := p.apiKey("terraform", "servers:read", "servers:write", "devices:write")
	reader := p.apiKey("reader", "servers:read")

	p.wantError(p.api(reader, "POST", "/api/v1/servers", map[string]any{"name": "Tokyo"}), http.StatusForbidden, "missing_scope")

	var srv apiServerJSON
	p.want(p.api(token, "POST", "/api/v1/servers", map[string]any{
		"name": "Tokyo", "country": "jp", "city": "Tokyo", "max_devices": 1000,
	}), http.StatusCreated, &srv)
	if srv.Status != "running" || srv.Capacity != 1021 || srv.Address != "10.9.0.1/22" || srv.Country != "JP" {
		t.Fatalf("created = %+v", srv)
	}

	for _, body := range []map[string]any{
		{"name": "Osaka", "max_devices": 70000},
		{"name": "Osaka", "max_devices": 0},
		{"name": "Osaka", "max_devices": 10, "address": "10.50.0.1/24"},
		{"name": "tokyo"},
	} {
		p.wantError(p.api(token, "POST", "/api/v1/servers", body), http.StatusUnprocessableEntity, "validation_failed")
	}

	path := fmt.Sprintf("/api/v1/servers/%d", srv.ID)
	p.want(p.api(token, "PATCH", path, map[string]any{"city": "Shibuya", "mtu": 1380}), http.StatusOK, &srv)
	if srv.City != "Shibuya" || srv.MTU != 1380 || srv.Status != "running" {
		t.Fatalf("patched = %+v", srv)
	}
	p.wantError(p.api(token, "PATCH", path, map[string]any{"max_devices": 5000}), http.StatusUnprocessableEntity, "validation_failed")

	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": srv.ID}), http.StatusCreated, nil)
	p.wantError(p.api(token, "DELETE", path, nil), http.StatusConflict, "server_not_empty")
	p.wantError(p.api(token, "DELETE", path+"?force=yes", nil), http.StatusBadRequest, "invalid_request")
	p.want(p.api(token, "DELETE", path+"?force=true", nil), http.StatusNoContent, nil)
	p.wantError(p.api(token, "GET", path, nil), http.StatusNotFound, "not_found")
	if _, ok := p.fake.Container(p.s.deploy.ContainerName(srv.ID)); ok {
		t.Fatal("container should be gone")
	}

	events, err := p.s.store.Events(t.Context(), store.EventFilter{Limit: 10, Families: []string{"server"}})
	if err != nil {
		t.Fatal(err)
	}
	var byKey int
	for _, e := range events {
		if e.InstanceName == "Tokyo" && e.Actor == "api:terraform" {
			byKey++
		}
	}
	if byKey != 3 {
		t.Fatalf("want created, updated and deleted by the key, got %+v", events)
	}

	var nodes pageJSON[struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		ServerCount int    `json:"server_count"`
	}]
	p.want(p.api(reader, "GET", "/api/v1/nodes", nil), http.StatusOK, &nodes)
	if len(nodes.Data) != 1 || nodes.Data[0].ID != 0 || nodes.Data[0].Status != "online" || nodes.Data[0].ServerCount != 1 {
		t.Fatalf("nodes = %+v", nodes)
	}
}

func TestSubnetBitsFor(t *testing.T) {
	for n, want := range map[int]int{1: 24, 253: 24, 254: 23, 1000: 22, 65533: 16} {
		if got, ok := subnetBitsFor(n); !ok || got != want {
			t.Errorf("subnetBitsFor(%d) = %d, %v; want %d", n, got, ok, want)
		}
	}
	for _, n := range []int{0, -1, 65534} {
		if _, ok := subnetBitsFor(n); ok {
			t.Errorf("subnetBitsFor(%d) should fail", n)
		}
	}
}

func TestNextFreeSubnetSkipsBigSubnets(t *testing.T) {
	existing := []wg.Instance{{Address: netip.MustParsePrefix("10.8.0.1/16")}, {Address: netip.MustParsePrefix("10.9.0.1/22")}}
	if got := nextFreeSubnet(existing, 24); got != netip.MustParsePrefix("10.10.0.1/24") {
		t.Fatalf("got %s", got)
	}
}

type groupJSON struct {
	ExternalID  string       `json:"external_id"`
	DeviceCount int          `json:"device_count"`
	MonthUsage  apiTraffic   `json:"month_usage"`
	Devices     []deviceJSON `json:"devices"`
}

func TestAPIGroups(t *testing.T) {
	p := newPanel(t)
	a := p.createInstance(map[string]any{"name": "Frankfurt"})
	b := p.createInstance(map[string]any{"name": "Amsterdam"})
	token := p.apiKey("billing", "devices:read", "devices:write")

	var other deviceJSON
	for _, body := range []map[string]any{
		{"server_id": a.ID, "external_id": "team/7"},
		{"server_id": b.ID, "external_id": "team/7", "enabled": false},
	} {
		p.want(p.api(token, "POST", "/api/v1/devices", body), http.StatusCreated, nil)
	}
	p.want(p.api(token, "POST", "/api/v1/devices", map[string]any{"server_id": a.ID, "external_id": "team/8"}),
		http.StatusCreated, &other)

	const path = "/api/v1/groups/team%2F7"
	var g groupJSON
	p.want(p.api(token, "GET", path, nil), http.StatusOK, &g)
	if g.ExternalID != "team/7" || g.DeviceCount != 2 || len(g.Devices) != 2 {
		t.Fatalf("group = %+v", g)
	}

	until := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	p.want(p.api(token, "PATCH", path, map[string]any{
		"enabled": true, "data_limit": 50 << 30, "limit_period": "total", "expires_at": until,
	}), http.StatusOK, &g)
	for _, d := range g.Devices {
		if !d.Enabled || d.DataLimit != 50<<30 || d.ExpiresAt == nil || !d.ExpiresAt.Equal(until) {
			t.Fatalf("patched device = %+v", d)
		}
	}

	p.wantError(p.api(token, "PATCH", path, map[string]any{"data_limit": -1}), http.StatusUnprocessableEntity, "validation_failed")
	p.wantError(p.api(token, "PATCH", path, map[string]any{"name": "x"}), http.StatusBadRequest, "invalid_request")
	p.want(p.api(token, "GET", path, nil), http.StatusOK, &g)
	if g.Devices[0].DataLimit != 50<<30 {
		t.Fatalf("a rejected change still changed %+v", g.Devices[0])
	}

	p.want(p.api(token, "POST", path+"/usage/reset", nil), http.StatusOK, &g)
	if g.DeviceCount != 2 {
		t.Fatalf("after reset = %+v", g)
	}

	p.want(p.api(token, "DELETE", path, nil), http.StatusNoContent, nil)
	p.wantError(p.api(token, "GET", path, nil), http.StatusNotFound, "not_found")
	var still deviceJSON
	p.want(p.api(token, "GET", fmt.Sprintf("/api/v1/devices/%d", other.ID), nil), http.StatusOK, &still)
	if still.DataLimit != 0 {
		t.Fatalf("another group changed: %+v", still)
	}

	events, err := p.s.store.Events(t.Context(), store.EventFilter{Limit: 50, Kinds: []string{"device.deleted", "device.usage_reset"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("want a reset and a delete per device, got %+v", events)
	}
}
