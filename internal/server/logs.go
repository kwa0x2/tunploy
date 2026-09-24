package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/httpx"
)

const (
	defaultLogTail = 200
	maxLogTail     = 5000
)

func (s *Server) handleInstanceLogs(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}

	tail := defaultLogTail
	if raw := r.URL.Query().Get("tail"); raw != "" {
		tail, err = strconv.Atoi(raw)
		if err != nil || tail < 1 || tail > maxLogTail {
			return httpx.BadRequest("tail must be between 1 and %d", maxLogTail)
		}
	}
	follow, _ := strconv.ParseBool(r.URL.Query().Get("follow"))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Shutdown waits for open requests; a followed stream would hold it.
	defer context.AfterFunc(s.closing, cancel)()

	logs, err := s.deploy.Logs(ctx, in.ID, tail, follow)
	if errors.Is(err, deploy.ErrNotDeployed) {
		return httpx.Conflict("this server has no container yet; deploy it first")
	}
	if err != nil {
		return deployError(err)
	}
	defer logs.Close()

	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	// Stops nginx and similar proxies from holding lines back.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	for {
		n, err := logs.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return nil
			}
			rc.Flush()
		}
		if err != nil {
			// The status line is out, so a failure can only be logged.
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				slog.Warn("stream container logs", "instance", in.ID, "error", err)
			}
			return nil
		}
	}
}
