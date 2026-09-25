// Package docker is the slice of the Docker Engine API Tunploy needs.
package docker

import (
	"context"
	"errors"
	"fmt"
	"net"

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

// New does not dial; an unreachable daemon surfaces on the first call.
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

// NewDialer reaches the daemon through dial, such as over SSH.
func NewDialer(dial func(ctx context.Context, network, addr string) (net.Conn, error)) (*Client, error) {
	api, err := client.New(client.WithHost("http://docker"), client.WithDialContext(dial))
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

type Daemon struct {
	ID            string `json:"-"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	KernelVersion string `json:"kernel_version"`
	CPUs          int    `json:"cpus"`
	Memory        int64  `json:"memory"`
}

// Daemon describes the machine the daemon runs on. ID tells two daemons apart.
func (c *Client) Daemon(ctx context.Context) (Daemon, error) {
	res, err := c.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		return Daemon{}, wrap(err, "info")
	}
	info := res.Info
	return Daemon{
		ID:            info.ID,
		Version:       info.ServerVersion,
		OS:            info.OperatingSystem,
		Arch:          info.Architecture,
		KernelVersion: info.KernelVersion,
		CPUs:          info.NCPU,
		Memory:        info.MemTotal,
	}, nil
}

// Matchable with errors.Is, so callers never import containerd's errdefs.
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
