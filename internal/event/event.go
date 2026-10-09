// Package event is where everything the panel reports goes: a log line, a
// row in the activity log, and the emails and webhooks that asked for it.
package event

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"time"

	"github.com/kwa0x2/tunploy/internal/geoip"
	"github.com/kwa0x2/tunploy/internal/notify"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/webhook"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type Recorder interface {
	Record(ctx context.Context, e store.Event)
}

// Func lets a plain function act as a Recorder.
type Func func(ctx context.Context, e store.Event)

func (f Func) Record(ctx context.Context, e store.Event) { f(ctx, e) }

// Discard drops every event, for tools that have nowhere to report them.
var Discard Recorder = Func(func(context.Context, store.Event) {})

// Journal is the panel's Recorder. Nothing in it fails the caller: a device
// must not go uncreated because its event could not be saved.
type Journal struct {
	store    *store.Store
	geo      *geoip.DB
	notifier *notify.Notifier
	webhooks *webhook.Dispatcher
}

func NewJournal(st *store.Store, geo *geoip.DB, notifier *notify.Notifier, webhooks *webhook.Dispatcher) *Journal {
	return &Journal{store: st, geo: geo, notifier: notifier, webhooks: webhooks}
}

func (j *Journal) Record(ctx context.Context, e store.Event) {
	ctx = context.WithoutCancel(ctx)
	if e.IP != "" && e.Country == "" {
		if addr, err := netip.ParseAddr(e.IP); err == nil {
			e.Country = j.geo.Country(addr)
		}
	}
	if e.Actor == "" {
		e.Actor = ActorFrom(ctx)
	}
	attrs := []any{"kind", e.Kind}
	for _, kv := range [][2]string{
		{"actor", e.Actor}, {"node", e.NodeName}, {"server", e.InstanceName}, {"device", e.PeerName}, {"ip", e.IP}, {"country", e.Country}, {"detail", e.Detail},
	} {
		if kv[1] != "" {
			attrs = append(attrs, kv[0], kv[1])
		}
	}
	slog.Info("event", attrs...)

	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	id, err := j.store.AddEvent(ctx, e)
	if err != nil {
		slog.Error("record event", "kind", e.Kind, "error", err)
	}
	j.notifier.Notify(e)
	// Webhooks carry the event's ID, so one that was not saved is not sent.
	if err == nil && IsPublic(e.Kind) {
		e.ID = id
		payload, err := json.Marshal(ToPublic(e))
		if err != nil {
			slog.Error("encode webhook payload", "kind", e.Kind, "error", err)
			return
		}
		j.webhooks.Queue(ctx, e.Kind, e.ID, payload)
	}
}

func ForInstance(kind string, in *wg.Instance) store.Event {
	return store.Event{Kind: kind, InstanceID: in.ID, InstanceName: in.Name}
}

func ForPeer(kind string, in *wg.Instance, p *wg.Peer) store.Event {
	e := ForInstance(kind, in)
	e.PeerID, e.PeerName = p.ID, p.Name
	return e
}

type actorKey struct{}

// WithActor names who acts in ctx, so the activity log shows who did what.
// Events without one are the admin's.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func ActorFrom(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	return actor
}
