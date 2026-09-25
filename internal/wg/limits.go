package wg

import "time"

type Traffic struct {
	RxBytes int64 `json:"rx_bytes"`
	TxBytes int64 `json:"tx_bytes"`
}

func (t Traffic) Total() int64 { return t.RxBytes + t.TxBytes }

type Block string

const (
	BlockExpired Block = "expired"
	BlockLimit   Block = "limit"
)

// Blocked is why an enabled peer is kept off the tunnel right now, or "".
func (p Peer) Blocked(month Traffic, now time.Time) Block {
	switch {
	case !p.Enabled:
		return ""
	case p.ExpiresAt != nil && !now.Before(*p.ExpiresAt):
		return BlockExpired
	case p.DataLimit > 0 && month.Total() >= p.DataLimit:
		return BlockLimit
	}
	return ""
}
