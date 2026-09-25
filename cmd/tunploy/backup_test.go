package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/backup"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// pipedPrompter reads like a non-terminal stdin, as with `echo pass | tunploy ...`.
func pipedPrompter(t *testing.T, input string) *prompter {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	os.WriteFile(path, []byte(input), 0o600)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return newPrompter(f, &bytes.Buffer{})
}

func TestBackupRestoreFromFile(t *testing.T) {
	ctx := context.Background()

	// The panel that made the backup, with its own passphrase.
	srcDir := t.TempDir()
	src, err := store.Open(filepath.Join(srcDir, "tunploy.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.CreateInstance(ctx, wg.NewInstance("Home", "vpn.example.com")); err != nil {
		t.Fatal(err)
	}
	f, err := backup.Create(ctx, src, srcDir, filepath.Join(srcDir, "tmp"), "v1.2.3", time.Now(), "correct horse battery")
	src.Close()
	if err != nil {
		t.Fatal(err)
	}

	// A fresh panel on a new server, which knows nothing of that passphrase.
	dataDir := t.TempDir()
	t.Setenv("TUNPLOY_DATA_DIR", dataDir)

	var out bytes.Buffer
	if err := backupRestore([]string{f.Path}, pipedPrompter(t, "correct horse battery\n"), &out); err == nil ||
		!strings.Contains(err.Error(), "--yes") {
		t.Fatalf("restore without --yes off a terminal = %v", err)
	}

	err = backupRestore([]string{"--yes", f.Path}, pipedPrompter(t, "wrong horse battery\n"), &out)
	if !errors.Is(err, backup.ErrWrongPassphrase) || strings.Contains(err.Error(), "not a Tunploy backup") {
		t.Fatalf("wrong passphrase = %v", err)
	}
	if backup.RebuildPending(dataDir) {
		t.Fatal("a failed restore asked for a rebuild")
	}

	out.Reset()
	if err := backupRestore([]string{"--yes", f.Path}, pipedPrompter(t, "correct horse battery\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Tunploy v1.2.3") || !strings.Contains(out.String(), "docker restart tunploy") {
		t.Fatalf("output = %q", out.String())
	}
	if !backup.RebuildPending(dataDir) {
		t.Fatal("no rebuild was asked for")
	}

	st, err := store.Open(filepath.Join(dataDir, "tunploy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	instances, _ := st.Instances(ctx)
	if len(instances) != 1 || instances[0].Name != "Home" {
		t.Fatalf("instances after restore = %+v", instances)
	}
}

func TestBackupRestoreNeedsOneSource(t *testing.T) {
	t.Setenv("TUNPLOY_DATA_DIR", t.TempDir())
	for _, args := range [][]string{{}, {"--s3", "x", "file.tar.gz"}} {
		if err := backupRestore(args, pipedPrompter(t, ""), &bytes.Buffer{}); err == nil {
			t.Errorf("restore %v succeeded", args)
		}
	}
	err := backupRestore([]string{"--yes", "--s3", "tunploy-backup-20260101-000000.tar.gz"}, pipedPrompter(t, ""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no S3 bucket") {
		t.Errorf("restore from S3 without a bucket = %v", err)
	}
}
