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

// Configs run as root, so a name must never end its comment line.
func commentSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
