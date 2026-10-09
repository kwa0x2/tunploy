package api

import (
	"net/http"
	"net/netip"
	"strconv"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	defaultEventLimit = 50
	maxEventLimit     = 500
)

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.EventFilter{Limit: defaultEventLimit, Category: q.Get("category")}

	ints := map[string]*int64{"before": &f.Before, "instance_id": &f.InstanceID, "peer_id": &f.PeerID}
	for name, dst := range ints {
		if raw := q.Get(name); raw != "" {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || v < 1 {
				return httpx.BadRequest("%s must be a positive number", name)
			}
			*dst = v
		}
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxEventLimit {
			return httpx.BadRequest("limit must be between 1 and %d", maxEventLimit)
		}
		f.Limit = n
	}
	switch f.Category {
	case "", store.CategoryConnection, store.CategoryAuth, store.CategoryChange:
	default:
		return httpx.BadRequest("unknown category %q", f.Category)
	}

	events, err := s.store.Events(r.Context(), f)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, events)
}

func (s *Server) endpointCountry(endpoint string) string {
	ap, err := netip.ParseAddrPort(endpoint)
	if err != nil {
		return ""
	}
	return s.geo.Country(ap.Addr())
}
