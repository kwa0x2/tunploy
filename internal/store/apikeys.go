package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrIdempotencyMismatch = errors.New("store: idempotency key was used for a different request")
	ErrIdempotencyBusy     = errors.New("store: a request with this idempotency key is still running")
)

type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	TokenHash  string     `json:"-"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP string     `json:"last_used_ip,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (k APIKey) Expired(now time.Time) bool {
	return k.ExpiresAt != nil && !now.Before(*k.ExpiresAt)
}

const apiKeyColumns = `id, name, prefix, token_hash, scopes, expires_at, last_used_at, last_used_ip, created_at`

func (s *Store) CreateAPIKey(ctx context.Context, k APIKey) (*APIKey, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (name, prefix, token_hash, scopes, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		k.Name, k.Prefix, k.TokenHash, strings.Join(k.Scopes, ","), nullTime(k.ExpiresAt), now)
	if err != nil {
		return nil, writeError("create api key", err)
	}
	if k.ID, err = res.LastInsertId(); err != nil {
		return nil, fmt.Errorf("read inserted api key id: %w", err)
	}
	k.CreatedAt = time.Unix(now, 0).UTC()
	return &k, nil
}

func (s *Store) APIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	keys := []APIKey{}
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *k)
	}
	return keys, rows.Err()
}

func (s *Store) APIKeyByID(ctx context.Context, id int64) (*APIKey, error) {
	return scanAPIKey(s.db.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys WHERE id = ?`, id))
}

func (s *Store) APIKeyByToken(ctx context.Context, tokenHash string) (*APIKey, error) {
	return scanAPIKey(s.db.QueryRowContext(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys WHERE token_hash = ?`, tokenHash))
}

func (s *Store) TouchAPIKey(ctx context.Context, id int64, ip string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ?, last_used_ip = ? WHERE id = ?`,
		at.Unix(), ip, id)
	if err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}

func (s *Store) DeleteAPIKey(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	return expectOneRow(res, "delete api key")
}

func scanAPIKey(row rowScanner) (*APIKey, error) {
	var (
		k             APIKey
		scopes        string
		expires, used sql.NullInt64
		created       int64
	)
	err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.TokenHash, &scopes, &expires, &used, &k.LastUsedIP, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan api key: %w", err)
	}
	k.Scopes = []string{}
	if scopes != "" {
		k.Scopes = strings.Split(scopes, ",")
	}
	k.ExpiresAt = timeOf(expires)
	k.LastUsedAt = timeOf(used)
	k.CreatedAt = time.Unix(created, 0).UTC()
	return &k, nil
}

type StoredResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

const (
	IdempotencyTTL = 24 * time.Hour
	// A reply this late means the panel stopped mid-request.
	idempotencyStale = 5 * time.Minute
)

// ClaimIdempotencyKey returns the stored reply to replay, or nil when the
// caller should run the request and then FinishIdempotencyKey.
func (s *Store) ClaimIdempotencyKey(ctx context.Context, keyID int64, key, requestHash string, now time.Time) (*StoredResponse, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin idempotency: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`DELETE FROM api_idempotency WHERE api_key_id = ? AND key = ? AND (created_at < ? OR (status = 0 AND created_at < ?))`,
		keyID, key, now.Add(-IdempotencyTTL).Unix(), now.Add(-idempotencyStale).Unix())
	if err != nil {
		return nil, fmt.Errorf("expire idempotency key: %w", err)
	}

	var (
		hash string
		out  StoredResponse
	)
	err = tx.QueryRowContext(ctx,
		`SELECT request_hash, status, content_type, body FROM api_idempotency WHERE api_key_id = ? AND key = ?`,
		keyID, key).Scan(&hash, &out.Status, &out.ContentType, &out.Body)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO api_idempotency (api_key_id, key, request_hash, created_at) VALUES (?, ?, ?, ?)`,
			keyID, key, requestHash, now.Unix()); err != nil {
			return nil, fmt.Errorf("claim idempotency key: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("claim idempotency key: %w", err)
		}
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("load idempotency key: %w", err)
	case hash != requestHash:
		return nil, ErrIdempotencyMismatch
	case out.Status == 0:
		return nil, ErrIdempotencyBusy
	}
	return &out, nil
}

func (s *Store) FinishIdempotencyKey(ctx context.Context, keyID int64, key string, r StoredResponse) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_idempotency SET status = ?, content_type = ?, body = ? WHERE api_key_id = ? AND key = ?`,
		r.Status, r.ContentType, r.Body, keyID, key)
	if err != nil {
		return fmt.Errorf("store idempotent reply: %w", err)
	}
	return nil
}

// ReleaseIdempotencyKey lets a failed request be tried again with the same key.
func (s *Store) ReleaseIdempotencyKey(ctx context.Context, keyID int64, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_idempotency WHERE api_key_id = ? AND key = ?`, keyID, key)
	if err != nil {
		return fmt.Errorf("release idempotency key: %w", err)
	}
	return nil
}

func (s *Store) DeleteIdempotencyKeysBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_idempotency WHERE created_at < ?`, t.Unix())
	if err != nil {
		return 0, fmt.Errorf("delete old idempotency keys: %w", err)
	}
	return res.RowsAffected()
}
