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

	"github.com/kwa0x2/tunploy/internal/api"
	"github.com/kwa0x2/tunploy/internal/backup"
	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/geoip"
	"github.com/kwa0x2/tunploy/internal/host"
	"github.com/kwa0x2/tunploy/internal/instance"
	"github.com/kwa0x2/tunploy/internal/node"
	"github.com/kwa0x2/tunploy/internal/notify"
	"github.com/kwa0x2/tunploy/internal/peer"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/tlscert"
	"github.com/kwa0x2/tunploy/internal/update"
	"github.com/kwa0x2/tunploy/internal/webhook"
)

// version is stamped at build time with -ldflags.
var version = "dev"

const usage = `usage: tunploy [command]

Without a command it runs the panel.

commands:
  admin     create the admin account, reset its password or turn off 2FA
  backup    list the backups in S3 and restore one
  version   print the version
  health    exit 0 when the panel on this machine answers
`

const (
	peerWatchInterval = 10 * time.Second
	eventRetention    = 90 * 24 * time.Hour
	deliveryRetention = 30 * 24 * time.Hour
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "admin":
			os.Exit(runAdmin(os.Args[2:]))
		case "backup":
			os.Exit(runBackup(os.Args[2:]))
		case "self-update":
			os.Exit(runSelfUpdate(os.Args[2:]))
		case "host-cli":
			os.Exit(runHostCLI(os.Args[2:]))
		case "health":
			os.Exit(runHealth())
		case "version", "--version":
			fmt.Println(version)
			return
		case "help", "-h", "--help":
			fmt.Print(usage)
			return
		default:
			// A typo must not start a second panel on the same database.
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
			os.Exit(2)
		}
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var geo *geoip.DB
	if cfg.GeoIP {
		geo = geoip.Open(filepath.Join(cfg.DataDir, "geoip"))
		go geo.Run(ctx)
	}

	notifier := notify.New()
	webhooks := webhook.New(st)
	events := event.NewJournal(st, geo, notifier, webhooks)

	certs := tlscert.New(filepath.Join(cfg.DataDir, "certs"), cfg.ACMEDirectory, cfg.HTTPSListen)
	if cfg.HTTPSListen == "" {
		certs.Disable("HTTPS is turned off with TUNPLOY_HTTPS=false")
	}
	updates := update.New(version, cfg.DataDir, dk, cfg.UpdateCheck, events)
	nodes := node.NewPool(st, events)
	mgr, err := deploy.New(st, host.NewLocal(dk, cfg.DataDir), nodes, cfg.ContainerPrefix, events)
	if err != nil {
		return err
	}
	backups := backup.NewService(st, cfg.DataDir, version, events, backup.Live{
		Settings: []backup.Loader{notifier, certs},
		VPN:      mgr,
		Nodes:    nodes,
	})
	handler := api.New(cfg, api.Deps{
		Store:     st,
		Docker:    dk,
		Deploy:    mgr,
		Instances: instance.New(st, mgr, geo, events, cfg.PublicHost),
		Peers:     peer.New(st, mgr, events),
		Nodes:     node.NewService(st, nodes, mgr, dk, events),
		Pool:      nodes,
		Geo:       geo,
		HTTPS:     certs,
		Notifier:  notifier,
		Webhooks:  webhooks,
		Events:    events,
		Backups:   backups,
		Updates:   updates,
	})
	updates.ReportLast(ctx)

	stored, err := st.Settings(ctx)
	if err != nil {
		slog.Error("load settings", "error", err)
	} else {
		notifier.Load(stored)
		backups.Load(stored)
	}

	// Marked before any node connects, so each one rebuilds as it does.
	rebuild := backup.RebuildPending(cfg.DataDir)
	if rebuild {
		if err := mgr.MarkRebuild(ctx); err != nil {
			return err
		}
	}
	if logDockerStatus(ctx, dk) {
		go reconcile(ctx, mgr, cfg.DataDir, rebuild)
	}
	if err := nodes.Start(ctx, mgr); err != nil {
		slog.Error("connect to nodes", "error", err)
	}
	go mgr.Watch(ctx, peerWatchInterval)
	go notifier.Run(ctx)
	go webhooks.Run(ctx)
	go backups.Run(ctx)
	go updates.Run(ctx)
	go updates.EnsureHostCLI(ctx)
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
		// Once the listeners are up, since Let's Encrypt checks the domain through them.
		certs.Load(stored)
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

// After a restore from the command line the same container name may belong to
// a different server, so every container is replaced instead.
func reconcile(ctx context.Context, mgr *deploy.Manager, dataDir string, rebuild bool) {
	if rebuild {
		slog.Info("rebuilding wireguard containers after a restore")
		if err := mgr.Reconcile(ctx); err != nil {
			slog.Error("rebuild wireguard containers; restart the panel to try again", "error", err)
			return
		}
		if err := backup.ClearRebuild(dataDir); err != nil {
			slog.Error("clear rebuild request", "error", err)
		}
		slog.Info("wireguard containers rebuilt")
		return
	}
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
		if _, err := st.DeleteIdempotencyKeysBefore(ctx, time.Now().Add(-store.IdempotencyTTL)); err != nil {
			slog.Error("purge idempotency keys", "error", err)
		}
		if _, err := st.DeleteDeliveriesBefore(ctx, time.Now().Add(-deliveryRetention)); err != nil {
			slog.Error("purge webhook deliveries", "error", err)
		}
	}
}
