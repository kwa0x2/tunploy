// Package server wires Tunploy's HTTP API together.
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/web"
)

const (
	loginMaxAttempts = 10
	loginWindow      = 15 * time.Minute
)

type Server struct {
	cfg           config.Config
	store         *store.Store
	docker        Docker
	deploy        *deploy.Manager
	loginThrottle *auth.Throttle
	handler       http.Handler

	// closing ends long-lived streams when the panel shuts down.
	closing     context.Context
	stopStreams context.CancelFunc
}

func New(cfg config.Config, st *store.Store, dk Docker, mgr *deploy.Manager) *Server {
	s := &Server{
		cfg:           cfg,
		store:         st,
		docker:        dk,
		deploy:        mgr,
		loginThrottle: auth.NewThrottle(loginMaxAttempts, loginWindow),
	}
	s.closing, s.stopStreams = context.WithCancel(context.Background())
	s.handler = chain(s.routes(), recoverPanics, logRequests)
	return s
}

// Close ends open log streams; register it with http.Server.RegisterOnShutdown.
func (s *Server) Close() { s.stopStreams() }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /api/health", httpx.Handler(s.handleHealth))
	mux.Handle("GET /api/setup", httpx.Handler(s.handleSetupStatus))
	mux.Handle("POST /api/setup", httpx.Handler(s.handleSetup))
	mux.Handle("POST /api/auth/login", httpx.Handler(s.handleLogin))

	private := http.NewServeMux()
	private.Handle("POST /api/auth/logout", httpx.Handler(s.handleLogout))
	private.Handle("GET /api/auth/me", httpx.Handler(s.handleMe))
	private.Handle("POST /api/auth/password", httpx.Handler(s.handleChangePassword))
	private.Handle("GET /api/system/docker", httpx.Handler(s.handleDockerStatus))
	private.Handle("GET /api/settings", httpx.Handler(s.handleGetSettings))
	private.Handle("PATCH /api/settings", httpx.Handler(s.handleUpdateSettings))

	private.Handle("GET /api/instances", httpx.Handler(s.handleListInstances))
	private.Handle("POST /api/instances", httpx.Handler(s.handleCreateInstance))
	private.Handle("GET /api/instances/defaults", httpx.Handler(s.handleInstanceDefaults))
	private.Handle("GET /api/instances/{id}", httpx.Handler(s.handleGetInstance))
	private.Handle("PATCH /api/instances/{id}", httpx.Handler(s.handleUpdateInstance))
	private.Handle("DELETE /api/instances/{id}", httpx.Handler(s.handleDeleteInstance))
	private.Handle("POST /api/instances/{id}/start", s.handleInstanceAction((*deploy.Manager).Start))
	private.Handle("POST /api/instances/{id}/stop", s.handleInstanceAction((*deploy.Manager).Stop))
	private.Handle("POST /api/instances/{id}/restart", s.handleInstanceAction((*deploy.Manager).Restart))
	private.Handle("GET /api/instances/{id}/logs", httpx.Handler(s.handleInstanceLogs))

	private.Handle("GET /api/instances/{id}/peers", httpx.Handler(s.handleListPeers))
	private.Handle("POST /api/instances/{id}/peers", httpx.Handler(s.handleCreatePeer))
	private.Handle("PATCH /api/instances/{id}/peers/{peerID}", httpx.Handler(s.handleUpdatePeer))
	private.Handle("DELETE /api/instances/{id}/peers/{peerID}", httpx.Handler(s.handleDeletePeer))
	private.Handle("GET /api/instances/{id}/peers/{peerID}/config", httpx.Handler(s.handlePeerConfig))

	// Also catches unmatched paths and method mismatches, so clients only
	// ever parse the JSON envelope.
	private.Handle("/api/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound("no such endpoint: %s %s", r.Method, r.URL.Path)
	}))

	mux.Handle("/api/", chain(private, s.requireAuth))
	mux.Handle("/", web.Handler())

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) error {
	return httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
