package wg

import "time"

type Traffic struct {
	RxBytes int64 `json:"rx_bytes"`
	TxBytes int64 `json:"tx_bytes"`
}

func (t Traffic) Total() int64 { return t.RxBytes + t.TxBytes }

type LimitPeriod string

const (
	// Counts from the first of each calendar month.
	PeriodMonthly LimitPeriod = "monthly"
	// Counts until a usage reset, for plans that do not follow the calendar.
	PeriodTotal LimitPeriod = "total"
)

func (lp LimitPeriod) Valid() bool { return lp == PeriodMonthly || lp == PeriodTotal }

// Zero-value peers count by month, as every peer did before periods existed.
func (lp LimitPeriod) OrDefault() LimitPeriod {
	if lp == "" {
		return PeriodMonthly
	}
	return lp
}

type Block string

const (
	BlockExpired Block = "expired"
	BlockLimit   Block = "limit"
)

// Blocked is why an enabled peer is kept off the tunnel right now, or "".
// used is what counts toward the limit in the current period.
func (p Peer) Blocked(used Traffic, now time.Time) Block {
	switch {
	case !p.Enabled:
		return ""
	case p.ExpiresAt != nil && !now.Before(*p.ExpiresAt):
		return BlockExpired
	case p.DataLimit > 0 && used.Total() >= p.DataLimit:
		return BlockLimit
	}
	return ""
}

// Status is a device's state as API clients see it.
type Status string

const (
	StatusActive       Status = "active"
	StatusDisabled     Status = "disabled"
	StatusExpired      Status = "expired"
	StatusLimitReached Status = "limit_reached"
)

func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusDisabled, StatusExpired, StatusLimitReached:
		return true
	}
	return false
}

// Status must match the status filter in store.Devices.
func (p Peer) Status(used Traffic, now time.Time) Status {
	if !p.Enabled {
		return StatusDisabled
	}
	switch p.Blocked(used, now) {
	case BlockExpired:
		return StatusExpired
	case BlockLimit:
		return StatusLimitReached
	}
	return StatusActive
}
