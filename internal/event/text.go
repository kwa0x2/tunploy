package event

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n)/unit, 0
	for v >= unit && i < 3 {
		v /= unit
		i++
	}
	if v < 10 && v != float64(int64(v)) {
		return fmt.Sprintf("%.1f %s", v, []string{"KB", "MB", "GB", "TB"}[i])
	}
	return fmt.Sprintf("%.0f %s", v, []string{"KB", "MB", "GB", "TB"}[i])
}

func Speed(kbit int64) string {
	if kbit < 1000 {
		return fmt.Sprintf("%d kbit/s", kbit)
	}
	return strconv.FormatFloat(float64(kbit)/1000, 'f', -1, 64) + " Mbit/s"
}

// Period finishes "used 3 GB of 5 GB …".
func Period(p wg.Peer, now time.Time) string {
	monthly := p.LimitPeriod != wg.PeriodTotal
	if r := p.UsageResetAt; r != nil && (!monthly || !r.Before(store.MonthStart(now))) {
		return "since " + r.In(time.Local).Format("2 Jan 2006 15:04")
	}
	if monthly {
		return "this month"
	}
	return "in total"
}

// The panel sets expiries to midnight, which reads better as the day before ends.
func Expiry(t time.Time) string {
	t = t.In(time.Local)
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		return "the end of " + t.AddDate(0, 0, -1).Format("2 Jan 2006")
	}
	return t.Format("2 Jan 2006 15:04")
}

func HasLimits(p *wg.Peer) bool { return p.DataLimit > 0 || p.ExpiresAt != nil || p.SpeedLimit > 0 }

func Limits(p *wg.Peer) string {
	var parts []string
	if p.DataLimit > 0 {
		per := " a month"
		if p.LimitPeriod == wg.PeriodTotal {
			per = " in total"
		}
		parts = append(parts, Bytes(p.DataLimit)+per)
	}
	if p.SpeedLimit > 0 {
		parts = append(parts, Speed(p.SpeedLimit))
	}
	if p.ExpiresAt != nil {
		parts = append(parts, "until "+Expiry(*p.ExpiresAt))
	}
	if len(parts) == 0 {
		return "no limits"
	}
	return strings.Join(parts, ", ")
}

func Duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
