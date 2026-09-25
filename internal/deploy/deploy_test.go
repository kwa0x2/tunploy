package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/docker/dockertest"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type fixture struct {
	store  *store.Store
	docker *dockertest.Fake
	m      *Manager
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	fk := dockertest.New()
	m, err := New(st, fk, t.TempDir(), DefaultContainerPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: st, docker: fk, m: m}
}

func (f *fixture) instance(t *testing.T, name string, port int) *wg.Instance {
	t.Helper()
	in := wg.NewInstance(name, "vpn.example.com")
	in.ListenPort = port
	created, err := f.store.CreateInstance(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func (f *fixture) state(t *testing.T, id int64) string {
	t.Helper()
	ct, ok := f.docker.Container(f.m.ContainerName(id))
	if !ok {
		return "missing"
	}
	return ct.State
}

func TestDeployCreatesContainer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := f.instance(t, "Home", 51820)

	if err := f.m.Deploy(ctx, in.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, in.ID); got != "running" {
		t.Fatalf("state = %s, want running", got)
	}

	spec, _ := f.docker.Spec(f.m.ContainerName(in.ID))
	if spec.Image != f.m.Image() || !strings.HasPrefix(spec.Image, "tunploy/wireguard:") {
		t.Errorf("image = %q", spec.Image)
	}
	if len(spec.Ports) != 1 || spec.Ports[0].Host != 51820 || spec.Ports[0].Container != 51820 || spec.Ports[0].Protocol != "udp" {
		t.Errorf("ports = %+v", spec.Ports)
	}
	if !slices.Contains(spec.CapAdd, "NET_ADMIN") {
		t.Errorf("cap_add = %v", spec.CapAdd)
	}
	if spec.Sysctls["net.ipv4.ip_forward"] != "1" {
		t.Errorf("sysctls = %v", spec.Sysctls)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Source != f.m.ConfigDir(in.ID) ||
		spec.Mounts[0].Target != "/etc/wireguard" || !spec.Mounts[0].ReadOnly {
		t.Errorf("mounts = %+v", spec.Mounts)
	}
	if spec.Labels[LabelInstance] != "1" {
		t.Errorf("labels = %v", spec.Labels)
	}

	conf := filepath.Join(f.m.ConfigDir(in.ID), "wg0.conf")
	info, err := os.Stat(conf)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v, want 0600", info.Mode().Perm())
	}
	body, _ := os.ReadFile(conf)
	if !strings.Contains(string(body), "PrivateKey = "+in.PrivateKey.String()) {
		t.Errorf("config does not hold the instance key:\n%s", body)
	}
}

func TestImageIsBuiltOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i, port := range []int{51820, 51821} {
		in := f.instance(t, "wg"+string(rune('a'+i)), port)
		if err := f.m.Deploy(ctx, in.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if f.docker.Builds != 1 {
		t.Fatalf("built %d times, want 1", f.docker.Builds)
	}
}

func TestRedeployKeepsStoppedInstancesStopped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := f.instance(t, "Home", 51820)

	if err := f.m.Deploy(ctx, in.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Stop(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Redeploy(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, in.ID); got != "created" {
		t.Fatalf("state = %s, want a fresh but unstarted container", got)
	}
}

func TestApplySyncsOnlyRunningInstances(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := f.instance(t, "Home", 51820)
	if err := f.m.Deploy(ctx, in.ID, true); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.CreatePeer(ctx, wg.NewPeer(in.ID, "phone")); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Apply(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.docker.Execs) != 1 || !slices.Equal(f.docker.Execs[0], []string{"tunploy-wg", "sync"}) {
		t.Fatalf("execs = %v", f.docker.Execs)
	}
	body, _ := os.ReadFile(filepath.Join(f.m.ConfigDir(in.ID), "wg0.conf"))
	if !strings.Contains(string(body), "# phone") {
		t.Fatalf("new peer not written:\n%s", body)
	}

	if err := f.m.Stop(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Apply(ctx, in.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.docker.Execs) != 1 {
		t.Fatalf("a stopped instance must not be exec'd into, execs = %v", f.docker.Execs)
	}
}

func TestStartDeploysMissingContainer(t *testing.T) {
	f := newFixture(t)
	in := f.instance(t, "Home", 51820)

	if err := f.m.Start(context.Background(), in.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, in.ID); got != "running" {
		t.Fatalf("state = %s, want running", got)
	}
}

func TestReconcile(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	missing := f.instance(t, "missing", 51820)
	outdated := f.instance(t, "outdated", 51821)
	healthy := f.instance(t, "healthy", 51822)

	for _, in := range []*wg.Instance{outdated, healthy} {
		if err := f.m.Deploy(ctx, in.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.m.Stop(ctx, outdated.ID); err != nil {
		t.Fatal(err)
	}
	// Pretend the outdated container predates the current image.
	f.docker.RemoveContainer(ctx, f.m.ContainerName(outdated.ID))
	f.docker.Add(docker.Container{
		ID: "old", Name: f.m.ContainerName(outdated.ID), Image: "tunploy/wireguard:old", State: "exited",
		Labels: map[string]string{docker.LabelManaged: "true", LabelInstance: "2"},
	})
	f.docker.Add(docker.Container{
		ID: "orphan", Name: f.m.ContainerName(99), Image: f.m.Image(), State: "running",
		Labels: map[string]string{docker.LabelManaged: "true", LabelInstance: "99"},
	})
	f.docker.Add(docker.Container{
		ID: "other", Name: "tunploy-something-else", State: "running",
		Labels: map[string]string{docker.LabelManaged: "true"},
	})
	addOtherPanel(f)

	if err := f.m.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	if got := f.state(t, missing.ID); got != "running" {
		t.Errorf("missing instance: state = %s, want running", got)
	}
	ct, _ := f.docker.Container(f.m.ContainerName(outdated.ID))
	if ct.Image != f.m.Image() || ct.State != "created" {
		t.Errorf("outdated instance: %+v, want the new image and still stopped", ct)
	}
	if got := f.state(t, healthy.ID); got != "running" {
		t.Errorf("healthy instance: state = %s", got)
	}
	if _, ok := f.docker.Container(f.m.ContainerName(99)); ok {
		t.Error("orphaned container was not removed")
	}
	if _, ok := f.docker.Container("tunploy-something-else"); !ok {
		t.Error("a managed container without an instance label must be left alone")
	}
	wantOtherPanel(t, f)
}

// Another panel on the same daemon, whose instance IDs overlap with ours.
func addOtherPanel(f *fixture) {
	for _, id := range []string{"1", "99"} {
		f.docker.Add(docker.Container{
			ID: "dev-" + id, Name: "tunploy-dev-" + id, State: "running",
			Labels: map[string]string{docker.LabelManaged: "true", LabelInstance: id},
		})
	}
}

func wantOtherPanel(t *testing.T, f *fixture) {
	t.Helper()
	for _, name := range []string{"tunploy-dev-1", "tunploy-dev-99"} {
		if ct, ok := f.docker.Container(name); !ok || ct.State != "running" {
			t.Errorf("%s belongs to another panel and must be left alone, got %+v", name, ct)
		}
	}
}

func TestStatusesWhenDockerIsDown(t *testing.T) {
	f := newFixture(t)
	in := f.instance(t, "Home", 51820)
	f.docker.Unavailable = true

	got := f.m.Statuses(context.Background(), []wg.Instance{*in})[in.ID]
	if got.State != StateUnknown || got.Error == "" {
		t.Fatalf("status = %+v, want unknown with a reason", got)
	}
}

func TestStatusReportsCrashLoop(t *testing.T) {
	f := newFixture(t)
	in := f.instance(t, "Home", 51820)
	f.docker.Add(docker.Container{
		ID: "x", Name: f.m.ContainerName(in.ID), State: "restarting",
		Labels: map[string]string{docker.LabelManaged: "true", LabelInstance: "1"},
	})

	if got := f.m.Status(context.Background(), in); got.State != StateRestarting || got.Error == "" {
		t.Fatalf("status = %+v", got)
	}
}

func TestProvisionReportsSteps(t *testing.T) {
	f := newFixture(t)
	in := f.instance(t, "Home", 51820)

	var steps []Step
	if err := f.m.Provision(context.Background(), in.ID, func(s Step) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	want := []Step{StepImage, StepContainer, StepInterface, StepFirewall, StepNAT}
	if !slices.Equal(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
}

func TestProvisionFailsWhenBootFails(t *testing.T) {
	f := newFixture(t)
	in := f.instance(t, "Home", 51820)
	f.docker.BootLog = "2026-09-24T10:00:00.123456789Z [#] ip link add wg0 type wireguard\n" +
		"RTNETLINK answers: Operation not supported\n"
	f.docker.BootExitCode = 1

	err := f.m.Provision(context.Background(), in.ID, nil)
	var boot *BootError
	if !errors.As(err, &boot) {
		t.Fatalf("want a BootError, got %v", err)
	}
	if !strings.Contains(boot.Reason, "code 1") {
		t.Errorf("reason = %q", boot.Reason)
	}
	want := []string{"[#] ip link add wg0 type wireguard", "RTNETLINK answers: Operation not supported"}
	if !slices.Equal(boot.Log, want) {
		t.Errorf("log = %q, want %q", boot.Log, want)
	}
}

func TestRebuildReplacesEveryContainer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	home := f.instance(t, "Home", 51820)
	lab := f.instance(t, "Lab", 51821)
	for _, in := range []*wg.Instance{home, lab} {
		if err := f.m.Deploy(ctx, in.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := f.docker.Container(f.m.ContainerName(home.ID))
	addOtherPanel(f)

	// As after a restore: Lab is gone from the database, Home is still there.
	if err := f.store.DeleteInstance(ctx, lab.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}

	after, ok := f.docker.Container(f.m.ContainerName(home.ID))
	if !ok || after.ID == before.ID || after.State != "running" {
		t.Fatalf("home after rebuild = %+v (before %s)", after, before.ID)
	}
	if f.state(t, lab.ID) != "missing" {
		t.Fatal("lab's container survived the rebuild")
	}
	if _, err := os.Stat(f.m.ConfigDir(lab.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lab's config dir survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.m.ConfigDir(home.ID), "wg0.conf")); err != nil {
		t.Fatalf("home's config: %v", err)
	}
	wantOtherPanel(t, f)
}
