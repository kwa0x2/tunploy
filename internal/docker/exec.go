package docker

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

func (c *Client) Exec(ctx context.Context, id string, cmd []string) ([]byte, error) {
	if err := c.ensureManaged(ctx, id); err != nil {
		return nil, err
	}

	created, err := c.api.ExecCreate(ctx, id, client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, wrap(err, "exec create")
	}

	attached, err := c.api.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return nil, wrap(err, "exec attach")
	}
	defer attached.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader); err != nil {
		return nil, fmt.Errorf("docker exec read output: %w", err)
	}

	inspected, err := c.api.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return nil, wrap(err, "exec inspect")
	}
	if inspected.ExitCode != 0 {
		return nil, fmt.Errorf("docker exec %s: exit code %d: %s",
			cmd[0], inspected.ExitCode, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
