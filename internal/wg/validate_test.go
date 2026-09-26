package wg

import (
	"net/netip"
	"strings"
	"testing"
)

func TestNewInstanceIsValid(t *testing.T) {
	in := NewInstance("Home", "vpn.example.com")
	if fields := in.Validate(); len(fields) > 0 {
		t.Fatalf("defaults should validate, got %v", fields)
	}
	if in.PublicKey != in.PrivateKey.PublicKey() {
		t.Fatal("public key does not belong to the private key")
	}
}

func TestInstanceValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Instance)
		field  string
	}{
		{"empty name", func(in *Instance) { in.Name = "  " }, "name"},
		{"long name", func(in *Instance) { in.Name = strings.Repeat("a", 65) }, "name"},
		{"newline in name", func(in *Instance) { in.Name = "a\nPostUp = id" }, "name"},
		{"missing address", func(in *Instance) { in.Address = netip.Prefix{} }, "address"},
		{"ipv6 address", func(in *Instance) { in.Address = netip.MustParsePrefix("fd00::1/64") }, "address"},
		{"subnet too large", func(in *Instance) { in.Address = netip.MustParsePrefix("10.0.0.1/8") }, "address"},
		{"subnet too small", func(in *Instance) { in.Address = netip.MustParsePrefix("10.8.0.1/31") }, "address"},
		{"network address", func(in *Instance) { in.Address = netip.MustParsePrefix("10.8.0.0/24") }, "address"},
		{"broadcast address", func(in *Instance) { in.Address = netip.MustParsePrefix("10.8.0.255/24") }, "address"},
		{"port zero", func(in *Instance) { in.ListenPort = 0 }, "listen_port"},
		{"port too high", func(in *Instance) { in.ListenPort = 70000 }, "listen_port"},
		{"missing endpoint", func(in *Instance) { in.Endpoint = "" }, "endpoint"},
		{"endpoint with port", func(in *Instance) { in.Endpoint = "vpn.example.com:51820" }, "endpoint"},
		{"endpoint with newline", func(in *Instance) { in.Endpoint = "vpn.example.com\nPostUp" }, "endpoint"},
		{"endpoint label dash", func(in *Instance) { in.Endpoint = "-vpn.example.com" }, "endpoint"},
		{"invalid dns", func(in *Instance) { in.DNS = []netip.Addr{{}} }, "dns"},
		{"mtu too low", func(in *Instance) { in.MTU = 576 }, "mtu"},
		{"negative keepalive", func(in *Instance) { in.PersistentKeepalive = -1 }, "persistent_keepalive"},
		{"no allowed ips", func(in *Instance) { in.ClientAllowedIPs = nil }, "client_allowed_ips"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := NewInstance("Home", "vpn.example.com")
			tt.mutate(&in)
			fields := in.Validate()
			if _, ok := fields[tt.field]; !ok {
				t.Fatalf("want an error on %q, got %v", tt.field, fields)
			}
			if len(fields) != 1 {
				t.Fatalf("want only %q to fail, got %v", tt.field, fields)
			}
		})
	}
}

func TestInstanceValidateAcceptsVariants(t *testing.T) {
	variants := []func(*Instance){
		func(in *Instance) { in.Endpoint = "203.0.113.7" },
		func(in *Instance) { in.Endpoint = "2001:db8::1" },
		func(in *Instance) { in.Endpoint = "localhost" },
		func(in *Instance) { in.Name = "Ofis — İstanbul" },
		func(in *Instance) { in.Address = netip.MustParsePrefix("172.16.5.9/16") },
		func(in *Instance) { in.MTU = 1420 },
		func(in *Instance) { in.PersistentKeepalive = 0 },
		func(in *Instance) { in.DNS = nil },
	}
	for i, mutate := range variants {
		in := NewInstance("Home", "vpn.example.com")
		mutate(&in)
		if fields := in.Validate(); len(fields) > 0 {
			t.Errorf("variant %d: unexpected errors %v", i, fields)
		}
	}
}

func TestPeerValidate(t *testing.T) {
	if fields := NewPeer(1, "phone").Validate(); len(fields) > 0 {
		t.Fatalf("unexpected errors %v", fields)
	}
	if fields := NewPeer(1, "").Validate(); fields["name"] == "" {
		t.Fatal("empty name should fail")
	}
	for _, kbit := range []int64{-1, MaxSpeedLimit + 1} {
		p := NewPeer(1, "phone")
		p.SpeedLimit = kbit
		if p.Validate()["speed_limit"] == "" {
			t.Errorf("speed limit %d should fail", kbit)
		}
	}
}
