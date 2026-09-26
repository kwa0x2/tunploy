// Package webhook sends panel events to URLs that other programs listen on.
// Deliveries live in the database, so a restart does not lose them, and a
// failed one is tried again later.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	SignatureHeader = "X-Tunploy-Signature"
	EventHeader     = "X-Tunploy-Event"
	DeliveryHeader  = "X-Tunploy-Delivery"

	timeout     = 10 * time.Second
	batchSize   = 20
	parallel    = 4
	pollEvery   = 5 * time.Second
	maxErrorLen = 300
)

// After the first try fails, the next ones wait this long in turn: seven
// tries over about a day, then the delivery is given up.
var retryDelays = []time.Duration{
	time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour,
}

// Sign is the X-Tunploy-Signature value for body sent at t. The timestamp is
// signed too, so a receiver can turn away old deliveries played back again.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

type Dispatcher struct {
	store  *store.Store
	client *http.Client
	wake   chan struct{}
	now    func() time.Time
}

func New(st *store.Store) *Dispatcher {
	return &Dispatcher{
		store: st,
		client: &http.Client{
			Timeout: timeout,
			// A redirect could carry the signed body somewhere the admin never chose.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		wake: make(chan struct{}, 1),
		now:  time.Now,
	}
}

// Queue never blocks the request that caused the event.
func (d *Dispatcher) Queue(ctx context.Context, kind string, eventID int64, payload []byte) {
	n, err := d.store.QueueDeliveries(context.WithoutCancel(ctx), kind, eventID, payload, d.now())
	if err != nil {
		slog.Error("queue webhook deliveries", "kind", kind, "error", err)
		return
	}
	if n > 0 {
		d.Wake()
	}
}

func (d *Dispatcher) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	for {
		// A full batch may mean more are waiting.
		for d.deliverDue(ctx) == batchSize {
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.wake:
		}
	}
}

// deliverDue sends one batch and reports its size.
func (d *Dispatcher) deliverDue(ctx context.Context) int {
	due, err := d.store.DueDeliveries(ctx, d.now(), batchSize)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("load webhook deliveries", "error", err)
		}
		return 0
	}
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for _, dl := range due {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			d.attempt(ctx, dl)
		}()
	}
	wg.Wait()
	return len(due)
}

func (d *Dispatcher) attempt(ctx context.Context, dl store.Delivery) {
	start := d.now()
	status, err := d.send(ctx, dl, start)
	if ctx.Err() != nil {
		// Shutting down: the delivery stays pending and goes out after the restart.
		return
	}
	a := store.Attempt{At: start, Status: status, Duration: d.now().Sub(start), Succeeded: err == nil}
	if err != nil {
		a.Error = truncate(err.Error(), maxErrorLen)
		if dl.Attempts < len(retryDelays) {
			next := start.Add(retryDelays[dl.Attempts])
			a.NextAttempt = &next
		}
		slog.Warn("webhook delivery failed", "delivery", dl.ID, "url", dl.URL, "attempt", dl.Attempts+1, "error", a.Error)
	}
	if err := d.store.FinishAttempt(context.WithoutCancel(ctx), dl.ID, a); err != nil {
		slog.Error("record webhook attempt", "delivery", dl.ID, "error", err)
	}
}

func (d *Dispatcher) send(ctx context.Context, dl store.Delivery, at time.Time) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dl.URL, bytes.NewReader(dl.Payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Tunploy-Webhook")
	req.Header.Set(EventHeader, dl.Kind)
	req.Header.Set(DeliveryHeader, strconv.FormatInt(dl.ID, 10))
	req.Header.Set(SignatureHeader, Sign(dl.Secret, at, dl.Payload))

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	// The body is never stored: the delivery log is readable with an API
	// key, and the URL might point at something inside the network.
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("the receiver answered %s", resp.Status)
	}
	return resp.StatusCode, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}
