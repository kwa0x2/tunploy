package wg

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

var ErrSubnetFull = errors.New("wg: no free addresses left in the subnet")

func NextAddress(subnet netip.Prefix, used []netip.Addr) (netip.Addr, error) {
	first, last, ok := hostRange(subnet)
	if !ok {
		return netip.Addr{}, ErrSubnetFull
	}

	taken := make(map[netip.Addr]bool, len(used))
	for _, a := range used {
		taken[a] = true
	}

	for n := first; n <= last; n++ {
		if a := addrFrom(n); !taken[a] {
			return a, nil
		}
	}
	return netip.Addr{}, ErrSubnetFull
}

func hostRange(subnet netip.Prefix) (first, last uint32, ok bool) {
	if !subnet.IsValid() || !subnet.Addr().Is4() || subnet.Bits() > 30 {
		return 0, 0, false
	}
	base := subnet.Masked().Addr().As4()
	network := binary.BigEndian.Uint32(base[:])
	size := uint32(1) << (32 - subnet.Bits())
	return network + 1, network + size - 2, true
}

func isHostAddress(p netip.Prefix) bool {
	first, last, ok := hostRange(p)
	if !ok {
		return false
	}
	a := p.Addr().As4()
	n := binary.BigEndian.Uint32(a[:])
	return n >= first && n <= last
}

func addrFrom(n uint32) netip.Addr {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], n)
	return netip.AddrFrom4(b)
}
