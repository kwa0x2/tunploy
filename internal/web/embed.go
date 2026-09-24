// Package web serves the compiled panel UI embedded in the binary.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// zeroTime disables the Last-Modified header; an embedded file has no
// meaningful modification time.
var zeroTime time.Time

//go:embed all:dist
var distFS embed.FS

const notBuiltMessage = `Tunploy's web UI is not built into this binary.

Run "make build" to compile it in, or "make dev" to work against the Vite dev
server instead.
`

// Handler serves the SPA: real files come from the build output, and every
// other path falls back to index.html so client-side routes work on reload.
func Handler() http.Handler {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		return notBuiltHandler()
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return notBuiltHandler()
	}

	files := http.FileServerFS(dist)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || !fs.ValidPath(name) {
			serveIndex(w, r, index)
			return
		}

		info, err := fs.Stat(dist, name)
		if err != nil || info.IsDir() {
			serveIndex(w, r, index)
			return
		}

		// Vite fingerprints everything under assets/, so it can be cached
		// indefinitely; index.html must never be.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, index []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", zeroTime, bytes.NewReader(index))
}

func notBuiltHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(notBuiltMessage))
	})
}
