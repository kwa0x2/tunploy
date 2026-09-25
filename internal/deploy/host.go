package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Host is a machine the containers run on: the panel's own, or a node it
// reaches over SSH.
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

// ErrNodeOffline means the panel cannot reach the node right now. What the
// database says is applied once it is back.
var ErrNodeOffline = errors.New("node is offline")

type localHost struct {
	Docker
	root string
}

func (h localHost) Root() string { return h.root }

func (h localHost) WriteFile(_ context.Context, path string, body []byte) error {
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

func (h localHost) RemoveAll(_ context.Context, path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func (h localHost) ReadDir(_ context.Context, path string) ([]string, error) {
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
