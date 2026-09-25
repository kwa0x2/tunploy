package geoip

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// testdata/country.mmdb was written with github.com/maxmind/mmdbwriter and holds
// 203.0.113.0/24 = TR, 2001:db8::/32 = DE and 10.0.0.0/8 = XX.
func fixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/country.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCountry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), fixture(t), 0o600); err != nil {
		t.Fatal(err)
	}
	db := Open(dir)

	for _, tt := range []struct {
		addr netip.Addr
		want string
	}{
		{netip.MustParseAddr("203.0.113.7"), "TR"},
		{netip.MustParseAddr("::ffff:203.0.113.7"), "TR"},
		{netip.MustParseAddr("2001:db8::1"), "DE"},
		{netip.MustParseAddr("198.51.100.1"), ""},
		// In the database, but a private address never has a country.
		{netip.MustParseAddr("10.1.2.3"), ""},
		{netip.MustParseAddr("127.0.0.1"), ""},
		{netip.Addr{}, ""},
	} {
		if got := db.Country(tt.addr); got != tt.want {
			t.Errorf("Country(%v) = %q, want %q", tt.addr, got, tt.want)
		}
	}

	var off *DB
	if got := off.Country(netip.MustParseAddr("203.0.113.7")); got != "" {
		t.Errorf("a nil DB (GeoIP turned off) gave %q", got)
	}
	if got := Open(t.TempDir()).Country(netip.MustParseAddr("203.0.113.7")); got != "" {
		t.Errorf("before the first download got %q", got)
	}
}

type mirror struct {
	mu    sync.Mutex
	files map[string][]byte
	hits  []string
}

func (m *mirror) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	month := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/dbip-country-lite-"), ".mmdb.gz")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hits = append(m.hits, month)
	body, ok := m.files[month]
	if !ok {
		http.NotFound(w, r)
		return
	}
	gz := gzip.NewWriter(w)
	gz.Write(body)
	gz.Close()
}

func (m *mirror) took() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	hits := m.hits
	m.hits = nil
	return hits
}

func serve(t *testing.T, files map[string][]byte) *mirror {
	t.Helper()
	m := &mirror{files: files}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	old := downloadURL
	downloadURL = srv.URL + "/dbip-country-lite-%s.mmdb.gz"
	t.Cleanup(func() { downloadURL = old })
	return m
}

func TestRefresh(t *testing.T) {
	ctx := context.Background()
	// The 31st, and October's file is not out yet.
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	m := serve(t, map[string][]byte{"2026-09": fixture(t)})

	dir := t.TempDir()
	db := Open(dir)
	if err := db.refresh(ctx, now); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if hits := m.took(); !slices.Equal(hits, []string{"2026-10", "2026-09"}) {
		t.Fatalf("asked for %v, want this month then the one before", hits)
	}
	if got := db.Country(netip.MustParseAddr("203.0.113.7")); got != "TR" {
		t.Fatalf("after the download got %q", got)
	}

	path := filepath.Join(dir, fileName)
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	if err := db.refresh(ctx, now.Add(maxAge-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if hits := m.took(); len(hits) != 0 {
		t.Fatalf("a fresh file was downloaded again: %v", hits)
	}

	// A broken download must neither replace the file nor the loaded reader.
	m.files = map[string][]byte{"2026-11": []byte("not a database")}
	later := now.Add(maxAge + time.Hour)
	if err := db.refresh(ctx, later); err == nil {
		t.Fatal("a broken file must be reported")
	}
	if got := db.Country(netip.MustParseAddr("203.0.113.7")); got != "TR" {
		t.Fatalf("after a broken download got %q", got)
	}
	if body, _ := os.ReadFile(path); !bytes.Equal(body, fixture(t)) {
		t.Fatal("a broken download replaced the good file")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}
