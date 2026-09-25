// Package deploy runs each WireGuard instance as a Docker container.
package deploy

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/moby/moby/api/types/network"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

//go:embed image
var imageFS embed.FS

const (
	LabelInstance = "io.tunploy.instance"

	// Panels sharing a Docker daemon need different prefixes. It must end in
	// "-" so a name splits back into exactly one prefix and instance ID.
	DefaultContainerPrefix = "tunploy-wg-"

	iface       = "wg0"
	stopTimeout = 10 * time.Second
)

type Docker interface {
	ImageExists(ctx context.Context, ref string) (bool, error)
	BuildImage(ctx context.Context, tag string, files map[string][]byte) error
	CreateContainer(ctx context.Context, spec docker.ContainerSpec) (string, error)
	InspectContainer(ctx context.Context, id string) (docker.Container, error)
	ListContainers(ctx context.Context) ([]docker.Container, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeout time.Duration) error
	RemoveContainer(ctx context.Context, id string) error
	Exec(ctx context.Context, id string, cmd []string) ([]byte, error)
	Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error)
}

type Manager struct {
	store  *store.Store
	local  Host
	remote func(nodeID int64) (Host, error)
	prefix string
	image  string
	files  map[string][]byte

	// One lock per node serialises its container changes, so requests never
	// race to recreate one and a slow node never holds up another.
	locksMu sync.Mutex
	locks   map[int64]*sync.Mutex

	activity     activity
	onPeerChange func(PeerChange)
	onPeerBlock  func(PeerBlock)
	now          func() time.Time

	stateMu sync.Mutex
	// What each instance's config was last written with.
	blocked map[int64]map[int64]wg.Block
	// Whether each instance looked down on the last watch.
	down           map[int64]bool
	onServerHealth func(ServerHealth)
	// Nodes to rebuild instead of reconcile when they next come up.
	rebuild map[int64]bool
}

type ServerHealth struct {
	InstanceID int64
	Down       bool
	Reason     string
}

type PeerBlock struct {
	InstanceID int64
	Peer       wg.Peer
	// Empty when the peer may connect again.
	Reason wg.Block
	Month  wg.Traffic
}

func New(st *store.Store, dk Docker, dataDir, prefix string) (*Manager, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(imageFS, "image", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := imageFS.ReadFile(path)
		files[filepath.Base(path)] = body
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("load image context: %w", err)
	}

	return &Manager{
		store:    st,
		local:    localHost{Docker: dk, root: dataDir},
		prefix:   prefix,
		image:    imageTag(files),
		files:    files,
		locks:    map[int64]*sync.Mutex{},
		activity: activity{peers: map[int64]map[wg.Key]sample{}},
		blocked:  map[int64]map[int64]wg.Block{},
		now:      time.Now,
		down:     map[int64]bool{},
		rebuild:  map[int64]bool{},
	}, nil
}

// SetRemote must be called before Watch starts or requests arrive; without
// it every node other than the panel's own counts as offline.
func (m *Manager) SetRemote(fn func(nodeID int64) (Host, error)) { m.remote = fn }

func (m *Manager) host(nodeID int64) (Host, error) {
	if nodeID == 0 {
		return m.local, nil
	}
	if m.remote == nil {
		return nil, ErrNodeOffline
	}
	return m.remote(nodeID)
}

func (m *Manager) lock(nodeID int64) func() {
	m.locksMu.Lock()
	mu, ok := m.locks[nodeID]
	if !ok {
		mu = &sync.Mutex{}
		m.locks[nodeID] = mu
	}
	m.locksMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// acquire loads the instance, reaches its node and locks it.
func (m *Manager) acquire(ctx context.Context, instanceID int64) (*wg.Instance, Host, func(), error) {
	in, err := m.store.InstanceByID(ctx, instanceID)
	if err != nil {
		return nil, nil, nil, err
	}
	h, err := m.host(in.NodeID)
	if err != nil {
		return nil, nil, nil, err
	}
	return in, h, m.lock(in.NodeID), nil
}

// A new tag per build context lets Reconcile move containers onto upgrades.
func imageTag(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)

	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(files[name]))
		h.Write(files[name])
	}
	return "tunploy/wireguard:" + hex.EncodeToString(h.Sum(nil))[:12]
}

func (m *Manager) Image() string { return m.image }

func (m *Manager) ContainerName(instanceID int64) string {
	return m.prefix + strconv.FormatInt(instanceID, 10)
}

// Another panel on the same daemon labels its containers the same way, so the
// name is what tells ours apart.
func (m *Manager) owns(ct docker.Container) bool {
	id, ok := ct.Labels[LabelInstance]
	return ok && ct.Name == m.prefix+id
}

// ConfigDir is where an instance on the panel's own machine keeps its config.
func (m *Manager) ConfigDir(instanceID int64) string {
	return configDir(m.local, instanceID)
}

func configDir(h Host, instanceID int64) string {
	return filepath.Join(h.Root(), "wireguard", strconv.FormatInt(instanceID, 10))
}

func (m *Manager) Deploy(ctx context.Context, instanceID int64, start bool) error {
	in, h, unlock, err := m.acquire(ctx, instanceID)
	if err != nil {
		return err
	}
	defer unlock()
	return m.deploy(ctx, h, in, start, nil)
}

func (m *Manager) Provision(ctx context.Context, instanceID int64, progress Progress) error {
	in, h, unlock, err := m.acquire(ctx, instanceID)
	if err != nil {
		return err
	}
	defer unlock()
	return m.deploy(ctx, h, in, true, progress)
}

// Port and MTU are fixed at container creation, so changing them needs this.
func (m *Manager) Redeploy(ctx context.Context, instanceID int64) error {
	in, h, unlock, err := m.acquire(ctx, instanceID)
	if err != nil {
		return err
	}
	defer unlock()

	ct, err := m.container(ctx, h, instanceID)
	if err != nil {
		return err
	}
	return m.deploy(ctx, h, in, ct == nil || ct.Running(), nil)
}

func (m *Manager) deploy(ctx context.Context, h Host, in *wg.Instance, start bool, progress Progress) error {
	if err := m.writeConfig(ctx, h, in); err != nil {
		return err
	}
	if err := m.ensureImage(ctx, h); err != nil {
		return err
	}
	progress.report(StepImage)

	name := m.ContainerName(in.ID)
	if err := h.RemoveContainer(ctx, name); err != nil && !errors.Is(err, docker.ErrNotFound) {
		return err
	}

	port := uint16(in.ListenPort)
	_, err := h.CreateContainer(ctx, docker.ContainerSpec{
		Name:   name,
		Image:  m.image,
		Labels: map[string]string{LabelInstance: strconv.FormatInt(in.ID, 10)},
		Ports:  []docker.Port{{Host: port, Container: port, Protocol: network.UDP}},
		Mounts: []docker.Mount{{Source: configDir(h, in.ID), Target: "/etc/wireguard", ReadOnly: true}},
		CapAdd: []string{"NET_ADMIN"},
		Sysctls: map[string]string{
			"net.ipv4.ip_forward": "1",
		},
	})
	if err != nil {
		return err
	}
	if !start {
		return nil
	}
	if err := h.StartContainer(ctx, name); err != nil {
		return err
	}
	progress.report(StepContainer)
	return m.waitReady(ctx, h, name, progress)
}

func (m *Manager) ensureImage(ctx context.Context, h Host) error {
	ok, err := h.ImageExists(ctx, m.image)
	if err != nil || ok {
		return err
	}
	return h.BuildImage(ctx, m.image, m.files)
}

// PrepareNode builds the WireGuard image on a node ahead of its first server.
func (m *Manager) PrepareNode(ctx context.Context, h Host) error {
	return m.ensureImage(ctx, h)
}

func (m *Manager) Start(ctx context.Context, instanceID int64) error {
	in, h, unlock, err := m.acquire(ctx, instanceID)
	if err != nil {
		return err
	}
	defer unlock()

	ct, err := m.container(ctx, h, instanceID)
	if err != nil {
		return err
	}
	if ct == nil {
		return m.deploy(ctx, h, in, true, nil)
	}
	if err := m.writeConfig(ctx, h, in); err != nil {
		return err
	}
	if ct.Running() {
		return nil
	}
	return h.StartContainer(ctx, ct.Name)
}

func (m *Manager) Stop(ctx context.Context, instanceID int64) error {
	_, h, unlock, err := m.acquire(ctx, instanceID)
	if err != nil {
		return err
	}
	defer unlock()

	ct, err := m.container(ctx, h, instanceID)
	if err != nil || ct == nil {
		return err
	}
	return h.StopContainer(ctx, ct.Name, stopTimeout)
}

func (m *Manager) Restart(ctx context.Context, instanceID int64) error {
	in, h, unlock, err := m.acquire(ctx, instanceID)
	if err != nil {
		return err
	}
	defer unlock()

	ct, err := m.container(ctx, h, instanceID)
	if err != nil {
		return err
	}
	if ct == nil {
		return m.deploy(ctx, h, in, true, nil)
	}
	if err := m.writeConfig(ctx, h, in); err != nil {
		return err
	}
	if err := h.StopContainer(ctx, ct.Name, stopTimeout); err != nil {
		return err
	}
	return h.StartContainer(ctx, ct.Name)
}

// Remove must run before the row is deleted. On an offline node it returns
// ErrNodeOffline; the node's next reconcile removes what is left.
func (m *Manager) Remove(ctx context.Context, instanceID int64) error {
	_, h, unlock, err := m.acquire(ctx, instanceID)
	if err != nil {
		return err
	}
	defer unlock()

	err = h.RemoveContainer(ctx, m.ContainerName(instanceID))
	if err != nil && !errors.Is(err, docker.ErrNotFound) {
		return err
	}
	if err := h.RemoveAll(ctx, configDir(h, instanceID)); err != nil {
		return err
	}
	m.forget(instanceID)
	return nil
}

func (m *Manager) forget(instanceID int64) {
	m.activity.forget(instanceID)
	m.stateMu.Lock()
	delete(m.blocked, instanceID)
	delete(m.down, instanceID)
	m.stateMu.Unlock()
}

// Rebuild replaces every container after the database was swapped out, since
// the same instance ID may now stand for a different server. Other nodes are
// rebuilt when they next come up.
func (m *Manager) Rebuild(ctx context.Context) error {
	if err := m.MarkRebuild(ctx); err != nil {
		return err
	}
	return m.NodeUp(ctx, 0)
}

// MarkRebuild makes every node rebuild instead of reconcile when it next comes up.
func (m *Manager) MarkRebuild(ctx context.Context) error {
	nodes, err := m.store.Nodes(ctx)
	if err != nil {
		return err
	}
	m.activity.reset()
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.blocked = map[int64]map[int64]wg.Block{}
	m.down = map[int64]bool{}
	m.rebuild = map[int64]bool{0: true}
	for _, n := range nodes {
		m.rebuild[n.ID] = true
	}
	return nil
}

func (m *Manager) takeRebuild(nodeID int64) bool {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	pending := m.rebuild[nodeID]
	delete(m.rebuild, nodeID)
	return pending
}

func (m *Manager) rebuildNode(ctx context.Context, nodeID int64, h Host, instances []wg.Instance) error {
	containers, err := h.ListContainers(ctx)
	if err != nil {
		return err
	}
	for _, ct := range containers {
		if !m.owns(ct) {
			continue
		}
		if err := h.RemoveContainer(ctx, ct.Name); err != nil && !errors.Is(err, docker.ErrNotFound) {
			return err
		}
	}
	if err := m.removeStaleConfigs(ctx, h, instances); err != nil {
		return err
	}

	var errs []error
	for i := range instances {
		if err := m.deploy(ctx, h, &instances[i], true, nil); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", instances[i].Name, err))
		}
	}
	return errors.Join(errs...)
}

// Only stale directories go: Docker Desktop loses track of a bind mount
// whose parent was deleted and made again.
func (m *Manager) removeStaleConfigs(ctx context.Context, h Host, instances []wg.Instance) error {
	known := map[string]bool{}
	for _, in := range instances {
		known[strconv.FormatInt(in.ID, 10)] = true
	}
	root := filepath.Join(h.Root(), "wireguard")
	names, err := h.ReadDir(ctx, root)
	if err != nil {
		return err
	}
	for _, name := range names {
		if !known[name] {
			if err := h.RemoveAll(ctx, filepath.Join(root, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// syncconf applies peer changes without dropping connected peers. On an
// offline node the change waits in the database for its next reconcile.
func (m *Manager) Apply(ctx context.Context, instanceID int64) error {
	in, h, unlock, err := m.acquire(ctx, instanceID)
	if errors.Is(err, ErrNodeOffline) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	return m.apply(ctx, h, in)
}

func (m *Manager) apply(ctx context.Context, h Host, in *wg.Instance) error {
	if err := m.writeConfig(ctx, h, in); err != nil {
		return err
	}
	ct, err := m.container(ctx, h, in.ID)
	if err != nil || ct == nil || !ct.Running() {
		return err
	}
	_, err = h.Exec(ctx, ct.Name, []string{"tunploy-wg", "sync"})
	return err
}

func (m *Manager) PeerStats(ctx context.Context, in *wg.Instance) (map[wg.Key]wg.PeerStats, error) {
	h, err := m.host(in.NodeID)
	if err != nil {
		return nil, err
	}
	return m.peerStats(ctx, h, in)
}

func (m *Manager) peerStats(ctx context.Context, h Host, in *wg.Instance) (map[wg.Key]wg.PeerStats, error) {
	ct, err := m.container(ctx, h, in.ID)
	if err != nil {
		return nil, err
	}
	if ct == nil || !ct.Running() {
		m.activity.forget(in.ID)
		return map[wg.Key]wg.PeerStats{}, nil
	}
	out, err := h.Exec(ctx, ct.Name, []string{"wg", "show", iface, "dump"})
	if err != nil {
		return nil, err
	}
	stats, err := wg.ParseDump(out)
	if err != nil {
		return nil, err
	}
	for _, c := range m.activity.observe(in.ID, stats, in.PersistentKeepalive, m.now()) {
		if m.onPeerChange != nil {
			m.onPeerChange(c)
		}
	}
	return stats, nil
}

// OnPeerChange must be set before Watch starts or requests arrive.
// Online is what the watcher last saw, without asking WireGuard.
func (m *Manager) Online(instanceID int64) map[wg.Key]bool { return m.activity.online(instanceID) }

func (m *Manager) OnPeerChange(fn func(PeerChange)) { m.onPeerChange = fn }

// OnPeerBlock must be set before Watch starts.
func (m *Manager) OnPeerBlock(fn func(PeerBlock)) { m.onPeerBlock = fn }

// OnServerHealth must be set before Watch starts.
func (m *Manager) OnServerHealth(fn func(ServerHealth)) { m.onServerHealth = fn }

// Watch is the only caller of RecordTraffic, which needs a single writer per
// instance. Nodes are watched side by side so a slow one delays no other.
func (m *Manager) Watch(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		instances, err := m.store.Instances(ctx)
		if err != nil {
			slog.Warn("watch peers", "error", err)
			continue
		}
		var nodes sync.WaitGroup
		for nodeID, group := range byNode(instances) {
			h, err := m.host(nodeID)
			if err != nil {
				continue
			}
			nodes.Go(func() {
				for i := range group {
					if err := m.watchInstance(ctx, h, &group[i]); err != nil && ctx.Err() == nil {
						slog.Debug("watch peers", "instance", group[i].ID, "error", err)
					}
				}
			})
		}
		nodes.Wait()
	}
}

func byNode(instances []wg.Instance) map[int64][]wg.Instance {
	out := map[int64][]wg.Instance{}
	for _, in := range instances {
		out[in.NodeID] = append(out[in.NodeID], in)
	}
	return out
}

func (m *Manager) watchInstance(ctx context.Context, h Host, in *wg.Instance) error {
	m.checkHealth(ctx, h, in.ID)
	stats, err := m.peerStats(ctx, h, in)
	if err != nil {
		return err
	}
	if err := m.store.RecordTraffic(ctx, in.ID, stats, m.now()); err != nil {
		return err
	}
	return m.enforceLimits(ctx, h, in)
}

// A stop from the panel exits cleanly, so only a crash loop or a failed exit counts as down.
func (m *Manager) checkHealth(ctx context.Context, h Host, instanceID int64) {
	ct, err := m.container(ctx, h, instanceID)
	if err != nil {
		return
	}
	var st Status
	if ct != nil {
		st = statusOf(*ct)
	}
	down := st.State == StateRestarting || (st.State == StateStopped && st.Error != "")

	m.stateMu.Lock()
	was, known := m.down[instanceID]
	m.down[instanceID] = down
	m.stateMu.Unlock()
	if known && was != down && m.onServerHealth != nil {
		m.onServerHealth(ServerHealth{InstanceID: instanceID, Down: down, Reason: st.Error})
	}
}

// Limits change with time and traffic alone, so nothing else would notice.
func (m *Manager) enforceLimits(ctx context.Context, h Host, in *wg.Instance) error {
	defer m.lock(in.NodeID)()

	peers, usage, next, err := m.blockedPeers(ctx, in.ID)
	if err != nil {
		return err
	}
	m.stateMu.Lock()
	prev, known := m.blocked[in.ID]
	m.stateMu.Unlock()
	if known && maps.Equal(prev, next) {
		return nil
	}
	if err := m.apply(ctx, h, in); err != nil {
		return err
	}
	if !known || m.onPeerBlock == nil {
		return nil
	}
	for _, p := range peers {
		if prev[p.ID] != next[p.ID] {
			m.onPeerBlock(PeerBlock{InstanceID: in.ID, Peer: p, Reason: next[p.ID], Month: usage[p.ID]})
		}
	}
	return nil
}

func (m *Manager) blockedPeers(ctx context.Context, instanceID int64) ([]wg.Peer, map[int64]wg.Traffic, map[int64]wg.Block, error) {
	now := m.now()
	peers, err := m.store.Peers(ctx, instanceID)
	if err != nil {
		return nil, nil, nil, err
	}
	usage, err := m.store.MonthUsage(ctx, instanceID, now)
	if err != nil {
		return nil, nil, nil, err
	}
	blocked := map[int64]wg.Block{}
	for _, p := range peers {
		if b := p.Blocked(usage[p.ID], now); b != "" {
			blocked[p.ID] = b
		}
	}
	return peers, usage, blocked, nil
}

var ErrNotDeployed = errors.New("instance has no container")

func (m *Manager) Logs(ctx context.Context, instanceID int64, tail int, follow bool) (io.ReadCloser, error) {
	in, err := m.store.InstanceByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	h, err := m.host(in.NodeID)
	if err != nil {
		return nil, err
	}
	ct, err := m.container(ctx, h, instanceID)
	if err != nil {
		return nil, err
	}
	if ct == nil {
		return nil, ErrNotDeployed
	}
	return h.Logs(ctx, ct.Name, tail, follow)
}

// container returns nil, nil when there is none.
func (m *Manager) container(ctx context.Context, h Host, instanceID int64) (*docker.Container, error) {
	ct, err := h.InspectContainer(ctx, m.ContainerName(instanceID))
	if errors.Is(err, docker.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ct, nil
}

// The container mounts the directory, not the file, so it sees the rename.
func (m *Manager) writeConfig(ctx context.Context, h Host, in *wg.Instance) error {
	peers, _, blocked, err := m.blockedPeers(ctx, in.ID)
	if err != nil {
		return err
	}
	allowed := slices.DeleteFunc(peers, func(p wg.Peer) bool { return blocked[p.ID] != "" })

	path := filepath.Join(configDir(h, in.ID), iface+".conf")
	if err := h.WriteFile(ctx, path, wg.ServerConfig(*in, allowed)); err != nil {
		return err
	}
	m.stateMu.Lock()
	m.blocked[in.ID] = blocked
	m.stateMu.Unlock()
	return nil
}
