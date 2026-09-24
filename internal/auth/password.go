// Package auth handles password hashing, session tokens and login throttling.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

var ErrPasswordMismatch = errors.New("auth: password does not match")

// OWASP's low-memory argon2id profile, sized for small VPS boxes.
const (
	argonMemory  = 19456
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword returns ErrPasswordMismatch when the password is wrong, and
// a different error when the stored hash itself is unusable.
func VerifyPassword(encoded, password string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return errors.New("auth: malformed password hash")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return fmt.Errorf("auth: unreadable hash version: %w", err)
	}
	if version != argon2.Version {
		return fmt.Errorf("auth: unsupported argon2 version %d", version)
	}

	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return fmt.Errorf("auth: unreadable hash parameters: %w", err)
	}

	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("auth: unreadable hash salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("auth: unreadable hash key: %w", err)
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

// DummyHash is verified against when no user matches, so a failed login costs
// the same time whether or not the account exists.
var DummyHash = mustHash("tunploy-timing-equaliser")

func mustHash(password string) string {
	h, err := HashPassword(password)
	if err != nil {
		panic("auth: cannot hash dummy password: " + err.Error())
	}
	return h
}
