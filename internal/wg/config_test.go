package wg

import (
	"bytes"
	"errors"
	"flag"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fixedKey keeps golden files stable across runs.
func fixedKey(b byte) Key {
	var k Key
	for i := range k {
		k[i] = b
	}
	return k
}

func fixture() (Instance, []Peer) {
	in := Instance{
		ID:                  1,
		Name:                "Home",
		Address:             netip.MustParsePrefix("10.8.0.1/24"),
		ListenPort:          51820,
		PrivateKey:          fixedKey(1),
		PublicKey:           fixedKey(1).PublicKey(),
		Endpoint:            "vpn.example.com",
		DNS:                 []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("1.0.0.1")},
		MTU:                 1420,
		PersistentKeepalive: 25,
		ClientAllowedIPs:    []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
	}
	peers := []Peer{
		{ID: 1, InstanceID: 1, Name: "laptop", Address: netip.MustParseAddr("10.8.0.2"),
			PrivateKey: fixedKey(2), PublicKey: fixedKey(2).PublicKey(), PresharedKey: fixedKey(3), Enabled: true},
		{ID: 2, InstanceID: 1, Name: "old phone", Address: netip.MustParseAddr("10.8.0.3"),
			PrivateKey: fixedKey(4), PublicKey: fixedKey(4).PublicKey(), PresharedKey: fixedKey(5), Enabled: false},
		{ID: 3, InstanceID: 1, Name: "phone", Address: netip.MustParseAddr("10.8.0.4"),
			PrivateKey: fixedKey(6), PublicKey: fixedKey(6).PublicKey(), PresharedKey: fixedKey(7), Enabled: true},
	}
	return in, peers
}

func TestServerConfigGolden(t *testing.T) {
	in, peers := fixture()
	golden(t, "server.conf", ServerConfig(in, peers))
}

func TestServerConfigDefaultsMTU(t *testing.T) {
	in, _ := fixture()
	in.MTU = 0
	if !strings.Contains(string(ServerConfig(in, nil)), "MTU = 1420\n") {
		t.Fatal("server config must pin the MTU when none is set")
	}
}

func TestClientConfigGolden(t *testing.T) {
	in, peers := fixture()
	golden(t, "client.conf", ClientConfig(in, peers[0]))
}

func TestClientConfigMinimal(t *testing.T) {
	in, peers := fixture()
	in.DNS = nil
	in.MTU = 0
	in.PersistentKeepalive = 0
	in.Endpoint = "2001:db8::1"
	golden(t, "client-minimal.conf", ClientConfig(in, peers[0]))
}

func TestKillSwitchConfigGolden(t *testing.T) {
	in, peers := fixture()
	conf, err := KillSwitchConfig(in, peers[0])
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "client-kill-switch.conf", conf)

	in.ClientAllowedIPs = []netip.Prefix{netip.MustParsePrefix("10.8.0.0/24")}
	if _, err := KillSwitchConfig(in, peers[0]); !errors.Is(err, ErrSplitTunnel) {
		t.Fatalf("split tunnel: %v", err)
	}
}

func TestServerConfigCannotBeInjectedThroughNames(t *testing.T) {
	in, peers := fixture()
	peers[0].Name = "evil\nPostUp = touch /pwned\r\n"

	for _, line := range strings.Split(string(ServerConfig(in, peers)), "\n") {
		if strings.HasPrefix(line, "PostUp") {
			t.Fatalf("name escaped its comment: %q", line)
		}
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file (run with -update to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
