package wg

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxNameLength = 64
	minSubnetBits = 16
	maxSubnetBits = 30
	minMTU        = 1280
	maxMTU        = 1500

	maxExternalIDLength = 255
	maxMetadataBytes    = 4096
)

func (in Instance) Validate() map[string]string {
	fields := map[string]string{}

	if msg := checkName(in.Name); msg != "" {
		fields["name"] = msg
	}

	switch {
	case !in.Address.IsValid():
		fields["address"] = "address is required"
	case !in.Address.Addr().Is4():
		fields["address"] = "address must be an IPv4 address with a prefix, e.g. 10.8.0.1/24"
	case in.Address.Bits() < minSubnetBits || in.Address.Bits() > maxSubnetBits:
		fields["address"] = "subnet must be between /16 and /30"
	case !isHostAddress(in.Address):
		fields["address"] = "address must be a host inside the subnet, not its network or broadcast address"
	}

	if in.ListenPort < 1 || in.ListenPort > 65535 {
		fields["listen_port"] = "listen port must be between 1 and 65535"
	}

	if in.Endpoint == "" {
		fields["endpoint"] = "endpoint is required"
	} else if !ValidHost(in.Endpoint) {
		fields["endpoint"] = "endpoint must be a hostname or IP address, without a port"
	}

	for _, a := range in.DNS {
		if !a.IsValid() {
			fields["dns"] = "dns contains an invalid address"
			break
		}
	}

	if in.MTU != 0 && (in.MTU < minMTU || in.MTU > maxMTU) {
		fields["mtu"] = "mtu must be between 1280 and 1500, or 0 for the default"
	}

	if in.PersistentKeepalive < 0 || in.PersistentKeepalive > 65535 {
		fields["persistent_keepalive"] = "persistent keepalive must be between 0 and 65535 seconds"
	}

	if len(in.ClientAllowedIPs) == 0 {
		fields["client_allowed_ips"] = "at least one allowed IP range is required"
	}
	for _, p := range in.ClientAllowedIPs {
		if !p.IsValid() {
			fields["client_allowed_ips"] = "client allowed IPs contains an invalid range"
			break
		}
	}

	return fields
}

func (p Peer) Validate() map[string]string {
	fields := map[string]string{}
	if msg := checkName(p.Name); msg != "" {
		fields["name"] = msg
	}
	if p.DataLimit < 0 {
		fields["data_limit"] = "data limit must be 0 (no limit) or more"
	}
	switch {
	case len(p.ExternalID) > maxExternalIDLength:
		fields["external_id"] = "external_id must be at most 255 bytes"
	case strings.IndexFunc(p.ExternalID, unicode.IsControl) >= 0:
		fields["external_id"] = "external_id must not contain control characters"
	}
	if len(p.Metadata) > 0 {
		switch {
		case len(p.Metadata) > maxMetadataBytes:
			fields["metadata"] = "metadata must be at most 4 KB"
		case !json.Valid(p.Metadata) || !bytes.HasPrefix(bytes.TrimSpace(p.Metadata), []byte("{")):
			fields["metadata"] = "metadata must be a JSON object"
		}
	}
	return fields
}

// A newline in a config comment would let a name smuggle in a PostUp line.
func checkName(name string) string {
	switch {
	case strings.TrimSpace(name) == "":
		return "name is required"
	case utf8.RuneCountInString(name) > maxNameLength:
		return "name must be at most 64 characters"
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "name must not contain control characters"
	}
	return ""
}

func ValidHost(host string) bool {
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !validLabel(label) {
			return false
		}
	}
	return true
}

func validLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
