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

	containerPrefix = "tunploy-wg-"
	iface           = "wg0"
	stopTimeout     = 10 * time.Second
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
	image   string
	files   map[string][]byte

	// Serialises container changes so requests never race to recreate one.
	mu sync.Mutex

	activity     activity
	onPeerChange func(PeerChange)
}

func New(st *store.Store, dk Docker, dataDir string) (*Manager, error) {
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
		image:    imageTag(files),
		files:    files,
		activity: activity{peers: map[int64]map[wg.Key]sample{}},
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

func ContainerName(instanceID int64) string {
	return containerPrefix + strconv.FormatInt(instanceID, 10)
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

	name := ContainerName(in.ID)
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

	err := m.docker.RemoveContainer(ctx, ContainerName(instanceID))
	if err != nil && !errors.Is(err, docker.ErrNotFound) {
		return err
	}
	if err := os.RemoveAll(m.ConfigDir(instanceID)); err != nil {
		return fmt.Errorf("remove config dir: %w", err)
	}
	m.activity.forget(instanceID)
	return nil
}

// syncconf applies peer changes without dropping connected peers.
func (m *Manager) Apply(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

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
	for _, c := range m.activity.observe(in.ID, stats, in.PersistentKeepalive, time.Now()) {
		if m.onPeerChange != nil {
			m.onPeerChange(c)
		}
	}
	return stats, nil
}

// OnPeerChange must be set before Watch starts or requests arrive.
func (m *Manager) OnPeerChange(fn func(PeerChange)) { m.onPeerChange = fn }

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
			if _, err := m.PeerStats(ctx, &instances[i]); err != nil && ctx.Err() == nil {
				slog.Debug("watch peers", "instance", instances[i].ID, "error", err)
			}
		}
	}
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
	ct, err := m.docker.InspectContainer(ctx, ContainerName(instanceID))
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
	peers, err := m.store.Peers(ctx, instanceID)
	if err != nil {
		return err
	}

	dir := m.ConfigDir(instanceID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".wg0-*.conf")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(wg.ServerConfig(*in, peers)); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, iface+".conf")); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
