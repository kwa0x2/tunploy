package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type v1Device struct {
	ID         int64           `json:"id"`
	ServerID   int64           `json:"server_id"`
	Name       string          `json:"name"`
	ExternalID string          `json:"external_id"`
	Metadata   json.RawMessage `json:"metadata"`
	Address    netip.Addr      `json:"address"`
	PublicKey  wg.Key          `json:"public_key"`
	// The client made the key pair; the panel never saw the private key.
	ClientKey bool      `json:"client_key"`
	Enabled   bool      `json:"enabled"`
	Status    wg.Status `json:"status"`
	// As of the watcher's last look, at most ten seconds old.
	Online        bool       `json:"online"`
	LastHandshake *time.Time `json:"last_handshake"`
	// Bytes per limit period, both directions; 0 means no limit.
	DataLimit   int64          `json:"data_limit"`
	LimitPeriod wg.LimitPeriod `json:"limit_period"`
	// What counts toward the limit: this period, or since the last reset.
	PeriodUsage  trafficView `json:"period_usage"`
	UsageResetAt *time.Time  `json:"usage_reset_at"`
	ExpiresAt    *time.Time  `json:"expires_at"`
	// kbit/s each way; 0 means no limit.
	SpeedLimit int64 `json:"speed_limit"`
	// The calendar month, whatever the limit period.
	MonthUsage trafficView `json:"month_usage"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	// Only in the reply to a create.
	Config string `json:"config,omitempty"`
}

type v1DeviceRequest struct {
	ServerID *serverRef `json:"server_id"`
	// Only with server_id "auto": where to look.
	Country     *string                   `json:"country"`
	City        *string                   `json:"city"`
	Name        *string                   `json:"name"`
	PublicKey   *string                   `json:"public_key"`
	ExternalID  *string                   `json:"external_id"`
	Metadata    optional[json.RawMessage] `json:"metadata"`
	Enabled     *bool                     `json:"enabled"`
	DataLimit   *int64                    `json:"data_limit"`
	LimitPeriod *wg.LimitPeriod           `json:"limit_period"`
	ExpiresAt   optional[time.Time]       `json:"expires_at"`
	SpeedLimit  *int64                    `json:"speed_limit"`
}

func (req v1DeviceRequest) apply(p *wg.Peer) map[string]string {
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
	if req.SpeedLimit != nil {
		p.SpeedLimit = *req.SpeedLimit
	}
	return fields
}

func (s *Server) v1Devices(ctx context.Context, peers []wg.Peer) ([]v1Device, error) {
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
	out := make([]v1Device, len(peers))
	for i, p := range peers {
		if _, ok := online[p.InstanceID]; !ok {
			online[p.InstanceID] = s.deploy.Online(p.InstanceID)
		}
		meta := p.Metadata
		if len(meta) == 0 {
			meta = json.RawMessage("{}")
		}
		out[i] = v1Device{
			ID:            p.ID,
			ServerID:      p.InstanceID,
			Name:          p.Name,
			ExternalID:    p.ExternalID,
			Metadata:      meta,
			Address:       p.Address,
			PublicKey:     p.PublicKey,
			ClientKey:     p.KeyOnClient(),
			Enabled:       p.Enabled,
			Status:        p.Status(used[p.ID], now),
			Online:        online[p.InstanceID][p.PublicKey],
			LastHandshake: p.LastHandshake,
			DataLimit:     p.DataLimit,
			LimitPeriod:   p.LimitPeriod,
			PeriodUsage:   newTrafficView(used[p.ID]),
			UsageResetAt:  p.UsageResetAt,
			ExpiresAt:     p.ExpiresAt,
			SpeedLimit:    p.SpeedLimit,
			MonthUsage:    newTrafficView(usage[p.ID]),
			CreatedAt:     p.CreatedAt,
			UpdatedAt:     p.UpdatedAt,
		}
	}
	return out, nil
}

func (s *Server) v1Device(ctx context.Context, p *wg.Peer) (v1Device, error) {
	views, err := s.v1Devices(ctx, []wg.Peer{*p})
	if err != nil {
		return v1Device{}, err
	}
	return views[0], nil
}

func (s *Server) v1ListDevices(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.DeviceFilter{ExternalID: q.Get("external_id"), Status: wg.Status(q.Get("status"))}
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
	if f.Status != "" && !f.Status.Valid() {
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
	views, err := s.v1Devices(r.Context(), peers)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, page[v1Device]{Data: views, HasMore: more})
}

func (s *Server) v1GetDevice(w http.ResponseWriter, r *http.Request) error {
	_, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	view, err := s.v1Device(r.Context(), p)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

func (s *Server) v1CreateDevice(w http.ResponseWriter, r *http.Request) error {
	var req v1DeviceRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	in, err := s.targetServer(r.Context(), req.placement(), 0)
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
		return apperr.Fields(p.Validate(), fields)
	}

	created, err := s.peers.Create(r.Context(), in, p)
	if errors.Is(err, wg.ErrSubnetFull) {
		return httpx.Errorf(http.StatusConflict, "server_full", "server %q has no free addresses left", in.Name)
	}
	if err != nil {
		return err
	}
	view, err := s.v1Device(r.Context(), created)
	if err != nil {
		return err
	}
	view.Config = string(wg.ClientConfig(*in, *created))
	return httpx.JSON(w, http.StatusCreated, view)
}

// serverRef is a server's ID, or "auto" to let the panel pick one.
type serverRef struct {
	ID   int64
	Auto bool
}

func (r *serverRef) UnmarshalJSON(b []byte) error {
	if string(b) == `"auto"` {
		r.Auto = true
		return nil
	}
	if err := json.Unmarshal(b, &r.ID); err != nil {
		return errors.New(`server_id must be a server's ID or "auto"`)
	}
	return nil
}

type placement struct {
	Server  *serverRef
	Country *string
	City    *string
}

func (req v1DeviceRequest) placement() placement {
	return placement{Server: req.ServerID, Country: req.Country, City: req.City}
}

// targetServer resolves where a device goes; skip is the server it is
// leaving, which "auto" never picks.
func (s *Server) targetServer(ctx context.Context, pl placement, skip int64) (*wg.Instance, error) {
	if pl.Server == nil {
		return nil, httpx.Invalid(map[string]string{"server_id": `server_id is required: a server's ID or "auto"`})
	}
	if !pl.Server.Auto {
		if pl.Country != nil || pl.City != nil {
			return nil, httpx.Invalid(map[string]string{"country": `country and city only go with server_id "auto"`})
		}
		in, err := s.store.InstanceByID(ctx, pl.Server.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, httpx.Invalid(map[string]string{"server_id": "no such server"})
		}
		return in, err
	}

	var country, city string
	if pl.Country != nil {
		country = *pl.Country
	}
	if pl.City != nil {
		city = *pl.City
	}
	return s.instances.Pick(ctx, country, city, skip)
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

func (s *Server) v1UpdateDevice(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	var req v1DeviceRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	next := *p
	fields := req.apply(&next)
	if req.ServerID != nil && (req.ServerID.Auto || req.ServerID.ID != p.InstanceID) {
		fields["server_id"] = "use POST /api/v1/devices/{id}/move to put a device on another server"
	}
	if req.Country != nil || req.City != nil {
		fields["country"] = "country and city only choose a server on create or move"
	}
	if req.PublicKey != nil {
		fields["public_key"] = "public_key cannot be changed; delete the device and create a new one"
	}
	if len(fields) > 0 {
		return apperr.Fields(next.Validate(), fields)
	}
	updated, err := s.peers.Update(r.Context(), in, *p, next)
	if err != nil {
		return err
	}
	view, err := s.v1Device(r.Context(), updated)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

func (s *Server) v1DeleteDevice(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	if err := s.peers.Delete(r.Context(), in, p); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) v1DeviceConfig(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	conf, err := clientConfig(r, in, p)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")

	switch format := r.URL.Query().Get("format"); format {
	case "", "conf":
		return writeConfig(w, p, conf)
	case "qr":
		if r.URL.Query().Get("kill_switch") == "true" {
			return httpx.BadRequest("the kill switch config is for Linux; phone apps refuse its PostUp lines, so there is no QR code for it")
		}
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

type v1Usage struct {
	MonthUsage   trafficView        `json:"month_usage"`
	PeriodUsage  trafficView        `json:"period_usage"`
	DataLimit    int64              `json:"data_limit"`
	LimitPeriod  wg.LimitPeriod     `json:"limit_period"`
	UsageResetAt *time.Time         `json:"usage_reset_at"`
	Daily        []store.UsagePoint `json:"daily"`
	Monthly      []store.UsagePoint `json:"monthly"`
}

func (s *Server) v1DeviceUsage(w http.ResponseWriter, r *http.Request) error {
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
	return httpx.JSON(w, http.StatusOK, v1Usage{
		MonthUsage:   newTrafficView(monthly[len(monthly)-1].Traffic),
		PeriodUsage:  newTrafficView(used[p.ID]),
		DataLimit:    p.DataLimit,
		LimitPeriod:  p.LimitPeriod,
		UsageResetAt: p.UsageResetAt,
		Daily:        daily,
		Monthly:      monthly,
	})
}

// For plans that renew on their own date: reset on renewal, with a total limit.
func (s *Server) v1ResetDeviceUsage(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	reset, err := s.peers.ResetUsage(r.Context(), in, p)
	if err != nil {
		return err
	}
	view, err := s.v1Device(r.Context(), reset)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, view)
}

type v1MoveRequest struct {
	ServerID *serverRef `json:"server_id"`
	Country  *string    `json:"country"`
	City     *string    `json:"city"`
}

// The device keeps its ID, keys, limits and history; its address and the
// server's key and endpoint change, so the reply carries the new config.
func (s *Server) v1MoveDevice(w http.ResponseWriter, r *http.Request) error {
	from, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	var req v1MoveRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	to, err := s.targetServer(r.Context(), placement{Server: req.ServerID, Country: req.Country, City: req.City}, p.InstanceID)
	if err != nil {
		return err
	}
	if to.ID == from.ID {
		return httpx.Invalid(map[string]string{"server_id": "the device is already on this server"})
	}
	moved, err := s.peers.Move(r.Context(), from, to, p)
	if errors.Is(err, wg.ErrSubnetFull) {
		return httpx.Errorf(http.StatusConflict, "server_full", "server %q has no free addresses left", to.Name)
	}
	if err != nil {
		return err
	}
	view, err := s.v1Device(r.Context(), moved)
	if err != nil {
		return err
	}
	view.Config = string(wg.ClientConfig(*to, *moved))
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
