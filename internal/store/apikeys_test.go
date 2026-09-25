package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/wg"
)

func TestIdempotencyKeys(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	k, err := st.CreateAPIKey(ctx, APIKey{Name: "billing", Prefix: "tp_abc", TokenHash: "h", Scopes: []string{"devices:write"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	if r, err := st.ClaimIdempotencyKey(ctx, k.ID, "a", "req1", now); r != nil || err != nil {
		t.Fatalf("first claim = %v, %v", r, err)
	}
	if _, err := st.ClaimIdempotencyKey(ctx, k.ID, "a", "req1", now); !errors.Is(err, ErrIdempotencyBusy) {
		t.Fatalf("while running: %v", err)
	}
	if err := st.FinishIdempotencyKey(ctx, k.ID, "a", StoredResponse{Status: 201, ContentType: "application/json", Body: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	r, err := st.ClaimIdempotencyKey(ctx, k.ID, "a", "req1", now)
	if err != nil || r == nil || r.Status != 201 || string(r.Body) != "{}" {
		t.Fatalf("replay = %+v, %v", r, err)
	}
	if _, err := st.ClaimIdempotencyKey(ctx, k.ID, "a", "req2", now); !errors.Is(err, ErrIdempotencyMismatch) {
		t.Fatalf("other request: %v", err)
	}
	if r, err := st.ClaimIdempotencyKey(ctx, k.ID, "a", "req2", now.Add(IdempotencyTTL+time.Minute)); r != nil || err != nil {
		t.Fatalf("after the TTL the key is free again: %v, %v", r, err)
	}

	// A request the panel never finished frees its key after a while.
	if _, err := st.ClaimIdempotencyKey(ctx, k.ID, "b", "req", now); err != nil {
		t.Fatal(err)
	}
	if r, err := st.ClaimIdempotencyKey(ctx, k.ID, "b", "req", now.Add(10*time.Minute)); r != nil || err != nil {
		t.Fatalf("stale claim = %v, %v", r, err)
	}
}

func TestBackupKeepsAPIKeysButNotReplies(t *testing.T) {
	ctx := context.Background()
	old := newTestStore(t)
	k, err := old.CreateAPIKey(ctx, APIKey{Name: "billing", Prefix: "tp_abc", TokenHash: "h", Scopes: []string{"devices:read"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.ClaimIdempotencyKey(ctx, k.ID, "a", "req", time.Now()); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(t.TempDir(), "snap.db")
	if err := old.Snapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}

	live := newTestStore(t)
	if err := live.Restore(ctx, snap, nil); err != nil {
		t.Fatal(err)
	}
	got, err := live.APIKeyByToken(ctx, "h")
	if err != nil || got.Name != "billing" || len(got.Scopes) != 1 {
		t.Fatalf("restored key = %+v, %v", got, err)
	}
	if r, err := live.ClaimIdempotencyKey(ctx, k.ID, "a", "other", time.Now()); r != nil || err != nil {
		t.Fatalf("stored replies must not come back: %v, %v", r, err)
	}
}

func TestClientKeyPeerRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	in := createInstance(t, st, "Home", 51820)
	pub := wg.GeneratePrivateKey().PublicKey()
	p := wg.NewClientPeer(in.ID, "phone", pub)
	p.ExternalID = "user_1"
	p.Metadata = json.RawMessage(`{"plan":"pro"}`)
	created, err := st.CreatePeer(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.PeerByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.KeyOnClient() || got.PublicKey != pub || got.ExternalID != "user_1" || string(got.Metadata) != `{"plan":"pro"}` {
		t.Fatalf("got %+v", got)
	}
}
