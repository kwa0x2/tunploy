package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/docker/dockertest"
)

type instanceJSON struct {
	ID                  int64    `json:"id"`
	Name                string   `json:"name"`
	Address             string   `json:"address"`
	ListenPort          int      `json:"listen_port"`
	PublicKey           string   `json:"public_key"`
	Endpoint            string   `json:"endpoint"`
	DNS                 []string `json:"dns"`
	MTU                 int      `json:"mtu"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
	ClientAllowedIPs    []string `json:"client_allowed_ips"`
	PeerCount           int      `json:"peer_count"`
	Status              struct {
		State string `json:"state"`
		Error string `json:"error"`
	} `json:"status"`
}

type peerJSON struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	PublicKey string `json:"public_key"`
	Enabled   bool   `json:"enabled"`
	Stats     *struct {
		Endpoint string `json:"endpoint"`
		RxBytes  int64  `json:"rx_bytes"`
	} `json:"stats"`
}

type apiError struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

type panel struct {
	t      *testing.T
	s      *Server
	fake   *dockertest.Fake
	cookie *http.Cookie
}

func newPanel(t *testing.T) *panel {
	t.Helper()
	fk := dockertest.New()
	s, _ := newTestServerWithDeploy(t, fakeDocker{}, fk)
	return &panel{t: t, s: s, fake: fk, cookie: loggedIn(t, s)}
}

func (p *panel) do(method, path string, body any) *httptest.ResponseRecorder {
	p.t.Helper()
	return do(p.t, p.s, method, path, body, p.cookie)
}

func (p *panel) want(rec *httptest.ResponseRecorder, status int, v any) {
	p.t.Helper()
	if rec.Code != status {
		p.t.Fatalf("want %d, got %d: %s", status, rec.Code, rec.Body)
	}
	if v != nil {
		decode(p.t, rec, v)
	}
}

func (p *panel) createInstance(body map[string]any) instanceJSON {
	p.t.Helper()
	var in instanceJSON
	p.want(p.do("POST", "/api/instances", body), http.StatusCreated, &in)
	return in
}

func (p *panel) createPeer(instanceID int64, name string) peerJSON {
	p.t.Helper()
	var peer peerJSON
	p.want(p.do("POST", fmt.Sprintf("/api/instances/%d/peers", instanceID), map[string]any{"name": name}),
		http.StatusCreated, &peer)
	return peer
}

func (p *panel) wantError(rec *httptest.ResponseRecorder, status int, code string) apiError {
	p.t.Helper()
	var e apiError
	p.want(rec, status, &e)
	if e.Error.Code != code {
		p.t.Fatalf("want error code %q, got %+v", code, e.Error)
	}
	return e
}

func TestOneClickInstance(t *testing.T) {
	p := newPanel(t)

	in := p.createInstance(map[string]any{"name": "Home"})
	if in.Address != "10.8.0.1/24" || in.ListenPort != 51820 || in.Endpoint != "vpn.example.com" {
		t.Fatalf("defaults not applied: %+v", in)
	}
	if in.Status.State != "running" {
		t.Fatalf("status = %+v, want running", in.Status)
	}
	if in.PublicKey == "" || !slices.Equal(in.DNS, []string{"1.1.1.1", "1.0.0.1"}) {
		t.Fatalf("instance incomplete: %+v", in)
	}

	second := p.createInstance(map[string]any{"name": "Office"})
	if second.Address != "10.9.0.1/24" || second.ListenPort != 51821 {
		t.Fatalf("second instance should get the next free subnet and port: %+v", second)
	}

	var list []instanceJSON
	p.want(p.do("GET", "/api/instances", nil), http.StatusOK, &list)
	if len(list) != 2 || list[0].Status.State != "running" {
		t.Fatalf("list = %+v", list)
	}
}

func TestInstanceDefaultsFollowExistingInstances(t *testing.T) {
	p := newPanel(t)

	var d instanceJSON
	p.want(p.do("GET", "/api/instances/defaults", nil), http.StatusOK, &d)
	if d.Address != "10.8.0.1/24" || d.ListenPort != 51820 || d.Endpoint != "vpn.example.com" {
		t.Fatalf("defaults = %+v", d)
	}

	p.createInstance(map[string]any{"name": "Home"})
	p.want(p.do("GET", "/api/instances/defaults", nil), http.StatusOK, &d)
	if d.Address != "10.9.0.1/24" || d.ListenPort != 51821 {
		t.Fatalf("defaults should skip the taken subnet and port: %+v", d)
	}
	if d.PublicKey != "" {
		t.Fatal("defaults must not carry keys")
	}
}

func TestInstanceResponsesNeverLeakKeys(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	p.createPeer(in.ID, "phone")

	for _, path := range []string{
		"/api/instances",
		fmt.Sprintf("/api/instances/%d", in.ID),
		fmt.Sprintf("/api/instances/%d/peers", in.ID),
	} {
		body := p.do("GET", path, nil).Body.String()
		if strings.Contains(body, "private_key") || strings.Contains(body, "preshared_key") {
			t.Errorf("%s exposes secret keys: %s", path, body)
		}
	}
}

func TestCreateInstanceValidation(t *testing.T) {
	p := newPanel(t)
	p.createInstance(map[string]any{"name": "Home"})

	tests := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"duplicate name", map[string]any{"name": "home"}, "name"},
		{"overlapping subnet", map[string]any{"name": "B", "address": "10.8.0.129/25"}, "address"},
		{"unparseable address", map[string]any{"name": "B", "address": "10.8.0.1"}, "address"},
		{"port taken", map[string]any{"name": "B", "listen_port": 51820}, "listen_port"},
		{"bad dns", map[string]any{"name": "B", "dns": []string{"one.one"}}, "dns"},
		{"endpoint with port", map[string]any{"name": "B", "endpoint": "vpn.example.com:51820"}, "endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := p.wantError(p.do("POST", "/api/instances", tt.body), http.StatusUnprocessableEntity, "validation_failed")
			if e.Error.Fields[tt.field] == "" {
				t.Fatalf("want an error on %s, got %v", tt.field, e.Error.Fields)
			}
		})
	}
}

func TestFailedDeployRollsBack(t *testing.T) {
	p := newPanel(t)
	p.fake.StartErr = errors.New("Bind for 0.0.0.0:51820 failed: port is already allocated")

	e := p.wantError(p.do("POST", "/api/instances", map[string]any{"name": "Home"}),
		http.StatusBadGateway, "deploy_failed")
	if !strings.Contains(e.Error.Message, "port is already allocated") {
		t.Fatalf("docker's reason should reach the admin: %q", e.Error.Message)
	}

	var list []instanceJSON
	p.want(p.do("GET", "/api/instances", nil), http.StatusOK, &list)
	if len(list) != 0 {
		t.Fatalf("instance should be rolled back, list = %+v", list)
	}
	if _, ok := p.fake.Container(p.s.deploy.ContainerName(1)); ok {
		t.Fatal("container should be removed on rollback")
	}

	p.fake.StartErr = nil
	if in := p.createInstance(map[string]any{"name": "Home"}); in.Status.State != "running" {
		t.Fatalf("retry after fixing the cause should work: %+v", in)
	}
}

func (p *panel) provision(body map[string]any) []map[string]json.RawMessage {
	p.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/instances", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	req.AddCookie(p.cookie)
	rec := httptest.NewRecorder()
	p.s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/x-ndjson" {
		p.t.Fatalf("want a 200 ndjson stream, got %d %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}

	var events []map[string]json.RawMessage
	for line := range strings.Lines(rec.Body.String()) {
		var ev map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			p.t.Fatalf("bad line %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

func TestProvisionStream(t *testing.T) {
	p := newPanel(t)

	events := p.provision(map[string]any{"name": "Home"})
	var steps []string
	for _, ev := range events[:len(events)-1] {
		steps = append(steps, strings.Trim(string(ev["step"]), `"`))
	}
	if want := []string{"image", "container", "interface", "firewall", "nat"}; !slices.Equal(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
	var in instanceJSON
	if err := json.Unmarshal(events[len(events)-1]["instance"], &in); err != nil || in.Status.State != "running" {
		t.Fatalf("last event should carry the running instance: %s", events[len(events)-1]["instance"])
	}
}

func TestProvisionStreamFailure(t *testing.T) {
	p := newPanel(t)
	p.fake.BootLog = "RTNETLINK answers: Operation not supported\n"
	p.fake.BootExitCode = 1

	events := p.provision(map[string]any{"name": "Home"})
	last := events[len(events)-1]
	var e struct{ Code, Message string }
	var log []string
	json.Unmarshal(last["error"], &e)
	json.Unmarshal(last["log"], &log)
	if e.Code != "deploy_failed" || !slices.Equal(log, []string{"RTNETLINK answers: Operation not supported"}) {
		t.Fatalf("want the failure with the container output, got %v", last)
	}

	var list []instanceJSON
	p.want(p.do("GET", "/api/instances", nil), http.StatusOK, &list)
	if len(list) != 0 {
		t.Fatalf("instance should be rolled back, list = %+v", list)
	}
}

func TestProvisionStreamValidatesFirst(t *testing.T) {
	p := newPanel(t)
	req := httptest.NewRequest("POST", "/api/instances", strings.NewReader(`{"name":""}`))
	req.Header.Set("Accept", "application/x-ndjson")
	req.AddCookie(p.cookie)
	rec := httptest.NewRecorder()
	p.s.ServeHTTP(rec, req)
	p.wantError(rec, http.StatusUnprocessableEntity, "validation_failed")
}

func TestCreateInstanceWithDockerDown(t *testing.T) {
	p := newPanel(t)
	p.fake.Unavailable = true

	p.wantError(p.do("POST", "/api/instances", map[string]any{"name": "Home"}),
		http.StatusServiceUnavailable, "docker_unavailable")

	p.fake.Unavailable = false
	var list []instanceJSON
	p.want(p.do("GET", "/api/instances", nil), http.StatusOK, &list)
	if len(list) != 0 {
		t.Fatalf("nothing should be saved, list = %+v", list)
	}
}

func TestListInstancesWithDockerDown(t *testing.T) {
	p := newPanel(t)
	p.createInstance(map[string]any{"name": "Home"})
	p.fake.Unavailable = true

	var list []instanceJSON
	p.want(p.do("GET", "/api/instances", nil), http.StatusOK, &list)
	if len(list) != 1 || list[0].Status.State != "unknown" || list[0].Status.Error == "" {
		t.Fatalf("list = %+v", list)
	}
}

func TestUpdateInstance(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	path := fmt.Sprintf("/api/instances/%d", in.ID)
	name := p.s.deploy.ContainerName(in.ID)
	before, _ := p.fake.Container(name)

	var got instanceJSON
	p.want(p.do("PATCH", path, map[string]any{"name": "Home 2", "dns": []string{"9.9.9.9"}}), http.StatusOK, &got)
	if got.Name != "Home 2" || !slices.Equal(got.DNS, []string{"9.9.9.9"}) || got.ListenPort != 51820 {
		t.Fatalf("patch result = %+v", got)
	}
	if after, _ := p.fake.Container(name); after.ID != before.ID {
		t.Fatal("a client-side change must not recreate the container")
	}

	p.want(p.do("PATCH", path, map[string]any{"listen_port": 51900}), http.StatusOK, &got)
	after, _ := p.fake.Container(name)
	spec, _ := p.fake.Spec(name)
	if after.ID == before.ID || spec.Ports[0].Host != 51900 || after.State != "running" {
		t.Fatalf("a port change must recreate the running container on the new port: %+v %+v", after, spec.Ports)
	}

	e := p.wantError(p.do("PATCH", path, map[string]any{"address": "10.50.0.1/24"}),
		http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["address"] == "" {
		t.Fatalf("address change must be refused: %+v", e)
	}
}

func TestInstanceActions(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	base := fmt.Sprintf("/api/instances/%d", in.ID)

	var got instanceJSON
	p.want(p.do("POST", base+"/stop", nil), http.StatusOK, &got)
	if got.Status.State != "stopped" {
		t.Fatalf("after stop: %+v", got.Status)
	}
	p.want(p.do("POST", base+"/start", nil), http.StatusOK, &got)
	if got.Status.State != "running" {
		t.Fatalf("after start: %+v", got.Status)
	}
	p.want(p.do("POST", base+"/restart", nil), http.StatusOK, &got)
	if got.Status.State != "running" {
		t.Fatalf("after restart: %+v", got.Status)
	}

	p.fake.RemoveContainer(t.Context(), p.s.deploy.ContainerName(in.ID))
	p.want(p.do("POST", base+"/start", nil), http.StatusOK, &got)
	if got.Status.State != "running" {
		t.Fatalf("start should redeploy a missing container: %+v", got.Status)
	}
}

func TestDeleteInstance(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	p.createPeer(in.ID, "phone")
	path := fmt.Sprintf("/api/instances/%d", in.ID)

	p.want(p.do("DELETE", path, nil), http.StatusNoContent, nil)
	if _, ok := p.fake.Container(p.s.deploy.ContainerName(in.ID)); ok {
		t.Fatal("container should be gone")
	}
	p.wantError(p.do("GET", path, nil), http.StatusNotFound, "not_found")
	p.wantError(p.do("GET", path+"/peers", nil), http.StatusNotFound, "not_found")
}

func TestUnknownInstance(t *testing.T) {
	p := newPanel(t)
	for _, path := range []string{"/api/instances/99", "/api/instances/abc", "/api/instances/99/peers"} {
		p.wantError(p.do("GET", path, nil), http.StatusNotFound, "not_found")
	}
}

func TestInstancesRequireAuth(t *testing.T) {
	s := newTestServer(t)
	for _, req := range [][2]string{{"GET", "/api/instances"}, {"POST", "/api/instances"}, {"DELETE", "/api/instances/1"}} {
		if rec := do(t, s, req[0], req[1], nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: want 401, got %d", req[0], req[1], rec.Code)
		}
	}
}

func TestPeerLifecycle(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	base := fmt.Sprintf("/api/instances/%d/peers", in.ID)

	laptop := p.createPeer(in.ID, "laptop")
	phone := p.createPeer(in.ID, "phone")
	if laptop.Address != "10.8.0.2" || phone.Address != "10.8.0.3" || !laptop.Enabled {
		t.Fatalf("peers = %+v %+v", laptop, phone)
	}
	if n := countSyncs(p.fake); n != 2 {
		t.Fatalf("each new peer should be synced into the running tunnel, got %d syncs", n)
	}

	p.fake.ExecOutput = func(name string, cmd []string) ([]byte, error) {
		return []byte("priv\tpub\t51820\toff\n" +
			laptop.PublicKey + "\tpsk\t203.0.113.9:4000\t10.8.0.2/32\t1758700000\t512\t1024\toff\n"), nil
	}
	var list []peerJSON
	p.want(p.do("GET", base, nil), http.StatusOK, &list)
	if len(list) != 2 || list[0].Stats == nil || list[0].Stats.RxBytes != 512 || list[1].Stats != nil {
		t.Fatalf("stats not merged: %+v", list)
	}

	var updated peerJSON
	p.want(p.do("PATCH", fmt.Sprintf("%s/%d", base, phone.ID), map[string]any{"enabled": false}), http.StatusOK, &updated)
	if updated.Enabled || updated.Name != "phone" {
		t.Fatalf("patch = %+v", updated)
	}

	e := p.wantError(p.do("POST", base, map[string]any{"name": "Laptop"}), http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["name"] == "" {
		t.Fatalf("duplicate peer name: %+v", e)
	}

	p.want(p.do("DELETE", fmt.Sprintf("%s/%d", base, phone.ID), nil), http.StatusNoContent, nil)
	p.want(p.do("GET", base, nil), http.StatusOK, &list)
	if len(list) != 1 {
		t.Fatalf("after delete: %+v", list)
	}
}

func countSyncs(fk *dockertest.Fake) int {
	n := 0
	for _, cmd := range fk.Execs {
		if slices.Equal(cmd, []string{"tunploy-wg", "sync"}) {
			n++
		}
	}
	return n
}

func TestPeerConfigDownload(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	peer := p.createPeer(in.ID, "Kwa's iPhone 15 Pro Max")

	rec := p.do("GET", fmt.Sprintf("/api/instances/%d/peers/%d/config", in.ID, peer.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="Kwa-s-iPhone-15.conf"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{"Address = 10.8.0.2/32", "PublicKey = " + in.PublicKey, "Endpoint = vpn.example.com:51820"} {
		if !strings.Contains(body, want) {
			t.Errorf("config missing %q:\n%s", want, body)
		}
	}
}

func TestPeerMustBelongToInstance(t *testing.T) {
	p := newPanel(t)
	home := p.createInstance(map[string]any{"name": "Home"})
	office := p.createInstance(map[string]any{"name": "Office"})
	peer := p.createPeer(home.ID, "phone")

	path := fmt.Sprintf("/api/instances/%d/peers/%d", office.ID, peer.ID)
	p.wantError(p.do("PATCH", path, map[string]any{"enabled": false}), http.StatusNotFound, "not_found")
	p.wantError(p.do("GET", path+"/config", nil), http.StatusNotFound, "not_found")
	p.wantError(p.do("DELETE", path, nil), http.StatusNotFound, "not_found")
}

func TestSubnetFull(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Tiny", "address": "10.20.0.1/30"})
	p.createPeer(in.ID, "only")

	p.wantError(p.do("POST", fmt.Sprintf("/api/instances/%d/peers", in.ID), map[string]any{"name": "extra"}),
		http.StatusConflict, "subnet_full")
}

func TestPeerSavedButNotApplied(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	p.fake.ExecOutput = func(string, []string) ([]byte, error) {
		return nil, fmt.Errorf("exec failed: %w", docker.ErrConflict)
	}

	p.wantError(p.do("POST", fmt.Sprintf("/api/instances/%d/peers", in.ID), map[string]any{"name": "phone"}),
		http.StatusBadGateway, "apply_failed")

	var list []peerJSON
	p.fake.ExecOutput = nil
	p.want(p.do("GET", fmt.Sprintf("/api/instances/%d/peers", in.ID), nil), http.StatusOK, &list)
	if len(list) != 1 {
		t.Fatalf("the peer is saved even though the sync failed: %+v", list)
	}
}

func TestMovePeer(t *testing.T) {
	p := newPanel(t)
	a := p.createInstance(map[string]any{"name": "Frankfurt"})
	b := p.createInstance(map[string]any{"name": "Amsterdam"})
	peer := p.createPeer(a.ID, "phone")
	path := fmt.Sprintf("/api/instances/%d/peers/%d/move", a.ID, peer.ID)

	var moved peerJSON
	p.want(p.do("POST", path, map[string]any{"instance_id": b.ID}), http.StatusOK, &moved)
	if moved.ID != peer.ID || moved.PublicKey != peer.PublicKey || !strings.HasPrefix(moved.Address, "10.9.0.") {
		t.Fatalf("moved = %+v", moved)
	}
	var left []peerJSON
	p.want(p.do("GET", fmt.Sprintf("/api/instances/%d/peers", a.ID), nil), http.StatusOK, &left)
	if len(left) != 0 {
		t.Fatalf("still on Frankfurt: %+v", left)
	}

	p.wantError(p.do("POST", path, map[string]any{"instance_id": b.ID}), http.StatusNotFound, "not_found")
	back := fmt.Sprintf("/api/instances/%d/peers/%d/move", b.ID, peer.ID)
	p.wantError(p.do("POST", back, map[string]any{"instance_id": b.ID}), http.StatusUnprocessableEntity, "validation_failed")
	p.wantError(p.do("POST", back, map[string]any{"instance_id": 99}), http.StatusUnprocessableEntity, "validation_failed")
}
