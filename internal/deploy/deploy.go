// Package deploy runs each WireGuard instance as a Docker container and keeps
// the containers in step with the database.
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
	// LabelInstance ties a container back to its row in wg_instances.
	LabelInstance = "io.tunploy.instance"

	containerPrefix = "tunploy-wg-"
	iface           = "wg0"
	stopTimeout     = 10 * time.Second
)

// Docker is the part of *docker.Client the manager drives; tests use a fake.
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

	// Serialises every change to containers and config files, so two
	// requests never race to recreate the same container.
	mu sync.Mutex
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
		store:   st,
		docker:  dk,
		dataDir: dataDir,
		image:   imageTag(files),
		files:   files,
	}, nil
}

// imageTag hashes the build context, so a Tunploy upgrade that changes the
// image gets a new tag and Reconcile moves containers onto it.
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

// ConfigDir is where the instance's wg0.conf lives on the host.
func (m *Manager) ConfigDir(instanceID int64) string {
	return filepath.Join(m.dataDir, "wireguard", strconv.FormatInt(instanceID, 10))
}

// Deploy writes the config and (re)creates the container. The container is
// started unless start is false.
func (m *Manager) Deploy(ctx context.Context, instanceID int64, start bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deploy(ctx, instanceID, start)
}

// Redeploy recreates the container and keeps it running or stopped as it
// was. Port and MTU changes need this; they are fixed at creation.
func (m *Manager) Redeploy(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ct, err := m.container(ctx, instanceID)
	if err != nil {
		return err
	}
	return m.deploy(ctx, instanceID, ct == nil || ct.Running())
}

func (m *Manager) deploy(ctx context.Context, instanceID int64, start bool) error {
	if err := m.writeConfig(ctx, instanceID); err != nil {
		return err
	}
	if err := m.ensureImage(ctx); err != nil {
		return err
	}

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
	return m.docker.StartContainer(ctx, name)
}

func (m *Manager) ensureImage(ctx context.Context) error {
	ok, err := m.docker.ImageExists(ctx, m.image)
	if err != nil || ok {
		return err
	}
	return m.docker.BuildImage(ctx, m.image, m.files)
}

// Start deploys the instance first if its container is missing.
func (m *Manager) Start(ctx context.Context, instanceID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ct, err := m.container(ctx, instanceID)
	if err != nil {
		return err
	}
	if ct == nil {
		return m.deploy(ctx, instanceID, true)
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
		return m.deploy(ctx, instanceID, true)
	}
	if err := m.writeConfig(ctx, instanceID); err != nil {
		return err
	}
	if err := m.docker.StopContainer(ctx, ct.Name, stopTimeout); err != nil {
		return err
	}
	return m.docker.StartContainer(ctx, ct.Name)
}

// Remove deletes the container and the instance's config directory.
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
	return nil
}

// Apply rewrites the config after a peer change and, if the interface is up,
// loads it with `wg syncconf` so connected peers stay connected.
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

// PeerStats is empty, not an error, while the instance is not running.
func (m *Manager) PeerStats(ctx context.Context, instanceID int64) (map[wg.Key]wg.PeerStats, error) {
	ct, err := m.container(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if ct == nil || !ct.Running() {
		return map[wg.Key]wg.PeerStats{}, nil
	}
	out, err := m.docker.Exec(ctx, ct.Name, []string{"wg", "show", iface, "dump"})
	if err != nil {
		return nil, err
	}
	return wg.ParseDump(out)
}

// ErrNotDeployed means the instance has no container to act on.
var ErrNotDeployed = errors.New("instance has no container")

// Logs works on stopped containers too: why it stopped is usually the point.
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

// container returns nil, nil when the instance has no container.
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

// writeConfig replaces the file atomically; the container mounts the
// directory rather than the file, so it sees the rename.
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
