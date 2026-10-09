package api

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/instance"
	"github.com/kwa0x2/tunploy/internal/node"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type v1Node struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type v1Server struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Node       v1Node `json:"node"`
	Country    string `json:"country"`
	City       string `json:"city"`
	Status     string `json:"status"`
	Endpoint   string `json:"endpoint"`
	ListenPort int    `json:"listen_port"`
	PublicKey  wg.Key `json:"public_key"`
	// The server's own address in its subnet.
	Address             netip.Prefix   `json:"address"`
	Subnet              netip.Prefix   `json:"subnet"`
	DNS                 []netip.Addr   `json:"dns"`
	DNSOnServer         bool           `json:"dns_on_server"`
	MTU                 int            `json:"mtu"`
	PersistentKeepalive int            `json:"persistent_keepalive"`
	ClientAllowedIPs    []netip.Prefix `json:"client_allowed_ips"`
	// How many more devices fit is Capacity minus DeviceCount.
	DeviceCount int       `json:"device_count"`
	Capacity    int       `json:"capacity"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Server) v1Servers(ctx context.Context, instances []wg.Instance) ([]v1Server, error) {
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

	out := make([]v1Server, len(instances))
	for i, in := range instances {
		out[i] = v1Server{
			ID:                  in.ID,
			Name:                in.Name,
			Node:                v1Node{ID: in.NodeID, Name: names[in.NodeID]},
			Country:             in.Country,
			City:                in.City,
			Status:              string(statuses[in.ID].State),
			Endpoint:            in.Endpoint,
			ListenPort:          in.ListenPort,
			PublicKey:           in.PublicKey,
			Address:             in.Address,
			Subnet:              in.Subnet(),
			DNS:                 in.DNS,
			DNSOnServer:         in.DNSOnServer,
			MTU:                 in.MTU,
			PersistentKeepalive: in.PersistentKeepalive,
			ClientAllowedIPs:    in.ClientAllowedIPs,
			DeviceCount:         counts[in.ID],
			Capacity:            wg.Capacity(in.Address.Bits()),
			CreatedAt:           in.CreatedAt,
		}
	}
	return out, nil
}

// Few enough to list whole; country narrows them to one location.
func (s *Server) v1ListServers(w http.ResponseWriter, r *http.Request) error {
	instances, err := s.store.Instances(r.Context())
	if err != nil {
		return err
	}
	if country := r.URL.Query().Get("country"); country != "" {
		instances = slices.DeleteFunc(instances, func(in wg.Instance) bool { return !strings.EqualFold(in.Country, country) })
	}
	views, err := s.v1Servers(r.Context(), instances)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, page[v1Server]{Data: views})
}

func (s *Server) v1GetServer(w http.ResponseWriter, r *http.Request) error {
	in, err := s.serverFromPath(r)
	if err != nil {
		return err
	}
	return s.writeV1Server(w, r, http.StatusOK, in)
}

func (s *Server) serverFromPath(r *http.Request) (*wg.Instance, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, httpx.NotFound("server not found")
	}
	in, err := s.store.InstanceByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, httpx.NotFound("server not found")
	}
	return in, err
}

func (s *Server) writeV1Server(w http.ResponseWriter, r *http.Request, status int, in *wg.Instance) error {
	views, err := s.v1Servers(r.Context(), []wg.Instance{*in})
	if err != nil {
		return err
	}
	return httpx.JSON(w, status, views[0])
}

// Deploys before it answers, like the panel's one click: a 201 is a running
// server, and a failed deploy leaves nothing behind.
func (s *Server) v1CreateServer(w http.ResponseWriter, r *http.Request) error {
	var req instance.Change
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	created, err := s.instances.Create(r.Context(), req)
	if err != nil {
		return err
	}
	if err := s.instances.Provision(r.Context(), created, nil); err != nil {
		return err
	}
	return s.writeV1Server(w, r, http.StatusCreated, created)
}

func (s *Server) v1UpdateServer(w http.ResponseWriter, r *http.Request) error {
	current, err := s.serverFromPath(r)
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
	return s.writeV1Server(w, r, http.StatusOK, updated)
}

// Deleting a server deletes its devices, so a server that has any needs
// ?force=true: a stale ID in a script should not cut off paying users.
func (s *Server) v1DeleteServer(w http.ResponseWriter, r *http.Request) error {
	in, err := s.serverFromPath(r)
	if err != nil {
		return err
	}
	force := r.URL.Query().Get("force")
	if force != "" && force != "true" && force != "false" {
		return httpx.BadRequest("force must be true or false")
	}
	if force != "true" {
		counts, err := s.store.PeerCounts(r.Context())
		if err != nil {
			return err
		}
		if n := counts[in.ID]; n > 0 {
			devices := "devices"
			if n == 1 {
				devices = "device"
			}
			return httpx.Errorf(http.StatusConflict, "server_not_empty",
				"server %q has %d %s; move them away first, or send ?force=true to delete them with it", in.Name, n, devices)
		}
	}
	if err := s.instances.Delete(r.Context(), in); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

type v1NodeView struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Host        string     `json:"host"`
	Status      node.State `json:"status"`
	ServerCount int        `json:"server_count"`
}

// Read only: adding a machine takes its SSH password, which stays in the panel.
func (s *Server) v1ListNodes(w http.ResponseWriter, r *http.Request) error {
	nodes, err := s.store.Nodes(r.Context())
	if err != nil {
		return err
	}
	counts, err := s.serverCounts(r.Context())
	if err != nil {
		return err
	}
	local, err := s.localNode(r.Context())
	if err != nil {
		return err
	}
	out := []v1NodeView{{Name: local.Name, Host: local.Host, Status: local.Status.State, ServerCount: counts[0]}}
	for _, n := range nodes {
		out = append(out, v1NodeView{ID: n.ID, Name: n.Name, Host: n.Host, Status: s.pool.Status(n.ID).State, ServerCount: counts[n.ID]})
	}
	return httpx.JSON(w, http.StatusOK, page[v1NodeView]{Data: out})
}
