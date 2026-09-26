package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestEvents(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	old := time.Now().Add(-100 * 24 * time.Hour)
	for _, e := range []Event{
		{Kind: "auth.login", IP: "203.0.113.7", CreatedAt: old},
		{Kind: "server.created", InstanceID: 1, InstanceName: "Home"},
		{Kind: "device.connected", InstanceID: 1, PeerID: 2, PeerName: "phone", Country: "TR"},
	} {
		if _, err := st.AddEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	all, err := st.Events(ctx, EventFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].Kind != "device.connected" || all[0].Country != "TR" {
		t.Fatalf("want newest first, got %+v", all)
	}

	for category, want := range map[string]string{
		CategoryConnection: "device.connected",
		CategoryAuth:       "auth.login",
		CategoryChange:     "server.created",
	} {
		got, err := st.Events(ctx, EventFilter{Limit: 10, Category: category})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Kind != want {
			t.Errorf("%s: got %+v", category, got)
		}
	}

	page, _ := st.Events(ctx, EventFilter{Limit: 10, Before: all[0].ID, InstanceID: 1})
	if len(page) != 1 || page[0].Kind != "server.created" {
		t.Fatalf("paging by id within an instance: %+v", page)
	}

	if n, err := st.DeleteEventsBefore(ctx, time.Now().Add(-90*24*time.Hour)); err != nil || n != 1 {
		t.Fatalf("purge removed %d, err %v", n, err)
	}
}
