package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	maxWebhooks        = 20
	maxWebhookURL      = 2048
	maxWebhookDesc     = 200
	webhookSecretBytes = 32
	webhookSecretPre   = "whsec_"
	pingKind           = "ping"
)

// Every kind a webhook can subscribe to: what /api/v1/events shows.
var webhookKinds = []string{
	"device.created", "device.deleted", "device.renamed", "device.moved", "device.enabled", "device.disabled",
	"device.limits_changed", "device.limit_reached", "device.expired", "device.unblocked", "device.usage_reset",
	"device.connected", "device.disconnected",
	"server.created", "server.deploy_failed", "server.updated", "server.deleted", "server.started",
	"server.stopped", "server.restarted", "server.down", "server.recovered",
	"node.added", "node.renamed", "node.deleted", "node.offline", "node.online",
}

type webhookRequest struct {
	URL         *string   `json:"url"`
	Events      *[]string `json:"events"`
	Description *string   `json:"description"`
	Enabled     *bool     `json:"enabled"`
}

type createdWebhook struct {
	store.Webhook
	// Shown once; receivers check signatures with it.
	Secret string `json:"secret"`
}

func (req webhookRequest) apply(w *store.Webhook) map[string]string {
	fields := map[string]string{}
	if req.URL != nil {
		w.URL = strings.TrimSpace(*req.URL)
		if msg := checkWebhookURL(w.URL); msg != "" {
			fields["url"] = msg
		}
	}
	if req.Events != nil {
		events, msg := normalizeEvents(*req.Events)
		if msg != "" {
			fields["events"] = msg
		}
		w.Events = events
	}
	if req.Description != nil {
		w.Description = strings.TrimSpace(*req.Description)
		switch {
		case utf8.RuneCountInString(w.Description) > maxWebhookDesc:
			fields["description"] = "description must be at most 200 characters"
		case strings.IndexFunc(w.Description, unicode.IsControl) >= 0:
			fields["description"] = "description must not contain control characters"
		}
	}
	if req.Enabled != nil {
		w.Enabled = *req.Enabled
	}
	return fields
}

func checkWebhookURL(raw string) string {
	u, err := url.Parse(raw)
	switch {
	case raw == "":
		return "url is required"
	case len(raw) > maxWebhookURL:
		return "url must be at most 2048 characters"
	case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "":
		return "url must be an http or https address such as https://example.com/hooks/tunploy"
	case u.User != nil:
		return "url must not hold a user name or password; check the signature instead"
	}
	return ""
}

// Kinds keep the order of webhookKinds, so saved lists compare equal.
func normalizeEvents(in []string) ([]string, string) {
	if slices.Contains(in, store.AllEvents) {
		return []string{store.AllEvents}, ""
	}
	for _, k := range in {
		if !slices.Contains(webhookKinds, k) {
			return nil, "unknown event " + strconv.Quote(k)
		}
	}
	out := slices.DeleteFunc(slices.Clone(webhookKinds), func(k string) bool { return !slices.Contains(in, k) })
	if len(out) == 0 {
		return nil, `choose at least one event, or "*" for all of them`
	}
	return out, ""
}

func newWebhookSecret() string {
	b := make([]byte, webhookSecretBytes)
	rand.Read(b)
	return webhookSecretPre + base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) error {
	hooks, err := s.store.Webhooks(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, hooks)
}

func (s *Server) apiListWebhooks(w http.ResponseWriter, r *http.Request) error {
	hooks, err := s.store.Webhooks(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, page[store.Webhook]{Data: hooks})
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) error {
	var req webhookRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	hook := store.Webhook{Enabled: true, Events: []string{store.AllEvents}}
	fields := req.apply(&hook)
	if req.URL == nil {
		fields["url"] = "url is required"
	}
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	existing, err := s.store.Webhooks(r.Context())
	if err != nil {
		return err
	}
	if len(existing) >= maxWebhooks {
		return httpx.Conflict("the panel holds at most %d webhooks; delete one first", maxWebhooks)
	}

	hook.Secret = newWebhookSecret()
	hook.CreatedBy = actorFrom(r.Context())
	created, err := s.store.CreateWebhook(r.Context(), hook)
	if err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "webhook.created", IP: clientIP(r), Detail: webhookDetail(created)})
	return httpx.JSON(w, http.StatusCreated, createdWebhook{Webhook: *created, Secret: created.Secret})
}

func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request) error {
	hook, err := s.webhookFromPath(r)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, hook)
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) error {
	hook, err := s.webhookFromPath(r)
	if err != nil {
		return err
	}
	var req webhookRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	next := *hook
	if fields := req.apply(&next); len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	updated, err := s.store.UpdateWebhook(r.Context(), next)
	if err != nil {
		return err
	}
	if webhookDetail(updated) != webhookDetail(hook) || updated.Enabled != hook.Enabled {
		detail := webhookDetail(updated)
		if !updated.Enabled {
			detail += ", turned off"
		}
		s.record(r.Context(), store.Event{Kind: "webhook.updated", IP: clientIP(r), Detail: detail})
	}
	return httpx.JSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) error {
	hook, err := s.webhookFromPath(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteWebhook(r.Context(), hook.ID); err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "webhook.deleted", IP: clientIP(r), Detail: webhookDetail(hook)})
	return httpx.NoContent(w)
}

// A ping goes out even when the webhook subscribes to nothing that happened yet.
func (s *Server) handlePingWebhook(w http.ResponseWriter, r *http.Request) error {
	hook, err := s.webhookFromPath(r)
	if err != nil {
		return err
	}
	if !hook.Enabled {
		return httpx.Conflict("the webhook is turned off")
	}
	now := time.Now()
	payload, err := json.Marshal(apiEvent{Kind: pingKind, CreatedAt: now.UTC().Truncate(time.Second),
		Detail: "a test delivery from the Tunploy panel", Actor: actorFrom(r.Context())})
	if err != nil {
		return err
	}
	d, err := s.store.QueueDelivery(r.Context(), hook.ID, pingKind, payload, now)
	if err != nil {
		return err
	}
	s.webhooks.Wake()
	return httpx.JSON(w, http.StatusAccepted, d)
}

func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) error {
	hook, err := s.webhookFromPath(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	before, err := queryID(q.Get("before"), "before")
	if err != nil {
		return err
	}
	limit, err := pageSize(q.Get("limit"))
	if err != nil {
		return err
	}
	out, err := s.store.Deliveries(r.Context(), hook.ID, before, limit+1)
	if err != nil {
		return err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return httpx.JSON(w, http.StatusOK, page[store.Delivery]{Data: out, HasMore: more})
}

func (s *Server) handleRetryDelivery(w http.ResponseWriter, r *http.Request) error {
	hook, err := s.webhookFromPath(r)
	if err != nil {
		return err
	}
	if !hook.Enabled {
		return httpx.Conflict("the webhook is turned off")
	}
	id, err := strconv.ParseInt(r.PathValue("deliveryID"), 10, 64)
	if err != nil {
		return httpx.NotFound("delivery not found")
	}
	d, err := s.store.DeliveryByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && d.WebhookID != hook.ID) {
		return httpx.NotFound("delivery not found")
	}
	if err != nil {
		return err
	}
	if d, err = s.store.RetryDelivery(r.Context(), d.ID, time.Now()); err != nil {
		return err
	}
	s.webhooks.Wake()
	return httpx.JSON(w, http.StatusAccepted, d)
}

func (s *Server) webhookFromPath(r *http.Request) (*store.Webhook, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, httpx.NotFound("webhook not found")
	}
	hook, err := s.store.WebhookByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpx.NotFound("webhook not found")
	}
	return hook, err
}

// The host alone: a path can carry a token the activity log should not keep.
func webhookDetail(w *store.Webhook) string {
	host := w.URL
	if u, err := url.Parse(w.URL); err == nil {
		host = u.Host
	}
	events := "all events"
	if w.Events[0] != store.AllEvents {
		events = strings.Join(w.Events, ", ")
	}
	return host + " (" + events + ")"
}

// queueWebhooks sends what /api/v1/events would show to the webhooks that want it.
func (s *Server) queueWebhooks(ctx context.Context, e store.Event) {
	family, _, _ := strings.Cut(e.Kind, ".")
	if !slices.Contains(apiEventFamilies, family) {
		return
	}
	payload, err := json.Marshal(newAPIEvent(e))
	if err != nil {
		slog.Error("encode webhook payload", "kind", e.Kind, "error", err)
		return
	}
	s.webhooks.Queue(ctx, e.Kind, e.ID, payload)
}
