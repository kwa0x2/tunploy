package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"sync"
	"testing"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func createInstance(t *testing.T, st *Store, name string, port int) *wg.Instance {
	t.Helper()
	in := wg.NewInstance(name, "vpn.example.com")
	in.ListenPort = port
	created, err := st.CreateInstance(context.Background(), in)
	if err != nil {
		t.Fatalf("create instance %s: %v", name, err)
	}
	return created
}

func createPeer(t *testing.T, st *Store, instanceID int64, name string) *wg.Peer {
	t.Helper()
	p, err := st.CreatePeer(context.Background(), wg.NewPeer(instanceID, name))
	if err != nil {
		t.Fatalf("create peer %s: %v", name, err)
	}
	return p
}

func TestInstanceRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	in := wg.NewInstance("Home", "vpn.example.com")
	in.MTU = 1420
	created, err := st.CreateInstance(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	got, err := st.InstanceByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, created) {
		t.Fatalf("round trip changed the instance\n got: %+v\nwant: %+v", got, created)
	}

	all, err := st.Instances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != created.ID {
		t.Fatalf("Instances = %+v", all)
	}
}

func TestEmptyListsEncodeAsEmptySlices(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	in := wg.NewInstance("Home", "vpn.example.com")
	in.DNS = nil
	created, err := st.CreateInstance(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.InstanceByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DNS == nil || len(got.DNS) != 0 {
		t.Fatalf("DNS = %#v, want an empty slice", got.DNS)
	}

	instances, err := st.Instances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	peers, err := st.Peers(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if instances == nil || peers == nil {
		t.Fatal("list queries must never return nil")
	}
}

func TestInstanceDuplicatesNameTheColumn(t *testing.T) {
	st := newTestStore(t)
	createInstance(t, st, "Home", 51820)

	tests := []struct {
		name   string
		port   int
		column string
	}{
		{"home", 51821, "name"},
		{"Office", 51820, "listen_port"},
	}
	for _, tt := range tests {
		in := wg.NewInstance(tt.name, "vpn.example.com")
		in.ListenPort = tt.port
		_, err := st.CreateInstance(context.Background(), in)

		var dup *DuplicateError
		if !errors.As(err, &dup) || dup.Column != tt.column {
			t.Errorf("%s:%d: want duplicate %s, got %v", tt.name, tt.port, tt.column, err)
		}
		if !errors.Is(err, ErrDuplicate) {
			t.Errorf("%s:%d: DuplicateError must match ErrDuplicate", tt.name, tt.port)
		}
	}
}

func TestUpdateInstanceKeepsAddressAndKeys(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	orig := createInstance(t, st, "Home", 51820)

	changed := *orig
	changed.Name = "Office"
	changed.ListenPort = 51900
	changed.Address = netip.MustParsePrefix("10.99.0.1/24")
	changed.PrivateKey = wg.GeneratePrivateKey()
	changed.DNS = []netip.Addr{netip.MustParseAddr("9.9.9.9")}

	got, err := st.UpdateInstance(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Office" || got.ListenPort != 51900 || got.DNS[0].String() != "9.9.9.9" {
		t.Fatalf("editable fields not saved: %+v", got)
	}
	if got.Address != orig.Address || got.PrivateKey != orig.PrivateKey {
		t.Fatal("address and keys must not change on update")
	}

	changed.ID = 404
	if _, err := st.UpdateInstance(ctx, changed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("updating a missing instance: want ErrNotFound, got %v", err)
	}
}

func TestPeersGetSequentialAddresses(t *testing.T) {
	st := newTestStore(t)
	in := createInstance(t, st, "Home", 51820)

	for i, want := range []string{"10.8.0.2", "10.8.0.3", "10.8.0.4"} {
		p := createPeer(t, st, in.ID, fmt.Sprintf("peer%d", i))
		if p.Address.String() != want {
			t.Fatalf("peer %d got %s, want %s", i, p.Address, want)
		}
	}
}

func TestDeletedPeerAddressIsReused(t *testing.T) {
	st := newTestStore(t)
	in := createInstance(t, st, "Home", 51820)

	createPeer(t, st, in.ID, "a")
	b := createPeer(t, st, in.ID, "b")
	createPeer(t, st, in.ID, "c")

	if err := st.DeletePeer(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}
	if d := createPeer(t, st, in.ID, "d"); d.Address != b.Address {
		t.Fatalf("want the freed %s back, got %s", b.Address, d.Address)
	}
}

func TestInstancesAllocateIndependently(t *testing.T) {
	st := newTestStore(t)
	home := createInstance(t, st, "Home", 51820)
	office := createInstance(t, st, "Office", 51821)

	a := createPeer(t, st, home.ID, "laptop")
	b := createPeer(t, st, office.ID, "laptop")
	if a.Address != b.Address {
		t.Fatalf("separate instances should both start at .2, got %s and %s", a.Address, b.Address)
	}
}

func TestConcurrentPeersGetDistinctAddresses(t *testing.T) {
	st := newTestStore(t)
	in := createInstance(t, st, "Home", 51820)

	const n = 30
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			if _, err := st.CreatePeer(context.Background(), wg.NewPeer(in.ID, fmt.Sprintf("p%d", i))); err != nil {
				t.Errorf("peer %d: %v", i, err)
			}
		}()
	}
	close(start)
	group.Wait()

	peers, err := st.Peers(context.Background(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[netip.Addr]bool{}
	for _, p := range peers {
		if seen[p.Address] {
			t.Fatalf("address %s handed out twice", p.Address)
		}
		seen[p.Address] = true
	}
	if len(peers) != n {
		t.Fatalf("created %d peers, want %d", len(peers), n)
	}
}

func TestCreatePeerReportsFullSubnet(t *testing.T) {
	st := newTestStore(t)
	in := wg.NewInstance("Tiny", "vpn.example.com")
	in.Address = netip.MustParsePrefix("10.8.0.1/30")
	created, err := st.CreateInstance(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	createPeer(t, st, created.ID, "only")
	_, err = st.CreatePeer(context.Background(), wg.NewPeer(created.ID, "one too many"))
	if !errors.Is(err, wg.ErrSubnetFull) {
		t.Fatalf("want ErrSubnetFull, got %v", err)
	}
}

func TestPeerNameIsUniquePerInstance(t *testing.T) {
	st := newTestStore(t)
	in := createInstance(t, st, "Home", 51820)
	createPeer(t, st, in.ID, "phone")

	_, err := st.CreatePeer(context.Background(), wg.NewPeer(in.ID, "Phone"))
	var dup *DuplicateError
	if !errors.As(err, &dup) || dup.Column != "name" {
		t.Fatalf("want duplicate name, got %v", err)
	}
}

func TestCreatePeerForMissingInstance(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreatePeer(context.Background(), wg.NewPeer(404, "ghost")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPeerRoundTripAndUpdate(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	in := createInstance(t, st, "Home", 51820)
	created := createPeer(t, st, in.ID, "phone")

	got, err := st.PeerByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, created) {
		t.Fatalf("round trip changed the peer\n got: %+v\nwant: %+v", got, created)
	}

	got.Name = "work phone"
	got.Enabled = false
	updated, err := st.UpdatePeer(ctx, *got)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "work phone" || updated.Enabled {
		t.Fatalf("update not saved: %+v", updated)
	}
}

func TestDeleteInstanceCascadesToPeers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	in := createInstance(t, st, "Home", 51820)
	p := createPeer(t, st, in.ID, "phone")

	if err := st.DeleteInstance(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PeerByID(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("peer should be gone with its instance, got %v", err)
	}
	if err := st.DeleteInstance(ctx, in.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting twice: want ErrNotFound, got %v", err)
	}
}
