package wg

import (
	"testing"
	"time"
)

func TestBlocked(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Hour)

	tests := []struct {
		name  string
		peer  Peer
		month Traffic
		want  Block
	}{
		{"no limits", Peer{Enabled: true}, Traffic{RxBytes: 1 << 40}, ""},
		{"under limit", Peer{Enabled: true, DataLimit: 100}, Traffic{RxBytes: 60, TxBytes: 39}, ""},
		{"limit counts both directions", Peer{Enabled: true, DataLimit: 100}, Traffic{RxBytes: 60, TxBytes: 40}, BlockLimit},
		{"not yet expired", Peer{Enabled: true, ExpiresAt: &future}, Traffic{}, ""},
		{"expired", Peer{Enabled: true, ExpiresAt: &now}, Traffic{}, BlockExpired},
		{"expiry wins over limit", Peer{Enabled: true, DataLimit: 1, ExpiresAt: &past}, Traffic{RxBytes: 5}, BlockExpired},
		{"disabled is not blocked", Peer{DataLimit: 1, ExpiresAt: &past}, Traffic{RxBytes: 5}, ""},
	}
	for _, tt := range tests {
		if got := tt.peer.Blocked(tt.month, now); got != tt.want {
			t.Errorf("%s: Blocked = %q, want %q", tt.name, got, tt.want)
		}
	}
}
