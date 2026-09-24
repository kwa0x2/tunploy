package auth

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1, cut to the last six digits.
func TestTOTPMatchesRFCVectors(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	for unix, want := range map[int64]string{
		59:         "287082",
		1111111109: "081804",
		1111111111: "050471",
		1234567890: "005924",
		2000000000: "279037",
	} {
		step, ok := VerifyTOTP(secret, want, time.Unix(unix, 0), 0)
		if !ok {
			t.Errorf("t=%d: code %s rejected", unix, want)
			continue
		}
		if step != unix/totpPeriod {
			t.Errorf("t=%d: matched step %d, want %d", unix, step, unix/totpPeriod)
		}
	}
}

func TestTOTPWindowAndReplay(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := b32.DecodeString(secret)
	now := time.Unix(1_800_000_000, 0)
	step := now.Unix() / totpPeriod

	if _, ok := VerifyTOTP(secret, totpCode(key, step-1), now, 0); !ok {
		t.Error("the previous step must still be accepted, for clock drift")
	}
	if _, ok := VerifyTOTP(secret, totpCode(key, step-2), now, 0); ok {
		t.Error("a code two steps old must be rejected")
	}
	if _, ok := VerifyTOTP(secret, totpCode(key, step), now, step); ok {
		t.Error("a step that was already used must be rejected")
	}
	code := totpCode(key, step)
	if _, ok := VerifyTOTP(strings.ToLower(secret), code[:3]+" "+code[3:], now, 0); !ok {
		t.Error("spaces in the code and a lower-case secret must be tolerated")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := VerifyTOTP(secret, bad, now, 0); ok {
			t.Errorf("malformed code %q accepted", bad)
		}
	}
}

func TestTOTPURI(t *testing.T) {
	u, err := url.Parse(TOTPURI("Tunploy", "admin@example.com", "ABC"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" || u.Path != "/Tunploy:admin@example.com" {
		t.Fatalf("uri = %s", u)
	}
	if q := u.Query(); q.Get("secret") != "ABC" || q.Get("issuer") != "Tunploy" {
		t.Fatalf("query = %v", q)
	}
}
