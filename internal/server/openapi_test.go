package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

type openAPIDoc struct {
	Paths map[string]map[string]struct {
		Scope string `json:"x-scope"`
	} `json:"paths"`
	Components struct {
		Schemas struct {
			EventKind struct {
				Enum []string `json:"enum"`
			} `json:"EventKind"`
		} `json:"schemas"`
	} `json:"components"`
}

// A route or scope added without its documentation fails here, and so does
// documentation for a route that is gone.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	var doc openAPIDoc
	if err := json.Unmarshal(openAPISpec, &doc); err != nil {
		t.Fatal(err)
	}
	documented := map[string]string{}
	for path, ops := range doc.Paths {
		for method, o := range ops {
			documented[strings.ToUpper(method)+" "+path] = o.Scope
		}
	}

	p := newPanel(t)
	for _, e := range p.s.apiEndpoints() {
		method, path, _ := strings.Cut(e.pattern, " ")
		key := method + " " + strings.TrimPrefix(path, "/api/v1")
		scope, ok := documented[key]
		if !ok {
			t.Errorf("%s is not in openapi.json", e.pattern)
			continue
		}
		if scope != e.scope {
			t.Errorf("%s: openapi.json says scope %q, the route needs %q", e.pattern, scope, e.scope)
		}
		delete(documented, key)
	}
	for key := range documented {
		t.Errorf("openapi.json documents %s, which has no route", key)
	}

	if !slices.Equal(doc.Components.Schemas.EventKind.Enum, webhookKinds) {
		t.Errorf("EventKind enum %v differs from webhookKinds %v", doc.Components.Schemas.EventKind.Enum, webhookKinds)
	}
}

func TestOpenAPIIsPublic(t *testing.T) {
	p := newPanel(t)
	rec := httptest.NewRecorder()
	p.s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/openapi.json", nil))
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET openapi.json = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
