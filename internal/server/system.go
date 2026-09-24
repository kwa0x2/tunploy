package server

import (
	"context"
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/httpx"
)

// Docker is the part of *docker.Client the API depends on; tests swap in a fake.
type Docker interface {
	Ping(ctx context.Context) (docker.Info, error)
}

type dockerStatusResponse struct {
	Available bool         `json:"available"`
	Error     string       `json:"error,omitempty"`
	Info      *docker.Info `json:"info,omitempty"`
}

// An unreachable daemon is a state to show, not a failed request, so this
// always answers 200. The raw error goes out on purpose: "permission denied
// on docker.sock" is exactly what the admin needs to fix the mount.
func (s *Server) handleDockerStatus(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	info, err := s.docker.Ping(ctx)
	if err != nil {
		return httpx.JSON(w, http.StatusOK, dockerStatusResponse{Error: err.Error()})
	}
	return httpx.JSON(w, http.StatusOK, dockerStatusResponse{Available: true, Info: &info})
}
