package deploy

import (
	"context"
	"testing"
)

func TestWatchReportsServerHealth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := f.instance(t, "Home", 51820)
	if err := f.m.Deploy(ctx, in.ID, true); err != nil {
		t.Fatal(err)
	}
	var got []ServerHealth
	f.m.OnServerHealth(func(h ServerHealth) { got = append(got, h) })

	set := func(state string, code int) {
		t.Helper()
		ct, _ := f.docker.Container(ContainerName(in.ID))
		ct.State, ct.ExitCode = state, code
		f.docker.Add(ct)
		f.m.watchInstance(ctx, in)
	}

	set("running", 0) // first look is never a change
	set("exited", 0)  // stopped from the panel
	set("running", 0)
	if len(got) != 0 {
		t.Fatalf("clean stops and starts are not outages: %+v", got)
	}
	set("restarting", 0)
	set("restarting", 0)
	set("running", 0)
	set("exited", 137)
	want := []ServerHealth{
		{InstanceID: in.ID, Down: true, Reason: "the container keeps exiting; check its logs"},
		{InstanceID: in.ID, Down: false},
		{InstanceID: in.ID, Down: true, Reason: "exited with code 137"},
	}
	if len(got) != len(want) {
		t.Fatalf("health = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("health[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
