package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/instance"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type instanceView struct {
	wg.Instance
	Status    deploy.Status `json:"status"`
	PeerCount int           `json:"peer_count"`
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) error {
	instances, err := s.store.Instances(r.Context())
	if err != nil {
		return err
	}
	counts, err := s.store.PeerCounts(r.Context())
	if err != nil {
		return err
	}

	statuses := s.deploy.Statuses(r.Context(), instances)

	views := make([]instanceView, len(instances))
	for i, in := range instances {
		views[i] = instanceView{Instance: in, Status: statuses[in.ID], PeerCount: counts[in.ID]}
	}
	return httpx.JSON(w, http.StatusOK, views)
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	return s.writeInstance(w, r, http.StatusOK, in)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) error {
	var req instance.Change
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	created, err := s.instances.Create(r.Context(), req)
	if err != nil {
		return err
	}
	if strings.Contains(r.Header.Get("Accept"), ndjson) {
		return s.streamProvision(w, r, created)
	}
	if err := s.instances.Provision(r.Context(), created, nil); err != nil {
		return err
	}
	return s.writeInstance(w, r, http.StatusCreated, created)
}

type provisionEvent struct {
	Step     deploy.Step   `json:"step,omitempty"`
	Instance *instanceView `json:"instance,omitempty"`
	Error    *httpx.Error  `json:"error,omitempty"`
	Log      []string      `json:"log,omitempty"`
}

// Validation already ran, so the status is 200 and the outcome is the last line.
func (s *Server) streamProvision(w http.ResponseWriter, r *http.Request, in *wg.Instance) error {
	send := streamNDJSON(w)

	err := s.instances.Provision(r.Context(), in, func(step deploy.Step) {
		send(provisionEvent{Step: step})
	})
	if err != nil {
		ev := provisionEvent{Error: httpx.ErrorFor(r, err)}
		var boot *deploy.BootError
		if errors.As(err, &boot) {
			ev.Log = boot.Log
		}
		send(ev)
		return nil
	}

	view, err := s.instanceView(r.Context(), in)
	if err != nil {
		send(provisionEvent{Error: httpx.ErrorFor(r, fmt.Errorf("load created instance %d: %w", in.ID, err))})
		return nil
	}
	send(provisionEvent{Instance: &view})
	return nil
}

func (s *Server) handleInstanceDefaults(w http.ResponseWriter, r *http.Request) error {
	var nodeID int64
	if raw := r.URL.Query().Get("node_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 0 {
			return httpx.BadRequest("node_id must be a node's ID")
		}
		nodeID = id
	}
	in, err := s.instances.Defaults(r.Context(), nodeID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, instanceDefaults{
		Address:             in.Address,
		ListenPort:          in.ListenPort,
		Endpoint:            in.Endpoint,
		DNS:                 in.DNS,
		MTU:                 in.MTU,
		PersistentKeepalive: in.PersistentKeepalive,
		ClientAllowedIPs:    in.ClientAllowedIPs,
		Country:             in.Country,
	})
}

type instanceDefaults struct {
	Address             netip.Prefix   `json:"address"`
	ListenPort          int            `json:"listen_port"`
	Endpoint            string         `json:"endpoint"`
	DNS                 []netip.Addr   `json:"dns"`
	MTU                 int            `json:"mtu"`
	PersistentKeepalive int            `json:"persistent_keepalive"`
	ClientAllowedIPs    []netip.Prefix `json:"client_allowed_ips"`
	// Guessed from the endpoint; empty when it cannot be.
	Country string `json:"country"`
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) error {
	current, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	var req instance.Change
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	updated, err := s.instances.Update(r.Context(), current, req)
	if err != nil {
		return err
	}
	return s.writeInstance(w, r, http.StatusOK, updated)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	if err := s.instances.Delete(r.Context(), in); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) handleInstanceAction(action func(context.Context, *wg.Instance) error) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		in, err := s.instanceFromPath(r)
		if err != nil {
			return err
		}
		if err := action(r.Context(), in); err != nil {
			return err
		}
		return s.writeInstance(w, r, http.StatusOK, in)
	}
}

func (s *Server) writeInstance(w http.ResponseWriter, r *http.Request, status int, in *wg.Instance) error {
	view, err := s.instanceView(r.Context(), in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, status, view)
}

func (s *Server) instanceView(ctx context.Context, in *wg.Instance) (instanceView, error) {
	peers, err := s.store.Peers(ctx, in.ID)
	if err != nil {
		return instanceView{}, err
	}
	return instanceView{
		Instance:  *in,
		Status:    s.deploy.Status(ctx, in),
		PeerCount: len(peers),
	}, nil
}

func (s *Server) instanceFromPath(r *http.Request) (*wg.Instance, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, httpx.NotFound("instance not found")
	}
	in, err := s.store.InstanceByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpx.NotFound("instance not found")
	}
	return in, err
}
