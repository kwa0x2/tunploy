package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "tunploy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewService(st, dir, "v9.9.9"), st
}

func TestArchiveRoundTrip(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	st.SaveSettings(ctx, map[string]string{"public_host": "vpn.example.com"})
	os.MkdirAll(filepath.Join(svc.dataDir, "certs"), 0o700)
	os.WriteFile(filepath.Join(svc.dataDir, "certs", "panel.example.com"), []byte("PEM"), 0o600)

	f, err := svc.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Remove()
	if !IsFileName(f.Name) || f.Size == 0 || len(f.SHA256) != 64 {
		t.Fatalf("file = %+v", f)
	}

	body, _ := os.Open(f.Path)
	defer body.Close()
	ex, err := Extract(body, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if ex.Manifest.Version != "v9.9.9" || ex.Manifest.Format != formatVersion {
		t.Fatalf("manifest = %+v", ex.Manifest)
	}
	if pem, _ := os.ReadFile(ex.Certs["panel.example.com"]); string(pem) != "PEM" {
		t.Fatalf("certs = %v", ex.Certs)
	}
	restored, err := store.Open(ex.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got, _ := restored.Settings(ctx); got["public_host"] != "vpn.example.com" {
		t.Fatalf("settings in archive = %v", got)
	}
}

func TestExtractRejectsOtherFiles(t *testing.T) {
	if _, err := Extract(bytes.NewReader([]byte("hello")), t.TempDir(), ""); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("plain text: %v", err)
	}

	archive := func(files map[string]string) *bytes.Buffer {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for name, body := range files {
			tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg})
			tw.Write([]byte(body))
		}
		tw.Close()
		gz.Close()
		return &buf
	}

	dir := t.TempDir()
	if _, err := Extract(archive(map[string]string{"manifest.json": `{"format":1}`, "../../evil": "x"}), dir, ""); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("no database: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "evil")); err == nil {
		t.Fatal("path traversal wrote outside the directory")
	}
	if _, err := Extract(archive(map[string]string{"manifest.json": `{"format":2}`, "tunploy.db": "x"}), dir, ""); !errors.Is(err, ErrNewerArchive) {
		t.Fatalf("newer format: %v", err)
	}
}

func TestSchedule(t *testing.T) {
	loc := time.FixedZone("TRT", 3*3600)
	daily := &Config{Schedule: ScheduleDaily, Hour: 3}
	weekly := &Config{Schedule: ScheduleWeekly, Hour: 3}
	// Thursday.
	now := time.Date(2026, 9, 24, 2, 30, 0, 0, loc)

	if got := daily.previous(now); !got.Equal(time.Date(2026, 9, 23, 3, 0, 0, 0, loc)) {
		t.Errorf("daily previous = %v", got)
	}
	if got := daily.Next(now); !got.Equal(time.Date(2026, 9, 24, 3, 0, 0, 0, loc)) {
		t.Errorf("daily next = %v", got)
	}
	if got := weekly.previous(now); !got.Equal(time.Date(2026, 9, 20, 3, 0, 0, 0, loc)) {
		t.Errorf("weekly previous = %v", got)
	}
	if got := weekly.Next(now); !got.Equal(time.Date(2026, 9, 27, 3, 0, 0, 0, loc)) {
		t.Errorf("weekly next = %v", got)
	}
	if got := (&Config{Schedule: ScheduleOff}).Next(now); !got.IsZero() {
		t.Errorf("off next = %v", got)
	}
}

func TestScheduledUploadPrunesAndReportsFailuresOnce(t *testing.T) {
	svc, _ := newTestService(t)
	fake, s3cfg := newFakeS3(t)
	ctx := context.Background()

	var events []store.Event
	svc.OnEvent(func(_ context.Context, e store.Event) { events = append(events, e) })

	clock := time.Date(2026, 9, 24, 4, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return clock }
	stored := Settings(&Config{S3: s3cfg, Schedule: ScheduleDaily, Hour: 3, Keep: 2})
	svc.Load(stored)

	if !svc.due() {
		t.Fatal("first run should be due at once")
	}
	for i := range 3 {
		clock = clock.Add(time.Duration(i+1) * time.Hour)
		if _, err := svc.Upload(ctx, true); err != nil {
			t.Fatal(err)
		}
	}
	if keys := fake.keys(); len(keys) != 2 || keys[1] != "panel/"+FileName(clock, false) {
		t.Fatalf("bucket after 3 backups with keep=2: %v", keys)
	}
	if svc.due() {
		t.Fatal("due again right after a backup")
	}
	if st := svc.Status(); st.LastBackupName != FileName(clock, false) || st.NextRunAt == nil {
		t.Fatalf("status = %+v", st)
	}

	clock = clock.Add(24 * time.Hour)
	fake.fail = 100
	for range 2 {
		svc.Upload(ctx, true)
	}
	if svc.due() {
		t.Fatal("a failed run must wait before retrying")
	}
	failed := 0
	for _, e := range events {
		if e.Kind == "backup.failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("backup.failed events = %d, want 1", failed)
	}
	if st := svc.Status(); st.LastError == "" {
		t.Fatalf("status after failure = %+v", st)
	}

	clock = clock.Add(retryAfter)
	if !svc.due() {
		t.Fatal("retry should be due")
	}
}

func TestUploadWithoutBucket(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Upload(context.Background(), false); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("upload = %v", err)
	}
}
