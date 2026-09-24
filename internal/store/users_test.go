package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// A count-then-insert version of this lets concurrent callers each believe the
// table is empty, which produced up to 12 administrators in 30 attempts.
func TestCreateFirstUserIsAtomic(t *testing.T) {
	st := newTestStore(t)

	const attempts = 30
	var wg sync.WaitGroup
	start := make(chan struct{})
	created := make([]bool, attempts)

	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := st.CreateFirstUser(context.Background(), "Admin",
				fmt.Sprintf("admin%d@example.com", i), "hash")
			switch {
			case err == nil:
				created[i] = true
			case errors.Is(err, ErrDuplicate):
			default:
				t.Errorf("attempt %d: unexpected error: %v", i, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	wins := 0
	for _, ok := range created {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("want exactly one caller to succeed, got %d", wins)
	}

	n, err := st.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 user in the database, got %d", n)
	}
}

func TestCreateFirstUserRefusesOnceSeeded(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.CreateFirstUser(context.Background(), "Kwa", "kwa@example.com", "hash"); err != nil {
		t.Fatalf("first admin: %v", err)
	}
	_, err := st.CreateFirstUser(context.Background(), "Other", "other@example.com", "hash")
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("want ErrDuplicate for a second admin, got %v", err)
	}
}

func TestEmailIsNormalisedOnWriteAndLookup(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.CreateFirstUser(context.Background(), "Kwa", "  KWA@Example.COM ", "hash"); err != nil {
		t.Fatalf("create: %v", err)
	}

	user, err := st.UserByEmail(context.Background(), "kwa@example.com")
	if err != nil {
		t.Fatalf("lookup by normalised address: %v", err)
	}
	if user.Email != "kwa@example.com" {
		t.Fatalf("stored email not normalised: %q", user.Email)
	}
	if user.Name != "Kwa" {
		t.Fatalf("name not stored: %q", user.Name)
	}
}

func TestUserLookupsReportMissingRows(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.UserByEmail(context.Background(), "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByEmail: want ErrNotFound, got %v", err)
	}
	if _, err := st.UserByID(context.Background(), 404); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID: want ErrNotFound, got %v", err)
	}
}
