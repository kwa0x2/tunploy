package server

import (
	"fmt"
	"net/http"
	"testing"
)

func TestInstanceLogs(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	p.fake.LogOutput = "2026-09-24T10:00:00Z wireguard wg0 is up\n"
	path := fmt.Sprintf("/api/instances/%d/logs", in.ID)

	rec := p.do("GET", path+"?tail=50&follow=1", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != p.fake.LogOutput {
		t.Fatalf("want the log text, got %d: %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("content type = %q", ct)
	}

	p.wantError(p.do("GET", path+"?tail=0", nil), http.StatusBadRequest, "invalid_request")
	p.wantError(p.do("GET", path+"?tail=abc", nil), http.StatusBadRequest, "invalid_request")

	p.fake.RemoveContainer(t.Context(), p.s.deploy.ContainerName(1))
	p.wantError(p.do("GET", path, nil), http.StatusConflict, "conflict")

	p.wantError(p.do("GET", "/api/instances/999/logs", nil), http.StatusNotFound, "not_found")
}
