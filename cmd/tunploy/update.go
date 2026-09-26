package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/hostcli"
	"github.com/kwa0x2/tunploy/internal/update"
)

// runSelfUpdate runs in the updater container the panel starts from the new
// image, since a container can't replace itself.
func runSelfUpdate(args []string) int {
	fs := flag.NewFlagSet("self-update", flag.ContinueOnError)
	id := fs.String("container", "", "the panel container to replace")
	image := fs.String("image", "", "the image to run it on")
	from := fs.String("from", "", "the version being replaced")
	to := fs.String("to", "", "the version being installed")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" || *image == "" {
		fmt.Fprintln(os.Stderr, "usage: tunploy self-update --container ID --image REF [--from V --to V]")
		return 2
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// Not config.Load: a setting the new version rejects must fail the new
	// panel's health check, where it is rolled back, not this command.
	dataDir := cmp.Or(os.Getenv("TUNPLOY_DATA_DIR"), "/var/lib/tunploy")
	opts := update.ApplyOptions{
		Container:     *id,
		Image:         *image,
		DataDir:       dataDir,
		From:          *from,
		To:            *to,
		StopTimeout:   30 * time.Second,
		HealthTimeout: 90 * time.Second,
		HealthEvery:   2 * time.Second,
	}

	dk, err := docker.New(os.Getenv("TUNPLOY_DOCKER_HOST"))
	if err != nil {
		slog.Error("update failed", "error", err)
		opts.Report(err)
		return 1
	}
	defer dk.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	slog.Info("replacing the panel container", "container", *id, "image", *image)
	if err := update.Apply(ctx, swapper{dk}, opts); err != nil {
		slog.Error("update failed", "error", err)
		return 1
	}
	slog.Info("panel updated", "version", *to)
	return 0
}

type swapper struct{ *docker.Client }

func (s swapper) Replace(ctx context.Context, id, image string, stop time.Duration) (update.Replacement, error) {
	r, err := s.Client.Replace(ctx, id, image, stop)
	if r == nil {
		return nil, err
	}
	return r, err
}

// runHealth is the updater's check that a new panel answers, run inside it.
func runHealth() int {
	addr := os.Getenv("TUNPLOY_LISTEN")
	if addr == "" {
		addr = ":3000"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "TUNPLOY_LISTEN:", err)
		return 1
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/api/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "health:", resp.Status)
		return 1
	}
	return 0
}

// runHostCLI runs in a helper container with the server's bin directory
// mounted, to put the tunploy command there.
func runHostCLI(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tunploy host-cli PATH IMAGE")
		return 2
	}
	changed, err := hostcli.Install(args[0], args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if changed {
		fmt.Println("installed the tunploy command")
	}
	return 0
}
