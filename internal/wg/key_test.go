package wg

import (
	"encoding/hex"
	"errors"
	"testing"
)

// RFC 7748 section 6.1, Alice's key pair.
func TestPublicKeyMatchesRFC7748(t *testing.T) {
	var priv Key
	mustHex(t, priv[:], "77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	var want Key
	mustHex(t, want[:], "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")

	if got := priv.PublicKey(); got != want {
		t.Fatalf("public key = %x, want %x", got, want)
	}
}

func TestGeneratePrivateKeyIsClamped(t *testing.T) {
	for range 50 {
		k := GeneratePrivateKey()
		if k[0]&7 != 0 || k[31]&128 != 0 || k[31]&64 == 0 {
			t.Fatalf("key %x is not clamped", k)
		}
	}
}

func TestGeneratedKeysDiffer(t *testing.T) {
	if GeneratePrivateKey() == GeneratePrivateKey() {
		t.Fatal("two generated private keys are equal")
	}
	if GeneratePresharedKey() == GeneratePresharedKey() {
		t.Fatal("two generated preshared keys are equal")
	}
}

func TestKeyTextRoundTrip(t *testing.T) {
	k := GeneratePrivateKey()
	text, err := k.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != 44 {
		t.Fatalf("encoded key is %d characters, want 44", len(text))
	}

	var back Key
	if err := back.UnmarshalText(text); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != k {
		t.Fatal("round trip changed the key")
	}
}

func TestParseKeyRejectsBadInput(t *testing.T) {
	for _, in := range []string{"", "not base64!", "AAAA", "YWJj" + "YWJjYWJjYWJjYWJjYWJjYWJjYWJjYWJjYWJjYWJjYWJjYWJj"} {
		if _, err := ParseKey(in); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("ParseKey(%q): want ErrInvalidKey, got %v", in, err)
		}
	}
}

func mustHex(t *testing.T, dst []byte, s string) {
	t.Helper()
	if _, err := hex.Decode(dst, []byte(s)); err != nil {
		t.Fatal(err)
	}
}
