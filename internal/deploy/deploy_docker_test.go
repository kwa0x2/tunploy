package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type dockerFixture struct {
	dk    *docker.Client
	store *store.Store
	m     *Manager
	// Unique per test, so a panel on the same daemon never sees our containers.
	prefix string
}

func newDockerFixture(t *testing.T, ctx context.Context) *dockerFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("needs a Docker daemon")
	}
	dk, err := docker.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dk.Close() })
	if _, err := dk.Ping(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	prefix := fmt.Sprintf("tunploy-test-%08x-", rand.Uint32())
	m, err := New(st, dk, t.TempDir(), prefix)
	if err != nil {
		t.Fatal(err)
	}
	return &dockerFixture{dk: dk, store: st, m: m, prefix: prefix}
}

func (f *dockerFixture) instance(t *testing.T, ctx context.Context, keepalive int) *wg.Instance {
	t.Helper()
	in := wg.NewInstance("Docker test", "vpn.example.com")
	in.ListenPort = 40000 + rand.IntN(20000)
	in.PersistentKeepalive = keepalive
	created, err := f.store.CreateInstance(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.m.Remove(context.Background(), created.ID) })
	return created
}

// Runs the real image against the local daemon: build, bring wg0 up, sync a
// peer in and read it back from `wg show`.
func TestDeployAgainstDocker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f := newDockerFixture(t, ctx)
	st, m := f.store, f.m
	created := f.instance(t, ctx, wg.DefaultKeepalive)

	if name := m.ContainerName(created.ID); !strings.HasPrefix(name, f.prefix) {
		t.Fatalf("container %q does not use the test prefix %q", name, f.prefix)
	}

	if err := m.Deploy(ctx, created.ID, true); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	waitForState(t, m, created.ID, StateRunning)

	stats, err := m.PeerStats(ctx, created)
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
	stats, err = m.PeerStats(ctx, created)
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
	if stats, _ = m.PeerStats(ctx, created); len(stats) != 0 {
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

// A second container from the same image dials in as a device, so the online
// and offline changes come from a real handshake and real silence.
func TestPeerActivityAgainstDocker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f := newDockerFixture(t, ctx)
	// Short, so going offline takes seconds: the window is 2*keepalive+10s.
	in := f.instance(t, ctx, 2)

	changes := make(chan PeerChange, 16)
	f.m.OnPeerChange(func(c PeerChange) { changes <- c })

	peer := wg.NewPeer(in.ID, "phone")
	peer.Address = netip.MustParseAddr("10.8.0.2")
	created, err := f.store.CreatePeer(ctx, peer)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.m.Deploy(ctx, in.ID, true); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	waitForState(t, f.m, in.ID, StateRunning)

	// First look: offline, and never reported as a change.
	stats, err := f.m.PeerStats(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := stats[created.PublicKey]; !ok || st.Online {
		t.Fatalf("before the device dials in want it listed and offline, got %+v", stats)
	}

	server := f.m.ContainerName(in.ID)
	out, err := f.dk.Exec(ctx, server, []string{"hostname", "-i"})
	if err != nil {
		t.Fatalf("server address: %v", err)
	}
	serverIP := strings.Fields(string(out))[0]

	// The config the panel hands out, pointed at the container and kept off
	// the client container's default route.
	device := *in
	device.Endpoint = serverIP
	device.DNS = nil
	device.ClientAllowedIPs = []netip.Prefix{in.Subnet()}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wg0.conf"), wg.ClientConfig(device, *created), 0o600); err != nil {
		t.Fatal(err)
	}

	client := f.prefix + "client"
	_, err = f.dk.CreateContainer(ctx, docker.ContainerSpec{
		Name:   client,
		Image:  f.m.Image(),
		Mounts: []docker.Mount{{Source: dir, Target: "/etc/wireguard", ReadOnly: true}},
		CapAdd: []string{"NET_ADMIN"},
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	t.Cleanup(func() { f.dk.RemoveContainer(context.Background(), client) })
	if err := f.dk.StartContainer(ctx, client); err != nil {
		t.Fatalf("start client: %v", err)
	}

	online := waitForChange(t, f.m, in, changes, 30*time.Second)
	if !online.Online || online.Key != created.PublicKey || online.Endpoint == "" || online.OnlineSince.IsZero() {
		t.Fatalf("want the device reported online with its endpoint, got %+v", online)
	}
	if out, err := f.dk.Exec(ctx, client, []string{"ping", "-c", "1", "-W", "2", in.Address.Addr().String()}); err != nil {
		t.Fatalf("ping through the tunnel: %v\n%s", err, out)
	}

	if err := f.dk.StopContainer(ctx, client, 5*time.Second); err != nil {
		t.Fatalf("stop client: %v", err)
	}
	offline := waitForChange(t, f.m, in, changes, 40*time.Second)
	if offline.Online || offline.Key != created.PublicKey {
		t.Fatalf("want the device reported offline, got %+v", offline)
	}
}

// Polls like Watch does until PeerStats reports a change.
func waitForChange(t *testing.T, m *Manager, in *wg.Instance, changes <-chan PeerChange, within time.Duration) PeerChange {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := m.PeerStats(context.Background(), in); err != nil {
			t.Fatalf("peer stats: %v", err)
		}
		select {
		case c := <-changes:
			return c
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatalf("no peer change within %s", within)
	return PeerChange{}
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
