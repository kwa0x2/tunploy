// Command tunploy runs the Tunploy control panel.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // TZ works even in an image without a zoneinfo directory

	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/geoip"
	"github.com/kwa0x2/tunploy/internal/server"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/tlscert"
)

// version is stamped at build time with -ldflags.
var version = "dev"

const (
	peerWatchInterval = 10 * time.Second
	eventRetention    = 90 * 24 * time.Hour
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		os.Exit(runAdmin(os.Args[2:]))
	}
	if err := run(); err != nil {
		slog.Error("tunploy stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})))
	slog.Info("starting tunploy", "version", version, "data_dir", cfg.DataDir)

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	dk, err := docker.New(cfg.DockerHost)
	if err != nil {
		return err
	}
	defer dk.Close()

	mgr, err := deploy.New(st, dk, cfg.DataDir)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var geo *geoip.DB
	if cfg.GeoIP {
		geo = geoip.Open(filepath.Join(cfg.DataDir, "geoip"))
		go geo.Run(ctx)
	}

	certs := tlscert.New(filepath.Join(cfg.DataDir, "certs"), cfg.ACMEDirectory, cfg.HTTPSListen)
	if cfg.HTTPSListen == "" {
		certs.Disable("HTTPS is turned off with TUNPLOY_HTTPS=false")
	}
	handler := server.New(cfg, st, dk, mgr, geo, certs)

	if logDockerStatus(ctx, dk) {
		go reconcile(ctx, mgr)
	}
	go mgr.Watch(ctx, peerWatchInterval)
	go handler.RunNotifications(ctx)
	go housekeeping(ctx, st)

	panel := newHTTPServer(cfg.Listen, handler)
	panel.RegisterOnShutdown(handler.Close)
	servers := []*http.Server{panel}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", panel.Addr)
		if err := panel.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	if cfg.HTTPSListen != "" {
		servers = append(servers, serveHTTPS(cfg, handler, certs)...)
		if err := handler.RestoreDomain(ctx); err != nil {
			slog.Error("restore panel domain", "error", err)
		}
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	}
	slog.Info("tunploy stopped cleanly")
	return nil
}

func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// Not fatal: the panel on its own port still works without HTTPS.
func serveHTTPS(cfg config.Config, h http.Handler, certs *tlscert.Manager) []*http.Server {
	secure := newHTTPServer(cfg.HTTPSListen, h)
	secure.TLSConfig = certs.TLSConfig()
	plain := newHTTPServer(cfg.HTTPListen, certs.HTTPHandler())

	for _, srv := range []*http.Server{secure, plain} {
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			slog.Warn("https is unavailable", "addr", srv.Addr, "error", err)
			certs.Disable(fmt.Sprintf("could not listen on %s: %v", srv.Addr, err))
			continue
		}
		slog.Info("listening", "addr", srv.Addr, "tls", srv.TLSConfig != nil)
		go func() {
			serve := srv.Serve
			if srv.TLSConfig != nil {
				serve = func(ln net.Listener) error { return srv.ServeTLS(ln, "", "") }
			}
			if err := serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("https listener stopped", "addr", srv.Addr, "error", err)
			}
		}()
	}
	return []*http.Server{secure, plain}
}

// Not fatal: the panel must come up to show what is wrong.
func logDockerStatus(ctx context.Context, dk *docker.Client) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	info, err := dk.Ping(ctx)
	if err != nil {
		slog.Warn("docker is not reachable; VPN services cannot be deployed", "error", err)
		return false
	}
	slog.Info("connected to docker", "version", info.Version, "api_version", info.APIVersion)
	return true
}

func reconcile(ctx context.Context, mgr *deploy.Manager) {
	if err := mgr.Reconcile(ctx); err != nil {
		slog.Error("reconcile wireguard containers", "error", err)
		return
	}
	slog.Info("wireguard containers reconciled")
}

func housekeeping(ctx context.Context, st *store.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if n, err := st.DeleteExpiredSessions(ctx); err != nil {
			slog.Error("purge expired sessions", "error", err)
		} else if n > 0 {
			slog.Info("purged expired sessions", "count", n)
		}
		if n, err := st.DeleteEventsBefore(ctx, time.Now().Add(-eventRetention)); err != nil {
			slog.Error("purge old events", "error", err)
		} else if n > 0 {
			slog.Info("purged old events", "count", n)
		}
	}
}
