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
	TOTPSecret   string    `json:"-"`
	TOTPLastStep int64     `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

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

// The emptiness check is inside the INSERT so two requests can't both be first.
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
		`SELECT `+userColumns+` FROM users WHERE email = ?`,
		NormalizeEmail(email)))
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	return s.updateUser(ctx, "update password",
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, time.Now().Unix(), id)
}

func (s *Store) EnableTOTP(ctx context.Context, id int64, secret string, usedStep int64) error {
	return s.updateUser(ctx, "enable totp",
		`UPDATE users SET totp_secret = ?, totp_last_step = ?, updated_at = ? WHERE id = ?`,
		secret, usedStep, time.Now().Unix(), id)
}

func (s *Store) DisableTOTP(ctx context.Context, id int64) error {
	return s.updateUser(ctx, "disable totp",
		`UPDATE users SET totp_secret = '', totp_last_step = 0, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), id)
}

// ClaimTOTPStep reports false when the step, or a later one, was already used,
// so a code seen over someone's shoulder can't be replayed within its window.
func (s *Store) ClaimTOTPStep(ctx context.Context, id, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, id, step)
	if err != nil {
		return false, fmt.Errorf("claim totp step: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim totp step: %w", err)
	}
	return n == 1, nil
}

func (s *Store) updateUser(ctx context.Context, what, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

const userColumns = `id, name, email, password_hash, totp_secret, totp_last_step, created_at, updated_at`

func (s *Store) scanUser(row rowScanner) (*User, error) {
	var u User
	var created, updated int64
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.TOTPLastStep, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	u.UpdatedAt = time.Unix(updated, 0).UTC()
	return &u, nil
}
