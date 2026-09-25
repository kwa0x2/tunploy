package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	resultName     = "update.json"
	snapshotSuffix = ".pre-update"
)

// The database with its write-ahead log: a copy of one without the other
// can lose the last writes.
var dbFiles = []string{"tunploy.db", "tunploy.db-wal", "tunploy.db-shm"}

// Result is what the updater leaves for whichever panel runs next.
type Result struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Error string `json:"error,omitempty"`
}

func WriteResult(dataDir string, r Result) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dataDir, resultName+".tmp")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write update result: %w", err)
	}
	return os.Rename(tmp, filepath.Join(dataDir, resultName))
}

func readResult(dataDir string) (Result, bool, error) {
	body, err := os.ReadFile(filepath.Join(dataDir, resultName))
	if errors.Is(err, fs.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	var r Result
	if err := json.Unmarshal(body, &r); err != nil {
		removeResult(dataDir)
		return Result{}, false, fmt.Errorf("update result: %w", err)
	}
	return r, true, nil
}

func removeResult(dataDir string) {
	if err := os.Remove(filepath.Join(dataDir, resultName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("remove update result", "error", err)
	}
}

// Swapper is the Docker side of Apply.
type Swapper interface {
	Replace(ctx context.Context, id, image string, stopTimeout time.Duration) (Replacement, error)
	Running(ctx context.Context, id string) (bool, int, error)
	ExecUnmanaged(ctx context.Context, id string, cmd []string) ([]byte, error)
	TailUnmanaged(ctx context.Context, id string, lines int) (string, error)
}

type Replacement interface {
	Start(ctx context.Context) (string, error)
	Commit(ctx context.Context) error
	Discard(ctx context.Context) error
	Restore(ctx context.Context) error
}

type ApplyOptions struct {
	Container string
	Image     string
	DataDir   string
	// From and To are the versions recorded in the result.
	From, To string
	// Longer than the panel's own graceful shutdown.
	StopTimeout time.Duration
	// How long the new panel has to answer its health check.
	HealthTimeout time.Duration
	HealthEvery   time.Duration
}

// Apply swaps the panel's container for one on the new image and leaves a
// Result. When the new panel doesn't come up healthy, the old container and
// its database come back.
func Apply(ctx context.Context, sw Swapper, o ApplyOptions) error {
	r, err := sw.Replace(ctx, o.Container, o.Image, o.StopTimeout)
	if err != nil {
		return o.rollBack(ctx, r, err, false)
	}

	// The new version may migrate the database, which the old one can't read.
	if err := snapshotDB(o.DataDir); err != nil {
		return o.rollBack(ctx, r, fmt.Errorf("back up the database: %w", err), false)
	}

	id, err := r.Start(ctx)
	if err == nil {
		err = waitHealthy(ctx, sw, id, o)
	}
	if err != nil {
		return o.rollBack(ctx, r, err, true)
	}

	if err := r.Commit(ctx); err != nil {
		slog.Warn("remove the previous panel container", "error", err)
	}
	dropSnapshot(o.DataDir)
	o.Report(nil)
	return nil
}

// The result is written before the old panel starts again, so it finds the
// reason as it comes up. r is nil when nothing had changed yet.
func (o ApplyOptions) rollBack(ctx context.Context, r Replacement, cause error, db bool) error {
	// A cancelled ctx must not leave the panel down.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()

	err := cause
	if r != nil {
		if derr := r.Discard(ctx); derr != nil {
			// The new container may still hold the database and the ports.
			err = errors.Join(err, fmt.Errorf("roll back: %w", derr))
			o.Report(err)
			return err
		}
	}
	if db {
		if derr := restoreDB(o.DataDir); derr != nil {
			err = errors.Join(err, fmt.Errorf("roll back the database: %w", derr))
		}
	} else {
		dropSnapshot(o.DataDir)
	}
	o.Report(err)

	if r != nil {
		if rerr := r.Restore(ctx); rerr != nil {
			err = errors.Join(err, fmt.Errorf("roll back: %w", rerr))
			o.Report(err)
		}
	}
	return err
}

// Report writes the result for whichever panel runs next.
func (o ApplyOptions) Report(err error) {
	res := Result{From: o.From, To: o.To}
	if err != nil {
		res.Error = err.Error()
	}
	if werr := WriteResult(o.DataDir, res); werr != nil {
		slog.Error("record update result", "error", werr)
	}
}

func waitHealthy(ctx context.Context, sw Swapper, id string, o ApplyOptions) error {
	ctx, cancel := context.WithTimeout(ctx, o.HealthTimeout)
	defer cancel()
	ticker := time.NewTicker(o.HealthEvery)
	defer ticker.Stop()

	var lastErr error
	for {
		running, code, err := sw.Running(ctx, id)
		switch {
		case err != nil:
			lastErr = err
		case !running:
			return fmt.Errorf("the new version stopped (exit code %d)%s", code, lastWords(ctx, sw, id))
		default:
			_, lastErr = sw.ExecUnmanaged(ctx, id, []string{"/usr/local/bin/tunploy", "health"})
			if lastErr == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the new version did not answer within %s: %w%s", o.HealthTimeout, lastErr, lastWords(ctx, sw, id))
		case <-ticker.C:
		}
	}
}

// lastWords is the new panel's final log line, which is its fatal error when
// it exits; the container is removed by the rollback.
func lastWords(ctx context.Context, sw Swapper, id string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	out, err := sw.TailUnmanaged(ctx, id, 1)
	if err != nil || strings.TrimSpace(out) == "" {
		return ""
	}
	var line struct {
		Msg   string `json:"msg"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(out), &line) == nil && line.Msg != "" {
		if line.Error != "" {
			return ": " + line.Msg + ": " + line.Error
		}
		return ": " + line.Msg
	}
	return ": " + strings.TrimSpace(out)
}

func snapshotDB(dir string) error {
	for _, name := range dbFiles {
		src, dst := filepath.Join(dir, name), filepath.Join(dir, name+snapshotSuffix)
		err := copyFile(src, dst)
		if errors.Is(err, fs.ErrNotExist) {
			err = os.Remove(dst)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// A log file missing from the snapshot did not exist then, so it goes too.
func restoreDB(dir string) error {
	for i, name := range dbFiles {
		dst, src := filepath.Join(dir, name), filepath.Join(dir, name+snapshotSuffix)
		err := os.Rename(src, dst)
		if errors.Is(err, fs.ErrNotExist) && i > 0 {
			err = os.Remove(dst)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func dropSnapshot(dir string) {
	for _, name := range dbFiles {
		if err := os.Remove(filepath.Join(dir, name+snapshotSuffix)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("remove database snapshot", "error", err)
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
