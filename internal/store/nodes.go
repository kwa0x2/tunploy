package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Node struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	// authorized_keys format.
	HostKey    string     `json:"host_key"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at,omitzero"`
	UpdatedAt  time.Time  `json:"updated_at,omitzero"`
}

const nodeColumns = `id, name, host, port, username, host_key, last_seen_at, created_at, updated_at`

func (s *Store) CreateNode(ctx context.Context, n Node) (*Node, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO nodes (name, host, port, username, host_key, last_seen_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name, n.Host, n.Port, n.Username, n.HostKey, now, now, now)
	if err != nil {
		return nil, writeError("create node", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read inserted node id: %w", err)
	}
	return s.NodeByID(ctx, id)
}

func (s *Store) Nodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nodeColumns+` FROM nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	nodes := []Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, *n)
	}
	return nodes, rows.Err()
}

func (s *Store) NodeByID(ctx context.Context, id int64) (*Node, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = ?`, id))
}

// Only the name: the rest was checked against the machine when it was added.
func (s *Store) RenameNode(ctx context.Context, id int64, name string) (*Node, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET name = ?, updated_at = ? WHERE id = ?`, name, time.Now().Unix(), id)
	if err != nil {
		return nil, writeError("rename node", err)
	}
	if err := expectOneRow(res, "rename node"); err != nil {
		return nil, err
	}
	return s.NodeByID(ctx, id)
}

func (s *Store) TouchNode(ctx context.Context, id int64, seen time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET last_seen_at = ? WHERE id = ?`, seen.Unix(), id)
	if err != nil {
		return fmt.Errorf("touch node: %w", err)
	}
	return nil
}

// DeleteNode takes the node's servers with it; their peers follow by cascade.
func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM wg_instances WHERE node_id = ?`, id); err != nil {
		return fmt.Errorf("delete node servers: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	if err := expectOneRow(res, "delete node"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	return nil
}

func scanNode(row rowScanner) (*Node, error) {
	var (
		n                Node
		seen             sql.NullInt64
		created, updated int64
	)
	err := row.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &n.Username, &n.HostKey, &seen, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan node: %w", err)
	}
	if seen.Valid {
		t := time.Unix(seen.Int64, 0).UTC()
		n.LastSeenAt = &t
	}
	n.CreatedAt = time.Unix(created, 0).UTC()
	n.UpdatedAt = time.Unix(updated, 0).UTC()
	return &n, nil
}
