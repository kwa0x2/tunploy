// Package geoip maps IPs to countries with the free DB-IP Lite database.
package geoip

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

const (
	fileName   = "dbip-country-lite.mmdb"
	maxAge     = 30 * 24 * time.Hour
	checkEvery = 24 * time.Hour
)

var downloadURL = "https://download.db-ip.com/free/dbip-country-lite-%s.mmdb.gz"

type DB struct {
	path string

	mu     sync.RWMutex
	reader *maxminddb.Reader
}

func Open(dir string) *DB {
	db := &DB{path: filepath.Join(dir, fileName)}
	if err := db.load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("load geoip database", "error", err)
	}
	return db
}

func (db *DB) Country(addr netip.Addr) string {
	addr = addr.Unmap()
	if db == nil || !addr.IsValid() || addr.IsPrivate() || addr.IsLoopback() {
		return ""
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.reader == nil {
		return ""
	}
	var rec struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := db.reader.Lookup(addr).Decode(&rec); err != nil {
		return ""
	}
	return rec.Country.ISOCode
}

func (db *DB) Run(ctx context.Context) {
	for {
		if err := db.refresh(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			slog.Warn("update geoip database", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkEvery):
		}
	}
}

func (db *DB) refresh(ctx context.Context, now time.Time) error {
	if info, err := os.Stat(db.path); err == nil && now.Sub(info.ModTime()) < maxAge {
		return nil
	}
	// A new month's file is published a few days in, so fall back a month.
	// From the 1st: on the 31st, AddDate(0, -1, 0) can land in the same month.
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var err error
	for _, month := range []time.Time{first, first.AddDate(0, -1, 0)} {
		if err = db.download(ctx, month.Format("2006-01")); err == nil {
			slog.Info("geoip database updated", "month", month.Format("2006-01"))
			return db.load()
		}
	}
	return err
}

func (db *DB) download(ctx context.Context, month string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(downloadURL, month), nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", month, res.Status)
	}

	gz, err := gzip.NewReader(res.Body)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(db.path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(db.path), ".geoip-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, gz); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	check, err := maxminddb.Open(tmp.Name())
	if err != nil {
		return fmt.Errorf("downloaded file is not a valid database: %w", err)
	}
	check.Close()
	return os.Rename(tmp.Name(), db.path)
}

func (db *DB) load() error {
	r, err := maxminddb.Open(db.path)
	if err != nil {
		return err
	}
	db.mu.Lock()
	old := db.reader
	db.reader = r
	db.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}
