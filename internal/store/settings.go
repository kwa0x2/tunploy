package store

import (
	"context"
	"fmt"
)

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SaveSettings(ctx context.Context, values map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	defer tx.Rollback()

	for k, v := range values {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return fmt.Errorf("save setting %s: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}
