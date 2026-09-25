package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	ScheduleOff    = "off"
	ScheduleDaily  = "daily"
	ScheduleWeekly = "weekly"

	DefaultKeep = 7
	// Weekly backups run on this day, at the configured hour.
	weeklyDay = time.Sunday

	checkEvery = time.Minute
	retryAfter = 30 * time.Minute

	settingPrefix    = "backup_"
	settingEndpoint  = "backup_s3_endpoint"
	settingRegion    = "backup_s3_region"
	settingBucket    = "backup_s3_bucket"
	settingPrefixDir = "backup_s3_prefix"
	settingAccessKey = "backup_s3_access_key"
	// Stored as is, like the SMTP password: whoever reads the database owns the VPN anyway.
	settingSecretKey = "backup_s3_secret_key"
	settingPathStyle = "backup_s3_path_style"
	settingSchedule  = "backup_schedule"
	settingHour      = "backup_hour"
	settingKeep      = "backup_keep"
	settingLastAt    = "backup_last_at"
	settingLastName  = "backup_last_name"
	settingLastSize  = "backup_last_size"
	// Kept apart from the bucket, since downloaded backups use it too.
	settingPassphrase = "backup_passphrase"
)

var (
	ErrBusy          = errors.New("another backup or restore is running")
	ErrNotConfigured = errors.New("no S3 bucket is connected")
)

// KeepSetting reports whether a setting belongs to backups, which a restore
// must not overwrite: the destination would vanish mid-disaster.
func KeepSetting(key string) bool { return strings.HasPrefix(key, settingPrefix) }

type Config struct {
	S3       S3Config
	Schedule string
	Hour     int
	// Keep is how many backups stay in the bucket; 0 keeps them all.
	Keep int
}

// LoadConfig returns nil when no bucket is connected.
func LoadConfig(stored map[string]string) *Config {
	if stored[settingBucket] == "" {
		return nil
	}
	hour, _ := strconv.Atoi(stored[settingHour])
	keep := DefaultKeep
	if raw, ok := stored[settingKeep]; ok {
		keep, _ = strconv.Atoi(raw)
	}
	schedule := stored[settingSchedule]
	if schedule == "" {
		schedule = ScheduleOff
	}
	return &Config{
		S3: S3Config{
			Endpoint:  stored[settingEndpoint],
			Region:    stored[settingRegion],
			Bucket:    stored[settingBucket],
			Prefix:    stored[settingPrefixDir],
			AccessKey: stored[settingAccessKey],
			SecretKey: stored[settingSecretKey],
			PathStyle: stored[settingPathStyle] == "true",
		},
		Schedule: schedule,
		Hour:     hour,
		Keep:     keep,
	}
}

// Settings with nil clears the connection.
func Settings(c *Config) map[string]string {
	if c == nil {
		c = &Config{Schedule: ScheduleOff, Keep: DefaultKeep}
	}
	return map[string]string{
		settingEndpoint:  c.S3.Endpoint,
		settingRegion:    c.S3.Region,
		settingBucket:    c.S3.Bucket,
		settingPrefixDir: c.S3.Prefix,
		settingAccessKey: c.S3.AccessKey,
		settingSecretKey: c.S3.SecretKey,
		settingPathStyle: strconv.FormatBool(c.S3.PathStyle),
		settingSchedule:  c.Schedule,
		settingHour:      strconv.Itoa(c.Hour),
		settingKeep:      strconv.Itoa(c.Keep),
	}
}

// Next is the first scheduled run after t; zero when the schedule is off.
func (c *Config) Next(t time.Time) time.Time {
	prev := c.previous(t)
	if prev.IsZero() {
		return time.Time{}
	}
	if c.Schedule == ScheduleWeekly {
		return prev.AddDate(0, 0, 7)
	}
	return prev.AddDate(0, 0, 1)
}

// previous is the latest scheduled run at or before t.
func (c *Config) previous(t time.Time) time.Time {
	if c.Schedule != ScheduleDaily && c.Schedule != ScheduleWeekly {
		return time.Time{}
	}
	slot := time.Date(t.Year(), t.Month(), t.Day(), c.Hour, 0, 0, 0, t.Location())
	if slot.After(t) {
		slot = slot.AddDate(0, 0, -1)
	}
	if c.Schedule == ScheduleWeekly {
		for slot.Weekday() != weeklyDay {
			slot = slot.AddDate(0, 0, -1)
		}
	}
	return slot
}

type Status struct {
	Running        bool       `json:"running"`
	LastBackupAt   *time.Time `json:"last_backup_at,omitempty"`
	LastBackupName string     `json:"last_backup_name,omitempty"`
	LastBackupSize int64      `json:"last_backup_size,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	LastErrorAt    *time.Time `json:"last_error_at,omitempty"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
}

type Service struct {
	store   *store.Store
	dataDir string
	tmpDir  string
	version string
	now     func() time.Time
	onEvent func(context.Context, store.Event)

	// Held for the whole of a backup or restore; never waited on.
	busy sync.Mutex

	mu         sync.Mutex
	cfg        *Config
	passphrase string
	status     Status
	failures   int
	retryAt    time.Time
}

func NewService(st *store.Store, dataDir, version string) *Service {
	tmp := filepath.Join(dataDir, "tmp")
	os.RemoveAll(tmp)
	return &Service{store: st, dataDir: dataDir, tmpDir: tmp, version: version, now: time.Now}
}

// OnEvent must be set before Run starts or requests arrive.
func (s *Service) OnEvent(fn func(context.Context, store.Event)) { s.onEvent = fn }

func (s *Service) DataDir() string { return s.dataDir }

func (s *Service) TempDir() string { return s.tmpDir }

// Load applies the saved settings, including when the last backup ran.
func (s *Service) Load(stored map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = LoadConfig(stored)
	s.passphrase = LoadPassphrase(stored)
	s.failures, s.retryAt = 0, time.Time{}
	s.status.LastBackupAt, s.status.LastBackupName, s.status.LastBackupSize = nil, "", 0
	if at, err := strconv.ParseInt(stored[settingLastAt], 10, 64); err == nil && at > 0 {
		t := time.Unix(at, 0)
		s.status.LastBackupAt = &t
		s.status.LastBackupName = stored[settingLastName]
		s.status.LastBackupSize, _ = strconv.ParseInt(stored[settingLastSize], 10, 64)
	}
}

func (s *Service) Config() *Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return nil
	}
	c := *s.cfg
	return &c
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	if s.cfg != nil {
		if next := s.cfg.Next(s.now()); !next.IsZero() {
			if !s.retryAt.IsZero() && s.retryAt.Before(next) {
				next = s.retryAt
			}
			st.NextRunAt = &next
		}
	}
	return st
}

// Passphrase is what new backups are encrypted with; empty leaves them plain.
func (s *Service) Passphrase() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.passphrase
}

func LoadPassphrase(stored map[string]string) string { return stored[settingPassphrase] }

// PassphraseSetting stores a new passphrase, or turns encryption off with "".
func PassphraseSetting(passphrase string) map[string]string {
	return map[string]string{settingPassphrase: passphrase}
}

// Remote returns a client for the connected bucket.
func (s *Service) Remote() (*S3, error) {
	cfg := s.Config()
	if cfg == nil {
		return nil, ErrNotConfigured
	}
	return NewS3(cfg.S3), nil
}

// Exclusive runs fn unless a backup or restore is already underway.
func (s *Service) Exclusive(fn func() error) error {
	if !s.busy.TryLock() {
		return ErrBusy
	}
	defer s.busy.Unlock()
	return fn()
}

// Export creates an archive for download; the caller removes it.
func (s *Service) Export(ctx context.Context) (*File, error) {
	var f *File
	err := s.Exclusive(func() error {
		var err error
		f, err = Create(ctx, s.store, s.dataDir, s.tmpDir, s.version, s.now(), s.Passphrase())
		return err
	})
	return f, err
}

// Upload creates a backup and stores it in the bucket, then drops the oldest
// ones beyond the configured count.
func (s *Service) Upload(ctx context.Context, scheduled bool) (Object, error) {
	var obj Object
	err := s.Exclusive(func() error {
		s.setRunning(true)
		defer s.setRunning(false)

		var err error
		obj, err = s.upload(ctx)
		s.finish(ctx, obj, scheduled, err)
		return err
	})
	return obj, err
}

func (s *Service) upload(ctx context.Context) (Object, error) {
	cfg := s.Config()
	if cfg == nil {
		return Object{}, ErrNotConfigured
	}
	remote := NewS3(cfg.S3)

	now := s.now()
	f, err := Create(ctx, s.store, s.dataDir, s.tmpDir, s.version, now, s.Passphrase())
	if err != nil {
		return Object{}, err
	}
	defer f.Remove()

	obj := Object{Key: remote.Key(f.Name), Name: f.Name, Size: f.Size, Modified: now, Encrypted: IsEncryptedName(f.Name)}
	if err := remote.PutFile(ctx, obj.Key, f); err != nil {
		return Object{}, fmt.Errorf("upload to %s: %w", cfg.S3.Bucket, err)
	}
	if cfg.Keep > 0 {
		if err := prune(ctx, remote, cfg.Keep); err != nil {
			slog.Warn("remove old backups", "bucket", cfg.S3.Bucket, "error", err)
		}
	}
	return obj, nil
}

func prune(ctx context.Context, remote *S3, keep int) error {
	objects, err := remote.List(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, o := range objects[min(keep, len(objects)):] {
		if err := remote.Delete(ctx, o.Key); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) setRunning(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Running = on
}

func (s *Service) finish(ctx context.Context, obj Object, scheduled bool, err error) {
	now := s.now()
	s.mu.Lock()
	firstFailure := false
	if err != nil {
		s.status.LastError, s.status.LastErrorAt = err.Error(), &now
		if scheduled {
			s.failures++
			firstFailure = s.failures == 1
			s.retryAt = now.Add(retryAfter)
		}
	} else {
		s.status.LastError, s.status.LastErrorAt = "", nil
		s.status.LastBackupAt, s.status.LastBackupName, s.status.LastBackupSize = &now, obj.Name, obj.Size
		s.failures, s.retryAt = 0, time.Time{}
	}
	s.mu.Unlock()

	if err != nil {
		// A bucket that stays unreachable would otherwise send an email every retry.
		if firstFailure {
			s.event(ctx, store.Event{Kind: "backup.failed", Detail: err.Error()})
		}
		return
	}
	if err := s.store.SaveSettings(context.WithoutCancel(ctx), map[string]string{
		settingLastAt:   strconv.FormatInt(now.Unix(), 10),
		settingLastName: obj.Name,
		settingLastSize: strconv.FormatInt(obj.Size, 10),
	}); err != nil {
		slog.Error("save last backup time", "error", err)
	}
	s.event(ctx, store.Event{Kind: "backup.created", Detail: obj.Name + " (" + FormatSize(obj.Size) + ")"})
}

func (s *Service) event(ctx context.Context, e store.Event) {
	if s.onEvent != nil {
		s.onEvent(context.WithoutCancel(ctx), e)
	}
}

// Run makes scheduled backups until ctx ends.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(checkEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.due() {
			if _, err := s.Upload(ctx, true); err != nil && !errors.Is(err, ErrBusy) {
				slog.Error("scheduled backup failed", "error", err)
			}
		}
	}
}

func (s *Service) due() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return false
	}
	now := s.now()
	slot := s.cfg.previous(now)
	if slot.IsZero() || now.Before(s.retryAt) {
		return false
	}
	return s.status.LastBackupAt == nil || s.status.LastBackupAt.Before(slot)
}

func FormatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
