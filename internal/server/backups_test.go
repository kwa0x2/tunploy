package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kwa0x2/tunploy/internal/deploy"
)

func TestExportAndImportBackup(t *testing.T) {
	p := newPanel(t)
	home := p.createInstance(map[string]any{"name": "Home"})
	p.createPeer(home.ID, "phone")

	rec := p.do("GET", "/api/backups/export", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/gzip" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "tunploy-backup-") {
		t.Fatalf("export = %d %v", rec.Code, rec.Header())
	}
	archive := rec.Body.Bytes()

	p.want(p.do("DELETE", fmt.Sprintf("/api/instances/%d", home.ID), nil), http.StatusNoContent, nil)
	office := p.createInstance(map[string]any{"name": "Office"})

	req := httptest.NewRequest("POST", "/api/backups/import?name=home.tar.gz", bytes.NewReader(archive))
	req.AddCookie(p.cookie)
	rec = httptest.NewRecorder()
	p.s.ServeHTTP(rec, req)
	var res restoreResult
	p.want(rec, http.StatusOK, &res)
	if res.Name != "home.tar.gz" || res.Version != "test" || len(res.Warnings) != 0 {
		t.Fatalf("restore result = %+v", res)
	}

	if rec := p.do("GET", "/api/instances", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old session after restore = %d, want 401", rec.Code)
	}
	p.cookie = sessionCookieFrom(t, do(t, p.s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2",
	}))

	var list []instanceJSON
	p.want(p.do("GET", "/api/instances", nil), http.StatusOK, &list)
	if len(list) != 1 || list[0].Name != "Home" || list[0].PeerCount != 1 || list[0].Status.State != "running" {
		t.Fatalf("instances after restore = %+v", list)
	}
	if _, ok := p.fake.Container(deploy.ContainerName(office.ID)); ok {
		t.Fatal("the Office container outlived the restore")
	}

	var events []struct{ Kind string }
	p.want(p.do("GET", "/api/events?limit=2", nil), http.StatusOK, &events)
	if len(events) != 2 || events[1].Kind != "backup.restored" {
		t.Fatalf("latest event = %+v", events)
	}
}

func TestImportRejectsOtherFiles(t *testing.T) {
	p := newPanel(t)
	req := httptest.NewRequest("POST", "/api/backups/import", strings.NewReader("not a backup"))
	req.AddCookie(p.cookie)
	rec := httptest.NewRecorder()
	p.s.ServeHTTP(rec, req)
	p.wantError(rec, http.StatusUnprocessableEntity, "invalid_backup")

	if rec := p.do("GET", "/api/auth/me", nil); rec.Code != http.StatusOK {
		t.Fatalf("a rejected file signed the admin out: %d", rec.Code)
	}
}

func TestBackupSettingsValidation(t *testing.T) {
	p := newPanel(t)

	var got backupSettings
	p.want(p.do("GET", "/api/settings/backups", nil), http.StatusOK, &got)
	if got.Connected || got.Schedule != "off" || got.Keep != 7 {
		t.Fatalf("defaults = %+v", got)
	}

	e := p.wantError(p.do("PUT", "/api/settings/backups", map[string]any{
		"endpoint": "ftp://x", "bucket": "a", "prefix": "../x", "schedule": "hourly", "hour": 24, "keep": -1,
	}), http.StatusUnprocessableEntity, "validation_failed")
	for _, f := range []string{"endpoint", "bucket", "prefix", "access_key", "secret_key", "schedule", "hour", "keep"} {
		if e.Error.Fields[f] == "" {
			t.Errorf("no error for %s: %v", f, e.Error.Fields)
		}
	}

	p.wantError(p.do("POST", "/api/backups", nil), http.StatusConflict, "conflict")
	p.wantError(p.do("GET", "/api/backups", nil), http.StatusConflict, "conflict")
	p.wantError(p.do("GET", "/api/backups/..%2Fetc", nil), http.StatusNotFound, "not_found")
}

func TestEncryptedBackups(t *testing.T) {
	p := newPanel(t)
	p.wantError(p.do("PUT", "/api/settings/backups/encryption", map[string]any{"passphrase": "short"}),
		http.StatusUnprocessableEntity, "validation_failed")

	var got backupSettings
	p.want(p.do("PUT", "/api/settings/backups/encryption", map[string]any{"passphrase": "correct horse battery"}),
		http.StatusOK, &got)
	if !got.Encrypted {
		t.Fatalf("settings = %+v", got)
	}

	rec := p.do("GET", "/api/backups/export", nil)
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".tar.gz.enc") {
		t.Fatalf("export = %v", rec.Header())
	}
	archive := rec.Body.Bytes()

	importBackup := func(passphrase string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/backups/import", bytes.NewReader(archive))
		req.AddCookie(p.cookie)
		if passphrase != "" {
			req.Header.Set("X-Backup-Passphrase", url.PathEscape(passphrase))
		}
		rec := httptest.NewRecorder()
		p.s.ServeHTTP(rec, req)
		return rec
	}

	p.want(p.do("PUT", "/api/settings/backups/encryption", map[string]any{"passphrase": "a different passphrase"}),
		http.StatusOK, nil)
	p.wantError(importBackup(""), http.StatusUnprocessableEntity, "wrong_passphrase")
	p.want(p.do("PUT", "/api/settings/backups/encryption", map[string]any{"passphrase": ""}), http.StatusOK, nil)
	p.wantError(importBackup(""), http.StatusUnprocessableEntity, "passphrase_required")

	// The panel's own passphrase opens it without asking, and survives the restore.
	p.want(p.do("PUT", "/api/settings/backups/encryption", map[string]any{"passphrase": "correct horse battery"}),
		http.StatusOK, nil)
	p.want(importBackup(""), http.StatusOK, nil)
	if p.s.backups.Passphrase() != "correct horse battery" {
		t.Fatal("the passphrase did not survive the restore")
	}
	p.cookie = sessionCookieFrom(t, do(t, p.s, "POST", "/api/auth/login", map[string]string{
		"email": "admin@example.com", "password": "hunter2hunter2",
	}))
	p.want(p.do("PUT", "/api/settings/backups/encryption", map[string]any{"passphrase": "şifre ğüç ı 42 +/%"}),
		http.StatusOK, nil)
	rec = p.do("GET", "/api/backups/export", nil)
	archive = rec.Body.Bytes()
	p.want(p.do("PUT", "/api/settings/backups/encryption", map[string]any{"passphrase": ""}), http.StatusOK, nil)
	p.want(importBackup("şifre ğüç ı 42 +/%"), http.StatusOK, nil)
}
