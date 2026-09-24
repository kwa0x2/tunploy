package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type peerRequest struct {
	Name    *string `json:"name"`
	Enabled *bool   `json:"enabled"`
}

type peerView struct {
	wg.Peer
	Stats *wg.PeerStats `json:"stats,omitempty"`
}

func (s *Server) handleListPeers(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	peers, err := s.store.Peers(r.Context(), in.ID)
	if err != nil {
		return err
	}

	// Stats are a bonus: the list still has to load while the interface is
	// coming up or Docker is away.
	stats, err := s.deploy.PeerStats(r.Context(), in.ID)
	if err != nil {
		slog.Warn("read peer stats", "instance", in.ID, "error", err)
	}

	views := make([]peerView, len(peers))
	for i, p := range peers {
		views[i] = peerView{Peer: p}
		if st, ok := stats[p.PublicKey]; ok {
			views[i].Stats = &st
		}
	}
	return httpx.JSON(w, http.StatusOK, views)
}

func (s *Server) handleCreatePeer(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	var req peerRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	p := wg.NewPeer(in.ID, "")
	req.apply(&p)
	if fields := p.Validate(); len(fields) > 0 {
		return httpx.Invalid(fields)
	}

	created, err := s.store.CreatePeer(r.Context(), p)
	if err != nil {
		if errors.Is(err, wg.ErrSubnetFull) {
			return httpx.Errorf(http.StatusConflict, "subnet_full",
				"no free addresses left in %s", in.Subnet())
		}
		return peerWriteError(err)
	}
	if err := s.deploy.Apply(r.Context(), in.ID); err != nil {
		return applyError(err)
	}
	return httpx.JSON(w, http.StatusCreated, peerView{Peer: *created})
}

func (s *Server) handleUpdatePeer(w http.ResponseWriter, r *http.Request) error {
	p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	var req peerRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	req.apply(p)
	if fields := p.Validate(); len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	updated, err := s.store.UpdatePeer(r.Context(), *p)
	if err != nil {
		return peerWriteError(err)
	}
	if err := s.deploy.Apply(r.Context(), updated.InstanceID); err != nil {
		return applyError(err)
	}
	return httpx.JSON(w, http.StatusOK, peerView{Peer: *updated})
}

func (s *Server) handleDeletePeer(w http.ResponseWriter, r *http.Request) error {
	p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	if err := s.store.DeletePeer(r.Context(), p.ID); err != nil {
		return err
	}
	if err := s.deploy.Apply(r.Context(), p.InstanceID); err != nil {
		return applyError(err)
	}
	return httpx.NoContent(w)
}

// handlePeerConfig serves the file a client imports. The download name
// becomes the tunnel name on most clients, which caps it at 15 characters.
func (s *Server) handlePeerConfig(w http.ResponseWriter, r *http.Request) error {
	p, err := s.peerFromPath(r)
	if err != nil {
		return err
	}
	in, err := s.store.InstanceByID(r.Context(), p.InstanceID)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.conf"`, tunnelName(p.Name)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(wg.ClientConfig(*in, *p))
	return err
}

// peerFromPath also checks the peer belongs to the instance in the URL.
func (s *Server) peerFromPath(r *http.Request) (*wg.Peer, error) {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(r.PathValue("peerID"), 10, 64)
	if err != nil {
		return nil, httpx.NotFound("peer not found")
	}
	p, err := s.store.PeerByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && p.InstanceID != in.ID) {
		return nil, httpx.NotFound("peer not found")
	}
	return p, err
}

func (req peerRequest) apply(p *wg.Peer) {
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
}

func peerWriteError(err error) error {
	var dup *store.DuplicateError
	if errors.As(err, &dup) && dup.Column == "name" {
		return httpx.Invalid(map[string]string{"name": "a peer with this name already exists"})
	}
	return err
}

// applyError is for changes that are saved but could not reach the running
// interface; the next start or restart picks them up.
func applyError(err error) error {
	return httpx.Errorf(http.StatusBadGateway, "apply_failed",
		"saved, but the running tunnel could not be updated (restart it to apply): %v", err)
}

func tunnelName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if b.Len() == 15 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("_=+.-", r):
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "wg"
	}
	return b.String()
}
