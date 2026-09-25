package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func TestSnapshotAndRestore(t *testing.T) {
	ctx := context.Background()

	old := newTestStore(t)
	if _, err := old.CreateUser(ctx, "Old", "old@example.com", "hash-old"); err != nil {
		t.Fatal(err)
	}
	home := createInstance(t, old, "Home", 51820)
	phone := createPeer(t, old, home.ID, "phone")
	createPeer(t, old, home.ID, "laptop")
	for _, rx := range []int64{0, 100} {
		stats := map[wg.Key]wg.PeerStats{phone.PublicKey: {RxBytes: rx}}
		if err := old.RecordTraffic(ctx, home.ID, stats, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.SaveSettings(ctx, map[string]string{"public_host": "old.example.com", "backup_bucket": "from-backup"}); err != nil {
		t.Fatal(err)
	}
	user, _ := old.UserByEmail(ctx, "old@example.com")
	if err := old.CreateSession(ctx, "token-old", user.ID, "test", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	snap := filepath.Join(t.TempDir(), "snap.db")
	if err := old.Snapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}

	live := newTestStore(t)
	if _, err := live.CreateUser(ctx, "New", "new@example.com", "hash-new"); err != nil {
		t.Fatal(err)
	}
	createInstance(t, live, "Office", 51821)
	createInstance(t, live, "Lab", 51822)
	if err := live.SaveSettings(ctx, map[string]string{"public_host": "new.example.com", "backup_bucket": "live"}); err != nil {
		t.Fatal(err)
	}
	liveUser, _ := live.UserByEmail(ctx, "new@example.com")
	if err := live.CreateSession(ctx, "token-new", liveUser.ID, "test", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	keep := func(k string) bool { return strings.HasPrefix(k, "backup_") }
	if err := live.Restore(ctx, snap, keep); err != nil {
		t.Fatal(err)
	}

	instances, _ := live.Instances(ctx)
	if len(instances) != 1 || instances[0].Name != "Home" || instances[0].PrivateKey != home.PrivateKey {
		t.Fatalf("instances after restore = %+v", instances)
	}
	peers, _ := live.Peers(ctx, instances[0].ID)
	if len(peers) != 2 {
		t.Fatalf("peers after restore = %d, want 2", len(peers))
	}
	if _, err := live.UserByEmail(ctx, "new@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("live user survived the restore: %v", err)
	}
	if _, err := live.UserByEmail(ctx, "old@example.com"); err != nil {
		t.Fatalf("backup user missing: %v", err)
	}
	usage, _ := live.MonthUsage(ctx, home.ID, time.Now())
	if usage[phone.ID].RxBytes != 100 {
		t.Fatalf("usage after restore = %+v", usage[phone.ID])
	}

	settings, _ := live.Settings(ctx)
	if settings["public_host"] != "old.example.com" || settings["backup_bucket"] != "live" {
		t.Fatalf("settings after restore = %v", settings)
	}
	for _, table := range []string{"sessions", "wg_peer_counters"} {
		var n int
		live.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&n)
		if n != 0 {
			t.Fatalf("%s has %d rows after restore, want 0", table, n)
		}
	}

	// New rows must not collide with restored IDs.
	after := createInstance(t, live, "After", 51830)
	if after.ID <= instances[0].ID {
		t.Fatalf("new instance ID %d reuses a restored one", after.ID)
	}
}

func TestRestoreRejectsNewerAndForeignDatabases(t *testing.T) {
	ctx := context.Background()
	live := newTestStore(t)

	newer := newTestStore(t)
	if _, err := newer.DB().Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES ('9999_future.sql', 0)`); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(t.TempDir(), "newer.db")
	if err := newer.Snapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if err := live.Restore(ctx, snap, nil); !errors.Is(err, ErrNewerBackup) {
		t.Fatalf("restore newer = %v, want ErrNewerBackup", err)
	}

	foreign := filepath.Join(t.TempDir(), "foreign.db")
	db, _ := openRaw(foreign)
	db.Exec(`CREATE TABLE notes (body TEXT)`)
	db.Close()
	if err := live.Restore(ctx, foreign, nil); !errors.Is(err, ErrInvalidBackup) {
		t.Fatalf("restore foreign = %v, want ErrInvalidBackup", err)
	}
}
