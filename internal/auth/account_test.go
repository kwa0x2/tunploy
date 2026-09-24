package auth

import "testing"

func TestValidateAccount(t *testing.T) {
	if fields := ValidateAccount("Kwa", " admin@example.com ", "hunter2hunter2"); len(fields) != 0 {
		t.Fatalf("valid account rejected: %v", fields)
	}

	for name, tc := range map[string]struct {
		name, email, password, field string
	}{
		"missing name":   {"  ", "admin@example.com", "hunter2hunter2", "name"},
		"long name":      {string(make([]rune, 81)), "admin@example.com", "hunter2hunter2", "name"},
		"empty email":    {"Kwa", "", "hunter2hunter2", "email"},
		"bad email":      {"Kwa", "not-an-email", "hunter2hunter2", "email"},
		"display name":   {"Kwa", "Kwa <admin@example.com>", "hunter2hunter2", "email"},
		"short password": {"Kwa", "admin@example.com", "short", "password"},
		"long password":  {"Kwa", "admin@example.com", string(make([]byte, 257)), "password"},
	} {
		t.Run(name, func(t *testing.T) {
			fields := ValidateAccount(tc.name, tc.email, tc.password)
			if fields[tc.field] == "" || len(fields) != 1 {
				t.Fatalf("want only a %s error, got %v", tc.field, fields)
			}
		})
	}
}
