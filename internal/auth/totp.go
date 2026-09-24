package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RFC 6238 with the parameters every authenticator app defaults to.
const (
	totpPeriod      = 30
	totpDigits      = 6
	totpSecretBytes = 20
	totpSkew        = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func NewTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	return b32.EncodeToString(raw), nil
}

// Guards against a client-chosen secret too short to be worth anything.
func ValidTOTPSecret(secret string) bool {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	return err == nil && len(key) >= 16
}

func TOTPURI(issuer, account, secret string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// VerifyTOTP returns the time step the code matched, so callers can refuse
// to accept the same step twice. Steps at or below after never match.
func VerifyTOTP(secret, code string, now time.Time, after int64) (int64, bool) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil || len(key) == 0 {
		return 0, false
	}
	code = strings.Join(strings.Fields(code), "")
	if len(code) != totpDigits {
		return 0, false
	}

	current := now.Unix() / totpPeriod
	for step := current - totpSkew; step <= current+totpSkew; step++ {
		if step <= after {
			continue
		}
		if hmac.Equal([]byte(totpCode(key, step)), []byte(code)) {
			return step, true
		}
	}
	return 0, false
}

func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("decode totp secret: %w", err)
	}
	return totpCode(key, t.Unix()/totpPeriod), nil
}

func totpCode(key []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, bin%1_000_000)
}
