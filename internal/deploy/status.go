package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/moby/moby/api/types/container"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type State string

const (
	StateRunning     State = "running"
	StateRestarting  State = "restarting"
	StateStopped     State = "stopped"
	StateNotDeployed State = "not_deployed"
	StateUnknown     State = "unknown"
)

type Status struct {
	State State  `json:"state"`
	Error string `json:"error,omitempty"`
}

func (m *Manager) Statuses(ctx context.Context, instances []wg.Instance) map[int64]Status {
	out := make(map[int64]Status, len(instances))
	for nodeID, group := range byNode(instances) {
		containers, err := m.containers(ctx, nodeID)
		for _, in := range group {
			if err != nil {
				out[in.ID] = Status{State: StateUnknown, Error: err.Error()}
				continue
			}
			ct, ok := containers[m.ContainerName(in.ID)]
			if !ok {
				out[in.ID] = Status{State: StateNotDeployed}
				continue
			}
			out[in.ID] = statusOf(ct)
		}
	}
	return out
}

func (m *Manager) containers(ctx context.Context, nodeID int64) (map[string]docker.Container, error) {
	h, err := m.host(nodeID)
	if err != nil {
		return nil, err
	}
	list, err := h.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]docker.Container, len(list))
	for _, ct := range list {
		byName[ct.Name] = ct
	}
	return byName, nil
}

func (m *Manager) Status(ctx context.Context, in *wg.Instance) Status {
	h, err := m.host(in.NodeID)
	if err != nil {
		return Status{State: StateUnknown, Error: err.Error()}
	}
	ct, err := m.container(ctx, h, in.ID)
	switch {
	case err != nil:
		return Status{State: StateUnknown, Error: err.Error()}
	case ct == nil:
		return Status{State: StateNotDeployed}
	default:
		return statusOf(*ct)
	}
}

func statusOf(ct docker.Container) Status {
	switch container.ContainerState(ct.State) {
	case container.StateRunning:
		return Status{State: StateRunning}
	case container.StateRestarting:
		return Status{State: StateRestarting, Error: "the container keeps exiting; check its logs"}
	case container.StateExited:
		if ct.ExitCode != 0 {
			return Status{State: StateStopped, Error: fmt.Sprintf("exited with code %d", ct.ExitCode)}
		}
	}
	return Status{State: StateStopped}
}

// Reconcile brings the panel's own machine in line with the database.
func (m *Manager) Reconcile(ctx context.Context) error {
	return m.NodeUp(ctx, 0)
}

// NodeUp brings a node that just came up in line with the database: it
// rebuilds after a restore and reconciles otherwise.
func (m *Manager) NodeUp(ctx context.Context, nodeID int64) error {
	h, err := m.host(nodeID)
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
func (m *Manager) reconcile(ctx context.Context, nodeID int64, h Host, instances []wg.Instance) error {
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
	h, err := m.host(nodeID)
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
