package deploy

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/host"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// Watch is the only caller of RecordTraffic, which needs a single writer per
// instance. Nodes are watched side by side so a slow one delays no other.
func (m *Manager) Watch(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		instances, err := m.store.Instances(ctx)
		if err != nil {
			slog.Warn("watch peers", "error", err)
			continue
		}
		var nodes sync.WaitGroup
		for nodeID, group := range byNode(instances) {
			h, err := m.hostFor(nodeID)
			if err != nil {
				continue
			}
			nodes.Go(func() {
				for i := range group {
					if err := m.watchInstance(ctx, h, &group[i]); err != nil && ctx.Err() == nil {
						slog.Debug("watch peers", "instance", group[i].ID, "error", err)
					}
				}
			})
		}
		nodes.Wait()
	}
}

func byNode(instances []wg.Instance) map[int64][]wg.Instance {
	out := map[int64][]wg.Instance{}
	for _, in := range instances {
		out[in.NodeID] = append(out[in.NodeID], in)
	}
	return out
}

func (m *Manager) watchInstance(ctx context.Context, h host.Host, in *wg.Instance) error {
	m.checkHealth(ctx, h, in)
	stats, err := m.peerStats(ctx, h, in)
	if err != nil {
		return err
	}
	if err := m.store.RecordTraffic(ctx, in.ID, stats, m.now()); err != nil {
		return err
	}
	return m.enforceLimits(ctx, h, in)
}

// A stop from the panel exits cleanly, so only a crash loop or a failed exit counts as down.
func (m *Manager) checkHealth(ctx context.Context, h host.Host, in *wg.Instance) {
	ct, err := m.container(ctx, h, in.ID)
	if err != nil {
		return
	}
	var st Status
	if ct != nil {
		st = statusOf(*ct)
	}
	down := st.State == StateRestarting || (st.State == StateStopped && st.Error != "")

	m.stateMu.Lock()
	was, known := m.down[in.ID]
	m.down[in.ID] = down
	m.stateMu.Unlock()
	if !known || was == down {
		return
	}
	kind := "server.recovered"
	if down {
		kind = "server.down"
	}
	e := event.ForInstance(kind, in)
	e.Detail = st.Error
	if in.NodeID != 0 {
		if n, err := m.store.NodeByID(ctx, in.NodeID); err == nil {
			e.NodeName = n.Name
		}
	}
	m.events.Record(ctx, e)
}

func (m *Manager) PeerStats(ctx context.Context, in *wg.Instance) (map[wg.Key]wg.PeerStats, error) {
	h, err := m.hostFor(in.NodeID)
	if err != nil {
		return nil, err
	}
	return m.peerStats(ctx, h, in)
}

func (m *Manager) peerStats(ctx context.Context, h host.Host, in *wg.Instance) (map[wg.Key]wg.PeerStats, error) {
	ct, err := m.container(ctx, h, in.ID)
	if err != nil {
		return nil, err
	}
	if ct == nil || !ct.Running() {
		m.activity.forget(in.ID)
		return map[wg.Key]wg.PeerStats{}, nil
	}
	out, err := h.Exec(ctx, ct.Name, []string{"wg", "show", iface, "dump"})
	if err != nil {
		return nil, err
	}
	stats, err := wg.ParseDump(out)
	if err != nil {
		return nil, err
	}
	if changes := m.activity.observe(in.ID, stats, in.PersistentKeepalive, m.now()); len(changes) > 0 {
		m.reportActivity(in, changes)
	}
	return stats, nil
}

// A page load can spot a change too, but the change is not the request's: it
// must outlive it and carry no actor.
func (m *Manager) reportActivity(in *wg.Instance, changes []PeerChange) {
	ctx := context.Background()
	peers, err := m.store.Peers(ctx, in.ID)
	if err != nil {
		return
	}
	for _, c := range changes {
		i := slices.IndexFunc(peers, func(p wg.Peer) bool { return p.PublicKey == c.Key })
		if i < 0 {
			continue
		}
		kind := "device.connected"
		if !c.Online {
			kind = "device.disconnected"
		}
		e := event.ForPeer(kind, in, &peers[i])
		e.IP = endpointHost(c.Endpoint)
		if !c.Online && !c.OnlineSince.IsZero() {
			e.Detail = "online for " + event.Duration(m.now().Sub(c.OnlineSince))
		}
		m.events.Record(ctx, e)
	}
}

func endpointHost(endpoint string) string {
	ap, err := netip.ParseAddrPort(endpoint)
	if err != nil {
		return ""
	}
	return ap.Addr().Unmap().String()
}

// Online is what the watcher last saw, without asking WireGuard.
func (m *Manager) Online(instanceID int64) map[wg.Key]bool { return m.activity.online(instanceID) }
