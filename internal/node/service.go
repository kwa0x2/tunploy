package node

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/host"
	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	maxNameLen = 64
	opTimeout  = 30 * time.Second
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)
	hostPattern     = regexp.MustCompile(`^[A-Za-z0-9.:\-\[\]]+$`)
)

// Connections keeps one connection per node; the Pool in production.
type Connections interface {
	Add(store.Node)
	Remove(nodeID int64)
	Rename(store.Node)
	Host(nodeID int64) (host.Host, error)
}

// Containers readies a new node and clears one that is let go; the deploy
// manager in production.
type Containers interface {
	PrepareNode(ctx context.Context, h host.Host) error
	RemoveNode(ctx context.Context, nodeID int64) error
}

// LocalDocker is the panel's own Docker, which no node may share.
type LocalDocker interface {
	Daemon(ctx context.Context) (docker.Daemon, error)
}

// Service adds, renames and removes nodes.
type Service struct {
	store      *store.Store
	conns      Connections
	containers Containers
	local      LocalDocker
	events     event.Recorder
}

func NewService(st *store.Store, conns Connections, containers Containers, local LocalDocker, events event.Recorder) *Service {
	return &Service{store: st, conns: conns, containers: containers, local: local, events: events}
}

// AddRequest is a machine to make a node of. The credentials are used once,
// to put the panel's own key on it.
type AddRequest struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	HostKey    string `json:"host_key"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`
}

// Check fails on a request Add would refuse, so a caller can answer it
// before setup starts reporting progress.
func (s *Service) Check(ctx context.Context, req AddRequest) error {
	_, err := s.check(ctx, req)
	return err
}

func (s *Service) check(ctx context.Context, req AddRequest) (store.Node, error) {
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
	checkName(n.Name, fields)
	checkTarget(Target{Host: n.Host, Port: n.Port}, fields)
	if !usernamePattern.MatchString(n.Username) {
		fields["username"] = "enter a Linux user name such as root or ubuntu"
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(n.HostKey)); err != nil {
		fields["host_key"] = "scan the server's host key first"
	}

	nodes, err := s.store.Nodes(ctx)
	if err != nil {
		return store.Node{}, err
	}
	for _, other := range nodes {
		if strings.EqualFold(other.Name, n.Name) {
			fields["name"] = "a node with this name already exists"
		}
		if strings.EqualFold(other.Host, n.Host) && other.Port == n.Port {
			fields["host"] = "this server is already a node: " + other.Name
		}
	}
	return n, apperr.Fields(fields)
}

// Add sets a machine up as a node and saves it, reporting each setup step
// to progress as it finishes.
func (s *Service) Add(ctx context.Context, req AddRequest, progress func(Step)) (*store.Node, error) {
	n, err := s.check(ctx, req)
	if err != nil {
		return nil, err
	}
	signer, err := LoadKey(ctx, s.store)
	if err != nil {
		return nil, err
	}
	setup := Setup{Signer: signer, Prepare: s.containers.PrepareNode, Progress: progress}
	if d, err := s.local.Daemon(ctx); err == nil {
		setup.LocalDaemon = d.ID
	}
	_, err = setup.Run(ctx, SetupRequest{
		Target:     Target{Host: n.Host, Port: n.Port, Username: n.Username},
		HostKey:    n.HostKey,
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
		Passphrase: req.Passphrase,
	})
	if err != nil {
		e := &apperr.Error{Kind: apperr.Upstream, Code: "node_setup_failed", Message: err.Error(), Err: err}
		var se *SetupError
		if errors.As(err, &se) {
			e.Fields = map[string]string{"step": string(se.Step)}
		}
		return nil, e
	}

	created, err := s.store.CreateNode(ctx, n)
	if err != nil {
		return nil, writeError(err)
	}
	s.conns.Add(*created)
	s.events.Record(ctx, store.Event{Kind: "node.added", NodeName: created.Name, Detail: created.Host})
	return created, nil
}

func (s *Service) Rename(ctx context.Context, n *store.Node, name string) (*store.Node, error) {
	name = strings.TrimSpace(name)
	fields := map[string]string{}
	checkName(name, fields)
	if err := apperr.Fields(fields); err != nil {
		return nil, err
	}
	renamed, err := s.store.RenameNode(ctx, n.ID, name)
	if err != nil {
		return nil, writeError(err)
	}
	s.conns.Rename(*renamed)
	if renamed.Name != n.Name {
		s.events.Record(ctx, store.Event{Kind: "node.renamed", NodeName: renamed.Name, Detail: "was " + n.Name})
	}
	return renamed, nil
}

// Remove lets a node go. One that is up is left as it was found, apart from
// Docker: no VPN containers, no data directory and no panel key. One that
// is down can only be forgotten, and force says the admin knows its servers
// keep running there.
func (s *Service) Remove(ctx context.Context, n *store.Node, force bool) error {
	h, err := s.conns.Host(n.ID)
	switch {
	case err == nil:
		if err := s.clear(ctx, n, h); err != nil {
			return err
		}
	case errors.Is(err, host.ErrOffline):
		if !force {
			return apperr.New(apperr.Conflict, "node_offline",
				"the node is offline, so its VPN containers cannot be removed; delete it anyway to forget it")
		}
	default:
		return err
	}

	s.conns.Remove(n.ID)
	if err := s.store.DeleteNode(ctx, n.ID); err != nil {
		return err
	}
	s.events.Record(ctx, store.Event{Kind: "node.deleted", NodeName: n.Name, Detail: n.Host})
	return nil
}

func (s *Service) clear(ctx context.Context, n *store.Node, h host.Host) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := s.containers.RemoveNode(ctx, n.ID); err != nil {
		return apperr.New(apperr.Upstream, "node_cleanup_failed", "remove the VPN containers: %v", err)
	}
	if err := RemoveData(ctx, h); err != nil {
		return apperr.New(apperr.Upstream, "node_cleanup_failed", "remove %s: %v", Root, err)
	}
	signer, err := LoadKey(ctx, s.store)
	if err != nil {
		return err
	}
	if err := Deauthorize(ctx, h, signer); err != nil {
		slog.Warn("remove the panel's key from a node", "node", n.ID, "error", err)
	}
	return nil
}

// Scan reads a machine's SSH host key, for the admin to compare with the
// server's own before trusting it.
func (s *Service) Scan(ctx context.Context, t Target) (string, error) {
	t.Host = strings.TrimSpace(t.Host)
	if t.Port == 0 {
		t.Port = 22
	}
	fields := map[string]string{}
	checkTarget(t, fields)
	if err := apperr.Fields(fields); err != nil {
		return "", err
	}
	key, err := ScanHostKey(ctx, t)
	if err != nil {
		return "", apperr.New(apperr.Upstream, "ssh_unreachable", "%v", err)
	}
	return key, nil
}

func checkName(name string, fields map[string]string) {
	switch {
	case name == "":
		fields["name"] = "name is required"
	case len(name) > maxNameLen:
		fields["name"] = "name must be at most " + strconv.Itoa(maxNameLen) + " characters"
	}
}

func checkTarget(t Target, fields map[string]string) {
	if t.Host == "" || len(t.Host) > 253 || !hostPattern.MatchString(t.Host) {
		fields["host"] = "enter the server's IP address or host name"
	}
	if t.Port < 1 || t.Port > 65535 {
		fields["port"] = "port must be between 1 and 65535"
	}
}

func writeError(err error) error {
	var dup *store.DuplicateError
	if errors.As(err, &dup) {
		switch dup.Column {
		case "name":
			return apperr.Field("name", "a node with this name already exists")
		case "port", "host":
			return apperr.Field("host", "this server is already a node")
		}
	}
	return err
}
