// Package server wires Tunploy's HTTP API together.
package server

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/geoip"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/notify"
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
	geo           *geoip.DB
	https         HTTPS
	notifier      *notify.Notifier
	loginThrottle *auth.Throttle
	handler       http.Handler
	lookupHost    func(ctx context.Context, host string) ([]string, error)

	closing     context.Context
	stopStreams context.CancelFunc
}

// Call New before mgr.Watch: it hooks into peer changes.
func New(cfg config.Config, st *store.Store, dk Docker, mgr *deploy.Manager, geo *geoip.DB, https HTTPS) *Server {
	s := &Server{
		cfg:           cfg,
		store:         st,
		docker:        dk,
		deploy:        mgr,
		geo:           geo,
		https:         https,
		notifier:      notify.New(),
		loginThrottle: auth.NewThrottle(loginMaxAttempts, loginWindow),
		lookupHost:    net.DefaultResolver.LookupHost,
	}
	mgr.OnPeerChange(s.peerChanged)
	mgr.OnPeerBlock(s.peerBlocked)
	mgr.OnServerHealth(s.serverHealth)
	s.closing, s.stopStreams = context.WithCancel(context.Background())
	s.handler = chain(s.routes(), recoverPanics, s.identifyClient, securityHeaders, logRequests)
	return s
}

func (s *Server) Close() { s.stopStreams() }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /api/health", httpx.Handler(s.handleHealth))
	mux.Handle("GET /api/setup", httpx.Handler(s.handleSetupStatus))
	mux.Handle("POST /api/auth/login", httpx.Handler(s.handleLogin))
	mux.Handle("GET /api/https", httpx.Handler(s.handleHTTPSInfo))

	private := http.NewServeMux()
	private.Handle("POST /api/auth/logout", httpx.Handler(s.handleLogout))
	private.Handle("GET /api/auth/me", httpx.Handler(s.handleMe))
	private.Handle("POST /api/auth/password", httpx.Handler(s.handleChangePassword))
	private.Handle("POST /api/auth/totp/setup", httpx.Handler(s.handleTOTPSetup))
	private.Handle("POST /api/auth/totp/enable", httpx.Handler(s.handleTOTPEnable))
	private.Handle("POST /api/auth/totp/disable", httpx.Handler(s.handleTOTPDisable))
	private.Handle("GET /api/system/docker", httpx.Handler(s.handleDockerStatus))
	private.Handle("GET /api/settings", httpx.Handler(s.handleGetSettings))
	private.Handle("PATCH /api/settings", httpx.Handler(s.handleUpdateSettings))
	private.Handle("GET /api/settings/domain", httpx.Handler(s.handleGetDomain))
	private.Handle("PUT /api/settings/domain", httpx.Handler(s.handleSetDomain))
	private.Handle("POST /api/settings/domain/retry", httpx.Handler(s.handleRetryDomain))
	private.Handle("GET /api/settings/notifications", httpx.Handler(s.handleGetNotifications))
	private.Handle("PUT /api/settings/notifications", httpx.Handler(s.handleSetNotifications))
	private.Handle("POST /api/settings/notifications/test", httpx.Handler(s.handleTestNotifications))
	private.Handle("GET /api/events", httpx.Handler(s.handleListEvents))

	private.Handle("GET /api/instances", httpx.Handler(s.handleListInstances))
	private.Handle("POST /api/instances", httpx.Handler(s.handleCreateInstance))
	private.Handle("GET /api/instances/defaults", httpx.Handler(s.handleInstanceDefaults))
	private.Handle("GET /api/instances/{id}", httpx.Handler(s.handleGetInstance))
	private.Handle("PATCH /api/instances/{id}", httpx.Handler(s.handleUpdateInstance))
	private.Handle("DELETE /api/instances/{id}", httpx.Handler(s.handleDeleteInstance))
	private.Handle("POST /api/instances/{id}/start", s.handleInstanceAction("server.started", (*deploy.Manager).Start))
	private.Handle("POST /api/instances/{id}/stop", s.handleInstanceAction("server.stopped", (*deploy.Manager).Stop))
	private.Handle("POST /api/instances/{id}/restart", s.handleInstanceAction("server.restarted", (*deploy.Manager).Restart))
	private.Handle("GET /api/instances/{id}/logs", httpx.Handler(s.handleInstanceLogs))

	private.Handle("GET /api/instances/{id}/peers", httpx.Handler(s.handleListPeers))
	private.Handle("POST /api/instances/{id}/peers", httpx.Handler(s.handleCreatePeer))
	private.Handle("PATCH /api/instances/{id}/peers/{peerID}", httpx.Handler(s.handleUpdatePeer))
	private.Handle("DELETE /api/instances/{id}/peers/{peerID}", httpx.Handler(s.handleDeletePeer))
	private.Handle("GET /api/instances/{id}/peers/{peerID}/config", httpx.Handler(s.handlePeerConfig))
	private.Handle("GET /api/instances/{id}/peers/{peerID}/usage", httpx.Handler(s.handlePeerUsage))

	// Unmatched paths get the JSON envelope too.
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
