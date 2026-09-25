package deploy

import (
	"sync"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

// WireGuard renews the handshake every two minutes on a live tunnel.
const handshakeWindow = 3 * time.Minute

// A keepalive client's rx stops growing long before its handshake ages out.
type activity struct {
	mu    sync.Mutex
	peers map[int64]map[wg.Key]sample
}

type sample struct {
	rx          int64
	since, seen time.Time
	// grew: since is when rx was seen growing, not just first observed.
	grew bool

	online      bool
	onlineSince time.Time
}

type PeerChange struct {
	InstanceID  int64
	Key         wg.Key
	Online      bool
	Endpoint    string
	OnlineSince time.Time
}

// A first observation is never a change, so a panel restart logs nothing.
func (a *activity) observe(instanceID int64, stats map[wg.Key]wg.PeerStats, keepalive int, now time.Time) []PeerChange {
	window := time.Duration(2*keepalive)*time.Second + 10*time.Second

	a.mu.Lock()
	defer a.mu.Unlock()

	var changes []PeerChange
	prev := a.peers[instanceID]
	next := make(map[wg.Key]sample, len(stats))
	for key, st := range stats {
		old, ok := prev[key]
		s := old
		switch {
		case !ok || st.RxBytes < s.rx:
			// New peer, or counters reset by a container restart.
			s = sample{rx: st.RxBytes, since: now}
		case st.RxBytes > s.rx:
			s = sample{rx: st.RxBytes, since: now, grew: now.Sub(s.seen) <= window}
		}
		s.seen = now
		st.Online = online(st, s, keepalive, window, now)
		stats[key] = st

		s.online, s.onlineSince = st.Online, old.onlineSince
		if st.Online && (!ok || !old.online) {
			s.onlineSince = now
		}
		next[key] = s

		if ok && st.Online != old.online {
			changes = append(changes, PeerChange{
				InstanceID:  instanceID,
				Key:         key,
				Online:      st.Online,
				Endpoint:    st.Endpoint,
				OnlineSince: s.onlineSince,
			})
		}
	}
	a.peers[instanceID] = next
	return changes
}

func online(st wg.PeerStats, s sample, keepalive int, window time.Duration, now time.Time) bool {
	recent := st.LatestHandshake != nil && now.Sub(*st.LatestHandshake) < handshakeWindow
	switch {
	case keepalive == 0:
		// An idle client sends nothing, so silence proves nothing.
		return recent
	case now.Sub(s.since) >= window:
		return false
	case s.grew:
		return true
	default:
		return recent
	}
}

func (a *activity) forget(instanceID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.peers, instanceID)
}

func (a *activity) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.peers = map[int64]map[wg.Key]sample{}
}
