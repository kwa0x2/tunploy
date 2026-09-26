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

func (s *Server) peerView(ctx context.Context, p *wg.Peer) (peerView, error) {
	views, err := s.peerViews(ctx, p.InstanceID, []wg.Peer{*p})
	if err != nil {
		return peerView{}, err
	}
	return views[0], nil
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
	reset, err := s.resetPeerUsage(r.Context(), in, p)
	if err != nil {
		return err
	}
	view, err := s.peerView(r.Context(), reset)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
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
	created, err := s.createPeer(r.Context(), in, p)
	if errors.Is(err, wg.ErrSubnetFull) {
		return httpx.Errorf(http.StatusConflict, "subnet_full", "no free addresses left in %s", in.Subnet())
	}
	if err != nil {
		return err
	}
	view, err := s.peerView(r.Context(), created)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, view)
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
	updated, err := s.updatePeer(r.Context(), in, *p, next)
	if err != nil {
		return err
	}
	view, err := s.peerView(r.Context(), updated)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

func (s *Server) handleDeletePeer(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	if err := s.deletePeer(r.Context(), in, p); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// The panel and /api/v1 share these three, so both record the same events.
// A failed apply still returns the saved peer along with the error.
func (s *Server) createPeer(ctx context.Context, in *wg.Instance, p wg.Peer) (*wg.Peer, error) {
	if fields := p.Validate(); len(fields) > 0 {
		return nil, httpx.Invalid(fields)
	}
	created, err := s.store.CreatePeer(ctx, p)
	if err != nil {
		if errors.Is(err, wg.ErrSubnetFull) {
			return nil, err
		}
		return nil, peerWriteError(err)
	}
	e := peerEvent("device.created", in, created)
	if hasLimits(created) {
		e.Detail = limitsDetail(created)
	}
	s.record(ctx, e)
	if err := s.deploy.Apply(ctx, in.ID); err != nil {
		return created, applyError(err)
	}
	return created, nil
}

func (s *Server) updatePeer(ctx context.Context, in *wg.Instance, before, p wg.Peer) (*wg.Peer, error) {
	if fields := p.Validate(); len(fields) > 0 {
		return nil, httpx.Invalid(fields)
	}
	updated, err := s.store.UpdatePeer(ctx, p)
	if err != nil {
		return nil, peerWriteError(err)
	}
	if updated.Name != before.Name {
		e := peerEvent("device.renamed", in, updated)
		e.Detail = "was " + before.Name
		s.record(ctx, e)
	}
	if updated.Enabled != before.Enabled {
		kind := "device.disabled"
		if updated.Enabled {
			kind = "device.enabled"
		}
		s.record(ctx, peerEvent(kind, in, updated))
	}
	if limitsDetail(updated) != limitsDetail(&before) {
		e := peerEvent("device.limits_changed", in, updated)
		e.Detail = limitsDetail(updated)
		s.record(ctx, e)
	}
	if err := s.deploy.Apply(ctx, updated.InstanceID); err != nil {
		return updated, applyError(err)
	}
	return updated, nil
}

// A device blocked by its limit comes back at once.
func (s *Server) resetPeerUsage(ctx context.Context, in *wg.Instance, p *wg.Peer) (*wg.Peer, error) {
	used, err := s.store.PeersLimitUsage(ctx, []int64{p.ID}, time.Now())
	if err != nil {
		return nil, err
	}
	reset, err := s.store.ResetPeerUsage(ctx, p.ID, time.Now())
	if err != nil {
		return nil, err
	}
	e := peerEvent("device.usage_reset", in, reset)
	e.Detail = fmt.Sprintf("%s used %s", formatBytes(used[p.ID].Total()), periodText(*p))
	s.record(ctx, e)
	if err := s.deploy.Apply(ctx, in.ID); err != nil {
		return reset, applyError(err)
	}
	return reset, nil
}

func (s *Server) deletePeer(ctx context.Context, in *wg.Instance, p *wg.Peer) error {
	if err := s.store.DeletePeer(ctx, p.ID); err != nil {
		return err
	}
	s.record(ctx, peerEvent("device.deleted", in, p))
	if err := s.deploy.Apply(ctx, p.InstanceID); err != nil {
		return applyError(err)
	}
	return nil
}

// Most clients name the tunnel after the file, capped at 15 characters.
func (s *Server) handlePeerConfig(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.conf"`, tunnelName(p.Name)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(wg.ClientConfig(*in, *p))
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
}

func hasLimits(p *wg.Peer) bool { return p.DataLimit > 0 || p.ExpiresAt != nil }

func limitsDetail(p *wg.Peer) string {
	var parts []string
	if p.DataLimit > 0 {
		per := " a month"
		if p.LimitPeriod == wg.PeriodTotal {
			per = " in total"
		}
		parts = append(parts, formatBytes(p.DataLimit)+per)
	}
	if p.ExpiresAt != nil {
		parts = append(parts, "until "+expiryText(*p.ExpiresAt))
	}
	if len(parts) == 0 {
		return "no limits"
	}
	return strings.Join(parts, ", ")
}

// periodText finishes "used 3 GB of 5 GB …".
func periodText(p wg.Peer) string {
	monthly := p.LimitPeriod != wg.PeriodTotal
	if r := p.UsageResetAt; r != nil && (!monthly || !r.Before(store.MonthStart(time.Now()))) {
		return "since " + r.In(time.Local).Format("2 Jan 2006 15:04")
	}
	if monthly {
		return "this month"
	}
	return "in total"
}

// The panel sets expiries at midnight, which reads better as the day before.
func expiryText(t time.Time) string {
	t = t.In(time.Local)
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		return "the end of " + t.AddDate(0, 0, -1).Format("2 Jan 2006")
	}
	return t.Format("2 Jan 2006 15:04")
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n)/unit, 0
	for v >= unit && i < 3 {
		v /= unit
		i++
	}
	if v < 10 && v != float64(int64(v)) {
		return fmt.Sprintf("%.1f %s", v, []string{"KB", "MB", "GB", "TB"}[i])
	}
	return fmt.Sprintf("%.0f %s", v, []string{"KB", "MB", "GB", "TB"}[i])
}

func peerWriteError(err error) error {
	var dup *store.DuplicateError
	if errors.As(err, &dup) {
		switch dup.Column {
		case "name":
			return httpx.Invalid(map[string]string{"name": "a peer with this name already exists"})
		case "public_key":
			return httpx.Invalid(map[string]string{"public_key": "another device already uses this public key"})
		}
	}
	return err
}

// Saved but not live; the next start or restart picks it up.
func applyError(err error) error {
	return httpx.Errorf(http.StatusBadGateway, "apply_failed",
		"saved, but the running tunnel could not be updated (restart it to apply): %v", err)
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
