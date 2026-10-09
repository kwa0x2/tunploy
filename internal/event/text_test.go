package event

import (
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func TestLimits(t *testing.T) {
	midnight := time.Date(2026, 10, 2, 0, 0, 0, 0, time.Local)
	odd := time.Date(2026, 10, 2, 9, 30, 0, 0, time.Local)
	tests := []struct {
		peer wg.Peer
		want string
	}{
		{wg.Peer{}, "no limits"},
		{wg.Peer{DataLimit: 5 << 30, ExpiresAt: &midnight}, "5 GB a month, until the end of 1 Oct 2026"},
		{wg.Peer{DataLimit: 1536 << 20, ExpiresAt: &odd}, "1.5 GB a month, until 2 Oct 2026 09:30"},
		{wg.Peer{DataLimit: 1 << 30, LimitPeriod: wg.PeriodTotal, SpeedLimit: 2500}, "1 GB in total, 2.5 Mbit/s"},
		{wg.Peer{SpeedLimit: 512}, "512 kbit/s"},
	}
	for _, tt := range tests {
		if got := Limits(&tt.peer); got != tt.want {
			t.Errorf("Limits = %q, want %q", got, tt.want)
		}
	}
}
