package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if err := VerifyPassword(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("VerifyPassword with correct password: %v", err)
	}
}

func TestHashPasswordUsesFreshSalt(t *testing.T) {
	a, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical, salt is not random")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("right-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	err = VerifyPassword(hash, "wrong-password")
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("want ErrPasswordMismatch, got %v", err)
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"not argon":        "$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA",
		"missing sections": "$argon2id$v=19$m=19456,t=2,p=1",
		"bad salt":         "$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaA",
	}
	for name, hash := range cases {
		t.Run(name, func(t *testing.T) {
			err := VerifyPassword(hash, "whatever")
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if errors.Is(err, ErrPasswordMismatch) {
				t.Fatal("malformed hash must not be reported as a plain mismatch")
			}
		})
	}
}

func TestSessionTokenHashing(t *testing.T) {
	token, hash, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if token == "" || hash == "" {
		t.Fatal("token and hash must both be set")
	}
	if token == hash {
		t.Fatal("stored hash must differ from the token handed to the client")
	}
	if got := HashToken(token); got != hash {
		t.Fatalf("HashToken is not stable: %s != %s", got, hash)
	}

	other, _, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if other == token {
		t.Fatal("two tokens are identical, generator is not random")
	}
}
