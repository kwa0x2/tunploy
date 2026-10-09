package api

import (
	"encoding/json"
	"net/http"
)

const ndjson = "application/x-ndjson"

// streamNDJSON starts a 200 that sends one JSON value per line as each is
// ready, for long work whose steps the client shows. Errors after this go
// out as lines too, since the status is already sent.
func streamNDJSON(w http.ResponseWriter) func(v any) {
	h := w.Header()
	h.Set("Content-Type", ndjson)
	h.Set("Cache-Control", "no-store")
	// Stops nginx and similar proxies from holding lines back.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	enc := json.NewEncoder(w)
	return func(v any) {
		if err := enc.Encode(v); err == nil {
			rc.Flush()
		}
	}
}
