package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func TestWatchEnforcesLimits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := f.instance(t, "Home", 51820)

	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.Local)
	f.m.now = func() time.Time { return now }
	var blocks []PeerBlock
	f.m.OnPeerBlock(func(b PeerBlock) { blocks = append(blocks, b) })

	capped := wg.NewPeer(in.ID, "capped")
	capped.DataLimit = 1000
	guest := wg.NewPeer(in.ID, "guest")
	until := now.Add(time.Hour)
	guest.ExpiresAt = &until
	for _, p := range []*wg.Peer{&capped, &guest} {
		created, err := f.store.CreatePeer(ctx, *p)
		if err != nil {
			t.Fatal(err)
		}
		*p = *created
	}
	if err := f.m.Deploy(ctx, in.ID, true); err != nil {
		t.Fatal(err)
	}

	var rx int64
	f.docker.ExecOutput = func(name string, cmd []string) ([]byte, error) {
		if cmd[0] != "wg" {
			return nil, nil
		}
		return []byte(fmt.Sprintf("priv\tpub\t51820\toff\n%s\tpsk\t(none)\t10.8.0.2/32\t0\t%d\t0\toff\n",
			capped.PublicKey, rx)), nil
	}
	watch := func() {
		t.Helper()
		if err := f.m.watchInstance(ctx, f.m.local, in); err != nil {
			t.Fatal(err)
		}
	}
	config := func() string {
		body, err := os.ReadFile(filepath.Join(f.m.ConfigDir(in.ID), "wg0.conf"))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	watch()
	rx = 999
	watch()
	if len(blocks) != 0 || !strings.Contains(config(), capped.PublicKey.String()) {
		t.Fatalf("blocked under the limit: %+v", blocks)
	}

	rx = 1000
	watch()
	if len(blocks) != 1 || blocks[0].Peer.ID != capped.ID || blocks[0].Reason != wg.BlockLimit || blocks[0].Used.RxBytes != 1000 {
		t.Fatalf("blocks = %+v", blocks)
	}
	if strings.Contains(config(), capped.PublicKey.String()) {
		t.Fatal("a peer over its limit is still in the config")
	}

	syncs := countSyncs(f)
	watch()
	if len(blocks) != 1 || countSyncs(f) != syncs {
		t.Fatal("an unchanged block should not be applied again")
	}

	now = until
	watch()
	if len(blocks) != 2 || blocks[1].Peer.ID != guest.ID || blocks[1].Reason != wg.BlockExpired {
		t.Fatalf("blocks = %+v", blocks)
	}

	now = time.Date(2026, 10, 1, 0, 0, 5, 0, time.Local)
	watch()
	if len(blocks) != 3 || blocks[2].Peer.ID != capped.ID || blocks[2].Reason != "" {
		t.Fatalf("a new month should let the peer back in: %+v", blocks)
	}
	if !strings.Contains(config(), capped.PublicKey.String()) || strings.Contains(config(), guest.PublicKey.String()) {
		t.Fatalf("config after the month turned:\n%s", config())
	}
}

func countSyncs(f *fixture) int {
	n := 0
	for _, cmd := range f.docker.Execs {
		if len(cmd) == 2 && cmd[0] == "tunploy-wg" && cmd[1] == "sync" {
			n++
		}
	}
	return n
}
