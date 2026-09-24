// Package wg models WireGuard instances and peers and renders their configs.
package wg

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// Key is a Curve25519 key in the raw 32-byte form WireGuard uses.
type Key [32]byte

var ErrInvalidKey = errors.New("wg: key must be 32 bytes encoded as base64")

// GeneratePrivateKey clamps the scalar the way `wg genkey` does, so exported
// keys are byte-for-byte what other WireGuard tools produce.
func GeneratePrivateKey() Key {
	var k Key
	rand.Read(k[:])
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	return k
}

func GeneratePresharedKey() Key {
	var k Key
	rand.Read(k[:])
	return k
}

func (k Key) PublicKey() Key {
	priv, err := ecdh.X25519().NewPrivateKey(k[:])
	if err != nil {
		// Only a wrong length fails, which [32]byte rules out.
		panic(err)
	}
	var pub Key
	copy(pub[:], priv.PublicKey().Bytes())
	return pub
}

func (k Key) IsZero() bool { return k == Key{} }

func (k Key) String() string { return base64.StdEncoding.EncodeToString(k[:]) }

func ParseKey(s string) (Key, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) != len(Key{}) {
		return Key{}, ErrInvalidKey
	}
	var k Key
	copy(k[:], raw)
	return k, nil
}

func (k Key) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

func (k *Key) UnmarshalText(text []byte) error {
	parsed, err := ParseKey(string(text))
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}
