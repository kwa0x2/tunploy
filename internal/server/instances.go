package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

// Pointers: PATCH touches only what it sends, create defaults the rest.
type instanceRequest struct {
	// Only on create: a server stays on the machine it was made on.
	NodeID              *int64    `json:"node_id"`
	Name                *string   `json:"name"`
	Address             *string   `json:"address"`
	ListenPort          *int      `json:"listen_port"`
	Endpoint            *string   `json:"endpoint"`
	DNS                 *[]string `json:"dns"`
	MTU                 *int      `json:"mtu"`
	PersistentKeepalive *int      `json:"persistent_keepalive"`
	ClientAllowedIPs    *[]string `json:"client_allowed_ips"`
	Country             *string   `json:"country"`
	City                *string   `json:"city"`
	// Only on create, instead of address: the smallest subnet that fits.
	MaxDevices *int `json:"max_devices"`
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

// A failed deploy rolls back, so one click either works or changes nothing.
func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) error {
	var req instanceRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	created, err := s.saveNewInstance(r.Context(), req)
	if err != nil {
		return err
	}
	if strings.Contains(r.Header.Get("Accept"), ndjson) {
		return s.streamProvision(w, r, created)
	}
	if err := s.provision(r.Context(), created, nil); err != nil {
		return deployError(err)
	}
	return s.writeInstance(w, r, http.StatusCreated, created)
}

// saveNewInstance validates a create request and stores the row; provision deploys it.
func (s *Server) saveNewInstance(ctx context.Context, req instanceRequest) (*wg.Instance, error) {
	existing, err := s.store.Instances(ctx)
	if err != nil {
		return nil, err
	}

	var nodeID int64
	if req.NodeID != nil {
		nodeID = *req.NodeID
	}
	in, err := s.defaultInstance(ctx, existing, nodeID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpx.Invalid(map[string]string{"node_id": "no such node"})
	}
	if err != nil {
		return nil, err
	}
	fields := req.apply(&in)
	if req.Country == nil {
		in.Country = s.hostCountry(ctx, in.Endpoint)
	}
	if req.MaxDevices != nil {
		switch bits, ok := subnetBitsFor(*req.MaxDevices); {
		case req.Address != nil:
			fields["max_devices"] = "send address or max_devices, not both"
		case !ok:
			fields["max_devices"] = fmt.Sprintf("max_devices must be between 1 and %d", subnetCapacity(wg.MinSubnetBits))
		default:
			in.Address = nextFreeSubnet(existing, bits)
		}
	}
	if req.Address != nil {
		if other := overlapping(existing, in.Address); other != nil {
			fields["address"] = fmt.Sprintf("subnet overlaps with %q (%s)", other.Name, other.Subnet())
		}
	}
	if err := validationError(in.Validate(), fields); err != nil {
		return nil, err
	}

	created, err := s.store.CreateInstance(ctx, in)
	if err != nil {
		return nil, instanceWriteError(err)
	}
	return created, nil
}

// provision deploys a new instance, and takes it away again if that fails.
func (s *Server) provision(ctx context.Context, in *wg.Instance, progress deploy.Progress) error {
	if err := s.deploy.Provision(ctx, in.ID, progress); err != nil {
		s.failedCreate(ctx, in, err)
		return err
	}
	s.record(ctx, instanceEvent("server.created", in))
	return nil
}

const ndjson = "application/x-ndjson"

type provisionEvent struct {
	Step     deploy.Step   `json:"step,omitempty"`
	Instance *instanceView `json:"instance,omitempty"`
	Error    *httpx.Error  `json:"error,omitempty"`
	Log      []string      `json:"log,omitempty"`
}

// Validation already ran, so the status is 200 and the outcome is the last line.
func (s *Server) streamProvision(w http.ResponseWriter, r *http.Request, in *wg.Instance) error {
	h := w.Header()
	h.Set("Content-Type", ndjson)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	enc := json.NewEncoder(w)
	send := func(ev provisionEvent) {
		if err := enc.Encode(ev); err == nil {
			rc.Flush()
		}
	}

	err := s.provision(r.Context(), in, func(step deploy.Step) {
		send(provisionEvent{Step: step})
	})
	if err != nil {
		ev := provisionEvent{}
		errors.As(deployError(err), &ev.Error)
		var boot *deploy.BootError
		if errors.As(err, &boot) {
			ev.Log = boot.Log
		}
		send(ev)
		return nil
	}

	view, err := s.instanceView(r.Context(), in)
	if err != nil {
		slog.Error("load created instance", "instance", in.ID, "error", err)
		send(provisionEvent{Error: httpx.Errorf(http.StatusInternalServerError, "internal_error", "something went wrong")})
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
	existing, err := s.store.Instances(r.Context())
	if err != nil {
		return err
	}
	in, err := s.defaultInstance(r.Context(), existing, nodeID)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.NotFound("node not found")
	}
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
		Country:             s.hostCountry(r.Context(), in.Endpoint),
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

// hostCountry guesses where a server is from its endpoint, for location lists.
func (s *Server) hostCountry(ctx context.Context, host string) string {
	if addr, err := netip.ParseAddr(host); err == nil {
		return s.geo.Country(addr)
	}
	if s.geo == nil || !wg.ValidHost(host) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addrs, err := s.lookupHost(ctx, host)
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if addr, err := netip.ParseAddr(a); err == nil {
			if c := s.geo.Country(addr); c != "" {
				return c
			}
		}
	}
	return ""
}

// Subnets stay apart across nodes too, so a client can hold configs for
// several servers at once; ports only need to differ per machine.
func (s *Server) defaultInstance(ctx context.Context, existing []wg.Instance, nodeID int64) (wg.Instance, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return wg.Instance{}, err
	}
	endpoint := settings.publicHost(s.cfg.PublicHost)
	if nodeID != 0 {
		n, err := s.store.NodeByID(ctx, nodeID)
		if err != nil {
			return wg.Instance{}, err
		}
		endpoint = n.Host
	}
	in := wg.NewInstance("", endpoint)
	in.NodeID = nodeID
	in.Address = nextFreeSubnet(existing, wg.DefaultAddress.Bits())
	in.ListenPort = nextFreePort(existing, nodeID)
	in.DNS = slices.Clone(settings.DefaultDNS)
	return in, nil
}

func (s *Server) failedCreate(ctx context.Context, in *wg.Instance, err error) {
	s.rollbackInstance(context.WithoutCancel(ctx), in.ID)
	e := instanceEvent("server.deploy_failed", in)
	e.Detail = err.Error()
	s.record(ctx, e)
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
	updated, err := s.updateInstance(r.Context(), current, req)
	if err != nil {
		return err
	}
	return s.writeInstance(w, r, http.StatusOK, updated)
}

func (s *Server) updateInstance(ctx context.Context, current *wg.Instance, req instanceRequest) (*wg.Instance, error) {
	in := *current
	fields := req.apply(&in)
	if req.NodeID != nil && *req.NodeID != current.NodeID {
		fields["node_id"] = "a server cannot move to another node"
	}
	if in.Address != current.Address {
		fields["address"] = "address cannot be changed once peers may hold configs for it"
	}
	if req.MaxDevices != nil {
		fields["max_devices"] = "max_devices only applies when creating a server"
	}
	if err := validationError(in.Validate(), fields); err != nil {
		return nil, err
	}

	updated, err := s.store.UpdateInstance(ctx, in)
	if err != nil {
		return nil, instanceWriteError(err)
	}

	// Port and MTU are fixed at container creation.
	s.record(ctx, instanceEvent("server.updated", updated))
	if updated.ListenPort != current.ListenPort || updated.MTU != current.MTU {
		if err := s.deploy.Redeploy(ctx, updated.ID); err != nil {
			return nil, deployError(err)
		}
	}
	return updated, nil
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) error {
	in, err := s.instanceFromPath(r)
	if err != nil {
		return err
	}
	if err := s.deleteInstance(r.Context(), in); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// Container first: a leftover row beats a tunnel the panel forgot. An
// offline node drops the orphan itself when it reconnects.
func (s *Server) deleteInstance(ctx context.Context, in *wg.Instance) error {
	if err := s.deploy.Remove(ctx, in.ID); err != nil && !errors.Is(err, deploy.ErrNodeOffline) {
		return deployError(err)
	}
	if err := s.store.DeleteInstance(ctx, in.ID); err != nil {
		return err
	}
	s.record(ctx, instanceEvent("server.deleted", in))
	return nil
}

func (s *Server) handleInstanceAction(kind string, action func(*deploy.Manager, context.Context, int64) error) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		in, err := s.instanceFromPath(r)
		if err != nil {
			return err
		}
		if err := action(s.deploy, r.Context(), in.ID); err != nil {
			return deployError(err)
		}
		s.record(r.Context(), instanceEvent(kind, in))
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
	if req.Country != nil {
		in.Country = strings.ToUpper(strings.TrimSpace(*req.Country))
	}
	if req.City != nil {
		in.City = strings.TrimSpace(*req.City)
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

// Parse errors win over the vaguer validation of the same field.
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
			return httpx.Invalid(map[string]string{"listen_port": "another server on this machine already uses this port"})
		}
	}
	return err
}

// Docker's message goes through: "port is already allocated" is what the admin needs.
func deployError(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return httpx.NotFound("instance not found")
	case errors.Is(err, deploy.ErrNodeOffline):
		return httpx.Errorf(http.StatusServiceUnavailable, "node_offline", "%v", err)
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

// Candidates start each 10.x block, so any size up to a /16 is aligned.
func nextFreeSubnet(existing []wg.Instance, bits int) netip.Prefix {
	for second := 8; second <= 255; second++ {
		p := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(second), 0, 1}), bits)
		if overlapping(existing, p) == nil {
			return p
		}
	}
	return netip.PrefixFrom(wg.DefaultAddress.Addr(), bits)
}

// subnetBitsFor is the prefix of the smallest subnet, a /24 at least, that holds n devices.
func subnetBitsFor(n int) (int, bool) {
	for bits := wg.DefaultAddress.Bits(); bits >= wg.MinSubnetBits; bits-- {
		if n <= subnetCapacity(bits) {
			return bits, n >= 1
		}
	}
	return 0, false
}

func nextFreePort(existing []wg.Instance, nodeID int64) int {
	used := make(map[int]bool, len(existing))
	for _, in := range existing {
		if in.NodeID == nodeID {
			used[in.ListenPort] = true
		}
	}
	port := wg.DefaultListenPort
	for used[port] && port < 65535 {
		port++
	}
	return port
}
