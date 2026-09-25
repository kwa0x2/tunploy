package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Event struct {
	ID           int64     `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	Kind         string    `json:"kind"`
	InstanceID   int64     `json:"instance_id,omitempty"`
	InstanceName string    `json:"instance_name,omitempty"`
	PeerID       int64     `json:"peer_id,omitempty"`
	PeerName     string    `json:"peer_name,omitempty"`
	NodeName     string    `json:"node_name,omitempty"`
	IP           string    `json:"ip,omitempty"`
	Country      string    `json:"country,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	// "" for the admin, otherwise "api:<key name>".
	Actor string `json:"actor,omitempty"`
}

const (
	CategoryConnection = "connection"
	CategoryAuth       = "auth"
	CategoryChange     = "change"
)

type EventFilter struct {
	Before int64
	After  int64
	// Oldest first, for pollers; newest first otherwise.
	Ascending  bool
	Limit      int
	InstanceID int64
	PeerID     int64
	Category   string
	Kinds      []string
	// Kind families such as "device", matched as "device.%".
	Families []string
}

const (
	connectionKinds = `kind IN ('device.connected', 'device.disconnected')`
	authKinds       = `kind LIKE 'auth.%'`
)

func (s *Store) AddEvent(ctx context.Context, e Event) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (created_at, kind, instance_id, instance_name, peer_id, peer_name, node_name, ip, country,
			detail, actor)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.CreatedAt.Unix(), e.Kind, nullID(e.InstanceID), e.InstanceName, nullID(e.PeerID), e.PeerName, e.NodeName,
		e.IP, e.Country, e.Detail, e.Actor)
	if err != nil {
		return fmt.Errorf("add event: %w", err)
	}
	return nil
}

func (s *Store) Events(ctx context.Context, f EventFilter) ([]Event, error) {
	where := []string{"1 = 1"}
	var args []any
	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	if f.After > 0 {
		where = append(where, "id > ?")
		args = append(args, f.After)
	}
	order := "DESC"
	if f.Ascending {
		order = "ASC"
	}
	if len(f.Kinds) > 0 {
		where = append(where, "kind IN (?"+strings.Repeat(", ?", len(f.Kinds)-1)+")")
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if len(f.Families) > 0 {
		var ors []string
		for _, fam := range f.Families {
			ors = append(ors, "kind LIKE ?")
			args = append(args, fam+".%")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	if f.InstanceID > 0 {
		where = append(where, "instance_id = ?")
		args = append(args, f.InstanceID)
	}
	if f.PeerID > 0 {
		where = append(where, "peer_id = ?")
		args = append(args, f.PeerID)
	}
	switch f.Category {
	case CategoryConnection:
		where = append(where, connectionKinds)
	case CategoryAuth:
		where = append(where, authKinds)
	case CategoryChange:
		where = append(where, "NOT "+connectionKinds, "NOT "+authKinds)
	}
	args = append(args, f.Limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, created_at, kind, instance_id, instance_name, peer_id, peer_name, node_name, ip, country, detail, actor
		 FROM events WHERE `+strings.Join(where, " AND ")+` ORDER BY id `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var e Event
		var at int64
		var instanceID, peerID sql.NullInt64
		if err := rows.Scan(&e.ID, &at, &e.Kind, &instanceID, &e.InstanceName, &peerID, &e.PeerName, &e.NodeName,
			&e.IP, &e.Country, &e.Detail, &e.Actor); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.CreatedAt = time.Unix(at, 0).UTC()
		e.InstanceID, e.PeerID = instanceID.Int64, peerID.Int64
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) DeleteEventsBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, t.Unix())
	if err != nil {
		return 0, fmt.Errorf("delete old events: %w", err)
	}
	return res.RowsAffected()
}

func nullID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id > 0}
}
