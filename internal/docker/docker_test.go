package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const testImage = "busybox:1.37"

// newTestClient skips rather than fails without a daemon, so `go test ./...`
// stays green on machines and CI runners that have no Docker.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	if testing.Short() {
		t.Skip("needs a Docker daemon")
	}
	c, err := New("")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Ping(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	return c
}

func uniqueName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	rand.Read(b)
	return "tunploy-test-" + hex.EncodeToString(b)
}

func TestPingUnreachableDaemon(t *testing.T) {
	c, err := New("tcp://127.0.0.1:1")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer c.Close()

	_, err = c.Ping(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestPing(t *testing.T) {
	c := newTestClient(t)

	info, err := c.Ping(context.Background())
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if info.Version == "" || info.APIVersion == "" || info.OS == "" {
		t.Fatalf("incomplete info: %+v", info)
	}
}

func TestContainerLifecycle(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	if err := c.EnsureImage(ctx, testImage); err != nil {
		t.Fatalf("ensure image: %v", err)
	}

	name := uniqueName(t)
	spec := ContainerSpec{
		Name:    name,
		Image:   testImage,
		Cmd:     []string{"sleep", "300"},
		Env:     map[string]string{"FOO": "bar"},
		Labels:  map[string]string{"io.tunploy.instance": "42", LabelManaged: "false"},
		Ports:   []Port{{Host: 0, Container: 51820, Protocol: network.UDP}},
		Mounts:  []Mount{{Source: t.TempDir(), Target: "/config", ReadOnly: true}},
		CapAdd:  []string{"NET_ADMIN"},
		Sysctls: map[string]string{"net.ipv4.ip_forward": "1"},
	}
	id, err := c.CreateContainer(ctx, spec)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		c.api.ContainerRemove(context.Background(), id, client.ContainerRemoveOptions{Force: true})
	})

	ct, err := c.InspectContainer(ctx, id)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if ct.Name != name || ct.State != "created" {
		t.Fatalf("after create: %+v", ct)
	}
	if ct.Labels[LabelManaged] != "true" {
		t.Fatal("caller labels must not be able to unset the managed label")
	}
	if ct.Labels["io.tunploy.instance"] != "42" {
		t.Fatalf("caller labels lost: %v", ct.Labels)
	}

	raw, err := c.api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("raw inspect: %v", err)
	}
	hc := raw.Container.HostConfig
	if !slices.Contains(raw.Container.Config.Env, "FOO=bar") {
		t.Errorf("env not applied: %v", raw.Container.Config.Env)
	}
	// The daemon normalises capability names to their CAP_ form.
	if !slices.Contains(hc.CapAdd, "CAP_NET_ADMIN") || hc.Sysctls["net.ipv4.ip_forward"] != "1" {
		t.Errorf("caps/sysctls not applied: %v %v", hc.CapAdd, hc.Sysctls)
	}
	if hc.RestartPolicy.Name != container.RestartPolicyUnlessStopped {
		t.Errorf("restart policy: %q", hc.RestartPolicy.Name)
	}
	if len(hc.Mounts) != 1 || !hc.Mounts[0].ReadOnly || hc.Mounts[0].Target != "/config" {
		t.Errorf("mounts: %+v", hc.Mounts)
	}
	if len(hc.PortBindings) != 1 {
		t.Errorf("port bindings: %v", hc.PortBindings)
	}

	if _, err := c.CreateContainer(ctx, spec); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: want ErrConflict, got %v", err)
	}

	if err := c.StartContainer(ctx, id); err != nil {
		t.Fatalf("start: %v", err)
	}
	if ct, _ := c.InspectContainer(ctx, id); !ct.Running() {
		t.Fatalf("after start: %+v", ct)
	}
	if err := c.StartContainer(ctx, id); err != nil {
		t.Fatalf("starting a running container must be a no-op: %v", err)
	}

	list, err := c.ListContainers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !slices.ContainsFunc(list, func(l Container) bool { return l.ID == id && l.Name == name }) {
		t.Fatalf("managed container missing from list: %+v", list)
	}

	// busybox sleep ignores SIGTERM as PID 1, so this also covers the SIGKILL fallback.
	if err := c.StopContainer(ctx, id, time.Second); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if ct, _ := c.InspectContainer(ctx, id); ct.State != "exited" {
		t.Fatalf("after stop: %+v", ct)
	}

	if err := c.RemoveContainer(ctx, id); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := c.InspectContainer(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after remove: want ErrNotFound, got %v", err)
	}
	if err := c.RemoveContainer(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove twice: want ErrNotFound, got %v", err)
	}
}

func TestRefusesUnmanagedContainer(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	if err := c.EnsureImage(ctx, testImage); err != nil {
		t.Fatalf("ensure image: %v", err)
	}
	resp, err := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:   uniqueName(t),
		Config: &container.Config{Image: testImage, Cmd: []string{"sleep", "300"}},
	})
	if err != nil {
		t.Fatalf("create unmanaged: %v", err)
	}
	t.Cleanup(func() {
		c.api.ContainerRemove(context.Background(), resp.ID, client.ContainerRemoveOptions{Force: true})
	})

	if err := c.StartContainer(ctx, resp.ID); !errors.Is(err, ErrNotManaged) {
		t.Errorf("start: want ErrNotManaged, got %v", err)
	}
	if err := c.StopContainer(ctx, resp.ID, time.Second); !errors.Is(err, ErrNotManaged) {
		t.Errorf("stop: want ErrNotManaged, got %v", err)
	}
	if err := c.RemoveContainer(ctx, resp.ID); !errors.Is(err, ErrNotManaged) {
		t.Errorf("remove: want ErrNotManaged, got %v", err)
	}

	list, err := c.ListContainers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if slices.ContainsFunc(list, func(l Container) bool { return l.ID == resp.ID }) {
		t.Fatal("unmanaged container leaked into the list")
	}
}

func TestPullMissingImage(t *testing.T) {
	c := newTestClient(t)

	err := c.PullImage(context.Background(), "busybox:tunploy-no-such-tag")
	if err == nil {
		t.Fatal("pulling a missing tag must fail")
	}
}
