package deploy

import (
	"context"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func TestActivity(t *testing.T) {
	key := wg.GeneratePrivateKey().PublicKey()
	start := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	handshake := start.Add(-30 * time.Second)

	a := activity{peers: map[int64]map[wg.Key]sample{}}
	check := func(at time.Duration, rx int64, want bool) {
		t.Helper()
		stats := map[wg.Key]wg.PeerStats{key: {RxBytes: rx, LatestHandshake: &handshake}}
		a.observe(1, stats, 25, start.Add(at))
		if got := stats[key].Online; got != want {
			t.Fatalf("at +%s with rx %d: online = %v, want %v", at, rx, got, want)
		}
	}

	check(0, 100, true)               // first look: trust the fresh handshake
	check(10*time.Second, 200, true)  // traffic arriving
	check(40*time.Second, 200, true)  // quiet, but within a keepalive or two
	check(80*time.Second, 200, false) // quiet for longer: the device left
	check(90*time.Second, 300, true)  // and it is back
	check(10*time.Minute, 400, false) // grew while unobserved, handshake is stale
	check(10*time.Minute+10*time.Second, 500, true)
}

// An idle client with no keepalive is silent, so only the handshake can tell.
func TestActivityWithoutKeepalive(t *testing.T) {
	key := wg.GeneratePrivateKey().PublicKey()
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	handshake := now.Add(-time.Minute)

	a := activity{peers: map[int64]map[wg.Key]sample{}}
	for _, tt := range []struct {
		at   time.Duration
		want bool
	}{{0, true}, {90 * time.Second, true}, {2*time.Minute + 30*time.Second, false}} {
		stats := map[wg.Key]wg.PeerStats{key: {RxBytes: 100, LatestHandshake: &handshake}}
		a.observe(1, stats, 0, now.Add(tt.at))
		if stats[key].Online != tt.want {
			t.Fatalf("at +%s: online = %v, want %v", tt.at, stats[key].Online, tt.want)
		}
	}
}

func TestActivityReportsChanges(t *testing.T) {
	key := wg.GeneratePrivateKey().PublicKey()
	start := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	handshake := start.Add(-time.Hour)

	a := activity{peers: map[int64]map[wg.Key]sample{}}
	step := func(at time.Duration, rx int64) []PeerChange {
		stats := map[wg.Key]wg.PeerStats{key: {RxBytes: rx, LatestHandshake: &handshake, Endpoint: "203.0.113.7:4000"}}
		return a.observe(1, stats, 25, start.Add(at))
	}

	if got := step(0, 100); len(got) != 0 {
		t.Fatalf("first look should not be a change: %+v", got)
	}
	got := step(10*time.Second, 200)
	if len(got) != 1 || !got[0].Online || got[0].Endpoint != "203.0.113.7:4000" {
		t.Fatalf("want a connect, got %+v", got)
	}
	if got := step(20*time.Second, 300); len(got) != 0 {
		t.Fatalf("still online is not a change: %+v", got)
	}
	got = step(2*time.Minute, 300)
	if len(got) != 1 || got[0].Online || !got[0].OnlineSince.Equal(start.Add(10*time.Second)) {
		t.Fatalf("want a disconnect with when it came online, got %+v", got)
	}
}

func TestActivityIsRecorded(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := f.instance(t, "Home", 51820)
	peer, err := f.store.CreatePeer(ctx, wg.NewPeer(in.ID, "phone"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.Local)
	f.m.now = func() time.Time { return now }

	f.m.reportActivity(in, []PeerChange{
		{Key: peer.PublicKey, Online: true, Endpoint: "[::ffff:203.0.113.7]:4000"},
		{Key: peer.PublicKey, OnlineSince: now.Add(-90 * time.Minute)},
		{Key: wg.GeneratePrivateKey().PublicKey(), Online: true},
	})
	if len(f.events) != 2 {
		t.Fatalf("an unknown key should be skipped: %+v", f.events)
	}
	if e := f.events[0]; e.Kind != "device.connected" || e.PeerName != "phone" || e.InstanceName != "Home" || e.IP != "203.0.113.7" {
		t.Errorf("connected = %+v", e)
	}
	if e := f.events[1]; e.Kind != "device.disconnected" || e.Detail != "online for 1h 30m" {
		t.Errorf("disconnected = %+v", e)
	}
}
