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
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/moby/moby/api/types/network"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/host"
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

type Manager struct {
	store  *store.Store
	local  host.Host
	nodes  Nodes
	prefix string
	image  string
	files  map[string][]byte

	// One lock per node serialises its container changes, so requests never
	// race to recreate one and a slow node never holds up another.
	locksMu sync.Mutex
	locks   map[int64]*sync.Mutex

	activity activity
	events   event.Recorder
	now      func() time.Time

	stateMu sync.Mutex
	// What each instance's config was last written with.
	blocked map[int64]map[int64]wg.Block
	// Whether each instance looked down on the last watch.
	down map[int64]bool
	// Nodes to rebuild instead of reconcile when they next come up.
	rebuild map[int64]bool
}

// Nodes reaches the machines other than the panel's own; node.Pool in production.
type Nodes interface {
	Host(nodeID int64) (host.Host, error)
}

// nodes may be nil when the panel has only its own machine.
func New(st *store.Store, local host.Host, nodes Nodes, prefix string, events event.Recorder) (*Manager, error) {
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
		local:    local,
		nodes:    nodes,
		prefix:   prefix,
		image:    imageTag(files),
		files:    files,
		locks:    map[int64]*sync.Mutex{},
		activity: activity{peers: map[int64]map[wg.Key]sample{}},
		events:   events,
		blocked:  map[int64]map[int64]wg.Block{},
		now:      time.Now,
		down:     map[int64]bool{},
		rebuild:  map[int64]bool{},
	}, nil
}

func (m *Manager) hostFor(nodeID int64) (host.Host, error) {
	if nodeID == 0 {
		return m.local, nil
	}
	if m.nodes == nil {
		return nil, host.ErrOffline
	}
	return m.nodes.Host(nodeID)
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
func (m *Manager) acquire(ctx context.Context, instanceID int64) (*wg.Instance, host.Host, func(), error) {
	in, err := m.store.InstanceByID(ctx, instanceID)
	if err != nil {
		return nil, nil, nil, err
	}
	h, err := m.hostFor(in.NodeID)
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

func configDir(h host.Host, instanceID int64) string {
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

func (m *Manager) deploy(ctx context.Context, h host.Host, in *wg.Instance, start bool, progress Progress) error {
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

func (m *Manager) ensureImage(ctx context.Context, h host.Host) error {
	ok, err := h.ImageExists(ctx, m.image)
	if err != nil || ok {
		return err
	}
	return h.BuildImage(ctx, m.image, m.files)
}

// PrepareNode builds the WireGuard image on a node ahead of its first server.
func (m *Manager) PrepareNode(ctx context.Context, h host.Host) error {
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
// host.ErrOffline; the node's next reconcile removes what is left.
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

// syncconf applies peer changes without dropping connected peers. On an
// offline node the change waits in the database for its next reconcile.
func (m *Manager) Apply(ctx context.Context, instanceID int64) error {
	in, h, unlock, err := m.acquire(ctx, instanceID)
	if errors.Is(err, host.ErrOffline) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	return m.apply(ctx, h, in)
}

func (m *Manager) apply(ctx context.Context, h host.Host, in *wg.Instance) error {
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

var ErrNotDeployed = errors.New("instance has no container")

func (m *Manager) Logs(ctx context.Context, instanceID int64, tail int, follow bool) (io.ReadCloser, error) {
	in, err := m.store.InstanceByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	h, err := m.hostFor(in.NodeID)
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
func (m *Manager) container(ctx context.Context, h host.Host, instanceID int64) (*docker.Container, error) {
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
const (
	resolverFile    = "dns-servers.conf"
	speedLimitsFile = "speed-limits.conf"
)

func (m *Manager) writeConfig(ctx context.Context, h host.Host, in *wg.Instance) error {
	peers, _, blocked, err := m.blockedPeers(ctx, in.ID)
	if err != nil {
		return err
	}
	allowed := slices.DeleteFunc(peers, func(p wg.Peer) bool { return blocked[p.ID] != "" })

	dir := configDir(h, in.ID)
	if err := h.WriteFile(ctx, filepath.Join(dir, iface+".conf"), wg.ServerConfig(*in, allowed)); err != nil {
		return err
	}
	// The container starts or stops its resolver by whether this file exists.
	resolver := filepath.Join(dir, resolverFile)
	if in.DNSOnServer {
		err = h.WriteFile(ctx, resolver, wg.ResolverConfig(*in))
	} else {
		err = h.RemoveAll(ctx, resolver)
	}
	if err != nil {
		return err
	}
	// The container shapes traffic by this file, and clears it once it is gone.
	speeds := filepath.Join(dir, speedLimitsFile)
	if limits := wg.SpeedLimitsConfig(allowed); len(limits) > 0 {
		err = h.WriteFile(ctx, speeds, limits)
	} else {
		err = h.RemoveAll(ctx, speeds)
	}
	if err != nil {
		return err
	}
	m.stateMu.Lock()
	m.blocked[in.ID] = blocked
	m.stateMu.Unlock()
	return nil
}
