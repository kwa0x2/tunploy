package peer

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type fakeTunnels struct {
	applied []int64
	err     error
}

func (f *fakeTunnels) Apply(_ context.Context, instanceID int64) error {
	f.applied = append(f.applied, instanceID)
	return f.err
}

type fixture struct {
	*Service
	store   *store.Store
	tunnels *fakeTunnels
	events  []store.Event
	servers int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{store: st, tunnels: &fakeTunnels{}}
	f.Service = New(st, f.tunnels, event.Func(func(_ context.Context, e store.Event) {
		f.events = append(f.events, e)
	}))
	return f
}

func (f *fixture) instance(t *testing.T, name string) *wg.Instance {
	t.Helper()
	n := f.servers
	f.servers++
	in := wg.NewInstance(name, "vpn.example.com")
	in.ListenPort += n
	in.Address = netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(8 + n), 0, 1}), 24)
	created, err := f.store.CreateInstance(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

// The device is saved even when the tunnel cannot take it yet; the next
// start picks it up, and the admin is told why it is not live.
func TestCreateKeepsTheDeviceWhenTheTunnelFails(t *testing.T) {
	f := newFixture(t)
	in := f.instance(t, "Frankfurt")
	f.tunnels.err = errors.New("container is gone")

	created, err := f.Create(context.Background(), in, wg.NewPeer(in.ID, "phone"))
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Kind != apperr.Upstream || ae.Code != "apply_failed" {
		t.Fatalf("err = %v, want apply_failed", err)
	}
	if created == nil {
		t.Fatal("want the saved device along with the error")
	}
	if peers, _ := f.store.Peers(context.Background(), in.ID); len(peers) != 1 {
		t.Fatalf("stored peers = %d, want 1", len(peers))
	}
	if len(f.events) != 1 || f.events[0].Kind != "device.created" {
		t.Fatalf("events = %+v", f.events)
	}
}

func TestGroupUpdateChecksEveryDeviceFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b := f.instance(t, "Frankfurt"), f.instance(t, "Amsterdam")
	phone, err := f.Create(ctx, a, wg.NewPeer(a.ID, "phone"))
	if err != nil {
		t.Fatal(err)
	}
	laptop, err := f.Create(ctx, b, wg.NewPeer(b.ID, "laptop"))
	if err != nil {
		t.Fatal(err)
	}
	f.tunnels.applied, f.events = nil, nil

	before := []wg.Peer{*phone, *laptop}
	after := []wg.Peer{*phone, *laptop}
	after[0].DataLimit = 1 << 30
	after[1].DataLimit = -1

	if _, err := f.UpdateGroup(ctx, before, after); err == nil {
		t.Fatal("want a validation error")
	}
	saved, err := f.store.PeerByID(ctx, phone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.DataLimit != 0 || len(f.events) != 0 || len(f.tunnels.applied) != 0 {
		t.Fatalf("a bad value changed something: limit %d, events %+v, applied %v", saved.DataLimit, f.events, f.tunnels.applied)
	}

	after[1].DataLimit = 1 << 30
	if _, err := f.UpdateGroup(ctx, before, after); err != nil {
		t.Fatal(err)
	}
	if len(f.tunnels.applied) != 2 {
		t.Fatalf("applied %v, want each server once", f.tunnels.applied)
	}
}
