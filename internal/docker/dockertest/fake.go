// Package dockertest is an in-memory Docker daemon for tests.
package dockertest

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
)

type Fake struct {
	mu         sync.Mutex
	images     map[string]bool
	containers map[string]*docker.Container
	specs      map[string]docker.ContainerSpec
	nextID     int

	Builds int
	Execs  [][]string

	// Unavailable makes every call fail as if the daemon were down.
	Unavailable bool
	// StartErr is returned by StartContainer, e.g. a port already in use.
	StartErr error
	// ExecOutput defaults to an empty `wg show dump`.
	ExecOutput func(name string, cmd []string) ([]byte, error)
	// LogOutput, when set, replaces every container's output.
	LogOutput string
	// BootLog is what a started container prints; defaults to ReadyLog.
	BootLog      string
	BootExitCode int

	logs map[string]string
}

// ReadyLog mirrors the markers deploy/image/tunploy-wg.sh prints.
const ReadyLog = "tunploy:step interface\ntunploy:step firewall\ntunploy:step nat\nwireguard wg0 is up\ntunploy:ready\n"

func New() *Fake {
	return &Fake{
		images:     map[string]bool{},
		containers: map[string]*docker.Container{},
		specs:      map[string]docker.ContainerSpec{},
		logs:       map[string]string{},
		BootLog:    ReadyLog,
	}
}

func (f *Fake) check() error {
	if f.Unavailable {
		return fmt.Errorf("fake: %w", docker.ErrUnavailable)
	}
	return nil
}

func (f *Fake) Ping(ctx context.Context) (docker.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return docker.Info{}, err
	}
	return docker.Info{Version: "fake", APIVersion: "fake", OS: "linux", Arch: "arm64"}, nil
}

func (f *Fake) ImageExists(ctx context.Context, ref string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.images[ref], f.check()
}

func (f *Fake) BuildImage(ctx context.Context, tag string, files map[string][]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return err
	}
	f.images[tag] = true
	f.Builds++
	return nil
}

func (f *Fake) CreateContainer(ctx context.Context, spec docker.ContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return "", err
	}
	if _, ok := f.containers[spec.Name]; ok {
		return "", fmt.Errorf("fake: name %s in use: %w", spec.Name, docker.ErrConflict)
	}
	f.nextID++
	labels := maps.Clone(spec.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[docker.LabelManaged] = "true"
	ct := &docker.Container{
		ID:     strconv.Itoa(f.nextID),
		Name:   spec.Name,
		Image:  spec.Image,
		State:  "created",
		Labels: labels,
	}
	f.containers[spec.Name] = ct
	f.specs[spec.Name] = spec
	return ct.ID, nil
}

func (f *Fake) InspectContainer(ctx context.Context, id string) (docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ct, err := f.find(id)
	if err != nil {
		return docker.Container{}, err
	}
	return *ct, nil
}

func (f *Fake) ListContainers(ctx context.Context) ([]docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return nil, err
	}
	out := make([]docker.Container, 0, len(f.containers))
	for _, ct := range f.containers {
		out = append(out, *ct)
	}
	return out, nil
}

func (f *Fake) StartContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ct, err := f.find(id)
	if err != nil {
		return err
	}
	if f.StartErr != nil {
		return f.StartErr
	}
	ct.State = "running"
	if f.BootExitCode != 0 {
		ct.State, ct.ExitCode = "exited", f.BootExitCode
	}
	f.logs[ct.Name] += f.BootLog
	return nil
}

func (f *Fake) StopContainer(ctx context.Context, id string, timeout time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ct, err := f.find(id)
	if err != nil {
		return err
	}
	ct.State = "exited"
	return nil
}

func (f *Fake) RemoveContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ct, err := f.find(id)
	if err != nil {
		return err
	}
	delete(f.containers, ct.Name)
	delete(f.specs, ct.Name)
	delete(f.logs, ct.Name)
	return nil
}

func (f *Fake) Exec(ctx context.Context, id string, cmd []string) ([]byte, error) {
	f.mu.Lock()
	ct, err := f.find(id)
	if err == nil && ct.State != "running" {
		err = fmt.Errorf("fake: container %s is not running: %w", ct.Name, docker.ErrConflict)
	}
	if err == nil {
		f.Execs = append(f.Execs, cmd)
	}
	hook := f.ExecOutput
	f.mu.Unlock()

	if err != nil {
		return nil, err
	}
	if hook != nil {
		return hook(ct.Name, cmd)
	}
	if cmd[0] == "wg" {
		return []byte("priv\tpub\t51820\toff\n"), nil
	}
	return nil, nil
}

func (f *Fake) Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ct, err := f.find(id)
	if err != nil {
		return nil, err
	}
	if f.LogOutput != "" {
		return io.NopCloser(strings.NewReader(f.LogOutput)), nil
	}
	return io.NopCloser(strings.NewReader(f.logs[ct.Name])), nil
}

func (f *Fake) Container(name string) (docker.Container, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ct, ok := f.containers[name]
	if !ok {
		return docker.Container{}, false
	}
	return *ct, true
}

func (f *Fake) Spec(name string) (docker.ContainerSpec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	spec, ok := f.specs[name]
	return spec, ok
}

func (f *Fake) Add(ct docker.Container) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := ct
	f.containers[ct.Name] = &c
}

func (f *Fake) find(id string) (*docker.Container, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	if ct, ok := f.containers[id]; ok {
		return ct, nil
	}
	for _, ct := range f.containers {
		if ct.ID == id {
			return ct, nil
		}
	}
	return nil, fmt.Errorf("fake: container %s: %w", id, docker.ErrNotFound)
}
