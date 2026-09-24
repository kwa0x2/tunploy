package wg

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type PeerStats struct {
	Endpoint        string     `json:"endpoint,omitempty"`
	LatestHandshake *time.Time `json:"latest_handshake,omitempty"`
	RxBytes         int64      `json:"rx_bytes"`
	TxBytes         int64      `json:"tx_bytes"`
}

// ParseDump reads `wg show <iface> dump`: an interface line, then one
// tab-separated line per peer.
func ParseDump(out []byte) (map[Key]PeerStats, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	stats := make(map[Key]PeerStats, max(len(lines)-1, 0))
	if len(lines) <= 1 {
		return stats, nil
	}

	for _, line := range lines[1:] {
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			return nil, fmt.Errorf("wg dump: want 8 fields per peer, got %d in %q", len(f), line)
		}
		key, err := ParseKey(f[0])
		if err != nil {
			return nil, fmt.Errorf("wg dump: peer key: %w", err)
		}
		handshake, err1 := strconv.ParseInt(f[4], 10, 64)
		rx, err2 := strconv.ParseInt(f[5], 10, 64)
		tx, err3 := strconv.ParseInt(f[6], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("wg dump: bad counters in %q", line)
		}

		s := PeerStats{RxBytes: rx, TxBytes: tx}
		if f[2] != "(none)" {
			s.Endpoint = f[2]
		}
		if handshake > 0 {
			t := time.Unix(handshake, 0).UTC()
			s.LatestHandshake = &t
		}
		stats[key] = s
	}
	return stats, nil
}
