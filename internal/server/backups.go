package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kwa0x2/tunploy/internal/backup"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	maxUploadBytes = 1 << 30
	maxKeep        = 1000
	backupTimeout  = 30 * time.Minute
)

var (
	bucketPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,254}$`)
	regionPattern = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	prefixPattern = regexp.MustCompile(`^[A-Za-z0-9!_.*'()/-]*$`)
)

type backupSettings struct {
	Connected    bool          `json:"connected"`
	Endpoint     string        `json:"endpoint"`
	Region       string        `json:"region"`
	Bucket       string        `json:"bucket"`
	Prefix       string        `json:"prefix"`
	AccessKey    string        `json:"access_key"`
	SecretKeySet bool          `json:"secret_key_set"`
	PathStyle    bool          `json:"path_style"`
	Schedule     string        `json:"schedule"`
	Hour         int           `json:"hour"`
	Keep         int           `json:"keep"`
	Encrypted    bool          `json:"encrypted"`
	Timezone     string        `json:"timezone"`
	Status       backup.Status `json:"status"`
}

type backupRequest struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"access_key"`
	// nil keeps the saved key, so the form never has to show it.
	SecretKey *string `json:"secret_key"`
	PathStyle bool    `json:"path_style"`
	Schedule  string  `json:"schedule"`
	Hour      int     `json:"hour"`
	Keep      *int    `json:"keep"`
}

type restoreResult struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Version   string    `json:"version"`
	// Problems that did not stop the restore, such as a server that failed to start.
	Warnings []string `json:"warnings"`
}

// RunBackups loads the saved bucket and makes scheduled backups until ctx ends.
func (s *Server) RunBackups(ctx context.Context) {
	stored, err := s.store.Settings(ctx)
	if err != nil {
		slog.Error("load backup settings", "error", err)
	} else {
		s.backups.Load(stored)
	}
	s.backups.Run(ctx)
}

func (s *Server) handleGetBackupSettings(w http.ResponseWriter, r *http.Request) error {
	v := backupSettings{
		Schedule:  backup.ScheduleOff,
		Keep:      backup.DefaultKeep,
		Encrypted: s.backups.Passphrase() != "",
		Timezone:  panelTimezone(),
	}
	if cfg := s.backups.Config(); cfg != nil {
		v.Connected = true
		v.Endpoint, v.Region, v.Bucket, v.Prefix = cfg.S3.Endpoint, cfg.S3.Region, cfg.S3.Bucket, cfg.S3.Prefix
		v.AccessKey, v.SecretKeySet, v.PathStyle = cfg.S3.AccessKey, cfg.S3.SecretKey != "", cfg.S3.PathStyle
		v.Schedule, v.Hour, v.Keep = cfg.Schedule, cfg.Hour, cfg.Keep
	}
	v.Status = s.backups.Status()
	return httpx.JSON(w, http.StatusOK, v)
}

func panelTimezone() string {
	if name := time.Local.String(); name != "Local" {
		return name
	}
	return "UTC" + time.Now().Format("-07:00")
}

// Saving checks the bucket first whenever the connection details change.
func (s *Server) handleSetBackupSettings(w http.ResponseWriter, r *http.Request) error {
	var req backupRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cur := s.backups.Config()
	cfg, fields := req.config(cur)
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	if cur == nil || cur.S3 != cfg.S3 {
		if err := s.checkBucket(r.Context(), cfg.S3); err != nil {
			return err
		}
	}
	if err := s.saveBackupSettings(r.Context(), cfg); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "settings.backups_changed", IP: clientIP(r), Detail: "bucket " + cfg.S3.Bucket})
	return s.handleGetBackupSettings(w, r)
}

func (s *Server) handleDeleteBackupSettings(w http.ResponseWriter, r *http.Request) error {
	if err := s.saveBackupSettings(r.Context(), nil); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "settings.backups_changed", IP: clientIP(r), Detail: "disconnected"})
	return s.handleGetBackupSettings(w, r)
}

func (s *Server) saveBackupSettings(ctx context.Context, cfg *backup.Config) error {
	if err := s.store.SaveSettings(ctx, backup.Settings(cfg)); err != nil {
		return err
	}
	stored, err := s.store.Settings(ctx)
	if err != nil {
		return err
	}
	s.backups.Load(stored)
	return nil
}

// An empty passphrase turns encryption off; backups made before keep theirs.
func (s *Server) handleSetBackupEncryption(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Passphrase != "" && utf8.RuneCountInString(req.Passphrase) < backup.MinPassphraseLen {
		return httpx.Invalid(map[string]string{
			"passphrase": fmt.Sprintf("use at least %d characters", backup.MinPassphraseLen),
		})
	}
	if err := s.store.SaveSettings(r.Context(), backup.PassphraseSetting(req.Passphrase)); err != nil {
		return err
	}
	stored, err := s.store.Settings(r.Context())
	if err != nil {
		return err
	}
	s.backups.Load(stored)

	detail := "encryption off"
	if req.Passphrase != "" {
		detail = "encryption on"
	}
	s.record(r.Context(), store.Event{Kind: "settings.backups_changed", IP: clientIP(r), Detail: detail})
	return s.handleGetBackupSettings(w, r)
}

// Tests the form as it is, before it is saved.
func (s *Server) handleTestBackupSettings(w http.ResponseWriter, r *http.Request) error {
	var req backupRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cfg, fields := req.config(s.backups.Config())
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	if err := s.checkBucket(r.Context(), cfg.S3); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) checkBucket(ctx context.Context, cfg backup.S3Config) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := backup.NewS3(cfg).Check(ctx); err != nil {
		return httpx.Errorf(http.StatusBadGateway, "s3_failed", "%v", err)
	}
	return nil
}

func (req backupRequest) config(cur *backup.Config) (*backup.Config, map[string]string) {
	fields := map[string]string{}
	cfg := &backup.Config{
		S3: backup.S3Config{
			Endpoint:  strings.TrimSuffix(strings.TrimSpace(req.Endpoint), "/"),
			Region:    strings.TrimSpace(req.Region),
			Bucket:    strings.TrimSpace(req.Bucket),
			Prefix:    strings.Trim(strings.TrimSpace(req.Prefix), "/"),
			AccessKey: strings.TrimSpace(req.AccessKey),
			PathStyle: req.PathStyle,
		},
		Schedule: req.Schedule,
		Hour:     req.Hour,
		Keep:     backup.DefaultKeep,
	}
	switch {
	case req.SecretKey != nil:
		cfg.S3.SecretKey = strings.TrimSpace(*req.SecretKey)
	case cur != nil:
		cfg.S3.SecretKey = cur.S3.SecretKey
	}
	if req.Keep != nil {
		cfg.Keep = *req.Keep
	}

	if cfg.S3.Endpoint != "" {
		raw := cfg.S3.Endpoint
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" {
			fields["endpoint"] = "enter the S3 address, such as https://s3.eu-central-1.amazonaws.com"
		}
	}
	if cfg.S3.Region != "" && !regionPattern.MatchString(cfg.S3.Region) {
		fields["region"] = "region looks like eu-central-1, or auto for Cloudflare R2"
	}
	switch {
	case cfg.S3.Bucket == "":
		fields["bucket"] = "bucket is required"
	case !bucketPattern.MatchString(cfg.S3.Bucket):
		fields["bucket"] = "bucket names use letters, numbers, dots and hyphens"
	}
	switch {
	case !prefixPattern.MatchString(cfg.S3.Prefix), strings.Contains(cfg.S3.Prefix, "//"),
		slices.Contains(strings.Split(cfg.S3.Prefix, "/"), ".."):
		fields["prefix"] = "folder may use letters, numbers, /, -, _ and ."
	case cfg.S3.Prefix != "":
		cfg.S3.Prefix += "/"
	}
	if cfg.S3.AccessKey == "" {
		fields["access_key"] = "access key is required"
	}
	if cfg.S3.SecretKey == "" {
		fields["secret_key"] = "secret key is required"
	}

	if cfg.Schedule == "" {
		cfg.Schedule = backup.ScheduleOff
	}
	if !slices.Contains([]string{backup.ScheduleOff, backup.ScheduleDaily, backup.ScheduleWeekly}, cfg.Schedule) {
		fields["schedule"] = "schedule must be off, daily or weekly"
	}
	if cfg.Hour < 0 || cfg.Hour > 23 {
		fields["hour"] = "hour must be between 0 and 23"
	}
	if cfg.Keep < 0 || cfg.Keep > maxKeep {
		fields["keep"] = "keep between 1 and 1000 backups, or 0 to keep all"
	}
	return cfg, fields
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) error {
	remote, err := s.remote()
	if err != nil {
		return err
	}
	objects, err := remote.List(r.Context())
	if err != nil {
		return httpx.Errorf(http.StatusBadGateway, "s3_failed", "could not list backups: %v", err)
	}
	if objects == nil {
		objects = []backup.Object{}
	}
	return httpx.JSON(w, http.StatusOK, objects)
}

func (s *Server) remote() (*backup.S3, error) {
	remote, err := s.backups.Remote()
	if errors.Is(err, backup.ErrNotConfigured) {
		return nil, httpx.Conflict("connect an S3 bucket first")
	}
	return remote, err
}

// Keeps going if the browser leaves, so a half-uploaded backup never lingers.
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), backupTimeout)
	defer cancel()
	obj, err := s.backups.Upload(ctx, false)
	switch {
	case errors.Is(err, backup.ErrBusy):
		return httpx.Conflict("%v", err)
	case errors.Is(err, backup.ErrNotConfigured):
		return httpx.Conflict("connect an S3 bucket first")
	case err != nil:
		return httpx.Errorf(http.StatusBadGateway, "backup_failed", "backup failed: %v", err)
	}
	return httpx.JSON(w, http.StatusCreated, obj)
}

func (s *Server) handleExportBackup(w http.ResponseWriter, r *http.Request) error {
	f, err := s.backups.Export(r.Context())
	if errors.Is(err, backup.ErrBusy) {
		return httpx.Conflict("%v", err)
	}
	if err != nil {
		return err
	}
	defer f.Remove()

	body, err := os.Open(f.Path)
	if err != nil {
		return err
	}
	defer body.Close()

	s.record(r.Context(), store.Event{Kind: "backup.downloaded", IP: clientIP(r), Detail: f.Name})
	serveArchive(w, f.Name, f.Size, body)
	return nil
}

func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) error {
	name, remote, err := s.remoteBackup(r)
	if err != nil {
		return err
	}
	body, size, err := remote.Get(r.Context(), remote.Key(name))
	if err != nil {
		return s3Failure(err)
	}
	defer body.Close()

	s.record(r.Context(), store.Event{Kind: "backup.downloaded", IP: clientIP(r), Detail: name})
	serveArchive(w, name, size, body)
	return nil
}

func serveArchive(w http.ResponseWriter, name string, size int64, body io.Reader) {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	if _, err := io.Copy(w, body); err != nil {
		slog.Warn("send backup", "name", name, "error", err)
	}
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) error {
	name, remote, err := s.remoteBackup(r)
	if err != nil {
		return err
	}
	if err := remote.Delete(r.Context(), remote.Key(name)); err != nil {
		return s3Failure(err)
	}
	s.record(r.Context(), store.Event{Kind: "backup.deleted", IP: clientIP(r), Detail: name})
	return httpx.NoContent(w)
}

func (s *Server) remoteBackup(r *http.Request) (string, *backup.S3, error) {
	name := r.PathValue("name")
	if !backup.IsFileName(name) {
		return "", nil, httpx.NotFound("no backup named %q", name)
	}
	remote, err := s.remote()
	return name, remote, err
}

func s3Failure(err error) error {
	if errors.Is(err, backup.ErrObjectNotFound) {
		return httpx.NotFound("%v", err)
	}
	return httpx.Errorf(http.StatusBadGateway, "s3_failed", "%v", err)
}

func (s *Server) handleRestoreRemote(w http.ResponseWriter, r *http.Request) error {
	name, remote, err := s.remoteBackup(r)
	if err != nil {
		return err
	}
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), backupTimeout)
	defer cancel()

	var res restoreResult
	err = s.backups.Exclusive(func() error {
		body, _, err := remote.Get(ctx, remote.Key(name))
		if err != nil {
			return s3Failure(err)
		}
		defer body.Close()
		res, err = s.restore(ctx, body, name, req.Passphrase, clientIP(r))
		return err
	})
	if err != nil {
		return restoreFailure(err)
	}
	return httpx.JSON(w, http.StatusOK, res)
}

func (s *Server) handleImportBackup(w http.ResponseWriter, r *http.Request) error {
	body := http.MaxBytesReader(w, r.Body, maxUploadBytes)
	defer body.Close()
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "uploaded file"
	}
	// Percent-encoded, since a header cannot carry every character a passphrase may hold.
	passphrase, err := url.PathUnescape(r.Header.Get("X-Backup-Passphrase"))
	if err != nil {
		return httpx.BadRequest("X-Backup-Passphrase must be percent-encoded")
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), backupTimeout)
	defer cancel()

	var res restoreResult
	err = s.backups.Exclusive(func() error {
		var err error
		res, err = s.restore(ctx, body, name, passphrase, clientIP(r))
		return err
	})
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return httpx.BadRequest("the file is larger than 1 GB")
		}
		return restoreFailure(err)
	}
	return httpx.JSON(w, http.StatusOK, res)
}

func restoreFailure(err error) error {
	switch {
	case errors.Is(err, backup.ErrBusy):
		return httpx.Conflict("%v", err)
	case errors.Is(err, backup.ErrPassphraseRequired):
		return httpx.Errorf(http.StatusUnprocessableEntity, "passphrase_required", "%v", backup.ErrPassphraseRequired)
	case errors.Is(err, backup.ErrWrongPassphrase):
		return httpx.Errorf(http.StatusUnprocessableEntity, "wrong_passphrase", "%v", backup.ErrWrongPassphrase)
	case errors.Is(err, backup.ErrInvalidArchive), errors.Is(err, store.ErrInvalidBackup):
		return httpx.Errorf(http.StatusUnprocessableEntity, "invalid_backup", "%v", err)
	case errors.Is(err, store.ErrNewerBackup), errors.Is(err, backup.ErrNewerArchive):
		return httpx.Errorf(http.StatusConflict, "newer_backup", "this backup was made by a newer version of Tunploy; update the panel first")
	}
	return err
}

// restore must run inside backups.Exclusive. Everyone, including the caller,
// is signed out, since the users come from the backup. An empty passphrase
// falls back to the panel's own.
func (s *Server) restore(ctx context.Context, src io.Reader, name, passphrase, ip string) (restoreResult, error) {
	if passphrase == "" {
		passphrase = s.backups.Passphrase()
	}
	m, warnings, err := backup.Restore(ctx, s.store, s.backups.DataDir(), s.backups.TempDir(), src, passphrase)
	if err != nil {
		return restoreResult{}, err
	}
	res := restoreResult{Name: name, CreatedAt: m.CreatedAt, Version: m.Version, Warnings: warnings}
	slog.Info("database restored from backup", "name", name, "made", m.CreatedAt)

	stored, err := s.store.Settings(ctx)
	if err != nil {
		return res, err
	}
	s.notifier.Configure(notifyConfig(stored))
	s.applyDomain(s.closing, stored)

	if err := s.deploy.Rebuild(ctx); err != nil {
		slog.Error("rebuild wireguard containers after restore", "error", err)
		res.Warnings = append(res.Warnings, "some VPN servers could not start: "+err.Error())
	}

	s.record(ctx, store.Event{Kind: "backup.restored", IP: ip,
		Detail: name + " (made " + m.CreatedAt.Local().Format("2006-01-02 15:04") + ")"})
	return res, nil
}
