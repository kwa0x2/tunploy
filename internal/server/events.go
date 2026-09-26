package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
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

// record never fails the request.
func (s *Server) record(ctx context.Context, e store.Event) {
	if e.IP != "" && e.Country == "" {
		if addr, err := netip.ParseAddr(e.IP); err == nil {
			e.Country = s.geo.Country(addr)
		}
	}

	if e.Actor == "" {
		e.Actor = actorFrom(ctx)
	}
	attrs := []any{"kind", e.Kind}
	for _, kv := range [][2]string{
		{"actor", e.Actor}, {"node", e.NodeName}, {"server", e.InstanceName}, {"device", e.PeerName}, {"ip", e.IP}, {"country", e.Country}, {"detail", e.Detail},
	} {
		if kv[1] != "" {
			attrs = append(attrs, kv[0], kv[1])
		}
	}
	slog.Info("event", attrs...)

	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	id, err := s.store.AddEvent(context.WithoutCancel(ctx), e)
	if err != nil {
		slog.Error("record event", "kind", e.Kind, "error", err)
	}
	s.notifier.Notify(e)
	// Without an ID a receiver could not tell it from the events API.
	if err == nil {
		e.ID = id
		s.queueWebhooks(ctx, e)
	}
}

func instanceEvent(kind string, in *wg.Instance) store.Event {
	return store.Event{Kind: kind, InstanceID: in.ID, InstanceName: in.Name}
}

func peerEvent(kind string, in *wg.Instance, p *wg.Peer) store.Event {
	e := instanceEvent(kind, in)
	e.PeerID, e.PeerName = p.ID, p.Name
	return e
}

func (s *Server) peerChanged(c deploy.PeerChange) {
	ctx := context.Background()
	in, err := s.store.InstanceByID(ctx, c.InstanceID)
	if err != nil {
		return
	}
	peers, err := s.store.Peers(ctx, c.InstanceID)
	if err != nil {
		return
	}
	for i := range peers {
		if peers[i].PublicKey != c.Key {
			continue
		}
		kind := "device.connected"
		if !c.Online {
			kind = "device.disconnected"
		}
		e := peerEvent(kind, in, &peers[i])
		e.IP = endpointHost(c.Endpoint)
		if !c.Online && !c.OnlineSince.IsZero() {
			e.Detail = "online for " + roughDuration(time.Since(c.OnlineSince))
		}
		s.record(ctx, e)
		return
	}
}

func (s *Server) peerBlocked(b deploy.PeerBlock) {
	ctx := context.Background()
	in, err := s.store.InstanceByID(ctx, b.InstanceID)
	if err != nil {
		return
	}
	var e store.Event
	switch b.Reason {
	case wg.BlockLimit:
		e = peerEvent("device.limit_reached", in, &b.Peer)
		e.Detail = fmt.Sprintf("used %s of %s %s", formatBytes(b.Used.Total()), formatBytes(b.Peer.DataLimit),
			periodText(b.Peer))
	case wg.BlockExpired:
		e = peerEvent("device.expired", in, &b.Peer)
	default:
		e = peerEvent("device.unblocked", in, &b.Peer)
	}
	s.record(ctx, e)
}

func endpointHost(endpoint string) string {
	ap, err := netip.ParseAddrPort(endpoint)
	if err != nil {
		return ""
	}
	return ap.Addr().Unmap().String()
}

func (s *Server) endpointCountry(endpoint string) string {
	ap, err := netip.ParseAddrPort(endpoint)
	if err != nil {
		return ""
	}
	return s.geo.Country(ap.Addr())
}

func roughDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
