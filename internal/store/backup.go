package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

var (
	ErrNewerBackup   = errors.New("store: backup was made by a newer version of tunploy")
	ErrInvalidBackup = errors.New("store: backup is not a tunploy database")
)

// Sessions would bring back logged-out logins; counters describe containers
// that a restore rebuilds from scratch.
var notRestored = []string{"sessions", "wg_peer_counters"}

// Snapshot writes a consistent copy of the database to path, without sessions.
func (s *Store) Snapshot(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	db, err := openRaw(path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	return db.Close()
}

// Restore replaces all data with the database at path, which it migrates
// first. Settings for which keep returns true survive, and everyone is
// logged out.
func (s *Store) Restore(ctx context.Context, path string, keep func(key string) bool) error {
	if err := checkBackup(ctx, path); err != nil {
		return err
	}
	bk, err := Open(path)
	if err != nil {
		return fmt.Errorf("upgrade backup: %w", err)
	}
	bk.Close()

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS backup`, path); err != nil {
		return fmt.Errorf("attach backup: %w", err)
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `DETACH DATABASE backup`)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	defer tx.Rollback()

	kept, err := keptSettings(ctx, tx, keep)
	if err != nil {
		return err
	}
	tables, err := tableNames(ctx, tx)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	// Every table is emptied before any is filled, or a cascading delete
	// would wipe rows that were just copied in.
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM main.`+quoteIdent(t)); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}
	for _, t := range tables {
		if slices.Contains(notRestored, t) {
			continue
		}
		cols, err := columns(ctx, tx, t)
		if err != nil {
			return err
		}
		list := strings.Join(cols, ", ")
		q := `INSERT INTO main.` + quoteIdent(t) + ` (` + list + `) SELECT ` + list + ` FROM backup.` + quoteIdent(t)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
	}
	for k, v := range kept {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return fmt.Errorf("keep setting %s: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	return nil
}

func openRaw(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+url.PathEscape(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func checkBackup(ctx context.Context, path string) error {
	db, err := openRaw(path)
	if err != nil {
		return err
	}
	defer db.Close()

	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil || result != "ok" {
		return ErrInvalidBackup
	}
	rows, err := db.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return ErrInvalidBackup
	}
	defer rows.Close()

	known, err := migrationNames()
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return ErrInvalidBackup
		}
		if !slices.Contains(known, name) {
			return ErrNewerBackup
		}
	}
	return rows.Err()
}

func keptSettings(ctx context.Context, tx *sql.Tx, keep func(string) bool) (map[string]string, error) {
	out := map[string]string{}
	if keep == nil {
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT key, value FROM main.settings`)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("read settings: %w", err)
		}
		if keep(k) {
			out[k] = v
		}
	}
	return out, rows.Err()
}

func tableNames(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT name FROM main.sqlite_master
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations'
		 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list tables: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// Named columns, since a table altered in a different order would shift SELECT *.
func columns(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?, 'main')`, table)
	if err != nil {
		return nil, fmt.Errorf("columns of %s: %w", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("columns of %s: %w", table, err)
		}
		out = append(out, quoteIdent(name))
	}
	return out, rows.Err()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
