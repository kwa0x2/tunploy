package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

type DeviceFilter struct {
	InstanceID int64
	ExternalID string
	// Matched by the same rule as wg.Peer.Status.
	Status wg.Status
	// Only devices with a larger ID, for paging.
	After int64
	Limit int
}

// Devices lists peers across servers, oldest first.
func (s *Store) Devices(ctx context.Context, f DeviceFilter, now time.Time) ([]wg.Peer, error) {
	where := []string{"id > ?"}
	args := []any{f.After}
	if f.InstanceID > 0 {
		where = append(where, "instance_id = ?")
		args = append(args, f.InstanceID)
	}
	if f.ExternalID != "" {
		where = append(where, "external_id = ?")
		args = append(args, f.ExternalID)
	}

	const live = `enabled = 1 AND (expires_at IS NULL OR expires_at > ?)`
	month, at := dayKey(MonthStart(now)), now.Unix()
	switch f.Status {
	case wg.StatusDisabled:
		where = append(where, "enabled = 0")
	case wg.StatusExpired:
		where = append(where, "enabled = 1 AND expires_at <= ?")
		args = append(args, at)
	case wg.StatusLimitReached:
		where = append(where, live, "data_limit > 0 AND "+limitUsed+" >= data_limit")
		args = append(args, at, month, month)
	case wg.StatusActive:
		where = append(where, live, "(data_limit = 0 OR "+limitUsed+" < data_limit)")
		args = append(args, at, month, month)
	}
	args = append(args, f.Limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+peerColumns+` FROM wg_peers WHERE `+strings.Join(where, " AND ")+` ORDER BY id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	peers := []wg.Peer{}
	for rows.Next() {
		p, err := scanPeer(rows)
		if err != nil {
			return nil, err
		}
		peers = append(peers, *p)
	}
	return peers, rows.Err()
}

// PeersMonthUsage is MonthUsage for peers on any server.
func (s *Store) PeersMonthUsage(ctx context.Context, ids []int64, now time.Time) (map[int64]wg.Traffic, error) {
	usage := map[int64]wg.Traffic{}
	if len(ids) == 0 {
		return usage, nil
	}
	args := []any{dayKey(MonthStart(now))}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT peer_id, SUM(rx_bytes), SUM(tx_bytes) FROM wg_peer_usage
		 WHERE day >= ? AND peer_id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)
		 GROUP BY peer_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("month usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var t wg.Traffic
		if err := rows.Scan(&id, &t.RxBytes, &t.TxBytes); err != nil {
			return nil, fmt.Errorf("scan month usage: %w", err)
		}
		usage[id] = t
	}
	return usage, rows.Err()
}
