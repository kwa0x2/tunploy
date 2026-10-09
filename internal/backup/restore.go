package backup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/kwa0x2/tunploy/internal/apperr"
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

// Live is the panel's state outside the database, which must catch up
// after a restore swaps it. main passes the real parts.
type Live struct {
	// Keep saved settings in memory: the email notifier, the HTTPS certificates.
	Settings []Loader
	// Rebuilds every VPN container, since an instance ID may now stand for
	// another server.
	VPN interface {
		MarkRebuild(ctx context.Context) error
		Reconcile(ctx context.Context) error
	}
	// Reconnects to the restored nodes; each rebuilds as it comes up.
	Nodes interface {
		Reload(ctx context.Context) error
	}
}

type Loader interface {
	Load(stored map[string]string)
}

// RestoreRequest names a backup and who asked for it, for the activity log.
type RestoreRequest struct {
	Name string
	// Empty falls back to the panel's own.
	Passphrase string
	IP         string
}

type Result struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Version   string    `json:"version"`
	// Problems that did not stop the restore, such as a server that failed to start.
	Warnings []string `json:"warnings"`
}

// Restore puts the archive in src in place of the panel's data and has the
// live parts catch up. Backup settings survive, and everyone is signed out,
// since the users come from the backup.
func (s *Service) Restore(ctx context.Context, src io.Reader, req RestoreRequest) (Result, error) {
	var res Result
	err := s.Exclusive(func() error {
		var err error
		res, err = s.restore(ctx, src, req)
		return err
	})
	return res, restoreError(err)
}

// RestoreRemote is Restore for a backup in the bucket.
func (s *Service) RestoreRemote(ctx context.Context, req RestoreRequest) (Result, error) {
	var res Result
	err := s.Exclusive(func() error {
		remote, err := s.Remote()
		if err != nil {
			return err
		}
		body, _, err := remote.Get(ctx, remote.Key(req.Name))
		if err != nil {
			return remoteError(err)
		}
		defer body.Close()
		res, err = s.restore(ctx, body, req)
		return err
	})
	return res, restoreError(err)
}

func (s *Service) restore(ctx context.Context, src io.Reader, req RestoreRequest) (Result, error) {
	passphrase := req.Passphrase
	if passphrase == "" {
		passphrase = s.Passphrase()
	}
	m, warnings, err := Restore(ctx, s.store, s.dataDir, s.tmpDir, src, passphrase)
	if err != nil {
		return Result{}, err
	}
	slog.Info("database restored from backup", "name", req.Name, "made", m.CreatedAt)
	res := Result{Name: req.Name, CreatedAt: m.CreatedAt, Version: m.Version, Warnings: warnings}

	more, err := s.catchUp(ctx)
	res.Warnings = append(res.Warnings, more...)
	if err != nil {
		return res, err
	}
	s.events.Record(ctx, store.Event{Kind: "backup.restored", IP: req.IP,
		Detail: req.Name + " (made " + m.CreatedAt.Local().Format("2006-01-02 15:04") + ")"})
	return res, nil
}

// Nodes reconnect after the rebuild is marked, so none of them only
// reconciles; failures past that point are warnings, as the data is back.
func (s *Service) catchUp(ctx context.Context) ([]string, error) {
	stored, err := s.store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range s.live.Settings {
		l.Load(stored)
	}

	var warnings []string
	if s.live.VPN != nil {
		if err := s.live.VPN.MarkRebuild(ctx); err != nil {
			return nil, err
		}
	}
	if s.live.Nodes != nil {
		if err := s.live.Nodes.Reload(ctx); err != nil {
			slog.Error("reconnect nodes after restore", "error", err)
			warnings = append(warnings, "could not reconnect the nodes: "+err.Error())
		}
	}
	if s.live.VPN != nil {
		if err := s.live.VPN.Reconcile(ctx); err != nil {
			slog.Error("rebuild wireguard containers after restore", "error", err)
			warnings = append(warnings, "some VPN servers could not start: "+err.Error())
		}
	}
	return warnings, nil
}

func restoreError(err error) error {
	var ae *apperr.Error
	fail := func(kind apperr.Kind, code, msg string) error {
		return &apperr.Error{Kind: kind, Code: code, Message: msg, Err: err}
	}
	switch {
	case err == nil, errors.As(err, &ae):
		return err
	case errors.Is(err, ErrBusy):
		return fail(apperr.Conflict, "conflict", err.Error())
	case errors.Is(err, ErrNotConfigured):
		return fail(apperr.Conflict, "conflict", "connect an S3 bucket first")
	case errors.Is(err, ErrPassphraseRequired):
		return fail(apperr.Invalid, "passphrase_required", ErrPassphraseRequired.Error())
	case errors.Is(err, ErrWrongPassphrase):
		return fail(apperr.Invalid, "wrong_passphrase", ErrWrongPassphrase.Error())
	case errors.Is(err, ErrInvalidArchive), errors.Is(err, store.ErrInvalidBackup):
		return fail(apperr.Invalid, "invalid_backup", err.Error())
	case errors.Is(err, store.ErrNewerBackup), errors.Is(err, ErrNewerArchive):
		return fail(apperr.Conflict, "newer_backup", "this backup was made by a newer version of Tunploy; update the panel first")
	}
	return err
}

func remoteError(err error) error {
	if errors.Is(err, ErrObjectNotFound) {
		return &apperr.Error{Kind: apperr.NotFound, Code: "not_found", Message: err.Error(), Err: err}
	}
	return &apperr.Error{Kind: apperr.Upstream, Code: "s3_failed", Message: err.Error(), Err: err}
}
