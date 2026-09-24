package wg

import (
	"errors"
	"net/netip"
	"testing"
)

func TestNextAddress(t *testing.T) {
	addrs := func(ss ...string) []netip.Addr {
		out := make([]netip.Addr, len(ss))
		for i, s := range ss {
			out[i] = netip.MustParseAddr(s)
		}
		return out
	}

	tests := []struct {
		name   string
		subnet string
		used   []netip.Addr
		want   string
	}{
		{"skips the network address", "10.8.0.0/24", nil, "10.8.0.1"},
		{"skips the server", "10.8.0.0/24", addrs("10.8.0.1"), "10.8.0.2"},
		{"fills gaps first", "10.8.0.0/24", addrs("10.8.0.1", "10.8.0.3"), "10.8.0.2"},
		{"masks a host prefix", "10.8.0.1/24", addrs("10.8.0.1"), "10.8.0.2"},
		{"crosses an octet", "10.8.0.0/16", addrs("10.8.0.1", "10.8.0.254", "10.8.0.255"), "10.8.0.2"},
		{"uses the last host", "10.8.0.0/30", addrs("10.8.0.1"), "10.8.0.2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextAddress(netip.MustParsePrefix(tt.subnet), tt.used)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNextAddressNeverReturnsBroadcast(t *testing.T) {
	subnet := netip.MustParsePrefix("10.8.0.0/24")
	var used []netip.Addr
	for {
		a, err := NextAddress(subnet, used)
		if errors.Is(err, ErrSubnetFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		used = append(used, a)
	}
	if len(used) != 254 {
		t.Fatalf("allocated %d addresses from a /24, want 254", len(used))
	}
	if last := used[len(used)-1]; last.String() != "10.8.0.254" {
		t.Fatalf("last allocation = %s, want 10.8.0.254", last)
	}
}

func TestNextAddressRejectsUnsupportedSubnets(t *testing.T) {
	for _, s := range []string{"10.8.0.0/31", "10.8.0.1/32", "fd00::/64"} {
		if _, err := NextAddress(netip.MustParsePrefix(s), nil); !errors.Is(err, ErrSubnetFull) {
			t.Errorf("%s: want ErrSubnetFull, got %v", s, err)
		}
	}
}
