package instance

import (
	"net/netip"
	"testing"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func TestSubnetBitsFor(t *testing.T) {
	for n, want := range map[int]int{1: 24, 253: 24, 254: 23, 1000: 22, 65533: 16} {
		if got, ok := subnetBitsFor(n); !ok || got != want {
			t.Errorf("subnetBitsFor(%d) = %d, %v; want %d", n, got, ok, want)
		}
	}
	for _, n := range []int{0, -1, 65534} {
		if _, ok := subnetBitsFor(n); ok {
			t.Errorf("subnetBitsFor(%d) should fail", n)
		}
	}
}

func TestNextFreeSubnetSkipsBigSubnets(t *testing.T) {
	existing := []wg.Instance{{Address: netip.MustParsePrefix("10.8.0.1/16")}, {Address: netip.MustParsePrefix("10.9.0.1/22")}}
	if got := nextFreeSubnet(existing, 24); got != netip.MustParsePrefix("10.10.0.1/24") {
		t.Fatalf("got %s", got)
	}
}
