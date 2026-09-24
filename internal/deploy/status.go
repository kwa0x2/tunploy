package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/moby/moby/api/types/container"

	"github.com/kwa0x2/tunploy/internal/docker"
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

func (m *Manager) Statuses(ctx context.Context, instanceIDs []int64) map[int64]Status {
	out := make(map[int64]Status, len(instanceIDs))

	containers, err := m.docker.ListContainers(ctx)
	if err != nil {
		for _, id := range instanceIDs {
			out[id] = Status{State: StateUnknown, Error: err.Error()}
		}
		return out
	}

	byName := make(map[string]docker.Container, len(containers))
	for _, ct := range containers {
		byName[ct.Name] = ct
	}
	for _, id := range instanceIDs {
		ct, ok := byName[ContainerName(id)]
		if !ok {
			out[id] = Status{State: StateNotDeployed}
			continue
		}
		out[id] = statusOf(ct)
	}
	return out
}

func (m *Manager) Status(ctx context.Context, instanceID int64) Status {
	ct, err := m.container(ctx, instanceID)
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

// Reconcile leaves stopped containers stopped.
func (m *Manager) Reconcile(ctx context.Context) error {
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

	byName := make(map[string]docker.Container, len(containers))
	for _, ct := range containers {
		byName[ct.Name] = ct
	}

	var errs []error
	known := make(map[string]bool, len(instances))
	for _, in := range instances {
		name := ContainerName(in.ID)
		known[name] = true

		ct, exists := byName[name]
		switch {
		case !exists:
			slog.Info("deploying missing wireguard container", "instance", in.ID)
			err = m.deploy(ctx, in.ID, true, nil)
		case ct.Image != m.image:
			slog.Info("moving wireguard container to new image", "instance", in.ID, "image", m.image)
			err = m.deploy(ctx, in.ID, ct.Running(), nil)
		default:
			err = m.writeConfig(ctx, in.ID)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("instance %d: %w", in.ID, err))
		}
	}

	for _, ct := range containers {
		if _, ok := ct.Labels[LabelInstance]; !ok || known[ct.Name] {
			continue
		}
		slog.Info("removing orphaned wireguard container", "container", ct.Name)
		if err := m.docker.RemoveContainer(ctx, ct.Name); err != nil && !errors.Is(err, docker.ErrNotFound) {
			errs = append(errs, fmt.Errorf("remove %s: %w", ct.Name, err))
		}
	}
	return errors.Join(errs...)
}
