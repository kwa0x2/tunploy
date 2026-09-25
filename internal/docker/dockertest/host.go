package dockertest

import (
	"context"
	"path"
	"slices"
	"strings"
	"sync"
)

// Host is a fake remote machine: a Fake daemon plus an in-memory file tree.
type Host struct {
	*Fake
	root string

	mu    sync.Mutex
	files map[string][]byte
}

func NewHost(root string) *Host {
	return &Host{Fake: New(), root: root, files: map[string][]byte{}}
}

func (h *Host) Root() string { return h.root }

func (h *Host) WriteFile(_ context.Context, p string, body []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.files[p] = slices.Clone(body)
	return nil
}

func (h *Host) RemoveAll(_ context.Context, p string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for name := range h.files {
		if name == p || strings.HasPrefix(name, p+"/") {
			delete(h.files, name)
		}
	}
	return nil
}

func (h *Host) ReadDir(_ context.Context, p string) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var names []string
	for name := range h.files {
		rest, ok := strings.CutPrefix(name, p+"/")
		if !ok {
			continue
		}
		first, _, _ := strings.Cut(rest, "/")
		if !slices.Contains(names, first) {
			names = append(names, first)
		}
	}
	slices.Sort(names)
	return names, nil
}

// File returns what was written to p.
func (h *Host) File(p string) ([]byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	body, ok := h.files[path.Clean(p)]
	return body, ok
}
