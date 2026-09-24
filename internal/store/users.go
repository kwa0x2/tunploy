package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type User struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NormalizeEmail is applied on both write and lookup so addresses match
// regardless of how they were typed.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func (s *Store) CreateUser(ctx context.Context, name, email, passwordHash string) (*User, error) {
	now := time.Now().Unix()
	email = NormalizeEmail(email)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (name, email, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		name, email, passwordHash, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return s.userFromInsert(res, name, email, passwordHash, now)
}

// CreateFirstUser creates the administrator only while the table is empty.
// The emptiness check lives inside the INSERT because a separate count would
// let two concurrent requests each believe they are the first.
func (s *Store) CreateFirstUser(ctx context.Context, name, email, passwordHash string) (*User, error) {
	now := time.Now().Unix()
	email = NormalizeEmail(email)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (name, email, password_hash, created_at, updated_at)
		 SELECT ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
		name, email, passwordHash, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("create first user: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("create first user: %w", err)
	}
	if affected == 0 {
		return nil, ErrDuplicate
	}
	return s.userFromInsert(res, name, email, passwordHash, now)
}

func (s *Store) userFromInsert(res sql.Result, name, email, passwordHash string, now int64) (*User, error) {
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read inserted user id: %w", err)
	}
	return &User{
		ID:           id,
		Name:         name,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    time.Unix(now, 0).UTC(),
		UpdatedAt:    time.Unix(now, 0).UTC(),
	}, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, name, email, password_hash, created_at, updated_at FROM users WHERE email = ?`,
		NormalizeEmail(email)))
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, name, email, password_hash, created_at, updated_at FROM users WHERE id = ?`, id))
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	var u User
	var created, updated int64
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	u.UpdatedAt = time.Unix(updated, 0).UTC()
	return &u, nil
}
