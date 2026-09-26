package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type limitedPeerJSON struct {
	ID         int64      `json:"id"`
	PublicKey  string     `json:"public_key"`
	DataLimit  int64      `json:"data_limit"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Blocked    string     `json:"blocked"`
	MonthUsage struct {
		RxBytes int64 `json:"rx_bytes"`
		TxBytes int64 `json:"tx_bytes"`
	} `json:"month_usage"`
}

func TestPeerLimits(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	base := fmt.Sprintf("/api/instances/%d/peers", in.ID)

	var peer limitedPeerJSON
	p.want(p.do("POST", base, map[string]any{"name": "guest", "expires_at": "2020-01-01T00:00:00Z"}),
		http.StatusCreated, &peer)
	if peer.ExpiresAt == nil || peer.Blocked != "expired" {
		t.Fatalf("created = %+v", peer)
	}

	path := fmt.Sprintf("%s/%d", base, peer.ID)
	peer = limitedPeerJSON{}
	p.want(p.do("PATCH", path, map[string]any{"expires_at": nil, "data_limit": 100}), http.StatusOK, &peer)
	if peer.ExpiresAt != nil || peer.DataLimit != 100 || peer.Blocked != "" {
		t.Fatalf("after clearing expiry = %+v", peer)
	}
	peer = limitedPeerJSON{}
	p.want(p.do("PATCH", path, map[string]any{"name": "guest"}), http.StatusOK, &peer)
	if peer.DataLimit != 100 {
		t.Fatalf("a missing field must leave the limit alone: %+v", peer)
	}

	key, err := wg.ParseKey(peer.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, rx := range []int64{0, 60, 120} {
		stats := map[wg.Key]wg.PeerStats{key: {RxBytes: rx, TxBytes: rx / 2}}
		if err := p.s.store.RecordTraffic(context.Background(), in.ID, stats, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var list []limitedPeerJSON
	p.want(p.do("GET", base, nil), http.StatusOK, &list)
	if len(list) != 1 || list[0].Blocked != "limit" || list[0].MonthUsage.RxBytes != 120 || list[0].MonthUsage.TxBytes != 60 {
		t.Fatalf("list = %+v", list)
	}

	e := p.wantError(p.do("PATCH", path, map[string]any{"data_limit": -1}), http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["data_limit"] == "" {
		t.Fatalf("negative limit: %+v", e)
	}

	var usage struct {
		Daily   []struct{ Start string } `json:"daily"`
		Monthly []struct {
			Start   string `json:"start"`
			RxBytes int64  `json:"rx_bytes"`
		} `json:"monthly"`
	}
	p.want(p.do("GET", path+"/usage", nil), http.StatusOK, &usage)
	if len(usage.Daily) != usageDays || len(usage.Monthly) != usageMonths || usage.Monthly[usageMonths-1].RxBytes != 120 {
		t.Fatalf("usage = %+v", usage)
	}

	var events []eventJSON
	p.want(p.do("GET", "/api/events?category=change", nil), http.StatusOK, &events)
	if len(events) < 2 || events[0].Kind != "device.limits_changed" || events[0].Detail != "100 B a month" {
		t.Fatalf("events = %+v", events)
	}
}

func TestPeerBlockIsRecorded(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	peer := p.createPeer(in.ID, "phone")

	p.s.peerBlocked(deploy.PeerBlock{
		InstanceID: in.ID,
		Peer:       wg.Peer{ID: peer.ID, Name: "phone", DataLimit: 10 << 30},
		Reason:     wg.BlockLimit,
		Used:       wg.Traffic{RxBytes: 8 << 30, TxBytes: 2 << 30},
	})
	p.s.peerBlocked(deploy.PeerBlock{InstanceID: in.ID, Peer: wg.Peer{ID: peer.ID, Name: "phone"}})

	var events []eventJSON
	p.want(p.do("GET", "/api/events?category=change", nil), http.StatusOK, &events)
	if events[0].Kind != "device.unblocked" || events[1].Kind != "device.limit_reached" ||
		events[1].Detail != "used 10 GB of 10 GB this month" {
		t.Fatalf("events = %+v", events)
	}
}

func TestLimitsDetail(t *testing.T) {
	midnight := time.Date(2026, 10, 2, 0, 0, 0, 0, time.Local)
	odd := time.Date(2026, 10, 2, 9, 30, 0, 0, time.Local)
	tests := []struct {
		peer wg.Peer
		want string
	}{
		{wg.Peer{}, "no limits"},
		{wg.Peer{DataLimit: 5 << 30, ExpiresAt: &midnight}, "5 GB a month, until the end of 1 Oct 2026"},
		{wg.Peer{DataLimit: 1536 << 20, ExpiresAt: &odd}, "1.5 GB a month, until 2 Oct 2026 09:30"},
		{wg.Peer{DataLimit: 1 << 30, LimitPeriod: wg.PeriodTotal, SpeedLimit: 2500}, "1 GB in total, 2.5 Mbit/s"},
		{wg.Peer{SpeedLimit: 512}, "512 kbit/s"},
	}
	for _, tt := range tests {
		if got := limitsDetail(&tt.peer); got != tt.want {
			t.Errorf("limitsDetail = %q, want %q", got, tt.want)
		}
	}
}

// The server's container shapes traffic from this file, so it lists only
// peers that may connect.
func TestSpeedLimit(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	base := fmt.Sprintf("/api/instances/%d/peers", in.ID)
	file := filepath.Join(p.s.deploy.ConfigDir(in.ID), "speed-limits.conf")
	limits := func() string {
		t.Helper()
		b, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	var peer struct {
		ID         int64 `json:"id"`
		SpeedLimit int64 `json:"speed_limit"`
	}
	p.want(p.do("POST", base, map[string]any{"name": "phone", "speed_limit": 10000}), http.StatusCreated, &peer)
	p.createPeer(in.ID, "laptop")
	if peer.SpeedLimit != 10000 || limits() != "10.8.0.2 10000\n" {
		t.Fatalf("peer = %+v, file = %q", peer, limits())
	}

	path := fmt.Sprintf("%s/%d", base, peer.ID)
	p.want(p.do("PATCH", path, map[string]any{"enabled": false}), http.StatusOK, nil)
	if limits() != "" {
		t.Fatalf("a disabled peer is still shaped: %q", limits())
	}
	p.want(p.do("PATCH", path, map[string]any{"enabled": true, "speed_limit": 0}), http.StatusOK, &peer)
	if peer.SpeedLimit != 0 || limits() != "" {
		t.Fatalf("peer = %+v, file = %q", peer, limits())
	}

	e := p.wantError(p.do("PATCH", path, map[string]any{"speed_limit": -1}), http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["speed_limit"] == "" {
		t.Fatalf("negative speed: %+v", e)
	}

	var events []eventJSON
	p.want(p.do("GET", "/api/events?category=change", nil), http.StatusOK, &events)
	created := slices.IndexFunc(events, func(e eventJSON) bool { return e.Kind == "device.created" && e.Detail == "10 Mbit/s" })
	if events[0].Kind != "device.limits_changed" || events[0].Detail != "no limits" || created < 0 {
		t.Fatalf("events = %+v", events)
	}
}
