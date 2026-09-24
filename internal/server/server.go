// Package server wires Tunploy's HTTP API together.
package server

import (
	"net/http"

	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

type Server struct {
	cfg     config.Config
	store   *store.Store
	handler http.Handler
}

func New(cfg config.Config, st *store.Store) *Server {
	s := &Server{cfg: cfg, store: st}
	s.handler = chain(s.routes(), recoverPanics, logRequests)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/health", httpx.Handler(s.handleHealth))

	// Catches unmatched API paths and method mismatches, so clients only
	// ever parse the JSON envelope.
	mux.Handle("/api/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound("no such endpoint: %s %s", r.Method, r.URL.Path)
	}))

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) error {
	return httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
