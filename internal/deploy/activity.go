package deploy

import (
	"sync"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

// WireGuard renews the handshake every two minutes on a live tunnel, so an
// older one means the peer has gone quiet.
const handshakeWindow = 3 * time.Minute

// activity decides which peers are online. The handshake alone keeps a
// device that just left looking online for minutes; a client with a
// keepalive sends at least that often, so a received-bytes counter that
// stops growing gives it away much sooner.
type activity struct {
	mu    sync.Mutex
	peers map[int64]map[wg.Key]sample
}

type sample struct {
	rx int64
	// since is when rx was first seen at this value, and seen the latest
	// observation.
	since, seen time.Time
	// grew is set when since is the moment rx was watched growing rather
	// than just the first time the peer was looked at.
	grew bool
}

// observe fills in Online on stats. It only knows as much as it is asked,
// which the panel does every few seconds while someone is looking.
func (a *activity) observe(instanceID int64, stats map[wg.Key]wg.PeerStats, keepalive int, now time.Time) {
	window := time.Duration(2*keepalive)*time.Second + 10*time.Second

	a.mu.Lock()
	defer a.mu.Unlock()

	prev := a.peers[instanceID]
	next := make(map[wg.Key]sample, len(stats))
	for key, st := range stats {
		s, ok := prev[key]
		switch {
		case !ok || st.RxBytes < s.rx:
			// New peer, or counters reset by a container restart.
			s = sample{rx: st.RxBytes, since: now}
		case st.RxBytes > s.rx:
			s = sample{rx: st.RxBytes, since: now, grew: now.Sub(s.seen) <= window}
		}
		s.seen = now
		next[key] = s

		st.Online = online(st, s, keepalive, window, now)
		stats[key] = st
	}
	a.peers[instanceID] = next
}

func online(st wg.PeerStats, s sample, keepalive int, window time.Duration, now time.Time) bool {
	recent := st.LatestHandshake != nil && now.Sub(*st.LatestHandshake) < handshakeWindow
	switch {
	case keepalive == 0:
		// An idle client sends nothing, so silence proves nothing.
		return recent
	case now.Sub(s.since) >= window:
		// Watched for longer than a keepalive takes and nothing arrived.
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
