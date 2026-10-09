package server

import (
	"context"
	"sync"

	"github.com/kwa0x2/tunploy/internal/host"
	"github.com/kwa0x2/tunploy/internal/node"
	"github.com/kwa0x2/tunploy/internal/store"
)

// fakeNodes stands in for the SSH pool: a node is online while it has a host.
type fakeNodes struct {
	mu      sync.Mutex
	hosts   map[int64]host.Host
	added   []store.Node
	removed []int64
	reloads int
}

func newFakeNodes() *fakeNodes { return &fakeNodes{hosts: map[int64]host.Host{}} }

func (f *fakeNodes) Add(n store.Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, n)
}

func (f *fakeNodes) Remove(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	delete(f.hosts, id)
}

func (f *fakeNodes) Rename(store.Node) {}

func (f *fakeNodes) set(id int64, h host.Host) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h == nil {
		delete(f.hosts, id)
		return
	}
	f.hosts[id] = h
}

func (f *fakeNodes) Status(id int64) node.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.hosts[id]; ok {
		return node.Status{State: node.StateOnline}
	}
	return node.Status{State: node.StateOffline}
}

func (f *fakeNodes) Host(id int64) (host.Host, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h, ok := f.hosts[id]; ok {
		return h, nil
	}
	return nil, host.ErrOffline
}

func (f *fakeNodes) Reload(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloads++
	return nil
}
