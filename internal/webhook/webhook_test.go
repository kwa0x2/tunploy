package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
)

func TestSign(t *testing.T) {
	got := Sign("whsec_test", time.Unix(1700000000, 0), []byte(`{"kind":"ping"}`))
	// printf '1700000000.{"kind":"ping"}' | openssl dgst -sha256 -hmac whsec_test
	want := "t=1700000000,v1=db12e0595715d2510c13dea54c40711d893816899ee5d343115eb8d29144001d"
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestDispatcherRetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	var gotSig, gotEvent, gotBody string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		body, _ := io.ReadAll(r.Body)
		gotSig, gotEvent, gotBody = r.Header.Get(SignatureHeader), r.Header.Get(EventHeader), string(body)
	}))
	defer receiver.Close()

	st := openStore(t)
	ctx := context.Background()
	hook, err := st.CreateWebhook(ctx, store.Webhook{URL: receiver.URL, Secret: "whsec_x",
		Events: []string{"device.created"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	d := New(st)
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }

	d.Queue(ctx, "device.deleted", 1, []byte(`{}`)) // not subscribed
	d.Queue(ctx, "device.created", 2, []byte(`{"id":2}`))
	if n := d.deliverDue(ctx); n != 1 {
		t.Fatalf("first round sent %d", n)
	}
	list, err := st.Deliveries(ctx, hook.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	first := list[0]
	if len(list) != 1 || first.State != store.DeliveryPending || first.Attempts != 1 || first.ResponseStatus != 503 ||
		first.NextAttemptAt == nil || !first.NextAttemptAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("after a failure: %+v", list)
	}

	if n := d.deliverDue(ctx); n != 0 {
		t.Fatalf("retried %d before its time", n)
	}
	now = now.Add(time.Minute)
	if n := d.deliverDue(ctx); n != 1 {
		t.Fatalf("retry round sent %d", n)
	}
	got, err := st.DeliveryByID(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.DeliverySucceeded || got.Attempts != 2 || got.NextAttemptAt != nil {
		t.Fatalf("after the retry: %+v", got)
	}
	if gotEvent != "device.created" || gotBody != `{"id":2}` || gotSig != Sign("whsec_x", now, []byte(gotBody)) {
		t.Fatalf("received %q %q %q", gotEvent, gotBody, gotSig)
	}
}

func TestDispatcherGivesUp(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/", http.StatusFound)
	}))
	defer receiver.Close()

	st := openStore(t)
	ctx := context.Background()
	hook, err := st.CreateWebhook(ctx, store.Webhook{URL: receiver.URL, Secret: "s", Events: []string{store.AllEvents},
		Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	d := New(st)
	now := time.Now()
	d.now = func() time.Time { return now }
	d.Queue(ctx, "server.down", 1, []byte(`{}`))
	for range len(retryDelays) + 1 {
		if n := d.deliverDue(ctx); n != 1 {
			t.Fatalf("sent %d", n)
		}
		now = now.Add(24 * time.Hour)
	}
	list, err := st.Deliveries(ctx, hook.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].State != store.DeliveryFailed || list[0].Attempts != len(retryDelays)+1 || list[0].ResponseStatus != 302 {
		t.Fatalf("after every try: %+v", list[0])
	}
	if n := d.deliverDue(ctx); n != 0 {
		t.Fatalf("a failed delivery went out again: %d", n)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
