package deploy

import (
	"context"
	"fmt"

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
	h, err := m.hostFor(nodeID)
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
	h, err := m.hostFor(in.NodeID)
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
