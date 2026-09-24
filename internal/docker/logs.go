package docker

import (
	"context"
	"io"
	"strconv"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// Lines are prefixed with an RFC 3339 timestamp.
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

// Closing the daemon's stream is what unblocks the demux goroutine.
type logStream struct {
	*io.PipeReader
	raw io.Closer
}

func (l logStream) Close() error {
	l.PipeReader.Close()
	return l.raw.Close()
}
