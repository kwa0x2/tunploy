package peer

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// A share link shows a device's owner its config and usage without an
// account on the panel: what a VPN seller sends a customer. The page always
// has the current config, so a device moved to another server needs no new link.

// Share makes a new link and drops the old one, so sending it again is also
// how an owner who leaked theirs gets a fresh one. A nil expiresAt keeps the
// link until it is removed.
func (s *Service) Share(ctx context.Context, in *wg.Instance, p *wg.Peer, expiresAt *time.Time) (*wg.Peer, error) {
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, apperr.Field("expires_at", "expires_at must be in the future")
	}
	shared, err := s.store.SetPeerShare(ctx, p.ID, rand.Text(), expiresAt)
	if err != nil {
		return nil, err
	}
	e := event.ForPeer("device.shared", in, shared)
	if expiresAt != nil {
		e.Detail = "until " + event.Expiry(*expiresAt)
	}
	s.events.Record(ctx, e)
	return shared, nil
}

// Unshare is not an error for a link that is not there, so a retry is safe.
func (s *Service) Unshare(ctx context.Context, in *wg.Instance, p *wg.Peer) error {
	if p.ShareToken == "" {
		return nil
	}
	if _, err := s.store.SetPeerShare(ctx, p.ID, "", nil); err != nil {
		return err
	}
	s.events.Record(ctx, event.ForPeer("device.unshared", in, p))
	return nil
}

// Shared is the device behind a link. A link that is gone, expired or never
// was is the same not_found: the page cannot tell them apart, and neither
// can a guesser.
func (s *Service) Shared(ctx context.Context, token string) (*wg.Instance, *wg.Peer, error) {
	gone := apperr.New(apperr.NotFound, "not_found", "this link has expired or was removed")
	p, err := s.store.PeerByShareToken(ctx, token)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, gone
	}
	if err != nil {
		return nil, nil, err
	}
	if p.ShareExpiresAt != nil && !time.Now().Before(*p.ShareExpiresAt) {
		return nil, nil, gone
	}
	in, err := s.store.InstanceByID(ctx, p.InstanceID)
	if err != nil {
		return nil, nil, err
	}
	return in, p, nil
}
