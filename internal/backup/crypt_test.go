package backup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
)

func init() { kdfTime, kdfMemory = 1, 1024 }

func encrypt(t *testing.T, plain []byte, passphrase string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := newEncryptWriter(&buf, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	// Uneven writes, so chunk edges fall inside them.
	for rest := plain; len(rest) > 0; {
		n := min(len(rest), 7000)
		w.Write(rest[:n])
		rest = rest[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decrypt(sealed []byte, passphrase string) ([]byte, error) {
	br := bufio.NewReader(bytes.NewReader(sealed))
	if !isEncrypted(br) {
		return nil, errors.New("not recognised as encrypted")
	}
	r, err := newDecryptReader(br, passphrase)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestEncryptRoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3 * chunkSize} {
		plain := make([]byte, size)
		rand.Read(plain)
		sealed := encrypt(t, plain, "correct horse battery")
		got, err := decrypt(sealed, "correct horse battery")
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("size %d: %v", size, err)
		}
		if _, err := decrypt(sealed, "wrong horse battery"); !errors.Is(err, ErrWrongPassphrase) {
			t.Fatalf("size %d with a wrong passphrase: %v", size, err)
		}
	}
}

func TestEncryptDetectsTampering(t *testing.T) {
	const pass = "correct horse battery"
	plain := make([]byte, 2*chunkSize+100)
	sealed := encrypt(t, plain, pass)
	sealedChunk := chunkSize + tagSize

	flipped := bytes.Clone(sealed)
	flipped[headerSize+10] ^= 1

	swapped := bytes.Clone(sealed)
	first := bytes.Clone(swapped[headerSize : headerSize+sealedChunk])
	copy(swapped[headerSize:], swapped[headerSize+sealedChunk:headerSize+2*sealedChunk])
	copy(swapped[headerSize+sealedChunk:], first)

	cases := map[string][]byte{
		"flipped byte":        flipped,
		"swapped chunks":      swapped,
		"cut at a chunk edge": sealed[:headerSize+2*sealedChunk],
		"cut mid chunk":       sealed[:len(sealed)-5],
	}
	for name, data := range cases {
		if _, err := decrypt(data, pass); err == nil {
			t.Errorf("%s: decrypted without an error", name)
		}
	}

	if _, err := decrypt(sealed, ""); !errors.Is(err, ErrPassphraseRequired) {
		t.Errorf("no passphrase: %v", err)
	}
	greedy := bytes.Clone(sealed)
	binary.BigEndian.PutUint32(greedy[len(cryptMagic)+1+4:], 1<<30)
	if _, err := decrypt(greedy, pass); !errors.Is(err, ErrInvalidArchive) {
		t.Errorf("huge key memory in header: %v", err)
	}
}

func TestEncryptedArchive(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	svc.Load(PassphraseSetting("correct horse battery"))

	f, err := svc.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Remove()
	if !IsEncryptedName(f.Name) || !IsFileName(f.Name) {
		t.Fatalf("name = %s", f.Name)
	}

	body, _ := os.ReadFile(f.Path)
	if _, err := Extract(bytes.NewReader(body), t.TempDir(), ""); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("no passphrase: %v", err)
	}
	if _, err := Extract(bytes.NewReader(body), t.TempDir(), "wrong horse battery"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	ex, err := Extract(bytes.NewReader(body), t.TempDir(), "correct horse battery")
	if err != nil || ex.Manifest.Version != "v9.9.9" {
		t.Fatalf("extract = %+v, %v", ex, err)
	}
}
