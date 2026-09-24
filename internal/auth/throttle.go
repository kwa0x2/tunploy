package auth

import (
	"sync"
	"time"
)

// In-memory: a single node runs one panel.
type Throttle struct {
	max    int
	window time.Duration

	mu      sync.Mutex
	entries map[string]*throttleEntry
}

type throttleEntry struct {
	failures int
	resetAt  time.Time
}

func NewThrottle(max int, window time.Duration) *Throttle {
	return &Throttle{max: max, window: window, entries: map[string]*throttleEntry{}}
}

func (t *Throttle) Allowed(key string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	e, ok := t.entries[key]
	if !ok {
		return true, 0
	}
	if time.Now().After(e.resetAt) {
		delete(t.entries, key)
		return true, 0
	}
	if e.failures >= t.max {
		return false, time.Until(e.resetAt)
	}
	return true, 0
}

func (t *Throttle) Fail(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.prune(now)

	e, ok := t.entries[key]
	if !ok || now.After(e.resetAt) {
		t.entries[key] = &throttleEntry{failures: 1, resetAt: now.Add(t.window)}
		return
	}
	e.failures++
	e.resetAt = now.Add(t.window)
}

func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, key)
}

func (t *Throttle) prune(now time.Time) {
	for k, e := range t.entries {
		if now.After(e.resetAt) {
			delete(t.entries, k)
		}
	}
}
