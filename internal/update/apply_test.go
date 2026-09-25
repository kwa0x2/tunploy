package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeSwap struct {
	dir        string
	steps      []string
	replaceErr error
	startErr   error
	exited     bool
	healthy    bool
	logLine    string
	// migrate stands for the new version rewriting the database on start.
	migrate func()
	// What the old panel would read as it starts again.
	resultAtRestore Result
}

func (f *fakeSwap) Replace(ctx context.Context, id, image string, stop time.Duration) (Replacement, error) {
	f.steps = append(f.steps, "stop old")
	if f.replaceErr != nil {
		return &fakeReplacement{f}, f.replaceErr
	}
	return &fakeReplacement{f}, nil
}

func (f *fakeSwap) Running(ctx context.Context, id string) (bool, int, error) {
	return !f.exited, 1, nil
}

func (f *fakeSwap) ExecUnmanaged(ctx context.Context, id string, cmd []string) ([]byte, error) {
	if !f.healthy {
		return nil, errors.New("connection refused")
	}
	return nil, nil
}

func (f *fakeSwap) TailUnmanaged(ctx context.Context, id string, lines int) (string, error) {
	return f.logLine, nil
}

type fakeReplacement struct{ f *fakeSwap }

func (r *fakeReplacement) Start(ctx context.Context) (string, error) {
	r.f.steps = append(r.f.steps, "start new")
	if r.f.migrate != nil {
		r.f.migrate()
	}
	return "new", r.f.startErr
}

func (r *fakeReplacement) Commit(ctx context.Context) error {
	r.f.steps = append(r.f.steps, "remove old")
	return nil
}

func (r *fakeReplacement) Discard(ctx context.Context) error {
	r.f.steps = append(r.f.steps, "remove new")
	return nil
}

func (r *fakeReplacement) Restore(ctx context.Context) error {
	r.f.steps = append(r.f.steps, "start old")
	r.f.resultAtRestore, _, _ = readResult(r.f.dir)
	return nil
}

func applyOpts(dir string) ApplyOptions {
	return ApplyOptions{
		Container: "old", Image: "tunploy:2", DataDir: dir, From: "1", To: "2",
		HealthTimeout: 200 * time.Millisecond, HealthEvery: 10 * time.Millisecond,
	}
}

func writeDB(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "tunploy.db"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readDB(t *testing.T, dir string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "tunploy.db"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestApplySuccess(t *testing.T) {
	dir := t.TempDir()
	writeDB(t, dir, "v1")
	f := &fakeSwap{dir: dir, healthy: true, migrate: func() { writeDB(t, dir, "v2") }}

	if err := Apply(context.Background(), f, applyOpts(dir)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"stop old", "start new", "remove old"}; !slices.Equal(f.steps, want) {
		t.Fatalf("steps = %v", f.steps)
	}
	if got := readDB(t, dir); got != "v2" {
		t.Fatalf("database = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "tunploy.db"+snapshotSuffix)); !os.IsNotExist(err) {
		t.Fatalf("snapshot left behind: %v", err)
	}
	if r, ok, _ := readResult(dir); !ok || r != (Result{From: "1", To: "2"}) {
		t.Fatalf("result = %+v, %v", r, ok)
	}
}

func TestApplyRollsBackAnUnhealthyPanel(t *testing.T) {
	dir := t.TempDir()
	writeDB(t, dir, "v1")
	f := &fakeSwap{
		dir:     dir,
		exited:  true,
		logLine: `{"level":"ERROR","msg":"tunploy stopped","error":"run migration 9: boom"}`,
		migrate: func() {
			writeDB(t, dir, "v2 half migrated")
			_ = os.WriteFile(filepath.Join(dir, "tunploy.db-wal"), []byte("wal"), 0o600)
		},
	}

	err := Apply(context.Background(), f, applyOpts(dir))
	if err == nil || !strings.Contains(err.Error(), "run migration 9: boom") {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"stop old", "start new", "remove new", "start old"}; !slices.Equal(f.steps, want) {
		t.Fatalf("steps = %v", f.steps)
	}
	if got := readDB(t, dir); got != "v1" {
		t.Fatalf("database = %q, want the one from before the update", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "tunploy.db-wal")); !os.IsNotExist(err) {
		t.Fatalf("the new version's log must go with it: %v", err)
	}
	if !strings.Contains(f.resultAtRestore.Error, "boom") {
		t.Fatalf("the old panel must find the reason as it starts: %+v", f.resultAtRestore)
	}
}

func TestApplyGivesUpOnAPanelThatNeverAnswers(t *testing.T) {
	dir := t.TempDir()
	writeDB(t, dir, "v1")
	f := &fakeSwap{dir: dir}

	err := Apply(context.Background(), f, applyOpts(dir))
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v", err)
	}
	if f.steps[len(f.steps)-1] != "start old" {
		t.Fatalf("steps = %v", f.steps)
	}
}

func TestApplyRestoresWhenStopFails(t *testing.T) {
	dir := t.TempDir()
	writeDB(t, dir, "v1")
	f := &fakeSwap{dir: dir, replaceErr: errors.New("stop container: timeout")}

	if err := Apply(context.Background(), f, applyOpts(dir)); err == nil {
		t.Fatal("want an error")
	}
	if want := []string{"stop old", "remove new", "start old"}; !slices.Equal(f.steps, want) {
		t.Fatalf("steps = %v", f.steps)
	}
	if got := readDB(t, dir); got != "v1" {
		t.Fatalf("database = %q", got)
	}
}
