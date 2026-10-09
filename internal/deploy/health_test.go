package deploy

import (
	"context"
	"testing"

	"github.com/kwa0x2/tunploy/internal/store"
)

func TestWatchReportsServerHealth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := f.instance(t, "Home", 51820)
	if err := f.m.Deploy(ctx, in.ID, true); err != nil {
		t.Fatal(err)
	}

	set := func(state string, code int) {
		t.Helper()
		ct, _ := f.docker.Container(f.m.ContainerName(in.ID))
		ct.State, ct.ExitCode = state, code
		f.docker.Add(ct)
		f.m.watchInstance(ctx, f.m.local, in)
	}

	set("running", 0) // first look is never a change
	set("exited", 0)  // stopped from the panel
	set("running", 0)
	health := func() []store.Event { return f.eventsOf("server.down", "server.recovered") }
	if len(health()) != 0 {
		t.Fatalf("clean stops and starts are not outages: %+v", health())
	}
	set("restarting", 0)
	set("restarting", 0)
	set("running", 0)
	set("exited", 137)
	want := []struct{ kind, detail string }{
		{"server.down", "the container keeps exiting; check its logs"},
		{"server.recovered", ""},
		{"server.down", "exited with code 137"},
	}
	got := health()
	if len(got) != len(want) {
		t.Fatalf("events = %+v", got)
	}
	for i, w := range want {
		if e := got[i]; e.Kind != w.kind || e.Detail != w.detail || e.InstanceName != "Home" {
			t.Errorf("event %d = %+v, want %s %q", i, e, w.kind, w.detail)
		}
	}
}
