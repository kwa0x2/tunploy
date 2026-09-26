package wg

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Disabled peers are left out, which is how disabling cuts a client off.
func ServerConfig(in Instance, peers []Peer) []byte {
	var b bytes.Buffer

	b.WriteString("[Interface]\n")
	field(&b, "PrivateKey", in.PrivateKey.String())
	field(&b, "Address", in.Address.String())
	field(&b, "ListenPort", strconv.Itoa(in.ListenPort))
	// wg-quick would take eth0's MTU, 65535 on some Docker setups.
	mtu := in.MTU
	if mtu == 0 {
		mtu = DefaultMTU
	}
	field(&b, "MTU", strconv.Itoa(mtu))

	for _, p := range peers {
		if !p.Enabled {
			continue
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "# %s\n", commentSafe(p.Name))
		b.WriteString("[Peer]\n")
		field(&b, "PublicKey", p.PublicKey.String())
		field(&b, "PresharedKey", p.PresharedKey.String())
		field(&b, "AllowedIPs", netip.PrefixFrom(p.Address, p.Address.BitLen()).String())
	}

	return b.Bytes()
}

func ClientConfig(in Instance, p Peer) []byte {
	var b bytes.Buffer

	b.WriteString("[Interface]\n")
	// The client that holds the key fills it in.
	if !p.KeyOnClient() {
		field(&b, "PrivateKey", p.PrivateKey.String())
	}
	field(&b, "Address", netip.PrefixFrom(p.Address, p.Address.BitLen()).String())
	if dns := in.ClientDNS(); len(dns) > 0 {
		field(&b, "DNS", join(dns))
	}
	if in.MTU > 0 {
		field(&b, "MTU", strconv.Itoa(in.MTU))
	}

	b.WriteString("\n[Peer]\n")
	field(&b, "PublicKey", in.PublicKey.String())
	field(&b, "PresharedKey", p.PresharedKey.String())
	field(&b, "AllowedIPs", join(in.ClientAllowedIPs))
	field(&b, "Endpoint", net.JoinHostPort(in.Endpoint, strconv.Itoa(in.ListenPort)))
	if in.PersistentKeepalive > 0 {
		field(&b, "PersistentKeepalive", strconv.Itoa(in.PersistentKeepalive))
	}

	return b.Bytes()
}

// ErrSplitTunnel: a kill switch blocks what bypasses the tunnel, which on a
// split tunnel is everything but the VPN.
var ErrSplitTunnel = errors.New("the server's clients send only some traffic through the tunnel")

// The rules from wg-quick(8): anything not leaving through the tunnel, and
// not WireGuard's own marked packets, is refused while it is up.
const killSwitchRule = "OUTPUT ! -o %i -m mark ! --mark $(wg show %i fwmark) -m addrtype ! --dst-type LOCAL -j REJECT"

// KillSwitchConfig is ClientConfig for wg-quick on Linux, with firewall rules
// that stop traffic while the tunnel is down. Phone and Windows apps refuse
// PostUp lines, and do the same with their own settings.
func KillSwitchConfig(in Instance, p Peer) ([]byte, error) {
	if !in.FullTunnel() {
		return nil, ErrSplitTunnel
	}
	conf := ClientConfig(in, p)
	var rules bytes.Buffer
	field(&rules, "PostUp", "iptables -I "+killSwitchRule+" && ip6tables -I "+killSwitchRule)
	field(&rules, "PreDown", "iptables -D "+killSwitchRule+" && ip6tables -D "+killSwitchRule)
	peer := bytes.Index(conf, []byte("\n[Peer]"))
	return slices.Concat(conf[:peer], rules.Bytes(), conf[peer:]), nil
}

// SpeedLimitsConfig lists each limited peer's address and kbit/s, one per
// line, for the shaper in the server's container.
func SpeedLimitsConfig(peers []Peer) []byte {
	var b bytes.Buffer
	for _, p := range peers {
		if p.Enabled && p.SpeedLimit > 0 {
			fmt.Fprintf(&b, "%s %d\n", p.Address, p.SpeedLimit)
		}
	}
	return b.Bytes()
}

// ClientDNS is what clients are told to ask.
func (in Instance) ClientDNS() []netip.Addr {
	if in.DNSOnServer {
		return []netip.Addr{in.Address.Addr()}
	}
	return in.DNS
}

// ResolverConfig is the upstream list for the resolver on the server, in
// dnsmasq's servers-file format.
func ResolverConfig(in Instance) []byte {
	var b bytes.Buffer
	for _, a := range in.DNS {
		fmt.Fprintf(&b, "server=%s\n", a)
	}
	return b.Bytes()
}

// FullTunnel reports whether clients send all IPv4 traffic through the server.
func (in Instance) FullTunnel() bool {
	return slices.ContainsFunc(in.ClientAllowedIPs, func(p netip.Prefix) bool {
		return p.Addr().Is4() && p.Bits() == 0
	})
}

func field(b *bytes.Buffer, key, value string) {
	fmt.Fprintf(b, "%s = %s\n", key, value)
}

func join[T fmt.Stringer](items []T) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = it.String()
	}
	return strings.Join(parts, ", ")
}

// Configs run as root, so a name must never end its comment line.
func commentSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
