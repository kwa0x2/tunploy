package instance

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// Defaults is a new server on a node as the create form starts it.
func (s *Service) Defaults(ctx context.Context, nodeID int64) (wg.Instance, error) {
	existing, err := s.store.Instances(ctx)
	if err != nil {
		return wg.Instance{}, err
	}
	in, err := s.defaults(ctx, existing, nodeID)
	if errors.Is(err, store.ErrNotFound) {
		return wg.Instance{}, apperr.New(apperr.NotFound, "not_found", "node not found")
	}
	if err != nil {
		return wg.Instance{}, err
	}
	in.Country = s.country(ctx, in.Endpoint)
	return in, nil
}

// Subnets stay apart across nodes too, so a client can hold configs for
// several servers at once; ports only need to differ per machine.
func (s *Service) defaults(ctx context.Context, existing []wg.Instance, nodeID int64) (wg.Instance, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return wg.Instance{}, err
	}
	endpoint := settings.PublicHost
	if endpoint == "" {
		endpoint = s.publicHost
	}
	if nodeID != 0 {
		n, err := s.store.NodeByID(ctx, nodeID)
		if err != nil {
			return wg.Instance{}, err
		}
		endpoint = n.Host
	}
	in := wg.NewInstance("", endpoint)
	in.NodeID = nodeID
	in.Address = nextFreeSubnet(existing, wg.DefaultAddress.Bits())
	in.ListenPort = nextFreePort(existing, nodeID)
	in.DNS = slices.Clone(settings.DefaultDNS)
	return in, nil
}

// country guesses where a server is from its endpoint, for location lists.
func (s *Service) country(ctx context.Context, host string) string {
	if addr, err := netip.ParseAddr(host); err == nil {
		return s.geo.Country(addr)
	}
	if s.geo == nil || !wg.ValidHost(host) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addrs, err := s.lookupHost(ctx, host)
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if addr, err := netip.ParseAddr(a); err == nil {
			if c := s.geo.Country(addr); c != "" {
				return c
			}
		}
	}
	return ""
}

// Pick is the running server with the fewest devices that still has room,
// in the country or city when they are given. It never picks skip, the
// server a device is leaving.
func (s *Service) Pick(ctx context.Context, country, city string, skip int64) (*wg.Instance, error) {
	country, city = strings.TrimSpace(country), strings.TrimSpace(city)
	instances, err := s.store.Instances(ctx)
	if err != nil {
		return nil, err
	}
	instances = slices.DeleteFunc(instances, func(in wg.Instance) bool {
		return in.ID == skip || (country != "" && !strings.EqualFold(in.Country, country)) ||
			(city != "" && !strings.EqualFold(in.City, city))
	})
	counts, err := s.store.PeerCounts(ctx)
	if err != nil {
		return nil, err
	}
	statuses := s.deploy.Statuses(ctx, instances)

	var best *wg.Instance
	for i, in := range instances {
		n := counts[in.ID]
		if statuses[in.ID].State != deploy.StateRunning || n >= wg.Capacity(in.Address.Bits()) {
			continue
		}
		if best == nil || n < counts[best.ID] {
			best = &instances[i]
		}
	}
	if best == nil {
		where := ""
		switch {
		case city != "":
			where = " in " + city
		case country != "":
			where = " in " + strings.ToUpper(country)
		}
		return nil, apperr.New(apperr.Conflict, "no_server_available", "no running server%s has free addresses", where)
	}
	return best, nil
}

func overlapping(existing []wg.Instance, addr netip.Prefix) *wg.Instance {
	if !addr.IsValid() {
		return nil
	}
	for i, in := range existing {
		if in.Subnet().Overlaps(addr.Masked()) {
			return &existing[i]
		}
	}
	return nil
}

// Candidates start each 10.x block, so any size up to a /16 is aligned.
func nextFreeSubnet(existing []wg.Instance, bits int) netip.Prefix {
	for second := 8; second <= 255; second++ {
		p := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(second), 0, 1}), bits)
		if overlapping(existing, p) == nil {
			return p
		}
	}
	return netip.PrefixFrom(wg.DefaultAddress.Addr(), bits)
}

// subnetBitsFor is the prefix of the smallest subnet, a /24 at least, that holds n devices.
func subnetBitsFor(n int) (int, bool) {
	for bits := wg.DefaultAddress.Bits(); bits >= wg.MinSubnetBits; bits-- {
		if n <= wg.Capacity(bits) {
			return bits, n >= 1
		}
	}
	return 0, false
}

func nextFreePort(existing []wg.Instance, nodeID int64) int {
	used := make(map[int]bool, len(existing))
	for _, in := range existing {
		if in.NodeID == nodeID {
			used[in.ListenPort] = true
		}
	}
	port := wg.DefaultListenPort
	for used[port] && port < 65535 {
		port++
	}
	return port
}
