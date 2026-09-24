// Command tunploy runs the Tunploy control panel.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/server"
	"github.com/kwa0x2/tunploy/internal/store"
)

// version is stamped at build time with -ldflags.
var version = "dev"

func main() {
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

	if logDockerStatus(ctx, dk) {
		go reconcile(ctx, mgr)
	}
	go purgeExpiredSessions(ctx, st)

	handler := server.New(cfg, st, dk, mgr)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	srv.RegisterOnShutdown(handler.Close)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	slog.Info("tunploy stopped cleanly")
	return nil
}

// A missing daemon is not fatal: the panel still has to come up so the
// admin can see what is wrong and fix it from there.
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

// reconcile runs beside the HTTP server because a first run may build the
// WireGuard image, and the panel should not wait on that to come up.
func reconcile(ctx context.Context, mgr *deploy.Manager) {
	if err := mgr.Reconcile(ctx); err != nil {
		slog.Error("reconcile wireguard containers", "error", err)
		return
	}
	slog.Info("wireguard containers reconciled")
}

func purgeExpiredSessions(ctx context.Context, st *store.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := st.DeleteExpiredSessions(ctx)
			if err != nil {
				slog.Error("purge expired sessions", "error", err)
				continue
			}
			if n > 0 {
				slog.Info("purged expired sessions", "count", n)
			}
		}
	}
}
