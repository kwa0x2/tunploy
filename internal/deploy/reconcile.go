package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/host"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// Reconcile brings the panel's own machine in line with the database.
func (m *Manager) Reconcile(ctx context.Context) error {
	return m.NodeUp(ctx, 0)
}

// NodeUp brings a node that just came up in line with the database: it
// rebuilds after a restore and reconciles otherwise.
func (m *Manager) NodeUp(ctx context.Context, nodeID int64) error {
	h, err := m.hostFor(nodeID)
	if err != nil {
		return err
	}
	defer m.lock(nodeID)()

	all, err := m.store.Instances(ctx)
	if err != nil {
		return err
	}
	instances := byNode(all)[nodeID]
	if m.takeRebuild(nodeID) {
		slog.Info("rebuilding wireguard containers", "node", nodeID)
		return m.rebuildNode(ctx, nodeID, h, instances)
	}
	return m.reconcile(ctx, nodeID, h, instances)
}

// Stopped containers stay stopped. Running ones get the config the database
// holds now, which may have changed while the node was out of reach.
func (m *Manager) reconcile(ctx context.Context, nodeID int64, h host.Host, instances []wg.Instance) error {
	containers, err := h.ListContainers(ctx)
	if err != nil {
		return err
	}
	byName := make(map[string]docker.Container, len(containers))
	for _, ct := range containers {
		byName[ct.Name] = ct
	}

	var errs []error
	known := make(map[string]bool, len(instances))
	for i := range instances {
		in := &instances[i]
		name := m.ContainerName(in.ID)
		known[name] = true

		ct, exists := byName[name]
		switch {
		case !exists:
			slog.Info("deploying missing wireguard container", "node", nodeID, "instance", in.ID)
			err = m.deploy(ctx, h, in, true, nil)
		case ct.Image != m.image:
			slog.Info("moving wireguard container to new image", "node", nodeID, "instance", in.ID, "image", m.image)
			err = m.deploy(ctx, h, in, ct.Running(), nil)
		default:
			err = m.apply(ctx, h, in)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("instance %d: %w", in.ID, err))
		}
	}

	for _, ct := range containers {
		if !m.owns(ct) || known[ct.Name] {
			continue
		}
		slog.Info("removing orphaned wireguard container", "node", nodeID, "container", ct.Name)
		if err := h.RemoveContainer(ctx, ct.Name); err != nil && !errors.Is(err, docker.ErrNotFound) {
			errs = append(errs, fmt.Errorf("remove %s: %w", ct.Name, err))
		}
	}
	if err := m.removeStaleConfigs(ctx, h, instances); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// RemoveNode takes every WireGuard container off a node that is being let go.
func (m *Manager) RemoveNode(ctx context.Context, nodeID int64) error {
	h, err := m.hostFor(nodeID)
	if err != nil {
		return err
	}
	defer m.lock(nodeID)()

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
	all, err := m.store.Instances(ctx)
	if err != nil {
		return err
	}
	for _, in := range byNode(all)[nodeID] {
		m.forget(in.ID)
	}
	return nil
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

func (m *Manager) rebuildNode(ctx context.Context, nodeID int64, h host.Host, instances []wg.Instance) error {
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
func (m *Manager) removeStaleConfigs(ctx context.Context, h host.Host, instances []wg.Instance) error {
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
