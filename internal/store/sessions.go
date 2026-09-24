package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Session struct {
	TokenHash string
	UserID    int64
	UserAgent string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, userAgent string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, user_agent, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, userAgent, time.Now().Unix(), expiresAt.Unix())
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// UserBySessionToken resolves a session hash to its user, treating an expired
// session as absent.
func (s *Store) UserBySessionToken(ctx context.Context, tokenHash string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT u.id, u.email, u.password_hash, u.created_at, u.updated_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = ? AND s.expires_at > ?`,
		tokenHash, time.Now().Unix())
	return s.scanUser(row)
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return n, nil
}

func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	var created, expires int64
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, user_agent, created_at, expires_at FROM sessions WHERE token_hash = ?`,
		tokenHash).Scan(&sess.TokenHash, &sess.UserID, &sess.UserAgent, &created, &expires)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan session: %w", err)
	}
	sess.CreatedAt = time.Unix(created, 0).UTC()
	sess.ExpiresAt = time.Unix(expires, 0).UTC()
	return &sess, nil
}
