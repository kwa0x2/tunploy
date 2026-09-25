package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

// Usage days and months follow the panel's time zone (TZ), like its logs.
func dayKey(t time.Time) string { return t.In(time.Local).Format(time.DateOnly) }

func MonthStart(t time.Time) time.Time {
	t = t.In(time.Local)
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local)
}

type UsagePoint struct {
	Start string `json:"start"`
	wg.Traffic
}

type counterRow struct {
	peerID         int64
	key            string
	rx, tx, shaken sql.NullInt64
}

// Must be called from one goroutine: an older reading applied after a newer
// one would look like a counter reset and be counted twice.
func (s *Store) RecordTraffic(ctx context.Context, instanceID int64, stats map[wg.Key]wg.PeerStats, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record traffic: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT p.id, p.public_key, c.rx_bytes, c.tx_bytes, p.last_handshake
		 FROM wg_peers p LEFT JOIN wg_peer_counters c ON c.peer_id = p.id
		 WHERE p.instance_id = ?`, instanceID)
	if err != nil {
		return fmt.Errorf("load peer counters: %w", err)
	}
	var peers []counterRow
	for rows.Next() {
		var r counterRow
		if err := rows.Scan(&r.peerID, &r.key, &r.rx, &r.tx, &r.shaken); err != nil {
			rows.Close()
			return fmt.Errorf("scan peer counters: %w", err)
		}
		peers = append(peers, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("load peer counters: %w", err)
	}

	live := make(map[string]wg.PeerStats, len(stats))
	for k, st := range stats {
		live[k.String()] = st
	}

	day := dayKey(now)
	for _, r := range peers {
		if err := recordPeer(ctx, tx, r, live, day); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record traffic: %w", err)
	}
	return nil
}

func recordPeer(ctx context.Context, tx *sql.Tx, r counterRow, live map[string]wg.PeerStats, day string) error {
	st, ok := live[r.key]
	if !ok {
		// Off the interface, so its counters start again from zero when it returns.
		if r.rx.Valid && (r.rx.Int64 != 0 || r.tx.Int64 != 0) {
			return setCounters(ctx, tx, r.peerID, 0, 0)
		}
		return nil
	}

	// With no earlier reading there is nothing to measure growth from.
	if r.rx.Valid {
		used := growth(r.rx.Int64, r.tx.Int64, st)
		if used.Total() > 0 {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO wg_peer_usage (peer_id, day, rx_bytes, tx_bytes) VALUES (?, ?, ?, ?)
				 ON CONFLICT (peer_id, day) DO UPDATE SET
					rx_bytes = rx_bytes + excluded.rx_bytes,
					tx_bytes = tx_bytes + excluded.tx_bytes`,
				r.peerID, day, used.RxBytes, used.TxBytes)
			if err != nil {
				return fmt.Errorf("add peer usage: %w", err)
			}
		}
	}
	if !r.rx.Valid || r.rx.Int64 != st.RxBytes || r.tx.Int64 != st.TxBytes {
		if err := setCounters(ctx, tx, r.peerID, st.RxBytes, st.TxBytes); err != nil {
			return err
		}
	}

	if hs := st.LatestHandshake; hs != nil && (!r.shaken.Valid || hs.Unix() > r.shaken.Int64) {
		if _, err := tx.ExecContext(ctx, `UPDATE wg_peers SET last_handshake = ? WHERE id = ?`,
			hs.Unix(), r.peerID); err != nil {
			return fmt.Errorf("save last handshake: %w", err)
		}
	}
	return nil
}

// A drop in either counter means both restarted from zero.
func growth(rx, tx int64, st wg.PeerStats) wg.Traffic {
	if st.RxBytes < rx || st.TxBytes < tx {
		return wg.Traffic{RxBytes: st.RxBytes, TxBytes: st.TxBytes}
	}
	return wg.Traffic{RxBytes: st.RxBytes - rx, TxBytes: st.TxBytes - tx}
}

func setCounters(ctx context.Context, tx *sql.Tx, peerID, rx, txBytes int64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO wg_peer_counters (peer_id, rx_bytes, tx_bytes) VALUES (?, ?, ?)
		 ON CONFLICT (peer_id) DO UPDATE SET rx_bytes = excluded.rx_bytes, tx_bytes = excluded.tx_bytes`,
		peerID, rx, txBytes)
	if err != nil {
		return fmt.Errorf("save peer counters: %w", err)
	}
	return nil
}

// Peers with no traffic this month are absent from the map.
func (s *Store) MonthUsage(ctx context.Context, instanceID int64, now time.Time) (map[int64]wg.Traffic, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.peer_id, SUM(u.rx_bytes), SUM(u.tx_bytes)
		 FROM wg_peer_usage u JOIN wg_peers p ON p.id = u.peer_id
		 WHERE p.instance_id = ? AND u.day >= ?
		 GROUP BY u.peer_id`, instanceID, dayKey(MonthStart(now)))
	if err != nil {
		return nil, fmt.Errorf("month usage: %w", err)
	}
	defer rows.Close()

	usage := map[int64]wg.Traffic{}
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

// PeerUsage returns the last days and months up to now, oldest first, with
// empty periods filled in.
func (s *Store) PeerUsage(ctx context.Context, peerID int64, now time.Time, days, months int) (daily, monthly []UsagePoint, err error) {
	today := now.In(time.Local)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	firstDay := today.AddDate(0, 0, -(days - 1))
	firstMonth := MonthStart(now).AddDate(0, -(months - 1), 0)
	from := firstDay
	if firstMonth.Before(from) {
		from = firstMonth
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT day, rx_bytes, tx_bytes FROM wg_peer_usage WHERE peer_id = ? AND day >= ?`,
		peerID, dayKey(from))
	if err != nil {
		return nil, nil, fmt.Errorf("peer usage: %w", err)
	}
	defer rows.Close()

	byDay := map[string]wg.Traffic{}
	byMonth := map[string]wg.Traffic{}
	for rows.Next() {
		var day string
		var t wg.Traffic
		if err := rows.Scan(&day, &t.RxBytes, &t.TxBytes); err != nil {
			return nil, nil, fmt.Errorf("scan peer usage: %w", err)
		}
		byDay[day] = t
		month := day[:len("2006-01")] + "-01"
		m := byMonth[month]
		byMonth[month] = wg.Traffic{RxBytes: m.RxBytes + t.RxBytes, TxBytes: m.TxBytes + t.TxBytes}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("peer usage: %w", err)
	}

	daily = make([]UsagePoint, days)
	for i := range daily {
		key := dayKey(firstDay.AddDate(0, 0, i))
		daily[i] = UsagePoint{Start: key, Traffic: byDay[key]}
	}
	monthly = make([]UsagePoint, months)
	for i := range monthly {
		key := dayKey(firstMonth.AddDate(0, i, 0))
		monthly[i] = UsagePoint{Start: key, Traffic: byMonth[key]}
	}
	return daily, monthly, nil
}
