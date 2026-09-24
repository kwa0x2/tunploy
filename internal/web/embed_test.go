package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestServesIndexAtRoot(t *testing.T) {
	rec := get(t, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<title>Tunploy</title>") {
		t.Fatalf("root did not serve index.html: %s", rec.Body.String()[:min(200, rec.Body.Len())])
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("index.html must not be cached, got %q", got)
	}
}

// Client-side routes must survive a reload, which means unknown paths fall
// back to index.html rather than 404.
func TestUnknownPathsFallBackToIndex(t *testing.T) {
	for _, path := range []string{"/login", "/setup", "/servers", "/deeply/nested/route"} {
		rec := get(t, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: want 200, got %d", path, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), "<div id=\"root\">") {
			t.Errorf("%s: did not serve the SPA shell", path)
		}
	}
}

func TestAssetsAreImmutablyCached(t *testing.T) {
	index := get(t, "/").Body.String()
	asset := extractAssetPath(t, index)

	rec := get(t, asset)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: want 200, got %d", asset, rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("fingerprinted asset should be immutable, got %q", got)
	}
}

func TestRejectsNonGetMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestTraversalCannotEscapeDist(t *testing.T) {
	for _, path := range []string{"/../go.mod", "/assets/../../go.mod", "/./../../../etc/passwd"} {
		rec := get(t, path)
		if rec.Code != http.StatusOK {
			continue // a refusal is fine too
		}
		if !strings.Contains(rec.Body.String(), "<div id=\"root\">") {
			t.Errorf("%s leaked something that is not the SPA shell", path)
		}
	}
}

func extractAssetPath(t *testing.T, index string) string {
	t.Helper()
	i := strings.Index(index, "/assets/")
	if i < 0 {
		t.Fatal("index.html references no assets")
	}
	rest := index[i:]
	end := strings.IndexAny(rest, "\"'")
	if end < 0 {
		t.Fatal("could not read the asset path out of index.html")
	}
	return rest[:end]
}
