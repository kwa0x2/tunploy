package backup

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// An encrypted backup is a header followed by AES-256-GCM chunks. Each chunk's
// nonce counts up and its additional data marks the last one, so chunks can be
// neither reordered nor cut off unnoticed.
const (
	cryptMagic   = "TPBKENC"
	cryptVersion = 1
	chunkSize    = 64 << 10
	tagSize      = 16
	saltSize     = 16
	nonceSize    = 12
	headerSize   = len(cryptMagic) + 1 + 4 + 4 + 1 + saltSize + nonceSize

	kdfThreads = 1
	// A crafted header must not make the panel spend a gigabyte or a minute on one key.
	maxKDFMemory = 1 << 20
	maxKDFTime   = 10

	MinPassphraseLen = 12
)

// Variables so tests can derive keys cheaply; the header records what was used.
var (
	kdfTime   uint32 = 3
	kdfMemory uint32 = 64 << 10
)

var (
	ErrPassphraseRequired = errors.New("this backup is encrypted; enter its passphrase")
	ErrWrongPassphrase    = errors.New("the passphrase is wrong, or the backup is damaged")
)

type encryptWriter struct {
	w      io.Writer
	aead   cipher.AEAD
	header []byte
	nonce  [nonceSize]byte
	count  uint64
	buf    []byte
}

func newEncryptWriter(w io.Writer, passphrase string) (*encryptWriter, error) {
	header := make([]byte, 0, headerSize)
	header = append(header, cryptMagic...)
	header = append(header, cryptVersion)
	header = binary.BigEndian.AppendUint32(header, kdfTime)
	header = binary.BigEndian.AppendUint32(header, kdfMemory)
	header = append(header, kdfThreads)
	random := make([]byte, saltSize+nonceSize)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	header = append(header, random...)

	aead, err := newAEAD(passphrase, header)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	e := &encryptWriter{w: w, aead: aead, header: header, buf: make([]byte, 0, chunkSize)}
	copy(e.nonce[:], header[headerSize-nonceSize:])
	return e, nil
}

func (e *encryptWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		// A full chunk waits until more data proves it is not the last.
		if len(e.buf) == chunkSize {
			if err := e.seal(false); err != nil {
				return 0, err
			}
		}
		take := min(chunkSize-len(e.buf), len(p))
		e.buf = append(e.buf, p[:take]...)
		p = p[take:]
	}
	return n, nil
}

func (e *encryptWriter) Close() error { return e.seal(true) }

func (e *encryptWriter) seal(last bool) error {
	out := e.aead.Seal(nil, chunkNonce(e.nonce, e.count), e.buf, chunkAAD(e.header, last))
	e.count++
	e.buf = e.buf[:0]
	_, err := e.w.Write(out)
	return err
}

type decryptReader struct {
	r      *bufio.Reader
	aead   cipher.AEAD
	header []byte
	nonce  [nonceSize]byte
	count  uint64
	chunk  []byte
	plain  []byte
	done   bool
}

// isEncrypted peeks without consuming, so a plain archive reads on as before.
func isEncrypted(r *bufio.Reader) bool {
	head, _ := r.Peek(len(cryptMagic))
	return string(head) == cryptMagic
}

func newDecryptReader(r *bufio.Reader, passphrase string) (*decryptReader, error) {
	if passphrase == "" {
		return nil, ErrPassphraseRequired
	}
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArchive, err)
	}
	if header[len(cryptMagic)] != cryptVersion {
		return nil, ErrNewerArchive
	}
	aead, err := newAEAD(passphrase, header)
	if err != nil {
		return nil, err
	}
	d := &decryptReader{r: r, aead: aead, header: header, chunk: make([]byte, chunkSize+tagSize)}
	copy(d.nonce[:], header[headerSize-nonceSize:])
	return d, nil
}

func (d *decryptReader) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.done {
			return 0, io.EOF
		}
		if err := d.open(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}

func (d *decryptReader) open() error {
	n, err := io.ReadFull(d.r, d.chunk)
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
		d.done = true
	case err != nil:
		return err
	default:
		if _, err := d.r.Peek(1); errors.Is(err, io.EOF) {
			d.done = true
		}
	}
	plain, err := d.aead.Open(d.chunk[:0], chunkNonce(d.nonce, d.count), d.chunk[:n], chunkAAD(d.header, d.done))
	if err != nil {
		return ErrWrongPassphrase
	}
	d.count++
	d.plain = plain
	return nil
}

func newAEAD(passphrase string, header []byte) (cipher.AEAD, error) {
	at := len(cryptMagic) + 1
	time := binary.BigEndian.Uint32(header[at:])
	memory := binary.BigEndian.Uint32(header[at+4:])
	threads := header[at+8]
	salt := header[at+9 : at+9+saltSize]
	if time == 0 || time > maxKDFTime || memory == 0 || memory > maxKDFMemory || threads == 0 {
		return nil, fmt.Errorf("%w: unusual key settings", ErrInvalidArchive)
	}
	key := argon2.IDKey([]byte(passphrase), salt, time, memory, threads, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(base [nonceSize]byte, n uint64) []byte {
	nonce := base
	var ctr [8]byte
	binary.BigEndian.PutUint64(ctr[:], n)
	for i := range ctr {
		nonce[nonceSize-8+i] ^= ctr[i]
	}
	return nonce[:]
}

func chunkAAD(header []byte, last bool) []byte {
	flag := byte(0)
	if last {
		flag = 1
	}
	return append(bytes.Clone(header), flag)
}
