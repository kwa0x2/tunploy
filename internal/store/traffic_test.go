package store

import (
	"context"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func TestRecordTraffic(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	in := createInstance(t, st, "Home", 51820)
	p := createPeer(t, st, in.ID, "phone")
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.Local)

	read := func(rx, tx int64) {
		t.Helper()
		stats := map[wg.Key]wg.PeerStats{}
		if rx >= 0 {
			stats[p.PublicKey] = wg.PeerStats{RxBytes: rx, TxBytes: tx}
		}
		if err := st.RecordTraffic(ctx, in.ID, stats, now); err != nil {
			t.Fatal(err)
		}
	}
	month := func() wg.Traffic {
		t.Helper()
		usage, err := st.MonthUsage(ctx, in.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		return usage[p.ID]
	}

	read(1000, 5000) // first reading is only a baseline
	if got := month(); got.Total() != 0 {
		t.Fatalf("baseline counted as usage: %+v", got)
	}
	read(1500, 7000)
	read(300, 100) // container restarted: counters began again from zero
	read(-1, 0)    // disabled: gone from the interface
	read(200, 400) // enabled again
	want := wg.Traffic{RxBytes: 500 + 300 + 200, TxBytes: 2000 + 100 + 400}
	if got := month(); got != want {
		t.Fatalf("month usage = %+v, want %+v", got, want)
	}
}

func TestRecordTrafficKeepsLastHandshake(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	in := createInstance(t, st, "Home", 51820)
	p := createPeer(t, st, in.ID, "phone")

	hs := time.Date(2026, 9, 24, 11, 58, 0, 0, time.UTC)
	stats := map[wg.Key]wg.PeerStats{p.PublicKey: {LatestHandshake: &hs}}
	if err := st.RecordTraffic(ctx, in.ID, stats, hs); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordTraffic(ctx, in.ID, map[wg.Key]wg.PeerStats{}, hs.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := st.PeerByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastHandshake == nil || !got.LastHandshake.Equal(hs) {
		t.Fatalf("last handshake = %v, want %v", got.LastHandshake, hs)
	}
}

func TestPeerUsageSeries(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	in := createInstance(t, st, "Home", 51820)
	p := createPeer(t, st, in.ID, "phone")

	add := func(at time.Time, rx int64) {
		t.Helper()
		// Reset the baseline so each call adds exactly rx.
		for _, v := range []int64{0, rx} {
			stats := map[wg.Key]wg.PeerStats{p.PublicKey: {RxBytes: v}}
			if err := st.RecordTraffic(ctx, in.ID, stats, at); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.RecordTraffic(ctx, in.ID, nil, at); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.Local)
	add(now, 10)
	add(now.Add(-time.Hour), 5)
	add(time.Date(2026, 9, 1, 0, 30, 0, 0, time.Local), 7)
	add(time.Date(2026, 8, 31, 23, 30, 0, 0, time.Local), 100)
	add(time.Date(2025, 9, 30, 12, 0, 0, 0, time.Local), 1000) // older than 12 months

	daily, monthly, err := st.PeerUsage(ctx, p.ID, now, 30, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 30 || daily[0].Start != "2026-08-26" || daily[29].Start != "2026-09-24" {
		t.Fatalf("daily range = %s..%s (%d)", daily[0].Start, daily[len(daily)-1].Start, len(daily))
	}
	if daily[29].RxBytes != 15 || daily[6].RxBytes != 7 || daily[5].RxBytes != 100 {
		t.Fatalf("daily = %+v", daily)
	}
	if len(monthly) != 12 || monthly[0].Start != "2025-10-01" || monthly[11].Start != "2026-09-01" {
		t.Fatalf("monthly range = %s..%s", monthly[0].Start, monthly[len(monthly)-1].Start)
	}
	if monthly[11].RxBytes != 22 || monthly[10].RxBytes != 100 {
		t.Fatalf("monthly = %+v", monthly)
	}

	usage, err := st.MonthUsage(ctx, in.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if usage[p.ID].RxBytes != 22 {
		t.Fatalf("month usage = %+v", usage[p.ID])
	}

	if err := st.DeletePeer(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM wg_peer_usage`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("usage left after delete: %d, %v", n, err)
	}
}

func TestPeerLimitsRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	in := createInstance(t, st, "Home", 51820)

	expires := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	p := wg.NewPeer(in.ID, "guest")
	p.DataLimit = 5 << 30
	p.ExpiresAt = &expires
	created, err := st.CreatePeer(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.PeerByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DataLimit != 5<<30 || got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("limits = %d %v", got.DataLimit, got.ExpiresAt)
	}

	got.DataLimit, got.ExpiresAt = 0, nil
	updated, err := st.UpdatePeer(ctx, *got)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DataLimit != 0 || updated.ExpiresAt != nil {
		t.Fatalf("limits not cleared: %d %v", updated.DataLimit, updated.ExpiresAt)
	}
}
