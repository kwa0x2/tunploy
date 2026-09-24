package deploy

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// Runs the real image against the local daemon: build, bring wg0 up, sync a
// peer in and read it back from `wg show`.
func TestDeployAgainstDocker(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a Docker daemon")
	}
	dk, err := docker.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dk.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := dk.Ping(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	m, err := New(st, dk, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	in := wg.NewInstance("Docker test", "vpn.example.com")
	in.ListenPort = 40000 + rand.IntN(20000)
	created, err := st.CreateInstance(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Remove(context.Background(), created.ID) })

	if err := m.Deploy(ctx, created.ID, true); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	waitForState(t, m, created.ID, StateRunning)

	stats, err := m.PeerStats(ctx, created.ID)
	if err != nil {
		t.Fatalf("stats with no peers: %v", err)
	}
	if len(stats) != 0 {
		t.Fatalf("want no peers yet, got %v", stats)
	}

	peer, err := st.CreatePeer(ctx, wg.NewPeer(created.ID, "phone"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(ctx, created.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	stats, err = m.PeerStats(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stats[peer.PublicKey]; !ok {
		t.Fatalf("synced peer missing from wg show: %v", stats)
	}

	peer.Enabled = false
	if _, err := st.UpdatePeer(ctx, *peer); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(ctx, created.ID); err != nil {
		t.Fatalf("apply disable: %v", err)
	}
	if stats, _ = m.PeerStats(ctx, created.ID); len(stats) != 0 {
		t.Fatalf("disabled peer still on the interface: %v", stats)
	}

	live, err := m.Logs(ctx, created.ID, 50, true)
	if err != nil {
		t.Fatalf("follow logs: %v", err)
	}
	defer live.Close()

	if err := m.Stop(ctx, created.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	// A followed stream has to end on its own once the container stops.
	out, err := io.ReadAll(live)
	if err != nil {
		t.Fatalf("read followed logs: %v", err)
	}
	first, _, _ := strings.Cut(string(out), "\n")
	stamp, text, _ := strings.Cut(first, " ")
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil || !strings.Contains(string(out), "wireguard wg0 is up") {
		t.Fatalf("want timestamped lines including the startup message, got %q (%q)", out, text)
	}
	if s := m.Status(ctx, created.ID); s.State != StateStopped || s.Error != "" {
		t.Fatalf("after a clean stop want stopped without error, got %+v", s)
	}

	if err := m.Remove(ctx, created.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if s := m.Status(ctx, created.ID); s.State != StateNotDeployed {
		t.Fatalf("after remove want not_deployed, got %+v", s)
	}
	if _, err := os.Stat(m.ConfigDir(created.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config dir should be gone, stat says %v", err)
	}
}

func waitForState(t *testing.T, m *Manager, id int64, want State) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var s Status
	for time.Now().Before(deadline) {
		s = m.Status(context.Background(), id)
		if s.State == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("instance %d never reached %s, last status %+v", id, want, s)
}
