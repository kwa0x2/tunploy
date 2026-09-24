package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// instanceRequest uses pointers so a PATCH only touches what it sends and a
// create falls back to defaults for what it leaves out.
type instanceRequest struct {
	Name                *string   `json:"name"`
	Address             *string   `json:"address"`
	ListenPort          *int      `json:"listen_port"`
	Endpoint            *string   `json:"endpoint"`
	DNS                 *[]string `json:"dns"`
	MTU                 *int      `json:"mtu"`
	PersistentKeepalive *int      `json:"persistent_keepalive"`
	ClientAllowedIPs    *[]string `json:"client_allowed_ips"`
}

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

	ids := make([]int64, len(instances))
	for i, in := range instances {
		ids[i] = in.ID
	}
	statuses := s.deploy.Statuses(r.Context(), ids)

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

// handleCreateInstance saves and deploys in one step. If the deploy fails
// the instance is rolled back, so one click either works or changes nothing.
func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) error {
	var req instanceRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	existing, err := s.store.Instances(r.Context())
	if err != nil {
		return err
	}

	in := s.defaultInstance(existing)
	fields := req.apply(&in)
	if req.Address != nil {
		if other := overlapping(existing, in.Address); other != nil {
			fields["address"] = fmt.Sprintf("subnet overlaps with %q (%s)", other.Name, other.Subnet())
		}
	}
	if err := validationError(in.Validate(), fields); err != nil {
		return err
	}

	created, err := s.store.CreateInstance(r.Context(), in)
	if err != nil {
		return instanceWriteError(err)
	}

	if err := s.deploy.Deploy(r.Context(), created.ID, true); err != nil {
		s.rollbackInstance(context.WithoutCancel(r.Context()), created.ID)
		return deployError(err)
	}
	return s.writeInstance(w, r, http.StatusCreated, created)
}

// handleInstanceDefaults tells the create form what an empty field turns into.
func (s *Server) handleInstanceDefaults(w http.ResponseWriter, r *http.Request) error {
	existing, err := s.store.Instances(r.Context())
	if err != nil {
		return err
	}
	in := s.defaultInstance(existing)
	return httpx.JSON(w, http.StatusOK, instanceDefaults{
		Address:             in.Address,
		ListenPort:          in.ListenPort,
		Endpoint:            in.Endpoint,
		DNS:                 in.DNS,
		MTU:                 in.MTU,
		PersistentKeepalive: in.PersistentKeepalive,
		ClientAllowedIPs:    in.ClientAllowedIPs,
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
}

func (s *Server) defaultInstance(existing []wg.Instance) wg.Instance {
	in := wg.NewInstance("", s.cfg.PublicHost)
	in.Address = nextFreeSubnet(existing)
	in.ListenPort = nextFreePort(existing)
	return in
}

func (s *Server) rollbackInstance(ctx context.Context, id int64) {
	if err := s.deploy.Remove(ctx, id); err != nil {
		slog.Warn("roll back instance container", "instance", id, "error", err)
	}
	if err := s.store.DeleteInstance(ctx, id); err != nil {
		slog.Error("roll back instance row", "instance", id, "error", err)
	}
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) error {
	current, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	var req instanceRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	in := *current
	fields := req.apply(&in)
	if in.Address != current.Address {
		fields["address"] = "address cannot be changed once peers may hold configs for it"
	}
	if err := validationError(in.Validate(), fields); err != nil {
		return err
	}

	updated, err := s.store.UpdateInstance(r.Context(), in)
	if err != nil {
		return instanceWriteError(err)
	}

	// Port bindings and the interface MTU are fixed when the container is
	// created; everything else only shows up in client configs.
	if updated.ListenPort != current.ListenPort || updated.MTU != current.MTU {
		if err := s.deploy.Redeploy(r.Context(), updated.ID); err != nil {
			return deployError(err)
		}
	}
	return s.writeInstance(w, r, http.StatusOK, updated)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	// Container first: if Docker is down we would rather keep the row than
	// leave a tunnel running that the panel no longer knows about.
	if err := s.deploy.Remove(r.Context(), in.ID); err != nil {
		return deployError(err)
	}
	if err := s.store.DeleteInstance(r.Context(), in.ID); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (s *Server) handleInstanceAction(action func(*deploy.Manager, context.Context, int64) error) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		in, err := s.instanceFromPath(r)
		if err != nil {
			return err
		}
		if err := action(s.deploy, r.Context(), in.ID); err != nil {
			return deployError(err)
		}
		return s.writeInstance(w, r, http.StatusOK, in)
	}
}

func (s *Server) writeInstance(w http.ResponseWriter, r *http.Request, status int, in *wg.Instance) error {
	peers, err := s.store.Peers(r.Context(), in.ID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, status, instanceView{
		Instance:  *in,
		Status:    s.deploy.Status(r.Context(), in.ID),
		PeerCount: len(peers),
	})
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

// apply copies the request onto in and returns parse errors by field.
func (req instanceRequest) apply(in *wg.Instance) map[string]string {
	fields := map[string]string{}

	if req.Name != nil {
		in.Name = strings.TrimSpace(*req.Name)
	}
	if req.Address != nil {
		p, err := netip.ParsePrefix(strings.TrimSpace(*req.Address))
		if err != nil {
			fields["address"] = "address must look like 10.8.0.1/24"
		}
		in.Address = p
	}
	if req.ListenPort != nil {
		in.ListenPort = *req.ListenPort
	}
	if req.Endpoint != nil {
		in.Endpoint = strings.TrimSpace(*req.Endpoint)
	}
	if req.DNS != nil {
		addrs, err := parseEach(*req.DNS, netip.ParseAddr)
		if err != nil {
			fields["dns"] = "dns must be a list of IP addresses"
		}
		in.DNS = addrs
	}
	if req.MTU != nil {
		in.MTU = *req.MTU
	}
	if req.PersistentKeepalive != nil {
		in.PersistentKeepalive = *req.PersistentKeepalive
	}
	if req.ClientAllowedIPs != nil {
		prefixes, err := parseEach(*req.ClientAllowedIPs, netip.ParsePrefix)
		if err != nil {
			fields["client_allowed_ips"] = "client allowed IPs must be a list of CIDR ranges"
		}
		in.ClientAllowedIPs = prefixes
	}
	return fields
}

func parseEach[T any](items []string, parse func(string) (T, error)) ([]T, error) {
	out := make([]T, 0, len(items))
	for _, s := range items {
		v, err := parse(strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// validationError prefers parse errors: validating a value that failed to
// parse would only add a vaguer message for the same field.
func validationError(validated, parsed map[string]string) error {
	for k, v := range parsed {
		validated[k] = v
	}
	if len(validated) > 0 {
		return httpx.Invalid(validated)
	}
	return nil
}

func instanceWriteError(err error) error {
	var dup *store.DuplicateError
	if errors.As(err, &dup) {
		switch dup.Column {
		case "name":
			return httpx.Invalid(map[string]string{"name": "an instance with this name already exists"})
		case "listen_port":
			return httpx.Invalid(map[string]string{"listen_port": "another instance already uses this port"})
		}
	}
	return err
}

// deployError passes Docker's own message through: "port is already
// allocated" is exactly what the admin needs to see.
func deployError(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return httpx.NotFound("instance not found")
	case errors.Is(err, docker.ErrUnavailable):
		return httpx.Errorf(http.StatusServiceUnavailable, "docker_unavailable", "docker is not reachable: %v", err)
	default:
		return httpx.Errorf(http.StatusBadGateway, "deploy_failed", "%v", err)
	}
}

func overlapping(existing []wg.Instance, addr netip.Prefix) *wg.Instance {
	if !addr.IsValid() {
		return nil
	}
	for i, in := range existing {
		if in.Subnet().Overlaps(addr.Masked()) {
			return &existing[i]
		}
	}
	return nil
}

// nextFreeSubnet walks 10.8.0.1/24, 10.9.0.1/24, ... so every instance gets
// its own range without the admin picking one.
func nextFreeSubnet(existing []wg.Instance) netip.Prefix {
	for second := 8; second <= 255; second++ {
		p := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(second), 0, 1}), 24)
		if overlapping(existing, p) == nil {
			return p
		}
	}
	return wg.DefaultAddress
}

func nextFreePort(existing []wg.Instance) int {
	used := make(map[int]bool, len(existing))
	for _, in := range existing {
		used[in.ListenPort] = true
	}
	port := wg.DefaultListenPort
	for used[port] && port < 65535 {
		port++
	}
	return port
}
