package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

const (
	usageDays   = 30
	usageMonths = 12
)

type peerRequest struct {
	Name        *string             `json:"name"`
	Enabled     *bool               `json:"enabled"`
	DataLimit   *int64              `json:"data_limit"`
	LimitPeriod *wg.LimitPeriod     `json:"limit_period"`
	ExpiresAt   optional[time.Time] `json:"expires_at"`
	SpeedLimit  *int64              `json:"speed_limit"`
}

// optional tells an explicit null, which clears a value, from a missing field.
type optional[T any] struct {
	Set   bool
	Value *T
}

func (o *optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	o.Value = new(T)
	return json.Unmarshal(b, o.Value)
}

type peerView struct {
	wg.Peer
	KeyOnClient bool          `json:"key_on_client,omitempty"`
	Stats       *wg.PeerStats `json:"stats,omitempty"`
	Country     string        `json:"country,omitempty"`
	MonthUsage  wg.Traffic    `json:"month_usage"`
	// What counts toward the data limit.
	PeriodUsage wg.Traffic `json:"period_usage"`
	Blocked     wg.Block   `json:"blocked,omitempty"`
}

type usageView struct {
	Daily   []store.UsagePoint `json:"daily"`
	Monthly []store.UsagePoint `json:"monthly"`
}

func (s *Server) handleListPeers(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	peers, err := s.store.Peers(r.Context(), in.ID)
	if err != nil {
		return err
	}

	// Stats are optional: the list must load while Docker is away.
	stats, err := s.deploy.PeerStats(r.Context(), in)
	if err != nil {
		slog.Warn("read peer stats", "instance", in.ID, "error", err)
	}

	views, err := s.peerViews(r.Context(), in.ID, peers)
	if err != nil {
		return err
	}
	for i := range views {
		if st, ok := stats[views[i].PublicKey]; ok {
			views[i].Stats = &st
			views[i].Country = s.endpointCountry(st.Endpoint)
		}
	}
	return httpx.JSON(w, http.StatusOK, views)
}

func (s *Server) peerViews(ctx context.Context, instanceID int64, peers []wg.Peer) ([]peerView, error) {
	now := time.Now()
	month, err := s.store.MonthUsage(ctx, instanceID, now)
	if err != nil {
		return nil, err
	}
	used, err := s.store.LimitUsage(ctx, instanceID, now)
	if err != nil {
		return nil, err
	}
	views := make([]peerView, len(peers))
	for i, p := range peers {
		views[i] = peerView{Peer: p, KeyOnClient: p.KeyOnClient(), MonthUsage: month[p.ID], PeriodUsage: used[p.ID],
			Blocked: p.Blocked(used[p.ID], now)}
	}
	return views, nil
}

func (s *Server) writePeer(w http.ResponseWriter, r *http.Request, status int, p *wg.Peer) error {
	views, err := s.peerViews(r.Context(), p.InstanceID, []wg.Peer{*p})
	if err != nil {
		return err
	}
	return httpx.JSON(w, status, views[0])
}

func (s *Server) handlePeerUsage(w http.ResponseWriter, r *http.Request) error {
	_, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	daily, monthly, err := s.store.PeerUsage(r.Context(), p.ID, time.Now(), usageDays, usageMonths)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, usageView{Daily: daily, Monthly: monthly})
}

func (s *Server) handleResetPeerUsage(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	reset, err := s.peers.ResetUsage(r.Context(), in, p)
	if err != nil {
		return err
	}
	return s.writePeer(w, r, http.StatusOK, reset)
}

func (s *Server) handleMovePeer(w http.ResponseWriter, r *http.Request) error {
	from, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	var req struct {
		InstanceID int64 `json:"instance_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.InstanceID == from.ID {
		return httpx.Invalid(map[string]string{"instance_id": "the peer is already on this server"})
	}
	to, err := s.store.InstanceByID(r.Context(), req.InstanceID)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.Invalid(map[string]string{"instance_id": "no such server"})
	}
	if err != nil {
		return err
	}
	moved, err := s.peers.Move(r.Context(), from, to, p)
	if err != nil {
		return err
	}
	return s.writePeer(w, r, http.StatusOK, moved)
}

func (s *Server) handleCreatePeer(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	var req peerRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	p := wg.NewPeer(in.ID, "")
	req.apply(&p)
	created, err := s.peers.Create(r.Context(), in, p)
	if err != nil {
		return err
	}
	return s.writePeer(w, r, http.StatusCreated, created)
}

func (s *Server) handleUpdatePeer(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	var req peerRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	next := *p
	req.apply(&next)
	updated, err := s.peers.Update(r.Context(), in, *p, next)
	if err != nil {
		return err
	}
	return s.writePeer(w, r, http.StatusOK, updated)
}

func (s *Server) handleDeletePeer(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	if err := s.peers.Delete(r.Context(), in, p); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// Most clients name the tunnel after the file, capped at 15 characters.
func (s *Server) handlePeerConfig(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	conf, err := clientConfig(r, in, p)
	if err != nil {
		return err
	}
	return writeConfig(w, p, conf)
}

// clientConfig is the device's config, with a Linux kill switch when the
// request asks for one.
func clientConfig(r *http.Request, in *wg.Instance, p *wg.Peer) ([]byte, error) {
	switch r.URL.Query().Get("kill_switch") {
	case "", "false":
		return wg.ClientConfig(*in, *p), nil
	case "true":
		conf, err := wg.KillSwitchConfig(*in, *p)
		if errors.Is(err, wg.ErrSplitTunnel) {
			return nil, httpx.Errorf(http.StatusConflict, "split_tunnel",
				"a kill switch needs a server whose clients send all traffic through it (client allowed IPs 0.0.0.0/0)")
		}
		return conf, err
	default:
		return nil, httpx.BadRequest("kill_switch must be true or false")
	}
}

func writeConfig(w http.ResponseWriter, p *wg.Peer, conf []byte) error {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.conf"`, tunnelName(p.Name)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(conf)
	return err
}

func (s *Server) peerFromPath(r *http.Request) (*wg.Instance, *wg.Peer, error) {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return nil, nil, err
	}
	id, err := strconv.ParseInt(r.PathValue("peerID"), 10, 64)
	if err != nil {
		return nil, nil, httpx.NotFound("peer not found")
	}
	p, err := s.store.PeerByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && p.InstanceID != in.ID) {
		return nil, nil, httpx.NotFound("peer not found")
	}
	return in, p, err
}

func (req peerRequest) apply(p *wg.Peer) {
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.DataLimit != nil {
		p.DataLimit = *req.DataLimit
	}
	if req.LimitPeriod != nil {
		p.LimitPeriod = *req.LimitPeriod
	}
	if req.ExpiresAt.Set {
		p.ExpiresAt = req.ExpiresAt.Value
	}
	if req.SpeedLimit != nil {
		p.SpeedLimit = *req.SpeedLimit
	}
}

func tunnelName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if b.Len() == 15 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("_=+.-", r):
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "wg"
	}
	return b.String()
}
