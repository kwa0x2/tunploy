package wg

import (
	"testing"
	"time"
)

func TestParseDump(t *testing.T) {
	a, b := fixedKey(2).PublicKey(), fixedKey(6).PublicKey()
	dump := fixedKey(1).String() + "\t" + fixedKey(1).PublicKey().String() + "\t51820\toff\n" +
		a.String() + "\t" + fixedKey(3).String() + "\t203.0.113.9:40211\t10.8.0.2/32\t1758700000\t1024\t2048\toff\n" +
		b.String() + "\t" + fixedKey(7).String() + "\t(none)\t10.8.0.4/32\t0\t0\t0\toff\n"

	stats, err := ParseDump([]byte(dump))
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("want 2 peers, got %d", len(stats))
	}

	got := stats[a]
	if got.Endpoint != "203.0.113.9:40211" || got.RxBytes != 1024 || got.TxBytes != 2048 {
		t.Fatalf("connected peer: %+v", got)
	}
	if got.LatestHandshake == nil || !got.LatestHandshake.Equal(time.Unix(1758700000, 0)) {
		t.Fatalf("handshake = %v", got.LatestHandshake)
	}

	idle := stats[b]
	if idle.Endpoint != "" || idle.LatestHandshake != nil {
		t.Fatalf("a peer that never connected should have no endpoint or handshake: %+v", idle)
	}
}

func TestParseDumpInterfaceOnly(t *testing.T) {
	stats, err := ParseDump([]byte(fixedKey(1).String() + "\tpub\t51820\toff\n"))
	if err != nil || len(stats) != 0 {
		t.Fatalf("want no peers, got %v, %v", stats, err)
	}
}

func TestParseDumpRejectsGarbage(t *testing.T) {
	if _, err := ParseDump([]byte("iface\nnot\ta\tpeer\n")); err == nil {
		t.Fatal("want an error for a malformed peer line")
	}
}
