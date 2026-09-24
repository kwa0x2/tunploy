package docker

import (
	"context"
	"io"
	"strconv"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// Logs streams a managed container's stdout and stderr merged into one, each
// line prefixed with an RFC 3339 timestamp. With follow it stays open until
// the container stops, ctx ends or the caller closes it.
func (c *Client) Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error) {
	if err := c.ensureManaged(ctx, id); err != nil {
		return nil, err
	}
	raw, err := c.api.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     follow,
		Tail:       strconv.Itoa(tail),
	})
	if err != nil {
		return nil, wrap(err, "container logs")
	}

	pr, pw := io.Pipe()
	go func() {
		_, err := stdcopy.StdCopy(pw, pw, raw)
		pw.CloseWithError(err)
	}()
	return logStream{PipeReader: pr, raw: raw}, nil
}

// logStream closes the daemon's stream too, which is what unblocks the
// demultiplexing goroutine while it waits on a quiet container.
type logStream struct {
	*io.PipeReader
	raw io.Closer
}

func (l logStream) Close() error {
	l.PipeReader.Close()
	return l.raw.Close()
}
