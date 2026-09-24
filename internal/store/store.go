// Package store owns Tunploy's SQLite database.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"modernc.org/sqlite" // pure-Go driver, keeps the binary cgo-free
)

var (
	ErrNotFound  = errors.New("store: not found")
	ErrDuplicate = errors.New("store: already exists")
)

type DuplicateError struct {
	Column string
}

func (e *DuplicateError) Error() string { return "store: duplicate " + e.Column }

func (e *DuplicateError) Is(target error) bool { return target == ErrDuplicate }

const sqliteConstraintUnique = 2067

func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteConstraintUnique
}

// In a composite index the last column is the one that clashed.
func asDuplicate(err error) *DuplicateError {
	if !isUniqueViolation(err) {
		return nil
	}
	_, cols, ok := strings.Cut(err.Error(), "UNIQUE constraint failed: ")
	if !ok {
		return &DuplicateError{}
	}
	cols, _, _ = strings.Cut(cols, " (")
	last := strings.TrimSpace(cols[strings.LastIndex(cols, ",")+1:])
	_, col, _ := strings.Cut(last, ".")
	return &DuplicateError{Column: col}
}

func writeError(op string, err error) error {
	if dup := asDuplicate(err); dup != nil {
		return dup
	}
	return fmt.Errorf("%s: %w", op, err)
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite serialises writes anyway; one connection rules out SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }
