package instance

import (
	"net/netip"
	"strings"

	"github.com/kwa0x2/tunploy/internal/wg"
)

// Change is a create or an update. Nil fields keep the default or the
// current value, so an update touches only what it sends.
type Change struct {
	// Only on create: a server stays on the machine it was made on.
	NodeID              *int64    `json:"node_id"`
	Name                *string   `json:"name"`
	Address             *string   `json:"address"`
	ListenPort          *int      `json:"listen_port"`
	Endpoint            *string   `json:"endpoint"`
	DNS                 *[]string `json:"dns"`
	DNSOnServer         *bool     `json:"dns_on_server"`
	MTU                 *int      `json:"mtu"`
	PersistentKeepalive *int      `json:"persistent_keepalive"`
	ClientAllowedIPs    *[]string `json:"client_allowed_ips"`
	Country             *string   `json:"country"`
	City                *string   `json:"city"`
	// Only on create, instead of address: the smallest subnet that fits.
	MaxDevices *int `json:"max_devices"`
}

// apply returns the fields that did not parse.
func (c Change) apply(in *wg.Instance) map[string]string {
	fields := map[string]string{}

	if c.Name != nil {
		in.Name = strings.TrimSpace(*c.Name)
	}
	if c.Address != nil {
		p, err := netip.ParsePrefix(strings.TrimSpace(*c.Address))
		if err != nil {
			fields["address"] = "address must look like 10.8.0.1/24"
		}
		in.Address = p
	}
	if c.ListenPort != nil {
		in.ListenPort = *c.ListenPort
	}
	if c.Endpoint != nil {
		in.Endpoint = strings.TrimSpace(*c.Endpoint)
	}
	if c.DNS != nil {
		addrs, err := parseEach(*c.DNS, netip.ParseAddr)
		if err != nil {
			fields["dns"] = "dns must be a list of IP addresses"
		}
		in.DNS = addrs
	}
	if c.DNSOnServer != nil {
		in.DNSOnServer = *c.DNSOnServer
	}
	if c.MTU != nil {
		in.MTU = *c.MTU
	}
	if c.PersistentKeepalive != nil {
		in.PersistentKeepalive = *c.PersistentKeepalive
	}
	if c.ClientAllowedIPs != nil {
		prefixes, err := parseEach(*c.ClientAllowedIPs, netip.ParsePrefix)
		if err != nil {
			fields["client_allowed_ips"] = "client allowed IPs must be a list of CIDR ranges"
		}
		in.ClientAllowedIPs = prefixes
	}
	if c.Country != nil {
		in.Country = strings.ToUpper(strings.TrimSpace(*c.Country))
	}
	if c.City != nil {
		in.City = strings.TrimSpace(*c.City)
	}
	return fields
}

func parseEach[T any](items []string, parse func(string) (T, error)) ([]T, error) {
	out := make([]T, 0, len(items))
	for _, s := range items {
		v, err := parse(strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
