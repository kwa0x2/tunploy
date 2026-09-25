package store

import (
	"context"
	"errors"
	"testing"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func TestNodes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	n, err := st.CreateNode(ctx, Node{Name: "Frankfurt", Host: "203.0.113.5", Port: 22, Username: "root", HostKey: "ssh-ed25519 AAAA"})
	if err != nil {
		t.Fatal(err)
	}
	if n.LastSeenAt == nil || n.Username != "root" {
		t.Fatalf("node = %+v", n)
	}
	if _, err := st.CreateNode(ctx, Node{Name: "frankfurt", Host: "203.0.113.6", Port: 22}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate name: err = %v", err)
	}
	if _, err := st.CreateNode(ctx, Node{Name: "Other", Host: "203.0.113.5", Port: 22}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate host: err = %v", err)
	}

	// The same port is free on another machine but not on the same one.
	createInstance(t, st, "Local", 51820)
	remote := wg.NewInstance("Remote", "203.0.113.5")
	remote.NodeID = n.ID
	created, err := st.CreateInstance(ctx, remote)
	if err != nil {
		t.Fatalf("same port on another node: %v", err)
	}
	if created.NodeID != n.ID {
		t.Fatalf("node id = %d, want %d", created.NodeID, n.ID)
	}
	remote.Name = "Remote 2"
	var dup *DuplicateError
	if _, err := st.CreateInstance(ctx, remote); !errors.As(err, &dup) || dup.Column != "listen_port" {
		t.Fatalf("same port on the same node: err = %v", err)
	}
	createPeer(t, st, created.ID, "phone")

	if err := st.DeleteNode(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InstanceByID(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("server of a deleted node: err = %v", err)
	}
	if left, _ := st.Instances(ctx); len(left) != 1 || left[0].Name != "Local" {
		t.Fatalf("instances left = %+v", left)
	}
	if err := st.DeleteNode(ctx, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: err = %v", err)
	}
}
