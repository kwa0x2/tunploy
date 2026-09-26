package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// A group is every device with one external_id: a customer's phone and
// laptop, changed together when their plan renews or ends.

type apiGroup struct {
	ExternalID  string `json:"external_id"`
	DeviceCount int    `json:"device_count"`
	// All the devices together, this calendar month.
	MonthUsage apiTraffic  `json:"month_usage"`
	Devices    []apiDevice `json:"devices"`
}

type apiGroupRequest struct {
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
	views, err := s.apiDevices(r.Context(), peers)
	if err != nil {
		return err
	}
	g := apiGroup{ExternalID: id, DeviceCount: len(views), Devices: views}
	for _, d := range views {
		g.MonthUsage.RxBytes += d.MonthUsage.RxBytes
		g.MonthUsage.TxBytes += d.MonthUsage.TxBytes
		g.MonthUsage.TotalBytes += d.MonthUsage.TotalBytes
	}
	return httpx.JSON(w, http.StatusOK, g)
}

func (s *Server) apiGetGroup(w http.ResponseWriter, r *http.Request) error {
	id, peers, err := s.groupFromPath(r)
	if err != nil {
		return err
	}
	return s.writeGroup(w, r, id, peers)
}

// Every device is checked before any is saved, so a bad value changes none.
func (s *Server) apiUpdateGroup(w http.ResponseWriter, r *http.Request) error {
	id, peers, err := s.groupFromPath(r)
	if err != nil {
		return err
	}
	var req apiGroupRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	change := apiDeviceRequest{Enabled: req.Enabled, DataLimit: req.DataLimit, LimitPeriod: req.LimitPeriod,
		ExpiresAt: req.ExpiresAt, SpeedLimit: req.SpeedLimit, Metadata: req.Metadata}
	next := make([]wg.Peer, len(peers))
	for i, p := range peers {
		next[i] = p
		fields := change.apply(&next[i])
		if err := validationError(next[i].Validate(), fields); err != nil {
			return err
		}
	}

	updated, err := s.eachInGroup(r.Context(), peers, func(in *wg.Instance, i int) (*wg.Peer, error) {
		return s.savePeer(r.Context(), in, peers[i], next[i])
	})
	if err != nil {
		return err
	}
	return s.writeGroup(w, r, id, updated)
}

func (s *Server) apiDeleteGroup(w http.ResponseWriter, r *http.Request) error {
	_, peers, err := s.groupFromPath(r)
	if err != nil {
		return err
	}
	_, err = s.eachInGroup(r.Context(), peers, func(in *wg.Instance, i int) (*wg.Peer, error) {
		return nil, s.removePeer(r.Context(), in, &peers[i])
	})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) apiResetGroupUsage(w http.ResponseWriter, r *http.Request) error {
	id, peers, err := s.groupFromPath(r)
	if err != nil {
		return err
	}
	reset, err := s.eachInGroup(r.Context(), peers, func(in *wg.Instance, i int) (*wg.Peer, error) {
		return s.saveUsageReset(r.Context(), in, &peers[i])
	})
	if err != nil {
		return err
	}
	return s.writeGroup(w, r, id, reset)
}

// eachInGroup saves a change to every peer, then updates each tunnel it
// touched once, even when a save failed partway.
func (s *Server) eachInGroup(ctx context.Context, peers []wg.Peer, save func(in *wg.Instance, i int) (*wg.Peer, error)) ([]wg.Peer, error) {
	instances := map[int64]*wg.Instance{}
	var touched []int64
	out := make([]wg.Peer, 0, len(peers))
	var saveErr error
	for i, p := range peers {
		in, ok := instances[p.InstanceID]
		if !ok {
			if in, saveErr = s.store.InstanceByID(ctx, p.InstanceID); saveErr != nil {
				break
			}
			instances[p.InstanceID] = in
			touched = append(touched, in.ID)
		}
		saved, err := save(in, i)
		if err != nil {
			saveErr = err
			break
		}
		if saved != nil {
			out = append(out, *saved)
		}
	}

	var applyErrs []error
	for _, id := range touched {
		applyErrs = append(applyErrs, s.deploy.Apply(ctx, id))
	}
	if saveErr != nil {
		return nil, saveErr
	}
	if err := errors.Join(applyErrs...); err != nil {
		return nil, applyError(err)
	}
	return out, nil
}
