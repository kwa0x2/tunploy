package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

const instanceColumns = `id, node_id, name, address, listen_port, private_key, public_key, endpoint,
	dns, mtu, persistent_keepalive, client_allowed_ips, country, city, created_at, updated_at`

const peerColumns = `id, instance_id, name, address, private_key, public_key, preshared_key,
	enabled, data_limit, limit_period, usage_reset_at, expires_at, last_handshake, external_id, metadata,
	created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) CreateInstance(ctx context.Context, in wg.Instance) (*wg.Instance, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO wg_instances (node_id, name, address, listen_port, private_key, public_key, endpoint,
			dns, mtu, persistent_keepalive, client_allowed_ips, country, city, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.NodeID, in.Name, in.Address.String(), in.ListenPort, in.PrivateKey.String(), in.PublicKey.String(),
		in.Endpoint, joinList(in.DNS), in.MTU, in.PersistentKeepalive, joinList(in.ClientAllowedIPs),
		in.Country, in.City, now, now)
	if err != nil {
		return nil, writeError("create instance", err)
	}
	if in.ID, err = res.LastInsertId(); err != nil {
		return nil, fmt.Errorf("read inserted instance id: %w", err)
	}
	in.CreatedAt = time.Unix(now, 0).UTC()
	in.UpdatedAt = in.CreatedAt
	return &in, nil
}

func (s *Store) Instances(ctx context.Context) ([]wg.Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+instanceColumns+` FROM wg_instances ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	defer rows.Close()

	instances := []wg.Instance{}
	for rows.Next() {
		in, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, *in)
	}
	return instances, rows.Err()
}

func (s *Store) InstanceByID(ctx context.Context, id int64) (*wg.Instance, error) {
	return scanInstance(s.db.QueryRowContext(ctx,
		`SELECT `+instanceColumns+` FROM wg_instances WHERE id = ?`, id))
}

// Address and keys stay: changing them would break every issued config.
func (s *Store) UpdateInstance(ctx context.Context, in wg.Instance) (*wg.Instance, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE wg_instances SET name = ?, listen_port = ?, endpoint = ?, dns = ?, mtu = ?,
			persistent_keepalive = ?, client_allowed_ips = ?, country = ?, city = ?, updated_at = ?
		 WHERE id = ?`,
		in.Name, in.ListenPort, in.Endpoint, joinList(in.DNS), in.MTU,
		in.PersistentKeepalive, joinList(in.ClientAllowedIPs), in.Country, in.City, time.Now().Unix(), in.ID)
	if err != nil {
		return nil, writeError("update instance", err)
	}
	if err := expectOneRow(res, "update instance"); err != nil {
		return nil, err
	}
	return s.InstanceByID(ctx, in.ID)
}

func (s *Store) DeleteInstance(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM wg_instances WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	return expectOneRow(res, "delete instance")
}

// One transaction on a one-connection pool serialises address allocation.
func (s *Store) CreatePeer(ctx context.Context, p wg.Peer) (*wg.Peer, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create peer: %w", err)
	}
	defer tx.Rollback()

	var raw string
	err = tx.QueryRowContext(ctx, `SELECT address FROM wg_instances WHERE id = ?`, p.InstanceID).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load instance address: %w", err)
	}
	server, err := netip.ParsePrefix(raw)
	if err != nil {
		return nil, fmt.Errorf("instance %d has a corrupt address: %w", p.InstanceID, err)
	}

	used, err := peerAddresses(ctx, tx, p.InstanceID)
	if err != nil {
		return nil, err
	}
	if p.Address, err = wg.NextAddress(server.Masked(), append(used, server.Addr())); err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO wg_peers (instance_id, name, address, private_key, public_key, preshared_key,
			enabled, data_limit, limit_period, expires_at, external_id, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.InstanceID, p.Name, p.Address.String(), privateKeyText(p), p.PublicKey.String(),
		p.PresharedKey.String(), p.Enabled, p.DataLimit, string(p.LimitPeriod.OrDefault()), nullTime(p.ExpiresAt),
		p.ExternalID, string(p.Metadata), now, now)
	if err != nil {
		return nil, writeError("create peer", err)
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return nil, fmt.Errorf("read inserted peer id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create peer: %w", err)
	}

	p.LimitPeriod = p.LimitPeriod.OrDefault()
	p.CreatedAt = time.Unix(now, 0).UTC()
	p.UpdatedAt = p.CreatedAt
	return &p, nil
}

// MovePeer puts a peer on another instance with a new address there. The row
// stays, so its ID, keys and usage history go with it.
func (s *Store) MovePeer(ctx context.Context, id, instanceID int64) (*wg.Peer, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin move peer: %w", err)
	}
	defer tx.Rollback()

	var raw string
	err = tx.QueryRowContext(ctx, `SELECT address FROM wg_instances WHERE id = ?`, instanceID).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load instance address: %w", err)
	}
	server, err := netip.ParsePrefix(raw)
	if err != nil {
		return nil, fmt.Errorf("instance %d has a corrupt address: %w", instanceID, err)
	}
	used, err := peerAddresses(ctx, tx, instanceID)
	if err != nil {
		return nil, err
	}
	addr, err := wg.NextAddress(server.Masked(), append(used, server.Addr()))
	if err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE wg_peers SET instance_id = ?, address = ?, last_handshake = NULL, updated_at = ? WHERE id = ?`,
		instanceID, addr.String(), time.Now().Unix(), id)
	if err != nil {
		return nil, writeError("move peer", err)
	}
	if err := expectOneRow(res, "move peer"); err != nil {
		return nil, err
	}
	// The new interface counts from zero; the old reading would hide its first bytes.
	if err := setCounters(ctx, tx, id, 0, 0); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit move peer: %w", err)
	}
	return s.PeerByID(ctx, id)
}

func peerAddresses(ctx context.Context, tx *sql.Tx, instanceID int64) ([]netip.Addr, error) {
	rows, err := tx.QueryContext(ctx, `SELECT address FROM wg_peers WHERE instance_id = ?`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list peer addresses: %w", err)
	}
	defer rows.Close()

	var used []netip.Addr
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan peer address: %w", err)
		}
		a, err := netip.ParseAddr(raw)
		if err != nil {
			return nil, fmt.Errorf("peer address %q is corrupt: %w", raw, err)
		}
		used = append(used, a)
	}
	return used, rows.Err()
}

func (s *Store) Peers(ctx context.Context, instanceID int64) ([]wg.Peer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+peerColumns+` FROM wg_peers WHERE instance_id = ? ORDER BY id`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
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

func (s *Store) PeerCounts(ctx context.Context) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT instance_id, COUNT(*) FROM wg_peers GROUP BY instance_id`)
	if err != nil {
		return nil, fmt.Errorf("count peers: %w", err)
	}
	defer rows.Close()

	counts := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan peer count: %w", err)
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

func (s *Store) PeerByID(ctx context.Context, id int64) (*wg.Peer, error) {
	return scanPeer(s.db.QueryRowContext(ctx, `SELECT `+peerColumns+` FROM wg_peers WHERE id = ?`, id))
}

func (s *Store) UpdatePeer(ctx context.Context, p wg.Peer) (*wg.Peer, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE wg_peers SET name = ?, enabled = ?, data_limit = ?, limit_period = ?, expires_at = ?, external_id = ?,
			metadata = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.Enabled, p.DataLimit, string(p.LimitPeriod.OrDefault()), nullTime(p.ExpiresAt), p.ExternalID,
		string(p.Metadata), time.Now().Unix(), p.ID)
	if err != nil {
		return nil, writeError("update peer", err)
	}
	if err := expectOneRow(res, "update peer"); err != nil {
		return nil, err
	}
	return s.PeerByID(ctx, p.ID)
}

func (s *Store) DeletePeer(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM wg_peers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete peer: %w", err)
	}
	return expectOneRow(res, "delete peer")
}

func scanInstance(row rowScanner) (*wg.Instance, error) {
	var (
		in                                  wg.Instance
		address, priv, pub, dns, allowedIPs string
		created, updated                    int64
	)
	err := row.Scan(&in.ID, &in.NodeID, &in.Name, &address, &in.ListenPort, &priv, &pub, &in.Endpoint,
		&dns, &in.MTU, &in.PersistentKeepalive, &allowedIPs, &in.Country, &in.City, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan instance: %w", err)
	}

	var errs [5]error
	in.Address, errs[0] = netip.ParsePrefix(address)
	in.PrivateKey, errs[1] = wg.ParseKey(priv)
	in.PublicKey, errs[2] = wg.ParseKey(pub)
	in.DNS, errs[3] = parseList(dns, netip.ParseAddr)
	in.ClientAllowedIPs, errs[4] = parseList(allowedIPs, netip.ParsePrefix)
	if err := errors.Join(errs[:]...); err != nil {
		return nil, fmt.Errorf("instance %d has corrupt data: %w", in.ID, err)
	}

	in.CreatedAt = time.Unix(created, 0).UTC()
	in.UpdatedAt = time.Unix(updated, 0).UTC()
	return &in, nil
}

func scanPeer(row rowScanner) (*wg.Peer, error) {
	var (
		p                       wg.Peer
		address, priv, pub, psk string
		metadata, period        string
		reset                   sql.NullInt64
		expires, handshake      sql.NullInt64
		created, updated        int64
	)
	err := row.Scan(&p.ID, &p.InstanceID, &p.Name, &address, &priv, &pub, &psk,
		&p.Enabled, &p.DataLimit, &period, &reset, &expires, &handshake, &p.ExternalID, &metadata, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan peer: %w", err)
	}

	var errs [4]error
	p.Address, errs[0] = netip.ParseAddr(address)
	if priv != "" {
		p.PrivateKey, errs[1] = wg.ParseKey(priv)
	}
	p.PublicKey, errs[2] = wg.ParseKey(pub)
	p.PresharedKey, errs[3] = wg.ParseKey(psk)
	if err := errors.Join(errs[:]...); err != nil {
		return nil, fmt.Errorf("peer %d has corrupt data: %w", p.ID, err)
	}

	if metadata != "" {
		p.Metadata = json.RawMessage(metadata)
	}
	p.LimitPeriod = wg.LimitPeriod(period)
	p.UsageResetAt = timeOf(reset)
	p.ExpiresAt = timeOf(expires)
	p.LastHandshake = timeOf(handshake)
	p.CreatedAt = time.Unix(created, 0).UTC()
	p.UpdatedAt = time.Unix(updated, 0).UTC()
	return &p, nil
}

func privateKeyText(p wg.Peer) string {
	if p.KeyOnClient() {
		return ""
	}
	return p.PrivateKey.String()
}

func nullTime(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}

func timeOf(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0).UTC()
	return &t
}

func expectOneRow(res sql.Result, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func joinList[T fmt.Stringer](items []T) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = it.String()
	}
	return strings.Join(parts, ",")
}

// Non-nil so the API encodes [] rather than null.
func parseList[T any](s string, parse func(string) (T, error)) ([]T, error) {
	out := []T{}
	if s == "" {
		return out, nil
	}
	for part := range strings.SplitSeq(s, ",") {
		v, err := parse(part)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
