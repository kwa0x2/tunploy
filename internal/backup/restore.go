package backup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kwa0x2/tunploy/internal/store"
)

const rebuildMarker = "rebuild-pending"

// Restore replaces the panel's data with the archive in r. Backup settings
// survive and everyone is signed out. Warnings are problems that did not stop it.
func Restore(ctx context.Context, st *store.Store, dataDir, tmpDir string, r io.Reader, passphrase string) (Manifest, []string, error) {
	warnings := []string{}
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return Manifest{}, warnings, err
	}
	dir, err := os.MkdirTemp(tmpDir, "restore-")
	if err != nil {
		return Manifest{}, warnings, err
	}
	defer os.RemoveAll(dir)

	ex, err := Extract(r, dir, passphrase)
	if err != nil {
		return Manifest{}, warnings, err
	}
	if err := st.Restore(ctx, ex.Database, KeepSetting); err != nil {
		return ex.Manifest, warnings, err
	}
	if err := ReplaceCerts(dataDir, ex.Certs); err != nil {
		slog.Error("restore certificates", "error", err)
		warnings = append(warnings, "HTTPS certificates could not be restored; they will be requested again")
	}
	return ex.Manifest, warnings, nil
}

// MarkRebuild tells the next panel start to recreate every VPN container,
// for a restore made while the panel was not the one doing it.
func MarkRebuild(dataDir string) error {
	return os.WriteFile(filepath.Join(dataDir, rebuildMarker), nil, 0o600)
}

func RebuildPending(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, rebuildMarker))
	return err == nil
}

func ClearRebuild(dataDir string) error {
	err := os.Remove(filepath.Join(dataDir, rebuildMarker))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
