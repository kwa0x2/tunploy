package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type v1Group struct {
	ExternalID  string `json:"external_id"`
	DeviceCount int    `json:"device_count"`
	// All the devices together, this calendar month.
	MonthUsage trafficView `json:"month_usage"`
	Devices    []v1Device  `json:"devices"`
}

type v1GroupRequest struct {
	Enabled     *bool                     `json:"enabled"`
	DataLimit   *int64                    `json:"data_limit"`
	LimitPeriod *wg.LimitPeriod           `json:"limit_period"`
	ExpiresAt   optional[time.Time]       `json:"expires_at"`
	SpeedLimit  *int64                    `json:"speed_limit"`
	Metadata    optional[json.RawMessage] `json:"metadata"`
}

func (s *Server) groupFromPath(r *http.Request) (string, []wg.Peer, error) {
	id := r.PathValue("external_id")
	peers, err := s.store.Devices(r.Context(), store.DeviceFilter{ExternalID: id, Limit: -1}, time.Now())
	if err != nil {
		return "", nil, err
	}
	if id == "" || len(peers) == 0 {
		return "", nil, httpx.NotFound("no devices have external_id %q", id)
	}
	return id, peers, nil
}

func (s *Server) writeGroup(w http.ResponseWriter, r *http.Request, id string, peers []wg.Peer) error {
	views, err := s.v1Devices(r.Context(), peers)
	if err != nil {
		return err
	}
	g := v1Group{ExternalID: id, DeviceCount: len(views), Devices: views}
	for _, d := range views {
		g.MonthUsage.RxBytes += d.MonthUsage.RxBytes
		g.MonthUsage.TxBytes += d.MonthUsage.TxBytes
		g.MonthUsage.TotalBytes += d.MonthUsage.TotalBytes
	}
	return httpx.JSON(w, http.StatusOK, g)
}

func (s *Server) v1GetGroup(w http.ResponseWriter, r *http.Request) error {
	id, peers, err := s.groupFromPath(r)
	if err != nil {
		return err
	}
	return s.writeGroup(w, r, id, peers)
}

func (s *Server) v1UpdateGroup(w http.ResponseWriter, r *http.Request) error {
	id, peers, err := s.groupFromPath(r)
	if err != nil {
		return err
	}
	var req v1GroupRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	change := v1DeviceRequest{Enabled: req.Enabled, DataLimit: req.DataLimit, LimitPeriod: req.LimitPeriod,
		ExpiresAt: req.ExpiresAt, SpeedLimit: req.SpeedLimit, Metadata: req.Metadata}
	next := make([]wg.Peer, len(peers))
	for i, p := range peers {
		next[i] = p
		if fields := change.apply(&next[i]); len(fields) > 0 {
			return apperr.Fields(next[i].Validate(), fields)
		}
	}
	updated, err := s.peers.UpdateGroup(r.Context(), peers, next)
	if err != nil {
		return err
	}
	return s.writeGroup(w, r, id, updated)
}

func (s *Server) v1DeleteGroup(w http.ResponseWriter, r *http.Request) error {
	_, peers, err := s.groupFromPath(r)
	if err != nil {
		return err
	}
	if err := s.peers.DeleteGroup(r.Context(), peers); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) v1ResetGroupUsage(w http.ResponseWriter, r *http.Request) error {
	id, peers, err := s.groupFromPath(r)
	if err != nil {
		return err
	}
	reset, err := s.peers.ResetGroupUsage(r.Context(), peers)
	if err != nil {
		return err
	}
	return s.writeGroup(w, r, id, reset)
}
