package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
)

func (c *Client) EnsureImage(ctx context.Context, ref string) error {
	ok, err := c.ImageExists(ctx, ref)
	if err != nil || ok {
		return err
	}
	return c.PullImage(ctx, ref)
}

func (c *Client) ImageExists(ctx context.Context, ref string) (bool, error) {
	_, err := c.api.ImageInspect(ctx, ref)
	if err == nil {
		return true, nil
	}
	if err = wrap(err, "inspect image"); errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

func (c *Client) BuildImage(ctx context.Context, tag string, files map[string][]byte) error {
	slog.Info("building image", "image", tag)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		mode := int64(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body))}); err != nil {
			return fmt.Errorf("docker build context: %w", err)
		}
		if _, err := tw.Write(body); err != nil {
			return fmt.Errorf("docker build context: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("docker build context: %w", err)
	}

	resp, err := c.api.ImageBuild(ctx, &buf, client.ImageBuildOptions{
		Tags:        []string{tag},
		Remove:      true,
		ForceRemove: true,
		Labels:      map[string]string{LabelManaged: "true"},
	})
	if err != nil {
		return wrap(err, "build image")
	}
	defer resp.Body.Close()

	// Like a pull, a failed RUN step arrives as a message in a 200 stream.
	dec := json.NewDecoder(resp.Body)
	for {
		var msg jsonstream.Message
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("docker build image %s: read output: %w", tag, err)
		}
		if msg.Error != nil {
			return fmt.Errorf("docker build image %s: %s", tag, msg.Error.Message)
		}
		if line := strings.TrimSpace(msg.Stream); line != "" {
			slog.Debug("build output", "image", tag, "line", line)
		}
	}

	slog.Info("built image", "image", tag)
	return nil
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	slog.Info("pulling image", "image", ref)

	resp, err := c.api.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return wrap(err, "pull image")
	}
	// Registry failures such as a missing tag only show up inside the stream.
	if err := resp.Wait(ctx); err != nil {
		return wrap(err, "pull image "+ref)
	}

	slog.Info("pulled image", "image", ref)
	return nil
}
