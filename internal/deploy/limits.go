package deploy

import (
	"context"
	"fmt"
	"maps"

	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/host"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// Limits change with time and traffic alone, so nothing else would notice.
func (m *Manager) enforceLimits(ctx context.Context, h host.Host, in *wg.Instance) error {
	defer m.lock(in.NodeID)()

	peers, usage, next, err := m.blockedPeers(ctx, in.ID)
	if err != nil {
		return err
	}
	m.stateMu.Lock()
	prev, known := m.blocked[in.ID]
	m.stateMu.Unlock()
	if known && maps.Equal(prev, next) {
		return nil
	}
	if err := m.apply(ctx, h, in); err != nil {
		return err
	}
	if !known {
		return nil
	}
	for _, p := range peers {
		if prev[p.ID] != next[p.ID] {
			m.events.Record(ctx, m.blockEvent(in, p, next[p.ID], usage[p.ID]))
		}
	}
	return nil
}

// An empty reason means the peer may connect again.
func (m *Manager) blockEvent(in *wg.Instance, p wg.Peer, reason wg.Block, used wg.Traffic) store.Event {
	switch reason {
	case wg.BlockLimit:
		e := event.ForPeer("device.limit_reached", in, &p)
		e.Detail = fmt.Sprintf("used %s of %s %s", event.Bytes(used.Total()), event.Bytes(p.DataLimit), event.Period(p, m.now()))
		return e
	case wg.BlockExpired:
		return event.ForPeer("device.expired", in, &p)
	}
	return event.ForPeer("device.unblocked", in, &p)
}

func (m *Manager) blockedPeers(ctx context.Context, instanceID int64) ([]wg.Peer, map[int64]wg.Traffic, map[int64]wg.Block, error) {
	now := m.now()
	peers, err := m.store.Peers(ctx, instanceID)
	if err != nil {
		return nil, nil, nil, err
	}
	usage, err := m.store.LimitUsage(ctx, instanceID, now)
	if err != nil {
		return nil, nil, nil, err
	}
	blocked := map[int64]wg.Block{}
	for _, p := range peers {
		if b := p.Blocked(usage[p.ID], now); b != "" {
			blocked[p.ID] = b
		}
	}
	return peers, usage, blocked, nil
}
