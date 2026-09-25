package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kwa0x2/tunploy/internal/docker/dockertest"
	"github.com/kwa0x2/tunploy/internal/node"
	"github.com/kwa0x2/tunploy/internal/store"
)

type nodeJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Local       bool   `json:"local"`
	ServerCount int    `json:"server_count"`
	Status      struct {
		State string `json:"state"`
	} `json:"status"`
}

// remoteNode adds a node straight to the database, as setup would, with a
// fake machine behind it that is online.
func (p *panel) remoteNode(name, host string) (*store.Node, *dockertest.Host) {
	p.t.Helper()
	n, err := p.s.store.CreateNode(context.Background(), store.Node{
		Name: name, Host: host, Port: 22, Username: "root", HostKey: "ssh-ed25519 AAAA",
	})
	if err != nil {
		p.t.Fatal(err)
	}
	h := dockertest.NewHost(node.Root)
	p.nodes().set(n.ID, h)
	return n, h
}

func (p *panel) nodes() *fakeNodes { return p.s.nodes.(*fakeNodes) }

func configPath(instanceID int64) string {
	return fmt.Sprintf("%s/wireguard/%d/wg0.conf", node.Root, instanceID)
}

func TestListNodesStartsWithThisServer(t *testing.T) {
	p := newPanel(t)
	n, _ := p.remoteNode("Frankfurt", "203.0.113.5")
	p.createInstance(map[string]any{"name": "Home"})
	p.createInstance(map[string]any{"name": "Remote", "node_id": n.ID})

	var nodes []nodeJSON
	p.want(p.do("GET", "/api/nodes", nil), http.StatusOK, &nodes)
	if len(nodes) != 2 || !nodes[0].Local || nodes[0].ID != 0 || nodes[0].ServerCount != 1 {
		t.Fatalf("nodes = %+v", nodes)
	}
	if nodes[1].Name != "Frankfurt" || nodes[1].Status.State != "online" || nodes[1].ServerCount != 1 {
		t.Fatalf("remote node = %+v", nodes[1])
	}
}

func TestServerOnRemoteNode(t *testing.T) {
	p := newPanel(t)
	n, h := p.remoteNode("Frankfurt", "203.0.113.5")

	var defaults struct {
		Endpoint   string `json:"endpoint"`
		ListenPort int    `json:"listen_port"`
	}
	local := p.createInstance(map[string]any{"name": "Home"})
	p.want(p.do("GET", fmt.Sprintf("/api/instances/defaults?node_id=%d", n.ID), nil), http.StatusOK, &defaults)
	if defaults.Endpoint != "203.0.113.5" || defaults.ListenPort != local.ListenPort {
		t.Fatalf("defaults = %+v; the port is free on another machine", defaults)
	}

	remote := p.createInstance(map[string]any{"name": "Remote", "node_id": n.ID})
	if remote.Endpoint != "203.0.113.5" || remote.Status.State != "running" {
		t.Fatalf("remote = %+v", remote)
	}
	if _, ok := h.Container("tunploy-wg-" + fmt.Sprint(remote.ID)); !ok {
		t.Fatal("no container on the node")
	}
	if _, ok := p.fake.Container("tunploy-wg-" + fmt.Sprint(remote.ID)); ok {
		t.Fatal("the remote server also runs on the panel's machine")
	}
	body, ok := h.File(configPath(remote.ID))
	if !ok || !strings.Contains(string(body), "ListenPort = ") {
		t.Fatalf("config on the node = %q", body)
	}

	// Moving is refused, a clashing port on the same node too.
	e := p.wantError(p.do("PATCH", fmt.Sprintf("/api/instances/%d", remote.ID), map[string]any{"node_id": 0}),
		http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["node_id"] == "" {
		t.Fatalf("fields = %v", e.Error.Fields)
	}
	e = p.wantError(p.do("POST", "/api/instances", map[string]any{"name": "Clash", "node_id": n.ID, "listen_port": remote.ListenPort}),
		http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["listen_port"] == "" {
		t.Fatalf("fields = %v", e.Error.Fields)
	}
	p.wantError(p.do("POST", "/api/instances", map[string]any{"name": "Nowhere", "node_id": 99}),
		http.StatusUnprocessableEntity, "validation_failed")
}

func TestOfflineNodeCatchesUpWhenBack(t *testing.T) {
	p := newPanel(t)
	n, h := p.remoteNode("Frankfurt", "203.0.113.5")
	remote := p.createInstance(map[string]any{"name": "Remote", "node_id": n.ID})
	gone := p.createInstance(map[string]any{"name": "Gone", "node_id": n.ID})

	p.nodes().set(n.ID, nil)

	// Changes land in the database and wait for the node.
	peer := p.createPeer(remote.ID, "phone")
	p.want(p.do("DELETE", fmt.Sprintf("/api/instances/%d", gone.ID), nil), http.StatusNoContent, nil)

	var list []instanceJSON
	p.want(p.do("GET", "/api/instances", nil), http.StatusOK, &list)
	if len(list) != 1 || list[0].Status.State != "unknown" {
		t.Fatalf("instances while offline = %+v", list)
	}
	p.wantError(p.do("POST", fmt.Sprintf("/api/instances/%d/restart", remote.ID), nil),
		http.StatusServiceUnavailable, "node_offline")

	body, _ := h.File(configPath(remote.ID))
	if strings.Contains(string(body), peer.PublicKey) {
		t.Fatal("the config changed while the node was offline")
	}

	p.nodes().set(n.ID, h)
	p.s.nodeConnected(context.Background(), n.ID)

	body, _ = h.File(configPath(remote.ID))
	if !strings.Contains(string(body), peer.PublicKey) {
		t.Fatalf("the new peer did not reach the node:\n%s", body)
	}
	if _, ok := h.Container(fmt.Sprintf("tunploy-wg-%d", gone.ID)); ok {
		t.Fatal("the deleted server's container is still on the node")
	}
	if _, ok := h.File(configPath(gone.ID)); ok {
		t.Fatal("the deleted server's config is still on the node")
	}
}

func TestDeleteNode(t *testing.T) {
	p := newPanel(t)
	n, h := p.remoteNode("Frankfurt", "203.0.113.5")
	remote := p.createInstance(map[string]any{"name": "Remote", "node_id": n.ID})

	p.want(p.do("DELETE", fmt.Sprintf("/api/nodes/%d", n.ID), nil), http.StatusNoContent, nil)
	if _, ok := h.Container(fmt.Sprintf("tunploy-wg-%d", remote.ID)); ok {
		t.Fatal("the node's container was left behind")
	}
	p.wantError(p.do("GET", fmt.Sprintf("/api/instances/%d", remote.ID), nil), http.StatusNotFound, "not_found")

	// An offline node is only forgotten when the admin insists.
	off, _ := p.remoteNode("Paris", "203.0.113.6")
	p.nodes().set(off.ID, nil)
	p.wantError(p.do("DELETE", fmt.Sprintf("/api/nodes/%d", off.ID), nil), http.StatusConflict, "node_offline")
	p.want(p.do("DELETE", fmt.Sprintf("/api/nodes/%d?force=true", off.ID), nil), http.StatusNoContent, nil)

	var nodes []nodeJSON
	p.want(p.do("GET", "/api/nodes", nil), http.StatusOK, &nodes)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %+v", nodes)
	}

	events, _ := p.s.store.Events(context.Background(), store.EventFilter{Limit: 10})
	if events[0].Kind != "node.deleted" || events[0].NodeName != "Paris" {
		t.Fatalf("last event = %+v", events[0])
	}
}

func TestCreateNodeValidates(t *testing.T) {
	p := newPanel(t)
	p.remoteNode("Frankfurt", "203.0.113.5")

	e := p.wantError(p.do("POST", "/api/nodes", map[string]any{
		"name": "frankfurt", "host": "203.0.113.5", "username": "bad name",
	}), http.StatusUnprocessableEntity, "validation_failed")
	for _, f := range []string{"name", "host", "username", "host_key"} {
		if e.Error.Fields[f] == "" {
			t.Errorf("no error for %s: %v", f, e.Error.Fields)
		}
	}
	p.wantError(p.do("POST", "/api/nodes/scan", map[string]any{"host": "a b"}),
		http.StatusUnprocessableEntity, "validation_failed")
}

func TestRenameNodeAndChanges(t *testing.T) {
	p := newPanel(t)
	n, _ := p.remoteNode("Frankfurt", "203.0.113.5")

	var got nodeJSON
	p.want(p.do("PATCH", fmt.Sprintf("/api/nodes/%d", n.ID), map[string]any{"name": "FRA-1"}), http.StatusOK, &got)
	if got.Name != "FRA-1" {
		t.Fatalf("renamed = %+v", got)
	}
	p.nodes().onChange(node.Change{Node: store.Node{Name: "FRA-1"}, Online: false, Error: "connection refused"})

	events, _ := p.s.store.Events(context.Background(), store.EventFilter{Limit: 10})
	if events[0].Kind != "node.offline" || events[0].NodeName != "FRA-1" || events[1].Kind != "node.renamed" {
		t.Fatalf("events = %+v", events[:2])
	}
}

func TestPanelKey(t *testing.T) {
	p := newPanel(t)
	var first, second panelKeyView
	p.want(p.do("GET", "/api/nodes/key", nil), http.StatusOK, &first)
	p.want(p.do("GET", "/api/nodes/key", nil), http.StatusOK, &second)
	if !strings.HasPrefix(first.PublicKey, "ssh-ed25519 ") || first != second {
		t.Fatalf("keys = %+v, %+v", first, second)
	}
}
