package auth

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

const (
	MinPasswordLength = 8
	maxPasswordLength = 256
	maxNameLength     = 80
)

func ValidateAccount(name, email, password string) map[string]string {
	fields := map[string]string{}

	switch name = strings.TrimSpace(name); {
	case name == "":
		fields["name"] = "name is required"
	case utf8.RuneCountInString(name) > maxNameLength:
		fields["name"] = "name must be at most 80 characters"
	}

	switch email = strings.TrimSpace(email); {
	case email == "":
		fields["email"] = "email is required"
	case !validEmail(email):
		fields["email"] = "email is not a valid address"
	}

	if msg := CheckPassword(password); msg != "" {
		fields["password"] = msg
	}
	return fields
}

// mail.ParseAddress alone accepts "Ada <ada@example.com>".
func validEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

func CheckPassword(p string) string {
	switch {
	case p == "":
		return "password is required"
	case len(p) < MinPasswordLength:
		return "password must be at least 8 characters"
	case len(p) > maxPasswordLength:
		return "password must be at most 256 characters"
	}
	return ""
}
