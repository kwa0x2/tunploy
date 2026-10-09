// Package host is a machine the VPN containers run on: the panel's own, or a
// node it reaches over SSH.
package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
)

type Docker interface {
	ImageExists(ctx context.Context, ref string) (bool, error)
	BuildImage(ctx context.Context, tag string, files map[string][]byte) error
	CreateContainer(ctx context.Context, spec docker.ContainerSpec) (string, error)
	InspectContainer(ctx context.Context, id string) (docker.Container, error)
	ListContainers(ctx context.Context) ([]docker.Container, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeout time.Duration) error
	RemoveContainer(ctx context.Context, id string) error
	Exec(ctx context.Context, id string, cmd []string) ([]byte, error)
	Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error)
}

type Host interface {
	Docker
	// Root is the data directory as that machine's Docker daemon sees it.
	Root() string
	// WriteFile replaces path in one step, readable by root only.
	WriteFile(ctx context.Context, path string, body []byte) error
	RemoveAll(ctx context.Context, path string) error
	// ReadDir lists the names in path; a missing directory is empty.
	ReadDir(ctx context.Context, path string) ([]string, error)
}

// ErrOffline means the panel cannot reach the node right now. What the
// database says is applied once it is back.
var ErrOffline = errors.New("node is offline")

// Local is the panel's own machine.
type Local struct {
	Docker
	root string
}

func NewLocal(dk Docker, root string) Local { return Local{Docker: dk, root: root} }

func (h Local) Root() string { return h.root }

func (h Local) WriteFile(_ context.Context, path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (h Local) RemoveAll(_ context.Context, path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func (h Local) ReadDir(_ context.Context, path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", path, err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}
