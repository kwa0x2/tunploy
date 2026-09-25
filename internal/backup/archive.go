// Package backup packs the panel's data into an archive and keeps copies in S3.
package backup

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	formatVersion = 1
	manifestName  = "manifest.json"
	databaseName  = "tunploy.db"
	certsDir      = "certs"

	filePrefix   = "tunploy-backup-"
	fileExt      = ".tar.gz"
	encryptedExt = ".enc"
	timeLayout   = "20060102-150405"

	// Far above any real panel; stops a gzip bomb from filling the disk.
	maxUnpackedBytes = 4 << 30
)

var (
	ErrInvalidArchive = errors.New("this file is not a Tunploy backup")
	ErrNewerArchive   = errors.New("this backup was made by a newer version of Tunploy; update the panel first")
	namePattern       = regexp.MustCompile(`^` + filePrefix + `\d{8}-\d{6}` + regexp.QuoteMeta(fileExt) + `(` + regexp.QuoteMeta(encryptedExt) + `)?$`)
)

type Manifest struct {
	Format    int       `json:"format"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

func FileName(t time.Time, encrypted bool) string {
	name := filePrefix + t.UTC().Format(timeLayout) + fileExt
	if encrypted {
		name += encryptedExt
	}
	return name
}

func IsFileName(name string) bool { return namePattern.MatchString(name) }

func IsEncryptedName(name string) bool { return strings.HasSuffix(name, encryptedExt) }

type File struct {
	Path   string
	Name   string
	Size   int64
	SHA256 string
}

func (f *File) Remove() { os.Remove(f.Path) }

// Create writes an archive of the database and TLS certificates into tmpDir,
// encrypted when passphrase is set.
func Create(ctx context.Context, st *store.Store, dataDir, tmpDir, version string, now time.Time, passphrase string) (*File, error) {
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	work, err := os.MkdirTemp(tmpDir, "snapshot-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(work)

	dbPath := filepath.Join(work, databaseName)
	if err := st.Snapshot(ctx, dbPath); err != nil {
		return nil, err
	}

	out, err := os.CreateTemp(tmpDir, "backup-*"+fileExt)
	if err != nil {
		return nil, fmt.Errorf("create backup file: %w", err)
	}
	f := &File{Path: out.Name(), Name: FileName(now, passphrase != "")}
	ok := false
	defer func() {
		if !ok {
			out.Close()
			f.Remove()
		}
	}()

	sum := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(out, sum)}
	var sink io.Writer = counter
	var enc *encryptWriter
	if passphrase != "" {
		if enc, err = newEncryptWriter(counter, passphrase); err != nil {
			return nil, fmt.Errorf("encrypt backup: %w", err)
		}
		sink = enc
	}
	gz := gzip.NewWriter(sink)
	tw := tar.NewWriter(gz)

	manifest, _ := json.MarshalIndent(Manifest{Format: formatVersion, Version: version, CreatedAt: now.UTC()}, "", "  ")
	if err := addBytes(tw, manifestName, manifest, now); err != nil {
		return nil, err
	}
	if err := addFile(tw, databaseName, dbPath); err != nil {
		return nil, err
	}
	certs, err := os.ReadDir(filepath.Join(dataDir, certsDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read certificates: %w", err)
	}
	for _, e := range certs {
		if !e.Type().IsRegular() {
			continue
		}
		if err := addFile(tw, certsDir+"/"+e.Name(), filepath.Join(dataDir, certsDir, e.Name())); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("write backup: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("write backup: %w", err)
	}
	if enc != nil {
		if err := enc.Close(); err != nil {
			return nil, fmt.Errorf("write backup: %w", err)
		}
	}
	if err := out.Close(); err != nil {
		return nil, fmt.Errorf("write backup: %w", err)
	}
	f.Size = counter.n
	f.SHA256 = hex.EncodeToString(sum.Sum(nil))
	ok = true
	return f, nil
}

func addBytes(tw *tar.Writer, name string, body []byte, mod time.Time) error {
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), ModTime: mod, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	if _, err := tw.Write(body); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

func addFile(tw *tar.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

type Extracted struct {
	Manifest Manifest
	Database string
	// Certs lists certificate files by name, pointing at their extracted copies.
	Certs map[string]string
}

// Extract unpacks an archive into dir, accepting only the files Create writes.
// passphrase is only needed for an encrypted one.
func Extract(r io.Reader, dir, passphrase string) (*Extracted, error) {
	br := bufio.NewReader(r)
	var src io.Reader = br
	if isEncrypted(br) {
		dec, err := newDecryptReader(br, passphrase)
		if err != nil {
			return nil, err
		}
		src = dec
	}
	gz, err := gzip.NewReader(src)
	if err != nil {
		return nil, unreadable(err)
	}
	defer gz.Close()

	ex := &Extracted{Certs: map[string]string{}}
	var sawManifest bool
	tr := tar.NewReader(io.LimitReader(gz, maxUnpackedBytes))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, unreadable(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		switch dirName, base := path.Split(hdr.Name); {
		case hdr.Name == manifestName:
			body, err := io.ReadAll(io.LimitReader(tr, 64<<10))
			if err != nil {
				return nil, unreadable(err)
			}
			if err := json.Unmarshal(body, &ex.Manifest); err != nil {
				return nil, fmt.Errorf("%w: unreadable manifest", ErrInvalidArchive)
			}
			sawManifest = true
		case hdr.Name == databaseName:
			ex.Database = filepath.Join(dir, databaseName)
			if err := writeFile(ex.Database, tr); err != nil {
				return nil, err
			}
		case dirName == certsDir+"/" && validCertName(base):
			dst := filepath.Join(dir, "cert-"+base)
			if err := writeFile(dst, tr); err != nil {
				return nil, err
			}
			ex.Certs[base] = dst
		}
	}

	switch {
	case !sawManifest || ex.Database == "":
		return nil, ErrInvalidArchive
	case ex.Manifest.Format > formatVersion:
		return nil, ErrNewerArchive
	case ex.Manifest.Format < 1:
		return nil, fmt.Errorf("%w: unknown format", ErrInvalidArchive)
	}
	return ex, nil
}

// A wrong passphrase surfaces from deep inside gzip or tar; it must not read
// as a file that is not a backup at all.
func unreadable(err error) error {
	if errors.Is(err, ErrWrongPassphrase) {
		return ErrWrongPassphrase
	}
	return fmt.Errorf("%w: %w", ErrInvalidArchive, err)
}

func validCertName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

func writeFile(dst string, r io.Reader) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("unpack backup: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return unreadable(err)
	}
	return f.Close()
}

// ReplaceCerts swaps the certificate directory's files for the backup's.
func ReplaceCerts(dataDir string, certs map[string]string) error {
	dir := filepath.Join(dataDir, certsDir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("replace certificates: %w", err)
	}
	if len(certs) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("replace certificates: %w", err)
	}
	for name, src := range certs {
		f, err := os.Open(src)
		if err != nil {
			return fmt.Errorf("replace certificates: %w", err)
		}
		err = writeFile(filepath.Join(dir, name), f)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
