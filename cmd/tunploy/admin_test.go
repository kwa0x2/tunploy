package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCreateAdmin(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if _, err := createAdmin(ctx, st, "Kwa", "not-an-email", "short"); err == nil ||
		!strings.Contains(err.Error(), "email") || !strings.Contains(err.Error(), "password") {
		t.Fatalf("want both field problems reported, got %v", err)
	}

	user, err := createAdmin(ctx, st, " Kwa ", "Admin@Example.com", "hunter2hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if user.Name != "Kwa" || user.Email != "admin@example.com" {
		t.Fatalf("user = %+v", user)
	}

	if _, err := createAdmin(ctx, st, "Other", "other@example.com", "hunter2hunter2"); !errors.Is(err, errAdminExists) {
		t.Fatalf("second admin: want errAdminExists, got %v", err)
	}
}

func TestResetPassword(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user, err := createAdmin(ctx, st, "Kwa", "admin@example.com", "hunter2hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "session-hash", user.ID, "test", user.CreatedAt.AddDate(1, 0, 0)); err != nil {
		t.Fatal(err)
	}

	if err := resetPassword(ctx, st, "nobody@example.com", "new-password-1"); err == nil {
		t.Fatal("unknown email must fail")
	}
	if err := resetPassword(ctx, st, "admin@example.com", "short"); err == nil {
		t.Fatal("short password must fail")
	}
	if err := resetPassword(ctx, st, "ADMIN@example.com", "new-password-1"); err != nil {
		t.Fatal(err)
	}

	got, err := st.UserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.VerifyPassword(got.PasswordHash, "new-password-1"); err != nil {
		t.Fatalf("new password does not verify: %v", err)
	}
	if _, err := st.SessionByTokenHash(ctx, "session-hash"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("sessions must be signed out, got %v", err)
	}
}

func TestDisableTOTP(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user, err := createAdmin(ctx, st, "Kwa", "admin@example.com", "hunter2hunter2")
	if err != nil {
		t.Fatal(err)
	}

	if err := disableTOTP(ctx, st, "admin@example.com"); err == nil {
		t.Fatal("disabling 2FA that is off must say so")
	}
	if err := st.EnableTOTP(ctx, user.ID, "SECRET", 1); err != nil {
		t.Fatal(err)
	}
	if err := disableTOTP(ctx, st, "nobody@example.com"); err == nil {
		t.Fatal("unknown email must fail")
	}
	if err := disableTOTP(ctx, st, "ADMIN@example.com"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.UserByID(ctx, user.ID); got.TOTPSecret != "" {
		t.Fatal("secret survived disable-2fa")
	}
}
