package api

import (
	"net/http"
	"time"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/wg"
)

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
	if err := s.peers.Unshare(r.Context(), in, p); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) v1GetShare(w http.ResponseWriter, r *http.Request) error {
	_, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	return s.writeShare(w, r, http.StatusOK, p)
}

func (s *Server) v1CreateShare(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	return s.createShare(w, r, in, p)
}

func (s *Server) v1DeleteShare(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.deviceFromPath(r)
	if err != nil {
		return err
	}
	if err := s.peers.Unshare(r.Context(), in, p); err != nil {
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

func (s *Server) createShare(w http.ResponseWriter, r *http.Request, in *wg.Instance, p *wg.Peer) error {
	var req shareRequest
	if r.ContentLength > 0 {
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
	}
	shared, err := s.peers.Share(r.Context(), in, p, req.ExpiresAt)
	if err != nil {
		return err
	}
	return s.writeShare(w, r, http.StatusCreated, shared)
}

// What the owner sees. The server's name stays out: it is the admin's label,
// and the location says what an owner needs to know.
type sharedDevice struct {
	Name          string         `json:"name"`
	Country       string         `json:"country"`
	City          string         `json:"city"`
	Status        wg.Status      `json:"status"`
	Online        bool           `json:"online"`
	LastHandshake *time.Time     `json:"last_handshake"`
	DataLimit     int64          `json:"data_limit"`
	LimitPeriod   wg.LimitPeriod `json:"limit_period"`
	PeriodUsage   trafficView    `json:"period_usage"`
	UsageResetAt  *time.Time     `json:"usage_reset_at"`
	MonthUsage    trafficView    `json:"month_usage"`
	ExpiresAt     *time.Time     `json:"expires_at"`
	SpeedLimit    int64          `json:"speed_limit"`
	// Empty when the device holds its own private key.
	Config        string     `json:"config,omitempty"`
	FullTunnel    bool       `json:"full_tunnel"`
	LinkExpiresAt *time.Time `json:"link_expires_at"`
}

func (s *Server) handleSharedDevice(w http.ResponseWriter, r *http.Request) error {
	in, p, err := s.peers.Shared(r.Context(), r.PathValue("token"))
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
		Status:        p.Status(used[p.ID], now),
		Online:        s.deploy.Online(in.ID)[p.PublicKey],
		LastHandshake: p.LastHandshake,
		DataLimit:     p.DataLimit,
		LimitPeriod:   p.LimitPeriod,
		PeriodUsage:   newTrafficView(used[p.ID]),
		UsageResetAt:  p.UsageResetAt,
		MonthUsage:    newTrafficView(month[p.ID]),
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
	in, p, err := s.peers.Shared(r.Context(), r.PathValue("token"))
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
