package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// Mutating calls refuse containers without this label: they are the operator's.
const LabelManaged = "io.tunploy.managed"

type Port struct {
	Host      uint16
	Container uint16
	Protocol  network.IPProtocol
}

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

type ContainerSpec struct {
	Name    string
	Image   string
	Cmd     []string
	Env     map[string]string
	Labels  map[string]string
	Ports   []Port
	Mounts  []Mount
	CapAdd  []string
	Sysctls map[string]string
}

type Container struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	State    string            `json:"state"`
	ExitCode int               `json:"exit_code"`
	Labels   map[string]string `json:"labels"`
}

func (c Container) Running() bool { return c.State == string(container.StateRunning) }

func (c *Client) CreateContainer(ctx context.Context, spec ContainerSpec) (string, error) {
	labels := make(map[string]string, len(spec.Labels)+1)
	for k, v := range spec.Labels {
		labels[k] = v
	}
	labels[LabelManaged] = "true"

	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}

	exposed := network.PortSet{}
	bindings := network.PortMap{}
	for _, p := range spec.Ports {
		port, ok := network.PortFrom(p.Container, p.Protocol)
		if !ok {
			return "", fmt.Errorf("docker create container: invalid port %d/%s", p.Container, p.Protocol)
		}
		exposed[port] = struct{}{}
		bindings[port] = append(bindings[port], network.PortBinding{HostPort: fmt.Sprint(p.Host)})
	}

	mounts := make([]mount.Mount, 0, len(spec.Mounts))
	for _, m := range spec.Mounts {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	resp, err := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: spec.Name,
		Config: &container.Config{
			Image:        spec.Image,
			Cmd:          spec.Cmd,
			Env:          env,
			Labels:       labels,
			ExposedPorts: exposed,
		},
		HostConfig: &container.HostConfig{
			PortBindings: bindings,
			Mounts:       mounts,
			CapAdd:       spec.CapAdd,
			Sysctls:      spec.Sysctls,
			// A stop from the panel is sticky across reboots; a crash is not.
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		},
	})
	if err != nil {
		return "", wrap(err, "create container")
	}
	return resp.ID, nil
}

func (c *Client) InspectContainer(ctx context.Context, id string) (Container, error) {
	resp, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return Container{}, wrap(err, "inspect container")
	}
	ct := resp.Container
	out := Container{
		ID:   ct.ID,
		Name: strings.TrimPrefix(ct.Name, "/"),
	}
	if ct.Config != nil {
		out.Image = ct.Config.Image
		out.Labels = ct.Config.Labels
	}
	if ct.State != nil {
		out.State = string(ct.State.Status)
		out.ExitCode = ct.State.ExitCode
	}
	return out, nil
}

func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	resp, err := c.api.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: client.Filters{}.Add("label", LabelManaged+"=true"),
	})
	if err != nil {
		return nil, wrap(err, "list containers")
	}
	out := make([]Container, 0, len(resp.Items))
	for _, s := range resp.Items {
		var name string
		if len(s.Names) > 0 {
			name = strings.TrimPrefix(s.Names[0], "/")
		}
		out = append(out, Container{
			ID:     s.ID,
			Name:   name,
			Image:  s.Image,
			State:  string(s.State),
			Labels: s.Labels,
		})
	}
	return out, nil
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	if err := c.ensureManaged(ctx, id); err != nil {
		return err
	}
	if _, err := c.api.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return wrap(err, "start container")
	}
	return nil
}

func (c *Client) StopContainer(ctx context.Context, id string, timeout time.Duration) error {
	if err := c.ensureManaged(ctx, id); err != nil {
		return err
	}
	secs := int(timeout.Seconds())
	if _, err := c.api.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &secs}); err != nil {
		return wrap(err, "stop container")
	}
	return nil
}

func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	if err := c.ensureManaged(ctx, id); err != nil {
		return err
	}
	if _, err := c.api.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true}); err != nil {
		return wrap(err, "remove container")
	}
	return nil
}

func (c *Client) ensureManaged(ctx context.Context, id string) error {
	ct, err := c.InspectContainer(ctx, id)
	if err != nil {
		return err
	}
	if ct.Labels[LabelManaged] != "true" {
		return fmt.Errorf("docker: %w: %s", ErrNotManaged, ct.Name)
	}
	return nil
}
