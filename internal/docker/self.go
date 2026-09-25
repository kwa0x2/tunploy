package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	// UpdaterName is the short-lived container that swaps the panel's image.
	UpdaterName = "tunploy-updater"

	labelComposeProject = "com.docker.compose.project"
)

// Self describes the container this process runs in.
type Self struct {
	ID    string
	Name  string
	Image string
	// Compose is the Compose project that owns the container, if any.
	Compose string
	// Source is the image's org.opencontainers.image.source label.
	Source string
}

// FindSelf finds the panel's own container. Docker names a container's
// hostname after its short ID; a custom --hostname still shows in its config.
func (c *Client) FindSelf(ctx context.Context) (Self, error) {
	host, err := os.Hostname()
	if err != nil {
		return Self{}, fmt.Errorf("hostname: %w", err)
	}
	for _, ref := range []string{host, "tunploy"} {
		resp, err := c.api.ContainerInspect(ctx, ref, client.ContainerInspectOptions{})
		if err != nil {
			if err = wrap(err, "inspect container"); errors.Is(err, ErrNotFound) {
				continue
			}
			return Self{}, err
		}
		ct := resp.Container
		if ct.Config == nil || ct.Config.Hostname != host {
			continue
		}
		self := Self{
			ID:      ct.ID,
			Name:    strings.TrimPrefix(ct.Name, "/"),
			Image:   ct.Config.Image,
			Compose: ct.Config.Labels[labelComposeProject],
		}
		if img, err := c.api.ImageInspect(ctx, ct.Image); err == nil && img.Config != nil {
			self.Source = img.Config.Labels["org.opencontainers.image.source"]
		}
		return self, nil
	}
	return Self{}, fmt.Errorf("docker: %w: no container with hostname %s", ErrNotFound, host)
}

// StartUpdater runs cmd in a throwaway container of image, with the same
// mounts and environment as self: the Docker socket and the data directory.
func (c *Client) StartUpdater(ctx context.Context, self Self, image string, cmd []string) error {
	resp, err := c.api.ContainerInspect(ctx, self.ID, client.ContainerInspectOptions{})
	if err != nil {
		return wrap(err, "inspect container")
	}
	ct := resp.Container

	if old, err := c.api.ContainerInspect(ctx, UpdaterName, client.ContainerInspectOptions{}); err == nil {
		if old.Container.State != nil && old.Container.State.Running {
			return fmt.Errorf("docker: %w: an update is already running", ErrConflict)
		}
		if _, err := c.api.ContainerRemove(ctx, UpdaterName, client.ContainerRemoveOptions{Force: true}); err != nil {
			return wrap(err, "remove container")
		}
	}

	created, err := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: UpdaterName,
		Config: &container.Config{
			Image: image,
			Cmd:   cmd,
			Env:   ct.Config.Env,
		},
		HostConfig: &container.HostConfig{
			Binds:      ct.HostConfig.Binds,
			Mounts:     ct.HostConfig.Mounts,
			AutoRemove: true,
		},
	})
	if err != nil {
		return wrap(err, "create container")
	}
	if _, err := c.api.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = c.api.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		return wrap(err, "start container")
	}
	return nil
}

// Replacement swaps a container for a copy of it on another image, in steps
// so the caller can act while neither of them runs.
type Replacement struct {
	c       *Client
	old     container.InspectResponse
	name    string
	cfg     *container.Config
	host    *container.HostConfig
	net     *network.NetworkingConfig
	newID   string
	renamed bool
	stopped bool
}

// Replace renames the container out of the way and stops it. Follow with
// Start, then Commit, or with Discard and Restore to undo.
func (c *Client) Replace(ctx context.Context, id, image string, stopTimeout time.Duration) (*Replacement, error) {
	resp, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, wrap(err, "inspect container")
	}
	r := &Replacement{c: c, old: resp.Container, name: strings.TrimPrefix(resp.Container.Name, "/")}
	if r.cfg, r.host, r.net, err = c.cloneConfig(ctx, r.old, image); err != nil {
		return nil, err
	}

	if _, err := c.api.ContainerRename(ctx, id, client.ContainerRenameOptions{NewName: r.name + "-previous"}); err != nil {
		return nil, wrap(err, "rename container")
	}
	r.renamed = true

	secs := int(stopTimeout.Seconds())
	if _, err := c.api.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &secs}); err != nil {
		return r, wrap(err, "stop container")
	}
	r.stopped = true
	return r, nil
}

// Start creates and starts the copy under the old name and returns its ID.
func (r *Replacement) Start(ctx context.Context) (string, error) {
	created, err := r.c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:             r.name,
		Config:           r.cfg,
		HostConfig:       r.host,
		NetworkingConfig: r.net,
	})
	if err != nil {
		return "", wrap(err, "create container")
	}
	r.newID = created.ID
	if _, err := r.c.api.ContainerStart(ctx, r.newID, client.ContainerStartOptions{}); err != nil {
		return r.newID, wrap(err, "start container")
	}
	return r.newID, nil
}

// Commit removes the old container.
func (r *Replacement) Commit(ctx context.Context) error {
	if _, err := r.c.api.ContainerRemove(ctx, r.old.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
		return wrap(err, "remove container")
	}
	return nil
}

// Discard removes the copy, if Start made one.
func (r *Replacement) Discard(ctx context.Context) error {
	if r.newID == "" {
		return nil
	}
	if _, err := r.c.api.ContainerRemove(ctx, r.newID, client.ContainerRemoveOptions{Force: true}); err != nil {
		return wrap(err, "remove container")
	}
	r.newID = ""
	return nil
}

// Restore gives the old container its name back and starts it again.
func (r *Replacement) Restore(ctx context.Context) error {
	if r.renamed {
		if _, err := r.c.api.ContainerRename(ctx, r.old.ID, client.ContainerRenameOptions{NewName: r.name}); err != nil {
			return wrap(err, "rename container")
		}
		r.renamed = false
	}
	if r.stopped {
		if _, err := r.c.api.ContainerStart(ctx, r.old.ID, client.ContainerStartOptions{}); err != nil {
			return wrap(err, "start container")
		}
		r.stopped = false
	}
	return nil
}

// Running reports whether id is up; false with its exit code once it quit.
func (c *Client) Running(ctx context.Context, id string) (bool, int, error) {
	resp, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return false, 0, wrap(err, "inspect container")
	}
	st := resp.Container.State
	if st == nil {
		return false, 0, nil
	}
	return st.Running && !st.Restarting, st.ExitCode, nil
}

// Values the old image set would shadow the new image's own, such as a
// stale version label, so only what the operator added is carried over.
func (c *Client) cloneConfig(ctx context.Context, old container.InspectResponse, image string) (
	*container.Config, *container.HostConfig, *network.NetworkingConfig, error,
) {
	if old.Config == nil || old.HostConfig == nil {
		return nil, nil, nil, fmt.Errorf("docker: container %s has no config", old.ID)
	}
	cfg := *old.Config
	cfg.Image = image
	if isShortID(old.ID, cfg.Hostname) {
		cfg.Hostname = ""
	}

	img, err := c.api.ImageInspect(ctx, old.Image)
	if err != nil {
		return nil, nil, nil, wrap(err, "inspect image")
	}
	if ic := img.Config; ic != nil {
		cfg.Env = slices.DeleteFunc(slices.Clone(cfg.Env), func(e string) bool { return slices.Contains(ic.Env, e) })
		labels := maps.Clone(cfg.Labels)
		for k, v := range ic.Labels {
			if labels[k] == v {
				delete(labels, k)
			}
		}
		cfg.Labels = labels
		if slices.Equal(cfg.Cmd, ic.Cmd) {
			cfg.Cmd = nil
		}
		if slices.Equal(cfg.Entrypoint, ic.Entrypoint) {
			cfg.Entrypoint = nil
		}
		if cfg.WorkingDir == ic.WorkingDir {
			cfg.WorkingDir = ""
		}
		if cfg.User == ic.User {
			cfg.User = ""
		}
	}

	host := *old.HostConfig
	var net *network.NetworkingConfig
	if old.NetworkSettings != nil && len(old.NetworkSettings.Networks) > 0 {
		net = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
		for name, ep := range old.NetworkSettings.Networks {
			if ep == nil {
				continue
			}
			net.EndpointsConfig[name] = &network.EndpointSettings{
				IPAMConfig: ep.IPAMConfig,
				Links:      ep.Links,
				Aliases:    slices.DeleteFunc(slices.Clone(ep.Aliases), func(a string) bool { return isShortID(old.ID, a) }),
				DriverOpts: ep.DriverOpts,
				GwPriority: ep.GwPriority,
			}
		}
	}
	return &cfg, &host, net, nil
}

// TailUnmanaged returns a container's last lines of output, without timestamps.
func (c *Client) TailUnmanaged(ctx context.Context, id string, lines int) (string, error) {
	rc, err := c.logs(ctx, id, lines, false)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("docker container logs: %w", err)
	}
	out := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, line := range out {
		if _, rest, ok := strings.Cut(line, " "); ok {
			out[i] = rest
		}
	}
	return strings.Join(out, "\n"), nil
}

// ExecUnmanaged is Exec for a container Tunploy did not create, such as its own.
func (c *Client) ExecUnmanaged(ctx context.Context, id string, cmd []string) ([]byte, error) {
	return c.exec(ctx, id, cmd)
}

func isShortID(id, s string) bool { return len(s) >= 12 && strings.HasPrefix(id, s) }
