package server

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"

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

var apiEventFamilies = []string{"device", "server", "node"}

type apiEndpoint struct {
	pattern string
	scope   string
	h       httpx.Handler
	// Replays a retried request's first reply; see idempotent.
	idempotent bool
}

// The table openapi.json is checked against.
func (s *Server) apiEndpoints() []apiEndpoint {
	return []apiEndpoint{
		{"GET /api/v1/devices", scopeDevicesRead, s.apiListDevices, false},
		{"POST /api/v1/devices", scopeDevicesWrite, s.apiCreateDevice, true},
		{"GET /api/v1/devices/{id}", scopeDevicesRead, s.apiGetDevice, false},
		{"PATCH /api/v1/devices/{id}", scopeDevicesWrite, s.apiUpdateDevice, false},
		{"DELETE /api/v1/devices/{id}", scopeDevicesWrite, s.apiDeleteDevice, false},
		{"GET /api/v1/devices/{id}/config", scopeDevicesRead, s.apiDeviceConfig, false},
		{"GET /api/v1/devices/{id}/usage", scopeDevicesRead, s.apiDeviceUsage, false},
		{"POST /api/v1/devices/{id}/usage/reset", scopeDevicesWrite, s.apiResetDeviceUsage, true},

		{"GET /api/v1/servers", scopeServersRead, s.apiListServers, false},
		{"GET /api/v1/servers/{id}", scopeServersRead, s.apiGetServer, false},

		{"GET /api/v1/events", scopeEventsRead, s.apiListEvents, false},

		{"GET /api/v1/webhooks", scopeWebhooksWrite, s.apiListWebhooks, false},
		{"POST /api/v1/webhooks", scopeWebhooksWrite, s.handleCreateWebhook, true},
		{"GET /api/v1/webhooks/{id}", scopeWebhooksWrite, s.handleGetWebhook, false},
		{"PATCH /api/v1/webhooks/{id}", scopeWebhooksWrite, s.handleUpdateWebhook, false},
		{"DELETE /api/v1/webhooks/{id}", scopeWebhooksWrite, s.handleDeleteWebhook, false},
		{"POST /api/v1/webhooks/{id}/ping", scopeWebhooksWrite, s.handlePingWebhook, false},
		{"GET /api/v1/webhooks/{id}/deliveries", scopeWebhooksWrite, s.handleListDeliveries, false},
		{"POST /api/v1/webhooks/{id}/deliveries/{deliveryID}/retry", scopeWebhooksWrite, s.handleRetryDelivery, false},
	}
}

func (s *Server) apiRoutes() http.Handler {
	v1 := http.NewServeMux()
	for _, e := range s.apiEndpoints() {
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

type apiTraffic struct {
	RxBytes    int64 `json:"rx_bytes"`
	TxBytes    int64 `json:"tx_bytes"`
	TotalBytes int64 `json:"total_bytes"`
}

func newAPITraffic(t wg.Traffic) apiTraffic {
	return apiTraffic{RxBytes: t.RxBytes, TxBytes: t.TxBytes, TotalBytes: t.Total()}
}

type apiDevice struct {
	ID         int64           `json:"id"`
	ServerID   int64           `json:"server_id"`
	Name       string          `json:"name"`
	ExternalID string          `json:"external_id"`
	Metadata   json.RawMessage `json:"metadata"`
	Address    netip.Addr      `json:"address"`
	PublicKey  wg.Key          `json:"public_key"`
	// The client made the key pair; the panel never saw the private key.
	ClientKey bool   `json:"client_key"`
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status"`
	// As of the watcher's last look, at most ten seconds old.
	Online        bool       `json:"online"`
	LastHandshake *time.Time `json:"last_handshake"`
	// Bytes per limit period, both directions; 0 means no limit.
	DataLimit   int64          `json:"data_limit"`
	LimitPeriod wg.LimitPeriod `json:"limit_period"`
	// What counts toward the limit: this period, or since the last reset.
	PeriodUsage  apiTraffic `json:"period_usage"`
	UsageResetAt *time.Time `json:"usage_reset_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	// The calendar month, whatever the limit period.
	MonthUsage apiTraffic `json:"month_usage"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	// Only in the reply to a create.
	Config string `json:"config,omitempty"`
}

type apiDeviceRequest struct {
	ServerID    *int64                    `json:"server_id"`
	Name        *string                   `json:"name"`
	PublicKey   *string                   `json:"public_key"`
	ExternalID  *string                   `json:"external_id"`
	Metadata    optional[json.RawMessage] `json:"metadata"`
	Enabled     *bool                     `json:"enabled"`
	DataLimit   *int64                    `json:"data_limit"`
	LimitPeriod *wg.LimitPeriod           `json:"limit_period"`
	ExpiresAt   optional[time.Time]       `json:"expires_at"`
}

func (req apiDeviceRequest) apply(p *wg.Peer) map[string]string {
	fields := map[string]string{}
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	}
	if req.ExternalID != nil {
		p.ExternalID = strings.TrimSpace(*req.ExternalID)
	}
	if req.Metadata.Set {
		p.Metadata = nil
		if req.Metadata.Value != nil {
			var b bytes.Buffer
			if err := json.Compact(&b, *req.Metadata.Value); err != nil {
				fields["metadata"] = "metadata must be a JSON object"
			} else if b.String() != "{}" {
				p.Metadata = b.Bytes()
			}
		}
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.DataLimit != nil {
		p.DataLimit = *req.DataLimit
	}
	if req.LimitPeriod != nil {
		p.LimitPeriod = *req.LimitPeriod
		if p.LimitPeriod == "" {
			fields["limit_period"] = "limit_period must be monthly or total"
		}
	}
	if req.ExpiresAt.Set {
		p.ExpiresAt = req.ExpiresAt.Value
	}
	return fields
}

func (s *Server) apiDevices(ctx context.Context, peers []wg.Peer) ([]apiDevice, error) {
	now := time.Now()
	ids := make([]int64, len(peers))
	for i, p := range peers {
		ids[i] = p.ID
	}
	usage, err := s.store.PeersMonthUsage(ctx, ids, now)
	if err != nil {
		return nil, err
	}
	used, err := s.store.PeersLimitUsage(ctx, ids, now)
	if err != nil {
		return nil, err
	}
	online := map[int64]map[wg.Key]bool{}
	out := make([]apiDevice, len(peers))
	for i, p := range peers {
		if _, ok := online[p.InstanceID]; !ok {
			online[p.InstanceID] = s.deploy.Online(p.InstanceID)
		}
		meta := p.Metadata
		if len(meta) == 0 {
			meta = json.RawMessage("{}")
		}
		out[i] = apiDevice{
			ID:            p.ID,
			ServerID:      p.InstanceID,
			Name:          p.Name,
			ExternalID:    p.ExternalID,
			Metadata:      meta,
			Address:       p.Address,
			PublicKey:     p.PublicKey,
			ClientKey:     p.KeyOnClient(),
			Enabled:       p.Enabled,
			Status:        store.DeviceStatus(p, used[p.ID], now),
			Online:        online[p.InstanceID][p.PublicKey],
			LastHandshake: p.LastHandshake,
			DataLimit:     p.DataLimit,
			LimitPeriod:   p.LimitPeriod,
			PeriodUsage:   newAPITraffic(used[p.ID]),
			UsageResetAt:  p.UsageResetAt,
			ExpiresAt:     p.ExpiresAt,
			MonthUsage:    newAPITraffic(usage[p.ID]),
			CreatedAt:     p.CreatedAt,
			UpdatedAt:     p.UpdatedAt,
		}
	}
	return out, nil
}

func (s *Server) apiDevice(ctx context.Context, p *wg.Peer) (apiDevice, error) {
	views, err := s.apiDevices(ctx, []wg.Peer{*p})
	if err != nil {
		return apiDevice{}, err
	}
	return views[0], nil
}

func (s *Server) apiListDevices(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.DeviceFilter{ExternalID: q.Get("external_id"), Status: q.Get("status")}
	var err error
	if f.InstanceID, err = queryID(q.Get("server_id"), "server_id"); err != nil {
		return err
	}
	if f.After, err = queryID(q.Get("after"), "after"); err != nil {
		return err
	}
	if f.Limit, err = pageSize(q.Get("limit")); err != nil {
		return err
	}
	if f.Status != "" && !store.ValidDeviceStatus(f.Status) {
		return httpx.BadRequest("status must be active, disabled, expired or limit_reached")
	}

	limit := f.Limit
	f.Limit++
	peers, err := s.store.Devices(r.Context(), f, time.Now())
	if err != nil {
		return err
	}
	more := len(peers) > limit
	if more {
		peers = peers[:limit]
	}
	views, err := s.apiDevices(r.Context(), peers)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, page[apiDevice]{Data: views, HasMore: more})
}

func (s *Server) apiGetDevice(w http.ResponseWriter, r *http.Request) error {
	_, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	view, err := s.apiDevice(r.Context(), p)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

func (s *Server) apiCreateDevice(w http.ResponseWriter, r *http.Request) error {
	var req apiDeviceRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.ServerID == nil {
		return httpx.Invalid(map[string]string{"server_id": "server_id is required"})
	}
	in, err := s.store.InstanceByID(r.Context(), *req.ServerID)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.Invalid(map[string]string{"server_id": "no such server"})
	}
	if err != nil {
		return err
	}

	p := wg.NewPeer(in.ID, "")
	if req.PublicKey != nil {
		key, err := wg.ParseKey(strings.TrimSpace(*req.PublicKey))
		if err != nil || key.IsZero() {
			return httpx.Invalid(map[string]string{"public_key": "public_key must be a WireGuard public key (44 characters of base64)"})
		}
		p = wg.NewClientPeer(in.ID, "", key)
	}
	fields := req.apply(&p)
	if req.Name == nil {
		p.Name = generatedName(p.ExternalID)
	}
	if len(fields) > 0 {
		return validationError(p.Validate(), fields)
	}

	created, err := s.createPeer(r.Context(), in, p)
	if errors.Is(err, wg.ErrSubnetFull) {
		return httpx.Errorf(http.StatusConflict, "server_full", "server %q has no free addresses left", in.Name)
	}
	if err != nil {
		return err
	}
	view, err := s.apiDevice(r.Context(), created)
	if err != nil {
		return err
	}
	view.Config = string(wg.ClientConfig(*in, *created))
	return httpx.JSON(w, http.StatusCreated, view)
}

// Names are unique per server, but API callers rarely care about them.
func generatedName(externalID string) string {
	var b [3]byte
	rand.Read(b[:])
	base := "device"
	if externalID != "" {
		base = externalID
		if r := []rune(base); len(r) > 50 {
			base = string(r[:50])
		}
	}
	return base + "-" + hex.EncodeToString(b[:])
}

func (s *Server) apiUpdateDevice(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	var req apiDeviceRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	next := *p
	fields := req.apply(&next)
	if req.ServerID != nil && *req.ServerID != p.InstanceID {
		fields["server_id"] = "a device cannot move to another server"
	}
	if req.PublicKey != nil {
		fields["public_key"] = "public_key cannot be changed; delete the device and create a new one"
	}
	if len(fields) > 0 {
		return validationError(next.Validate(), fields)
	}
	updated, err := s.updatePeer(r.Context(), in, *p, next)
	if err != nil {
		return err
	}
	view, err := s.apiDevice(r.Context(), updated)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

func (s *Server) apiDeleteDevice(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	if err := s.deletePeer(r.Context(), in, p); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) apiDeviceConfig(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	conf := wg.ClientConfig(*in, *p)
	w.Header().Set("Cache-Control", "no-store")

	switch format := r.URL.Query().Get("format"); format {
	case "", "conf":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.conf"`, tunnelName(p.Name)))
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(conf)
		return err
	case "qr":
		if p.KeyOnClient() {
			return httpx.Errorf(http.StatusConflict, "client_key",
				"the client holds this device's private key, so there is no complete config to put in a QR code")
		}
		code, err := qr.Encode(string(conf), qr.M)
		if err != nil {
			return fmt.Errorf("encode qr: %w", err)
		}
		code.Scale = 8
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(code.PNG())
		return err
	default:
		return httpx.BadRequest("format must be conf or qr")
	}
}

type apiUsage struct {
	MonthUsage   apiTraffic         `json:"month_usage"`
	PeriodUsage  apiTraffic         `json:"period_usage"`
	DataLimit    int64              `json:"data_limit"`
	LimitPeriod  wg.LimitPeriod     `json:"limit_period"`
	UsageResetAt *time.Time         `json:"usage_reset_at"`
	Daily        []store.UsagePoint `json:"daily"`
	Monthly      []store.UsagePoint `json:"monthly"`
}

func (s *Server) apiDeviceUsage(w http.ResponseWriter, r *http.Request) error {
	_, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	now := time.Now()
	daily, monthly, err := s.store.PeerUsage(r.Context(), p.ID, now, usageDays, usageMonths)
	if err != nil {
		return err
	}
	used, err := s.store.PeersLimitUsage(r.Context(), []int64{p.ID}, now)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, apiUsage{
		MonthUsage:   newAPITraffic(monthly[len(monthly)-1].Traffic),
		PeriodUsage:  newAPITraffic(used[p.ID]),
		DataLimit:    p.DataLimit,
		LimitPeriod:  p.LimitPeriod,
		UsageResetAt: p.UsageResetAt,
		Daily:        daily,
		Monthly:      monthly,
	})
}

// For plans that renew on their own date: reset on renewal, with a total limit.
func (s *Server) apiResetDeviceUsage(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	reset, err := s.resetPeerUsage(r.Context(), in, p)
	if err != nil {
		return err
	}
	view, err := s.apiDevice(r.Context(), reset)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

func (s *Server) deviceFromPath(r *http.Request) (*wg.Instance, *wg.Peer, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, nil, httpx.NotFound("device not found")
	}
	p, err := s.store.PeerByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, httpx.NotFound("device not found")
	}
	if err != nil {
		return nil, nil, err
	}
	in, err := s.store.InstanceByID(r.Context(), p.InstanceID)
	if err != nil {
		return nil, nil, err
	}
	return in, p, nil
}

type apiNode struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type apiServer struct {
	ID         int64        `json:"id"`
	Name       string       `json:"name"`
	Node       apiNode      `json:"node"`
	Country    string       `json:"country"`
	City       string       `json:"city"`
	Status     string       `json:"status"`
	Endpoint   string       `json:"endpoint"`
	ListenPort int          `json:"listen_port"`
	PublicKey  wg.Key       `json:"public_key"`
	Subnet     netip.Prefix `json:"subnet"`
	DNS        []netip.Addr `json:"dns"`
	// How many more devices fit is Capacity minus DeviceCount.
	DeviceCount int       `json:"device_count"`
	Capacity    int       `json:"capacity"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Server) apiServers(ctx context.Context, instances []wg.Instance) ([]apiServer, error) {
	counts, err := s.store.PeerCounts(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := s.store.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{0: "This server"}
	for _, n := range nodes {
		names[n.ID] = n.Name
	}
	statuses := s.deploy.Statuses(ctx, instances)

	out := make([]apiServer, len(instances))
	for i, in := range instances {
		out[i] = apiServer{
			ID:          in.ID,
			Name:        in.Name,
			Node:        apiNode{ID: in.NodeID, Name: names[in.NodeID]},
			Country:     in.Country,
			City:        in.City,
			Status:      string(statuses[in.ID].State),
			Endpoint:    in.Endpoint,
			ListenPort:  in.ListenPort,
			PublicKey:   in.PublicKey,
			Subnet:      in.Subnet(),
			DNS:         in.DNS,
			DeviceCount: counts[in.ID],
			Capacity:    capacity(in.Subnet()),
			CreatedAt:   in.CreatedAt,
		}
	}
	return out, nil
}

// Every host address but the server's own.
func capacity(subnet netip.Prefix) int {
	return 1<<(32-subnet.Bits()) - 3
}

// Few enough to list whole; country narrows them to one location.
func (s *Server) apiListServers(w http.ResponseWriter, r *http.Request) error {
	instances, err := s.store.Instances(r.Context())
	if err != nil {
		return err
	}
	if country := r.URL.Query().Get("country"); country != "" {
		instances = slices.DeleteFunc(instances, func(in wg.Instance) bool { return !strings.EqualFold(in.Country, country) })
	}
	views, err := s.apiServers(r.Context(), instances)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, page[apiServer]{Data: views})
}

func (s *Server) apiGetServer(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return httpx.NotFound("server not found")
	}
	in, err := s.store.InstanceByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.NotFound("server not found")
	}
	if err != nil {
		return err
	}
	views, err := s.apiServers(r.Context(), []wg.Instance{*in})
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, views[0])
}

type apiEvent struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	CreatedAt  time.Time `json:"created_at"`
	ServerID   int64     `json:"server_id,omitempty"`
	ServerName string    `json:"server_name,omitempty"`
	DeviceID   int64     `json:"device_id,omitempty"`
	DeviceName string    `json:"device_name,omitempty"`
	NodeName   string    `json:"node_name,omitempty"`
	IP         string    `json:"ip,omitempty"`
	Country    string    `json:"country,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	Actor      string    `json:"actor,omitempty"`
}

// Oldest first from after, so a poller keeps the last ID it saw and asks
// again. Sign-ins and settings stay out: a key reaches devices, not the panel.
func (s *Server) apiListEvents(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.EventFilter{Families: apiEventFamilies, Ascending: true}
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
	out := make([]apiEvent, len(events))
	for i, e := range events {
		out[i] = newAPIEvent(e)
	}
	return httpx.JSON(w, http.StatusOK, page[apiEvent]{Data: out, HasMore: more})
}

// Webhooks send the same shape, so a receiver can use either.
func newAPIEvent(e store.Event) apiEvent {
	return apiEvent{
		ID: e.ID, Kind: e.Kind, CreatedAt: e.CreatedAt.UTC().Truncate(time.Second),
		ServerID: e.InstanceID, ServerName: e.InstanceName, DeviceID: e.PeerID, DeviceName: e.PeerName,
		NodeName: e.NodeName, IP: e.IP, Country: e.Country, Detail: e.Detail, Actor: e.Actor,
	}
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
