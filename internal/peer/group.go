package peer

import (
	"context"
	"errors"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// A group is every device with one external_id: a customer's phone and
// laptop, changed together when their plan renews or ends.

// UpdateGroup checks every device before it saves any, so a bad value
// changes none.
func (s *Service) UpdateGroup(ctx context.Context, before, after []wg.Peer) ([]wg.Peer, error) {
	for _, p := range after {
		if err := apperr.Fields(p.Validate()); err != nil {
			return nil, err
		}
	}
	return s.each(ctx, before, func(in *wg.Instance, i int) (*wg.Peer, error) {
		return s.save(ctx, in, before[i], after[i])
	})
}

func (s *Service) DeleteGroup(ctx context.Context, peers []wg.Peer) error {
	_, err := s.each(ctx, peers, func(in *wg.Instance, i int) (*wg.Peer, error) {
		return nil, s.remove(ctx, in, &peers[i])
	})
	return err
}

func (s *Service) ResetGroupUsage(ctx context.Context, peers []wg.Peer) ([]wg.Peer, error) {
	return s.each(ctx, peers, func(in *wg.Instance, i int) (*wg.Peer, error) {
		return s.resetUsage(ctx, in, &peers[i])
	})
}

// each saves a change to every peer, then updates each tunnel it touched
// once, even when a save failed partway.
func (s *Service) each(ctx context.Context, peers []wg.Peer, save func(in *wg.Instance, i int) (*wg.Peer, error)) ([]wg.Peer, error) {
	instances := map[int64]*wg.Instance{}
	var touched []int64
	out := make([]wg.Peer, 0, len(peers))
	var saveErr error
	for i, p := range peers {
		in, ok := instances[p.InstanceID]
		if !ok {
			if in, saveErr = s.store.InstanceByID(ctx, p.InstanceID); saveErr != nil {
				break
			}
			instances[p.InstanceID] = in
			touched = append(touched, in.ID)
		}
		saved, err := save(in, i)
		if err != nil {
			saveErr = err
			break
		}
		if saved != nil {
			out = append(out, *saved)
		}
	}

	var applyErrs []error
	for _, id := range touched {
		applyErrs = append(applyErrs, s.tunnels.Apply(ctx, id))
	}
	if saveErr != nil {
		return nil, saveErr
	}
	if err := errors.Join(applyErrs...); err != nil {
		return nil, applyError(err)
	}
	return out, nil
}
