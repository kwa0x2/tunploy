package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/node"
	"github.com/kwa0x2/tunploy/internal/store"
)

// NodePool reports how each node's connection is doing; node.Pool in production.
type NodePool interface {
	Status(nodeID int64) node.Status
}

type nodeView struct {
	store.Node
	Local       bool        `json:"local"`
	Status      node.Status `json:"status"`
	ServerCount int         `json:"server_count"`
	Fingerprint string      `json:"host_key_fingerprint,omitempty"`
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) error {
	nodes, err := s.store.Nodes(r.Context())
	if err != nil {
		return err
	}
	counts, err := s.serverCounts(r.Context())
	if err != nil {
		return err
	}

	local, err := s.localNode(r.Context())
	if err != nil {
		return err
	}
	local.ServerCount = counts[0]
	views := []nodeView{local}
	for _, n := range nodes {
		views = append(views, s.nodeView(n, counts[n.ID]))
	}
	return httpx.JSON(w, http.StatusOK, views)
}

// The panel's own machine is listed first, with ID 0.
func (s *Server) localNode(ctx context.Context) (nodeView, error) {
	host, err := s.instances.PublicHost(ctx)
	if err != nil {
		return nodeView{}, err
	}
	v := nodeView{
		Node:  store.Node{Name: "This server", Host: host},
		Local: true,
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if d, err := s.docker.Daemon(ctx); err != nil {
		v.Status = node.Status{State: node.StateOffline, Error: err.Error()}
	} else {
		v.Status = node.Status{State: node.StateOnline, Daemon: &d}
	}
	return v, nil
}

func (s *Server) nodeView(n store.Node, servers int) nodeView {
	return nodeView{
		Node:        n,
		Status:      s.pool.Status(n.ID),
		ServerCount: servers,
		Fingerprint: node.Fingerprint(n.HostKey),
	}
}

func (s *Server) serverCounts(ctx context.Context) (map[int64]int, error) {
	instances, err := s.store.Instances(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	for _, in := range instances {
		counts[in.NodeID]++
	}
	return counts, nil
}

type panelKeyView struct {
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}

func (s *Server) handlePanelKey(w http.ResponseWriter, r *http.Request) error {
	signer, err := node.LoadKey(r.Context(), s.store)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, panelKeyView{
		PublicKey:   node.AuthorizedKey(signer),
		Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
	})
}

type scanRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type scanView struct {
	HostKey     string `json:"host_key"`
	Fingerprint string `json:"fingerprint"`
	Algorithm   string `json:"algorithm"`
}

func (s *Server) handleScanNode(w http.ResponseWriter, r *http.Request) error {
	var req scanRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	key, err := s.nodes.Scan(r.Context(), node.Target{Host: req.Host, Port: req.Port})
	if err != nil {
		return err
	}
	algo, _, _ := strings.Cut(key, " ")
	return httpx.JSON(w, http.StatusOK, scanView{HostKey: key, Fingerprint: node.Fingerprint(key), Algorithm: algo})
}

type nodeEvent struct {
	Step  node.Step    `json:"step,omitempty"`
	Node  *nodeView    `json:"node,omitempty"`
	Error *httpx.Error `json:"error,omitempty"`
	Log   []string     `json:"log,omitempty"`
}

// Validation runs first and fails as a normal response; after that the
// setup steps stream as NDJSON and the last line is the outcome.
func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) error {
	var req node.AddRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := s.nodes.Check(r.Context(), req); err != nil {
		return err
	}

	send := streamNDJSON(w)

	created, err := s.nodes.Add(r.Context(), req, func(st node.Step) { send(nodeEvent{Step: st}) })
	if err != nil {
		ev := nodeEvent{Error: httpx.ErrorFor(r, err)}
		var se *node.SetupError
		if errors.As(err, &se) {
			ev.Log = se.Log
		}
		send(ev)
		return nil
	}
	view := s.nodeView(*created, 0)
	send(nodeEvent{Node: &view})
	return nil
}

func (s *Server) nodeFromPath(r *http.Request) (*store.Node, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return nil, httpx.NotFound("node not found")
	}
	n, err := s.store.NodeByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpx.NotFound("node not found")
	}
	return n, err
}

type renameNodeRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) error {
	n, err := s.nodeFromPath(r)
	if err != nil {
		return err
	}
	var req renameNodeRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	renamed, err := s.nodes.Rename(r.Context(), n, req.Name)
	if err != nil {
		return err
	}
	counts, err := s.serverCounts(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, s.nodeView(*renamed, counts[n.ID]))
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) error {
	n, err := s.nodeFromPath(r)
	if err != nil {
		return err
	}
	force := r.URL.Query().Get("force") == "true"
	if err := s.nodes.Remove(r.Context(), n, force); err != nil {
		return err
	}
	return httpx.NoContent(w)
}
