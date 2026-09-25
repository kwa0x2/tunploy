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
	"os"
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
	store   *store.Store
	docker  Docker
	dataDir string
	prefix  string
	image   string
	files   map[string][]byte

	// Serialises container changes so requests never race to recreate one.
	mu sync.Mutex

	activity     activity
	onPeerChange func(PeerChange)

	// What each instance's config was last written with; guarded by mu.
	blocked     map[int64]map[int64]wg.Block
	onPeerBlock func(PeerBlock)
	now         func() time.Time

	// Whether each instance looked down on the last watch; only Watch touches it.
	down           map[int64]bool
	onServerHealth func(ServerHealth)
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
		docker:   dk,
		dataDir:  dataDir,
		prefix:   prefix,
		image:    imageTag(files),
		files:    files,
		activity: activity{peers: map[int64]map[wg.Key]sample{}},
		blocked:  map[int64]map[int64]wg.Block{},
		now:      time.Now,
		down:     map[int64]bool{},
	}, nil
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

func (m *Manager) ConfigDir(instanceID int64) string {
	return filepath.Join(m.dataDir, "wireguard", strconv.FormatInt(instanceID, 10))
}

func (m *Manager) Deploy(ctx context.Context, instanceID int64, start bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deploy(ctx, instanceID, start, nil)
}

func (m *Manager) Provision(ctx context.Context, instanceID int64, progress Progress) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deploy(ctx, instanceID, true, progress)
}

// Port and MTU are fixed at container creation, so changing them needs this.
func (m *Manager) Redeploy(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ct, err := m.container(ctx, instanceID)
	if err != nil {
		return err
	}
	return m.deploy(ctx, instanceID, ct == nil || ct.Running(), nil)
}

func (m *Manager) deploy(ctx context.Context, instanceID int64, start bool, progress Progress) error {
	if err := m.writeConfig(ctx, instanceID); err != nil {
		return err
	}
	if err := m.ensureImage(ctx); err != nil {
		return err
	}
	progress.report(StepImage)

	in, err := m.store.InstanceByID(ctx, instanceID)
	if err != nil {
		return err
	}

	name := m.ContainerName(in.ID)
	if err := m.docker.RemoveContainer(ctx, name); err != nil && !errors.Is(err, docker.ErrNotFound) {
		return err
	}

	port := uint16(in.ListenPort)
	_, err = m.docker.CreateContainer(ctx, docker.ContainerSpec{
		Name:   name,
		Image:  m.image,
		Labels: map[string]string{LabelInstance: strconv.FormatInt(in.ID, 10)},
		Ports:  []docker.Port{{Host: port, Container: port, Protocol: network.UDP}},
		Mounts: []docker.Mount{{Source: m.ConfigDir(in.ID), Target: "/etc/wireguard", ReadOnly: true}},
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
	if err := m.docker.StartContainer(ctx, name); err != nil {
		return err
	}
	progress.report(StepContainer)
	return m.waitReady(ctx, name, progress)
}

func (m *Manager) ensureImage(ctx context.Context) error {
	ok, err := m.docker.ImageExists(ctx, m.image)
	if err != nil || ok {
		return err
	}
	return m.docker.BuildImage(ctx, m.image, m.files)
}

func (m *Manager) Start(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ct, err := m.container(ctx, instanceID)
	if err != nil {
		return err
	}
	if ct == nil {
		return m.deploy(ctx, instanceID, true, nil)
	}
	if err := m.writeConfig(ctx, instanceID); err != nil {
		return err
	}
	if ct.Running() {
		return nil
	}
	return m.docker.StartContainer(ctx, ct.Name)
}

func (m *Manager) Stop(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ct, err := m.container(ctx, instanceID)
	if err != nil || ct == nil {
		return err
	}
	return m.docker.StopContainer(ctx, ct.Name, stopTimeout)
}

func (m *Manager) Restart(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ct, err := m.container(ctx, instanceID)
	if err != nil {
		return err
	}
	if ct == nil {
		return m.deploy(ctx, instanceID, true, nil)
	}
	if err := m.writeConfig(ctx, instanceID); err != nil {
		return err
	}
	if err := m.docker.StopContainer(ctx, ct.Name, stopTimeout); err != nil {
		return err
	}
	return m.docker.StartContainer(ctx, ct.Name)
}

func (m *Manager) Remove(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	err := m.docker.RemoveContainer(ctx, m.ContainerName(instanceID))
	if err != nil && !errors.Is(err, docker.ErrNotFound) {
		return err
	}
	if err := os.RemoveAll(m.ConfigDir(instanceID)); err != nil {
		return fmt.Errorf("remove config dir: %w", err)
	}
	m.activity.forget(instanceID)
	delete(m.blocked, instanceID)
	return nil
}

// Rebuild replaces every container after the database was swapped out, since
// the same instance ID may now stand for a different server.
func (m *Manager) Rebuild(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instances, err := m.store.Instances(ctx)
	if err != nil {
		return err
	}
	containers, err := m.docker.ListContainers(ctx)
	if err != nil {
		return err
	}
	for _, ct := range containers {
		if !m.owns(ct) {
			continue
		}
		if err := m.docker.RemoveContainer(ctx, ct.Name); err != nil && !errors.Is(err, docker.ErrNotFound) {
			return err
		}
	}

	// Only stale directories go: Docker Desktop loses track of a bind mount
	// whose parent was deleted and made again.
	known := map[string]bool{}
	for _, in := range instances {
		known[strconv.FormatInt(in.ID, 10)] = true
	}
	root := filepath.Join(m.dataDir, "wireguard")
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("list config dirs: %w", err)
	}
	for _, e := range entries {
		if !known[e.Name()] {
			if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
				return fmt.Errorf("remove config dir: %w", err)
			}
		}
	}
	m.activity.reset()
	m.blocked = map[int64]map[int64]wg.Block{}

	var errs []error
	for _, in := range instances {
		if err := m.deploy(ctx, in.ID, true, nil); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", in.Name, err))
		}
	}
	return errors.Join(errs...)
}

// syncconf applies peer changes without dropping connected peers.
func (m *Manager) Apply(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.apply(ctx, instanceID)
}

func (m *Manager) apply(ctx context.Context, instanceID int64) error {
	if err := m.writeConfig(ctx, instanceID); err != nil {
		return err
	}
	ct, err := m.container(ctx, instanceID)
	if err != nil || ct == nil || !ct.Running() {
		return err
	}
	_, err = m.docker.Exec(ctx, ct.Name, []string{"tunploy-wg", "sync"})
	return err
}

func (m *Manager) PeerStats(ctx context.Context, in *wg.Instance) (map[wg.Key]wg.PeerStats, error) {
	ct, err := m.container(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if ct == nil || !ct.Running() {
		m.activity.forget(in.ID)
		return map[wg.Key]wg.PeerStats{}, nil
	}
	out, err := m.docker.Exec(ctx, ct.Name, []string{"wg", "show", iface, "dump"})
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
func (m *Manager) OnPeerChange(fn func(PeerChange)) { m.onPeerChange = fn }

// OnPeerBlock must be set before Watch starts.
func (m *Manager) OnPeerBlock(fn func(PeerBlock)) { m.onPeerBlock = fn }

// OnServerHealth must be set before Watch starts.
func (m *Manager) OnServerHealth(fn func(ServerHealth)) { m.onServerHealth = fn }

// Watch is the only caller of RecordTraffic, which needs a single writer.
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
		for i := range instances {
			if err := m.watchInstance(ctx, &instances[i]); err != nil && ctx.Err() == nil {
				slog.Debug("watch peers", "instance", instances[i].ID, "error", err)
			}
		}
	}
}

func (m *Manager) watchInstance(ctx context.Context, in *wg.Instance) error {
	m.checkHealth(ctx, in.ID)
	stats, err := m.PeerStats(ctx, in)
	if err != nil {
		return err
	}
	if err := m.store.RecordTraffic(ctx, in.ID, stats, m.now()); err != nil {
		return err
	}
	return m.enforceLimits(ctx, in.ID)
}

// A stop from the panel exits cleanly, so only a crash loop or a failed exit counts as down.
func (m *Manager) checkHealth(ctx context.Context, instanceID int64) {
	ct, err := m.container(ctx, instanceID)
	if err != nil {
		return
	}
	var st Status
	if ct != nil {
		st = statusOf(*ct)
	}
	down := st.State == StateRestarting || (st.State == StateStopped && st.Error != "")

	was, known := m.down[instanceID]
	m.down[instanceID] = down
	if known && was != down && m.onServerHealth != nil {
		m.onServerHealth(ServerHealth{InstanceID: instanceID, Down: down, Reason: st.Error})
	}
}

// Limits change with time and traffic alone, so nothing else would notice.
func (m *Manager) enforceLimits(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	peers, usage, next, err := m.blockedPeers(ctx, instanceID)
	if err != nil {
		return err
	}
	prev, known := m.blocked[instanceID]
	if known && maps.Equal(prev, next) {
		return nil
	}
	if err := m.apply(ctx, instanceID); err != nil {
		return err
	}
	if !known || m.onPeerBlock == nil {
		return nil
	}
	for _, p := range peers {
		if prev[p.ID] != next[p.ID] {
			m.onPeerBlock(PeerBlock{InstanceID: instanceID, Peer: p, Reason: next[p.ID], Month: usage[p.ID]})
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
	ct, err := m.container(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if ct == nil {
		return nil, ErrNotDeployed
	}
	return m.docker.Logs(ctx, ct.Name, tail, follow)
}

// container returns nil, nil when there is none.
func (m *Manager) container(ctx context.Context, instanceID int64) (*docker.Container, error) {
	ct, err := m.docker.InspectContainer(ctx, m.ContainerName(instanceID))
	if errors.Is(err, docker.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ct, nil
}

// The container mounts the directory, not the file, so it sees the rename.
func (m *Manager) writeConfig(ctx context.Context, instanceID int64) error {
	in, err := m.store.InstanceByID(ctx, instanceID)
	if err != nil {
		return err
	}
	peers, _, blocked, err := m.blockedPeers(ctx, instanceID)
	if err != nil {
		return err
	}
	allowed := slices.DeleteFunc(peers, func(p wg.Peer) bool { return blocked[p.ID] != "" })

	dir := m.ConfigDir(instanceID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".wg0-*.conf")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(wg.ServerConfig(*in, allowed)); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, iface+".conf")); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	m.blocked[instanceID] = blocked
	return nil
}
