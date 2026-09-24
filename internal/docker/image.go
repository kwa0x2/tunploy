package docker

import (
	"context"
	"errors"
	"log/slog"

	"github.com/moby/moby/client"
)

// EnsureImage pulls ref only when it is not already present, so starting a
// service never depends on the registry being reachable once it has run.
func (c *Client) EnsureImage(ctx context.Context, ref string) error {
	_, err := c.api.ImageInspect(ctx, ref)
	if err == nil {
		return nil
	}
	if err = wrap(err, "inspect image"); !errors.Is(err, ErrNotFound) {
		return err
	}
	return c.PullImage(ctx, ref)
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	slog.Info("pulling image", "image", ref)

	resp, err := c.api.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return wrap(err, "pull image")
	}
	// The HTTP call succeeds as soon as the stream opens; registry failures
	// such as a missing tag only show up inside it, which Wait surfaces.
	if err := resp.Wait(ctx); err != nil {
		return wrap(err, "pull image "+ref)
	}

	slog.Info("pulled image", "image", ref)
	return nil
}
