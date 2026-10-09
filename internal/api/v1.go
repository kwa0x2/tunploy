package api

import (
	_ "embed"
	"net/http"
	"strconv"
	"strings"

	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// /api/v1 is the stable contract for other programs. It has its own views so
// the panel's /api can change freely underneath.

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

type v1Endpoint struct {
	pattern string
	scope   string
	h       httpx.Handler
	// Replays a retried request's first reply; see idempotent.
	idempotent bool
}

// The table openapi.json is checked against.
func (s *Server) v1Endpoints() []v1Endpoint {
	return []v1Endpoint{
		{"GET /api/v1/devices", scopeDevicesRead, s.v1ListDevices, false},
		{"POST /api/v1/devices", scopeDevicesWrite, s.v1CreateDevice, true},
		{"GET /api/v1/devices/{id}", scopeDevicesRead, s.v1GetDevice, false},
		{"PATCH /api/v1/devices/{id}", scopeDevicesWrite, s.v1UpdateDevice, false},
		{"DELETE /api/v1/devices/{id}", scopeDevicesWrite, s.v1DeleteDevice, false},
		{"GET /api/v1/devices/{id}/config", scopeDevicesRead, s.v1DeviceConfig, false},
		{"GET /api/v1/devices/{id}/usage", scopeDevicesRead, s.v1DeviceUsage, false},
		{"POST /api/v1/devices/{id}/usage/reset", scopeDevicesWrite, s.v1ResetDeviceUsage, true},
		{"POST /api/v1/devices/{id}/move", scopeDevicesWrite, s.v1MoveDevice, true},
		{"GET /api/v1/devices/{id}/share", scopeDevicesRead, s.v1GetShare, false},
		{"POST /api/v1/devices/{id}/share", scopeDevicesWrite, s.v1CreateShare, true},
		{"DELETE /api/v1/devices/{id}/share", scopeDevicesWrite, s.v1DeleteShare, false},

		{"GET /api/v1/groups/{external_id}", scopeDevicesRead, s.v1GetGroup, false},
		{"PATCH /api/v1/groups/{external_id}", scopeDevicesWrite, s.v1UpdateGroup, false},
		{"DELETE /api/v1/groups/{external_id}", scopeDevicesWrite, s.v1DeleteGroup, false},
		{"POST /api/v1/groups/{external_id}/usage/reset", scopeDevicesWrite, s.v1ResetGroupUsage, true},

		{"GET /api/v1/servers", scopeServersRead, s.v1ListServers, false},
		{"GET /api/v1/servers/{id}", scopeServersRead, s.v1GetServer, false},
		{"POST /api/v1/servers", scopeServersWrite, s.v1CreateServer, true},
		{"PATCH /api/v1/servers/{id}", scopeServersWrite, s.v1UpdateServer, false},
		{"DELETE /api/v1/servers/{id}", scopeServersWrite, s.v1DeleteServer, false},
		{"GET /api/v1/nodes", scopeServersRead, s.v1ListNodes, false},

		{"GET /api/v1/events", scopeEventsRead, s.v1ListEvents, false},

		{"GET /api/v1/webhooks", scopeWebhooksWrite, s.v1ListWebhooks, false},
		{"POST /api/v1/webhooks", scopeWebhooksWrite, s.handleCreateWebhook, true},
		{"GET /api/v1/webhooks/{id}", scopeWebhooksWrite, s.handleGetWebhook, false},
		{"PATCH /api/v1/webhooks/{id}", scopeWebhooksWrite, s.handleUpdateWebhook, false},
		{"DELETE /api/v1/webhooks/{id}", scopeWebhooksWrite, s.handleDeleteWebhook, false},
		{"POST /api/v1/webhooks/{id}/ping", scopeWebhooksWrite, s.handlePingWebhook, false},
		{"GET /api/v1/webhooks/{id}/deliveries", scopeWebhooksWrite, s.handleListDeliveries, false},
		{"POST /api/v1/webhooks/{id}/deliveries/{deliveryID}/retry", scopeWebhooksWrite, s.handleRetryDelivery, false},
	}
}

func (s *Server) v1Routes() http.Handler {
	v1 := http.NewServeMux()
	for _, e := range s.v1Endpoints() {
		h := requireScope(e.scope, e.h)
		if e.idempotent {
			h = s.idempotent(h)
		}
		v1.Handle(e.pattern, h)
	}
	v1.Handle("/api/v1/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound("no such endpoint: %s %s", r.Method, r.URL.Path)
	}))
	return chain(v1, s.requireAPIKey)
}

//go:embed openapi.json
var openAPISpec []byte

// Public, so tools can fetch it before anyone has a key.
func handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(openAPISpec)
}

type page[T any] struct {
	Data    []T  `json:"data"`
	HasMore bool `json:"has_more"`
}

type trafficView struct {
	RxBytes    int64 `json:"rx_bytes"`
	TxBytes    int64 `json:"tx_bytes"`
	TotalBytes int64 `json:"total_bytes"`
}

func newTrafficView(t wg.Traffic) trafficView {
	return trafficView{RxBytes: t.RxBytes, TxBytes: t.TxBytes, TotalBytes: t.Total()}
}

// Oldest first from after, so a poller keeps the last ID it saw and asks
// again. Sign-ins and settings stay out: a key reaches devices, not the panel.
func (s *Server) v1ListEvents(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.EventFilter{Families: event.PublicFamilies, Ascending: true}
	var err error
	if f.After, err = queryID(q.Get("after"), "after"); err != nil {
		return err
	}
	if f.InstanceID, err = queryID(q.Get("server_id"), "server_id"); err != nil {
		return err
	}
	if f.PeerID, err = queryID(q.Get("device_id"), "device_id"); err != nil {
		return err
	}
	if f.Limit, err = pageSize(q.Get("limit")); err != nil {
		return err
	}
	if raw := q.Get("kind"); raw != "" {
		f.Kinds = strings.Split(raw, ",")
	}
	limit := f.Limit
	f.Limit++
	events, err := s.store.Events(r.Context(), f)
	if err != nil {
		return err
	}
	more := len(events) > limit
	if more {
		events = events[:limit]
	}
	out := make([]event.Public, len(events))
	for i, e := range events {
		out[i] = event.ToPublic(e)
	}
	return httpx.JSON(w, http.StatusOK, page[event.Public]{Data: out, HasMore: more})
}

func queryID(raw, name string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 1 {
		return 0, httpx.BadRequest("%s must be a positive number", name)
	}
	return v, nil
}

func pageSize(raw string) (int, error) {
	if raw == "" {
		return defaultPageSize, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxPageSize {
		return 0, httpx.BadRequest("limit must be between 1 and %d", maxPageSize)
	}
	return n, nil
}
