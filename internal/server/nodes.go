package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/node"
	"github.com/kwa0x2/tunploy/internal/store"
)

type Nodes interface {
	OnConnect(func(ctx context.Context, nodeID int64))
	OnChange(func(node.Change))
	Add(store.Node)
	Remove(nodeID int64)
	Rename(store.Node)
	Status(nodeID int64) node.Status
	Host(nodeID int64) (deploy.Host, error)
	Reload(ctx context.Context) error
}

const (
	maxNodeName   = 64
	nodeOpTimeout = 30 * time.Second
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)
	hostPattern     = regexp.MustCompile(`^[A-Za-z0-9.:\-\[\]]+$`)
)

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
	settings, err := s.settings(ctx)
	if err != nil {
		return nodeView{}, err
	}
	v := nodeView{
		Node:  store.Node{Name: "This server", Host: settings.publicHost(s.cfg.PublicHost)},
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
		Status:      s.nodes.Status(n.ID),
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

// The admin compares the fingerprint with the server's before trusting it.
func (s *Server) handleScanNode(w http.ResponseWriter, r *http.Request) error {
	var req scanRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	t := node.Target{Host: strings.TrimSpace(req.Host), Port: req.Port}
	if t.Port == 0 {
		t.Port = 22
	}
	fields := map[string]string{}
	validateTarget(t, fields)
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}

	key, err := node.ScanHostKey(r.Context(), t)
	if err != nil {
		return httpx.Errorf(http.StatusBadGateway, "ssh_unreachable", "%v", err)
	}
	algo, _, _ := strings.Cut(key, " ")
	return httpx.JSON(w, http.StatusOK, scanView{HostKey: key, Fingerprint: node.Fingerprint(key), Algorithm: algo})
}

type nodeRequest struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	HostKey    string `json:"host_key"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`
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
	var req nodeRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	n, fields := req.node()
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(n.HostKey)); err != nil {
		fields["host_key"] = "scan the server's host key first"
	}
	if err := s.checkNodeUnique(r.Context(), n, fields); err != nil {
		return err
	}
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	signer, err := node.LoadKey(r.Context(), s.store)
	if err != nil {
		return err
	}

	h := w.Header()
	h.Set("Content-Type", ndjson)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	enc := json.NewEncoder(w)
	send := func(ev nodeEvent) {
		if err := enc.Encode(ev); err == nil {
			rc.Flush()
		}
	}

	setup := node.Setup{
		Signer:   signer,
		Prepare:  s.deploy.PrepareNode,
		Progress: func(st node.Step) { send(nodeEvent{Step: st}) },
	}
	if d, err := s.docker.Daemon(r.Context()); err == nil {
		setup.LocalDaemon = d.ID
	}
	_, err = setup.Run(r.Context(), node.SetupRequest{
		Target:     node.Target{Host: n.Host, Port: n.Port, Username: n.Username},
		HostKey:    n.HostKey,
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
		Passphrase: req.Passphrase,
	})
	if err != nil {
		ev := nodeEvent{Error: httpx.Errorf(http.StatusBadGateway, "node_setup_failed", "%v", err)}
		var se *node.SetupError
		if errors.As(err, &se) {
			ev.Error.Fields = map[string]string{"step": string(se.Step)}
			ev.Log = se.Log
		}
		send(ev)
		return nil
	}

	created, err := s.store.CreateNode(r.Context(), n)
	if err != nil {
		var he *httpx.Error
		if !errors.As(nodeWriteError(err), &he) {
			slog.Error("save node", "error", err)
			he = httpx.Errorf(http.StatusInternalServerError, "internal_error", "something went wrong")
		}
		send(nodeEvent{Error: he})
		return nil
	}
	s.nodes.Add(*created)
	s.record(r.Context(), store.Event{Kind: "node.added", NodeName: created.Name, Detail: created.Host})
	view := s.nodeView(*created, 0)
	send(nodeEvent{Node: &view})
	return nil
}

func (req nodeRequest) node() (store.Node, map[string]string) {
	n := store.Node{
		Name:     strings.TrimSpace(req.Name),
		Host:     strings.TrimSpace(req.Host),
		Port:     req.Port,
		Username: strings.TrimSpace(req.Username),
		HostKey:  strings.TrimSpace(req.HostKey),
	}
	if n.Port == 0 {
		n.Port = 22
	}
	if n.Username == "" {
		n.Username = "root"
	}
	fields := map[string]string{}
	validateNodeName(n.Name, fields)
	validateTarget(node.Target{Host: n.Host, Port: n.Port}, fields)
	if !usernamePattern.MatchString(n.Username) {
		fields["username"] = "enter a Linux user name such as root or ubuntu"
	}
	return n, fields
}

func validateNodeName(name string, fields map[string]string) {
	switch {
	case name == "":
		fields["name"] = "name is required"
	case len(name) > maxNodeName:
		fields["name"] = "name must be at most " + strconv.Itoa(maxNodeName) + " characters"
	}
}

func validateTarget(t node.Target, fields map[string]string) {
	if t.Host == "" || len(t.Host) > 253 || !hostPattern.MatchString(t.Host) {
		fields["host"] = "enter the server's IP address or host name"
	}
	if t.Port < 1 || t.Port > 65535 {
		fields["port"] = "port must be between 1 and 65535"
	}
}

func (s *Server) checkNodeUnique(ctx context.Context, n store.Node, fields map[string]string) error {
	nodes, err := s.store.Nodes(ctx)
	if err != nil {
		return err
	}
	for _, other := range nodes {
		if other.ID == n.ID {
			continue
		}
		if strings.EqualFold(other.Name, n.Name) {
			fields["name"] = "a node with this name already exists"
		}
		if strings.EqualFold(other.Host, n.Host) && other.Port == n.Port {
			fields["host"] = "this server is already a node: " + other.Name
		}
	}
	return nil
}

func nodeWriteError(err error) error {
	var dup *store.DuplicateError
	if errors.As(err, &dup) {
		switch dup.Column {
		case "name":
			return httpx.Invalid(map[string]string{"name": "a node with this name already exists"})
		case "port", "host":
			return httpx.Invalid(map[string]string{"host": "this server is already a node"})
		}
	}
	return err
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
	name := strings.TrimSpace(req.Name)
	fields := map[string]string{}
	validateNodeName(name, fields)
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	renamed, err := s.store.RenameNode(r.Context(), n.ID, name)
	if err != nil {
		return nodeWriteError(err)
	}
	s.nodes.Rename(*renamed)
	if renamed.Name != n.Name {
		s.record(r.Context(), store.Event{Kind: "node.renamed", NodeName: renamed.Name, Detail: "was " + n.Name})
	}
	counts, err := s.serverCounts(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, s.nodeView(*renamed, counts[n.ID]))
}

// A node that is up is left as it was found, apart from Docker: no VPN
// containers, no data directory and no panel key. One that is down can
// only be forgotten, and ?force=true says the admin knows its servers keep
// running there.
func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) error {
	n, err := s.nodeFromPath(r)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), nodeOpTimeout)
	defer cancel()

	h, err := s.nodes.Host(n.ID)
	switch {
	case err == nil:
		if err := s.deploy.RemoveNode(ctx, n.ID); err != nil {
			return httpx.Errorf(http.StatusBadGateway, "node_cleanup_failed", "remove the VPN containers: %v", err)
		}
		if err := node.RemoveData(ctx, h); err != nil {
			return httpx.Errorf(http.StatusBadGateway, "node_cleanup_failed", "remove %s: %v", node.Root, err)
		}
		signer, err := node.LoadKey(ctx, s.store)
		if err != nil {
			return err
		}
		if err := node.Deauthorize(ctx, h, signer); err != nil {
			slog.Warn("remove the panel's key from a node", "node", n.ID, "error", err)
		}
	case errors.Is(err, deploy.ErrNodeOffline):
		if r.URL.Query().Get("force") != "true" {
			return httpx.Errorf(http.StatusConflict, "node_offline",
				"the node is offline, so its VPN containers cannot be removed; delete it anyway to forget it")
		}
	default:
		return err
	}

	s.nodes.Remove(n.ID)
	if err := s.store.DeleteNode(r.Context(), n.ID); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "node.deleted", NodeName: n.Name, Detail: n.Host})
	return httpx.NoContent(w)
}

func (s *Server) nodeChanged(c node.Change) {
	kind := "node.online"
	if !c.Online {
		kind = "node.offline"
	}
	s.record(context.Background(), store.Event{Kind: kind, NodeName: c.Node.Name, Detail: c.Error})
}

func (s *Server) nodeConnected(ctx context.Context, nodeID int64) {
	if err := s.deploy.NodeUp(ctx, nodeID); err != nil && ctx.Err() == nil {
		slog.Error("bring node in line", "node", nodeID, "error", err)
	}
}
