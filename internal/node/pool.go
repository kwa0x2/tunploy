package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	keepaliveEvery = 10 * time.Second
	keepaliveWait  = 15 * time.Second
	touchEvery     = time.Minute
	retryMin       = 5 * time.Second
	retryMax       = time.Minute

	settingKey = "ssh_private_key"
)

type State string

const (
	StateConnecting State = "connecting"
	StateOnline     State = "online"
	StateOffline    State = "offline"
)

type Status struct {
	State  State          `json:"state"`
	Error  string         `json:"error,omitempty"`
	Since  time.Time      `json:"since,omitzero"`
	Daemon *docker.Daemon `json:"daemon,omitempty"`
}

// Change is a node going offline after it was online, or coming back.
type Change struct {
	Node   store.Node
	Online bool
	Error  string
}

// Pool keeps one SSH connection per node and reconnects on its own.
type Pool struct {
	store     *store.Store
	onConnect func(ctx context.Context, nodeID int64)
	onChange  func(Change)

	mu     sync.Mutex
	ctx    context.Context
	signer ssh.Signer
	conns  map[int64]*conn
}

func NewPool(st *store.Store) *Pool {
	return &Pool{store: st, conns: map[int64]*conn{}}
}

// OnConnect runs after every successful connection, in its own goroutine,
// so the node can be brought in line with the database. Set it before Start.
func (p *Pool) OnConnect(fn func(ctx context.Context, nodeID int64)) { p.onConnect = fn }

// OnChange must be set before Start.
func (p *Pool) OnChange(fn func(Change)) { p.onChange = fn }

// Start connects to every node and keeps at it until ctx ends.
func (p *Pool) Start(ctx context.Context) error {
	p.mu.Lock()
	p.ctx = ctx
	p.mu.Unlock()
	return p.Reload(ctx)
}

// Reload drops every connection and starts over from the database, as after
// a restore swapped it out.
func (p *Pool) Reload(ctx context.Context) error {
	signer, err := LoadKey(ctx, p.store)
	if err != nil {
		return err
	}
	nodes, err := p.store.Nodes(ctx)
	if err != nil {
		return err
	}

	p.mu.Lock()
	old := p.conns
	p.conns = map[int64]*conn{}
	p.signer = signer
	p.mu.Unlock()
	for _, c := range old {
		c.stop()
	}
	for _, n := range nodes {
		p.Add(n)
	}
	return nil
}

func (p *Pool) Add(n store.Node) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		return
	}
	if old, ok := p.conns[n.ID]; ok {
		go old.stop()
	}
	ctx, cancel := context.WithCancel(p.ctx)
	c := &conn{
		pool:   p,
		node:   n,
		signer: p.signer,
		cancel: cancel,
		done:   make(chan struct{}),
		status: Status{State: StateConnecting, Since: time.Now()},
	}
	p.conns[n.ID] = c
	go c.run(ctx)
}

// Remove disconnects from a node and waits until it is left alone.
func (p *Pool) Remove(nodeID int64) {
	p.mu.Lock()
	c, ok := p.conns[nodeID]
	delete(p.conns, nodeID)
	p.mu.Unlock()
	if ok {
		c.stop()
	}
}

// Rename keeps change reports in step with the node's name.
func (p *Pool) Rename(n store.Node) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.conns[n.ID]; ok {
		c.mu.Lock()
		c.node.Name = n.Name
		c.mu.Unlock()
	}
}

func (p *Pool) conn(nodeID int64) *conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conns[nodeID]
}

// Host is the deploy manager's way in; it fails fast while a node is down.
func (p *Pool) Host(nodeID int64) (deploy.Host, error) {
	c := p.conn(nodeID)
	if c == nil {
		return nil, deploy.ErrNodeOffline
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.host == nil {
		return nil, deploy.ErrNodeOffline
	}
	return c.host, nil
}

func (p *Pool) Status(nodeID int64) Status {
	c := p.conn(nodeID)
	if c == nil {
		return Status{State: StateOffline, Error: "not connected"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (p *Pool) Signer() ssh.Signer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.signer
}

// LoadKey returns the panel's SSH key, making one the first time. It lives in
// the database so a restored panel can still reach its nodes.
func LoadKey(ctx context.Context, st *store.Store) (ssh.Signer, error) {
	settings, err := st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	pemKey := settings[settingKey]
	if pemKey == "" {
		if pemKey, err = GenerateKey(); err != nil {
			return nil, err
		}
		if err := st.SaveSettings(ctx, map[string]string{settingKey: pemKey}); err != nil {
			return nil, err
		}
	}
	signer, err := ssh.ParsePrivateKey([]byte(pemKey))
	if err != nil {
		return nil, fmt.Errorf("panel ssh key is corrupt: %w", err)
	}
	return signer, nil
}

type conn struct {
	pool   *Pool
	signer ssh.Signer
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	node   store.Node
	host   *Host
	status Status
	// Only a node reported offline is reported back online.
	reportedDown bool
}

func (c *conn) stop() {
	c.cancel()
	<-c.done
}

func (c *conn) run(ctx context.Context) {
	defer close(c.done)
	retry := retryMin
	for ctx.Err() == nil {
		err := c.session(ctx)
		if ctx.Err() != nil {
			break
		}
		if err == nil {
			// The connection dropped after working; try again straight away so
			// a blip never reports the node offline.
			retry = retryMin
			continue
		}
		c.setOffline(err)
		select {
		case <-ctx.Done():
		case <-time.After(retry):
		}
		retry = min(retry*2, retryMax)
	}
	c.mu.Lock()
	c.clearHost()
	c.mu.Unlock()
}

// session connects and stays until the connection drops. An error means it
// never got up.
func (c *conn) session(ctx context.Context) error {
	c.mu.Lock()
	n := c.node
	c.mu.Unlock()

	client, err := dial(ctx, Target{Host: n.Host, Port: n.Port, Username: n.Username},
		[]ssh.AuthMethod{ssh.PublicKeys(c.signer)}, n.HostKey)
	if err != nil {
		return err
	}
	defer client.Close()

	h, err := newHost(shell{client: client, sudo: n.Username != "root"})
	if err != nil {
		return err
	}
	infoCtx, cancel := context.WithTimeout(ctx, keepaliveWait)
	daemon, err := h.Daemon(infoCtx)
	cancel()
	if err != nil {
		h.Close()
		return fmt.Errorf("docker on the node: %w", err)
	}
	c.setOnline(h, daemon)
	if c.pool.onConnect != nil {
		go c.pool.onConnect(ctx, n.ID)
	}

	err = keepalive(ctx, client, func() { c.touch(ctx, n.ID) })
	if ctx.Err() == nil {
		slog.Info("node connection dropped", "node", n.ID, "error", err)
		c.touch(ctx, n.ID)
	}
	c.mu.Lock()
	c.clearHost()
	c.mu.Unlock()
	return nil
}

func keepalive(ctx context.Context, client *ssh.Client, touch func()) error {
	closed := make(chan error, 1)
	go func() { closed <- client.Wait() }()

	tick := time.NewTicker(keepaliveEvery)
	defer tick.Stop()
	lastTouch := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-closed:
			return err
		case <-tick.C:
		}
		reply := make(chan error, 1)
		go func() {
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			reply <- err
		}()
		select {
		case err := <-reply:
			if err != nil {
				return err
			}
		case <-time.After(keepaliveWait):
			return errors.New("keepalive timed out")
		case <-ctx.Done():
			return ctx.Err()
		}
		if time.Since(lastTouch) >= touchEvery {
			touch()
			lastTouch = time.Now()
		}
	}
}

func (c *conn) touch(ctx context.Context, nodeID int64) {
	if err := c.pool.store.TouchNode(ctx, nodeID, time.Now()); err != nil && ctx.Err() == nil {
		slog.Warn("record node last seen", "node", nodeID, "error", err)
	}
}

// clearHost needs c.mu held.
func (c *conn) clearHost() {
	if c.host != nil {
		c.host.Close()
		c.host = nil
	}
}

func (c *conn) setOnline(h *Host, daemon docker.Daemon) {
	c.mu.Lock()
	was, back := c.status.State, c.reportedDown
	c.clearHost()
	c.host = h
	if was != StateOnline {
		c.status = Status{State: StateOnline, Since: time.Now()}
	}
	c.status.Daemon = &daemon
	c.status.Error = ""
	c.reportedDown = false
	n := c.node
	c.mu.Unlock()

	c.touch(context.Background(), n.ID)
	if back {
		slog.Info("node is back online", "node", n.ID)
		c.report(Change{Node: n, Online: true})
	}
}

func (c *conn) setOffline(err error) {
	c.mu.Lock()
	was := c.status.State
	c.clearHost()
	if was != StateOffline {
		c.status = Status{State: StateOffline, Since: time.Now(), Daemon: c.status.Daemon}
	}
	c.status.Error = err.Error()
	if was == StateOnline {
		c.reportedDown = true
	}
	n := c.node
	c.mu.Unlock()

	if was != StateOffline {
		slog.Warn("node is offline", "node", n.ID, "error", err)
	}
	if was == StateOnline {
		c.report(Change{Node: n, Online: false, Error: err.Error()})
	}
}

func (c *conn) report(ch Change) {
	if c.pool.onChange != nil {
		c.pool.onChange(ch)
	}
}
