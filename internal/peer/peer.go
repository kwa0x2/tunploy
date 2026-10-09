// Package peer runs the commands on devices. Each one saves the change,
// records an event and updates the running tunnel, so the panel and
// /api/v1 behave the same.
package peer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// Tunnels brings a running server in line with what is saved; the deploy
// manager in production.
type Tunnels interface {
	Apply(ctx context.Context, instanceID int64) error
}

// Commands save first and then update the tunnel. When only the update
// fails, they return the saved peer along with an apply_failed error.
type Service struct {
	store   *store.Store
	tunnels Tunnels
	events  event.Recorder
}

func New(st *store.Store, tunnels Tunnels, events event.Recorder) *Service {
	return &Service{store: st, tunnels: tunnels, events: events}
}

func (s *Service) Create(ctx context.Context, in *wg.Instance, p wg.Peer) (*wg.Peer, error) {
	if err := apperr.Fields(p.Validate()); err != nil {
		return nil, err
	}
	created, err := s.store.CreatePeer(ctx, p)
	if errors.Is(err, wg.ErrSubnetFull) {
		return nil, subnetFull(in, err)
	}
	if err != nil {
		return nil, writeError(err)
	}
	e := event.ForPeer("device.created", in, created)
	if event.HasLimits(created) {
		e.Detail = event.Limits(created)
	}
	s.events.Record(ctx, e)
	return created, s.apply(ctx, in.ID)
}

func (s *Service) Update(ctx context.Context, in *wg.Instance, before, after wg.Peer) (*wg.Peer, error) {
	updated, err := s.save(ctx, in, before, after)
	if err != nil {
		return nil, err
	}
	return updated, s.apply(ctx, in.ID)
}

// save is Update without touching the tunnel, for changes to many peers.
func (s *Service) save(ctx context.Context, in *wg.Instance, before, after wg.Peer) (*wg.Peer, error) {
	if err := apperr.Fields(after.Validate()); err != nil {
		return nil, err
	}
	updated, err := s.store.UpdatePeer(ctx, after)
	if err != nil {
		return nil, writeError(err)
	}
	if updated.Name != before.Name {
		e := event.ForPeer("device.renamed", in, updated)
		e.Detail = "was " + before.Name
		s.events.Record(ctx, e)
	}
	if updated.Enabled != before.Enabled {
		kind := "device.disabled"
		if updated.Enabled {
			kind = "device.enabled"
		}
		s.events.Record(ctx, event.ForPeer(kind, in, updated))
	}
	if event.Limits(updated) != event.Limits(&before) {
		e := event.ForPeer("device.limits_changed", in, updated)
		e.Detail = event.Limits(updated)
		s.events.Record(ctx, e)
	}
	return updated, nil
}

// ResetUsage starts the limit count over, so a device blocked by its limit
// comes back at once.
func (s *Service) ResetUsage(ctx context.Context, in *wg.Instance, p *wg.Peer) (*wg.Peer, error) {
	reset, err := s.resetUsage(ctx, in, p)
	if err != nil {
		return nil, err
	}
	return reset, s.apply(ctx, in.ID)
}

func (s *Service) resetUsage(ctx context.Context, in *wg.Instance, p *wg.Peer) (*wg.Peer, error) {
	now := time.Now()
	used, err := s.store.PeersLimitUsage(ctx, []int64{p.ID}, now)
	if err != nil {
		return nil, err
	}
	reset, err := s.store.ResetPeerUsage(ctx, p.ID, now)
	if err != nil {
		return nil, err
	}
	e := event.ForPeer("device.usage_reset", in, reset)
	e.Detail = fmt.Sprintf("%s used %s", event.Bytes(used[p.ID].Total()), event.Period(*p, now))
	s.events.Record(ctx, e)
	return reset, nil
}

// Move keeps the device's keys, limits and history; it gets an address on
// the new server. Both tunnels change: the old one lets it go, the new one
// takes it.
func (s *Service) Move(ctx context.Context, from, to *wg.Instance, p *wg.Peer) (*wg.Peer, error) {
	moved, err := s.store.MovePeer(ctx, p.ID, to.ID)
	var dup *store.DuplicateError
	switch {
	case errors.Is(err, wg.ErrSubnetFull):
		return nil, subnetFull(to, err)
	case errors.As(err, &dup) && dup.Column == "name":
		return nil, apperr.Field("name", "%q already has a device named %q; rename one first", to.Name, p.Name)
	case err != nil:
		return nil, err
	}
	e := event.ForPeer("device.moved", to, moved)
	e.Detail = "from " + from.Name
	s.events.Record(ctx, e)
	if err := errors.Join(s.tunnels.Apply(ctx, from.ID), s.tunnels.Apply(ctx, to.ID)); err != nil {
		return moved, applyError(err)
	}
	return moved, nil
}

func (s *Service) Delete(ctx context.Context, in *wg.Instance, p *wg.Peer) error {
	if err := s.remove(ctx, in, p); err != nil {
		return err
	}
	return s.apply(ctx, in.ID)
}

func (s *Service) remove(ctx context.Context, in *wg.Instance, p *wg.Peer) error {
	if err := s.store.DeletePeer(ctx, p.ID); err != nil {
		return err
	}
	s.events.Record(ctx, event.ForPeer("device.deleted", in, p))
	return nil
}

func (s *Service) apply(ctx context.Context, instanceID int64) error {
	if err := s.tunnels.Apply(ctx, instanceID); err != nil {
		return applyError(err)
	}
	return nil
}

// The panel's code; /api/v1 answers server_full, found with errors.Is.
func subnetFull(in *wg.Instance, err error) error {
	return &apperr.Error{Kind: apperr.Conflict, Code: "subnet_full", Err: err,
		Message: fmt.Sprintf("no free addresses left in %s", in.Subnet())}
}

func writeError(err error) error {
	var dup *store.DuplicateError
	if errors.As(err, &dup) {
		switch dup.Column {
		case "name":
			return apperr.Field("name", "a peer with this name already exists")
		case "public_key":
			return apperr.Field("public_key", "another device already uses this public key")
		}
	}
	return err
}

// Saved but not live; the next start or restart picks it up.
func applyError(err error) error {
	return &apperr.Error{Kind: apperr.Upstream, Code: "apply_failed", Err: err,
		Message: "saved, but the running tunnel could not be updated (restart it to apply): " + err.Error()}
}
