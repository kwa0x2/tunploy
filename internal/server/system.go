package server

import (
	"context"
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/httpx"
)

type Docker interface {
	Ping(ctx context.Context) (docker.Info, error)
	Daemon(ctx context.Context) (docker.Daemon, error)
}

type dockerStatusResponse struct {
	Available bool         `json:"available"`
	Error     string       `json:"error,omitempty"`
	Info      *docker.Info `json:"info,omitempty"`
}

// Always 200: a down daemon is state to show, and its raw error says how to fix it.
func (s *Server) handleDockerStatus(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	info, err := s.docker.Ping(ctx)
	if err != nil {
		return httpx.JSON(w, http.StatusOK, dockerStatusResponse{Error: err.Error()})
	}
	return httpx.JSON(w, http.StatusOK, dockerStatusResponse{Available: true, Info: &info})
}
