package server

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// A share link shows a device's owner its config and usage without an
// account on the panel: what a VPN seller sends a customer. The page always
// has the current config, so a device moved to another server needs no new link.

type shareRequest struct {
	// Empty keeps the link until it is removed.
	ExpiresAt *time.Time `json:"expires_at"`
}

type shareLink struct {
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (s *Server) shareLinkOf(r *http.Request, p *wg.Peer) shareLink {
	return shareLink{URL: s.panelURL(r) + "/share/" + p.ShareToken, ExpiresAt: p.ShareExpiresAt}
}

func (s *Server) handleGetShare(w http.ResponseWriter, r *http.Request) error {
	_, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	return s.writeShare(w, r, http.StatusOK, p)
}

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	return s.createShare(w, r, in, p)
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	if err := s.removeShare(r.Context(), in, p); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) apiGetShare(w http.ResponseWriter, r *http.Request) error {
	_, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	return s.writeShare(w, r, http.StatusOK, p)
}

func (s *Server) apiCreateShare(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	return s.createShare(w, r, in, p)
}

func (s *Server) apiDeleteShare(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	if err := s.removeShare(r.Context(), in, p); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) writeShare(w http.ResponseWriter, r *http.Request, status int, p *wg.Peer) error {
	if p.ShareToken == "" {
		return httpx.NotFound("this device has no share link")
	}
	w.Header().Set("Cache-Control", "no-store")
	return httpx.JSON(w, status, s.shareLinkOf(r, p))
}

// A new link always replaces the old one, so sending it again is also how
// an owner who leaked theirs gets a fresh one.
func (s *Server) createShare(w http.ResponseWriter, r *http.Request, in *wg.Instance, p *wg.Peer) error {
	var req shareRequest
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return httpx.Invalid(map[string]string{"expires_at": "expires_at must be in the future"})
	}
	shared, err := s.store.SetPeerShare(r.Context(), p.ID, rand.Text(), req.ExpiresAt)
	if err != nil {
		return err
	}
	e := peerEvent("device.shared", in, shared)
	if req.ExpiresAt != nil {
		e.Detail = "until " + expiryText(*req.ExpiresAt)
	}
	s.record(r.Context(), e)
	return s.writeShare(w, r, http.StatusCreated, shared)
}

// Removing a link that is not there is not an error, so a retry is safe.
func (s *Server) removeShare(ctx context.Context, in *wg.Instance, p *wg.Peer) error {
	if p.ShareToken == "" {
		return nil
	}
	if _, err := s.store.SetPeerShare(ctx, p.ID, "", nil); err != nil {
		return err
	}
	s.record(ctx, peerEvent("device.unshared", in, p))
	return nil
}

// What the owner sees. The server's name stays out: it is the admin's label,
// and the location says what an owner needs to know.
type sharedDevice struct {
	Name          string         `json:"name"`
	Country       string         `json:"country"`
	City          string         `json:"city"`
	Status        string         `json:"status"`
	Online        bool           `json:"online"`
	LastHandshake *time.Time     `json:"last_handshake"`
	DataLimit     int64          `json:"data_limit"`
	LimitPeriod   wg.LimitPeriod `json:"limit_period"`
	PeriodUsage   apiTraffic     `json:"period_usage"`
	UsageResetAt  *time.Time     `json:"usage_reset_at"`
	MonthUsage    apiTraffic     `json:"month_usage"`
	ExpiresAt     *time.Time     `json:"expires_at"`
	SpeedLimit    int64          `json:"speed_limit"`
	// Empty when the device holds its own private key.
	Config        string     `json:"config,omitempty"`
	FullTunnel    bool       `json:"full_tunnel"`
	LinkExpiresAt *time.Time `json:"link_expires_at"`
}

// sharedPeer is the link's device, or not_found for a link that is gone,
// expired or never was: the page cannot tell them apart, and neither can a guesser.
func (s *Server) sharedPeer(r *http.Request) (*wg.Instance, *wg.Peer, error) {
	gone := httpx.NotFound("this link has expired or was removed")
	p, err := s.store.PeerByShareToken(r.Context(), r.PathValue("token"))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, gone
	}
	if err != nil {
		return nil, nil, err
	}
	if p.ShareExpiresAt != nil && !time.Now().Before(*p.ShareExpiresAt) {
		return nil, nil, gone
	}
	in, err := s.store.InstanceByID(r.Context(), p.InstanceID)
	if err != nil {
		return nil, nil, err
	}
	return in, p, nil
}

func (s *Server) handleSharedDevice(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.sharedPeer(r)
	if err != nil {
		return err
	}
	now := time.Now()
	month, err := s.store.PeersMonthUsage(r.Context(), []int64{p.ID}, now)
	if err != nil {
		return err
	}
	used, err := s.store.PeersLimitUsage(r.Context(), []int64{p.ID}, now)
	if err != nil {
		return err
	}
	d := sharedDevice{
		Name:          p.Name,
		Country:       in.Country,
		City:          in.City,
		Status:        store.DeviceStatus(*p, used[p.ID], now),
		Online:        s.deploy.Online(in.ID)[p.PublicKey],
		LastHandshake: p.LastHandshake,
		DataLimit:     p.DataLimit,
		LimitPeriod:   p.LimitPeriod,
		PeriodUsage:   newAPITraffic(used[p.ID]),
		UsageResetAt:  p.UsageResetAt,
		MonthUsage:    newAPITraffic(month[p.ID]),
		ExpiresAt:     p.ExpiresAt,
		SpeedLimit:    p.SpeedLimit,
		FullTunnel:    in.FullTunnel(),
		LinkExpiresAt: p.ShareExpiresAt,
	}
	if !p.KeyOnClient() {
		d.Config = string(wg.ClientConfig(*in, *p))
	}
	w.Header().Set("Cache-Control", "no-store")
	return httpx.JSON(w, http.StatusOK, d)
}

func (s *Server) handleSharedConfig(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.sharedPeer(r)
	if err != nil {
		return err
	}
	conf, err := clientConfig(r, in, p)
	if err != nil {
		return err
	}
	return writeConfig(w, p, conf)
}

// Share pages hold a working VPN config; search engines must not keep one.
func noIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}
