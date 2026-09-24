// Package docker is the narrow slice of the Docker Engine API that Tunploy
// needs to run VPN services as containers.
package docker

import (
	"context"
	"errors"
	"fmt"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

var (
	ErrUnavailable = errors.New("daemon is unreachable")
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
	ErrNotManaged  = errors.New("container is not managed by tunploy")
)

type Client struct {
	api *client.Client
}

// New does not dial; an unreachable daemon only surfaces on the first call.
// An empty host falls back to DOCKER_HOST, then the local socket.
func New(host string) (*Client, error) {
	opts := []client.Opt{client.FromEnv}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	api, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Client{api: api}, nil
}

func (c *Client) Close() error { return c.api.Close() }

type Info struct {
	Version    string `json:"version"`
	APIVersion string `json:"api_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
}

func (c *Client) Ping(ctx context.Context) (Info, error) {
	if _, err := c.api.Ping(ctx, client.PingOptions{NegotiateAPIVersion: true}); err != nil {
		return Info{}, wrap(err, "ping")
	}
	v, err := c.api.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return Info{}, wrap(err, "server version")
	}
	return Info{
		Version:    v.Version,
		APIVersion: c.api.ClientVersion(),
		OS:         v.Os,
		Arch:       v.Arch,
	}, nil
}

// wrap keeps the daemon's message but makes the failure class matchable
// with errors.Is, so callers never import containerd's errdefs.
func wrap(err error, op string) error {
	var kind error
	switch {
	case client.IsErrConnectionFailed(err):
		kind = ErrUnavailable
	case cerrdefs.IsNotFound(err):
		kind = ErrNotFound
	case cerrdefs.IsConflict(err):
		kind = ErrConflict
	default:
		return fmt.Errorf("docker %s: %w", op, err)
	}
	return fmt.Errorf("docker %s: %w: %w", op, kind, err)
}
