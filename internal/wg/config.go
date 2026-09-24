package wg

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
)

// ServerConfig renders the wg-quick config for the instance. Disabled peers
// are left out, which is how disabling cuts a client off.
func ServerConfig(in Instance, peers []Peer) []byte {
	var b bytes.Buffer

	b.WriteString("[Interface]\n")
	field(&b, "PrivateKey", in.PrivateKey.String())
	field(&b, "Address", in.Address.String())
	field(&b, "ListenPort", strconv.Itoa(in.ListenPort))
	// Left to itself wg-quick derives the MTU from the container's eth0,
	// which is 65535 on some Docker setups and would fragment every packet.
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

// ClientConfig renders the file a peer imports into its WireGuard app.
func ClientConfig(in Instance, p Peer) []byte {
	var b bytes.Buffer

	b.WriteString("[Interface]\n")
	field(&b, "PrivateKey", p.PrivateKey.String())
	field(&b, "Address", netip.PrefixFrom(p.Address, p.Address.BitLen()).String())
	if len(in.DNS) > 0 {
		field(&b, "DNS", join(in.DNS))
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

// commentSafe backs up Validate: config files run as root, so a name must
// never be able to end its comment line.
func commentSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
