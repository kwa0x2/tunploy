// Package instance runs the commands on VPN servers. Each one checks its
// input, saves it, records an event and brings the container in line, so the
// panel and /api/v1 behave the same.
package instance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/geoip"
	"github.com/kwa0x2/tunploy/internal/host"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

type Service struct {
	store      *store.Store
	deploy     *deploy.Manager
	geo        *geoip.DB
	events     event.Recorder
	publicHost string
	lookupHost func(ctx context.Context, host string) ([]string, error)
}

// publicHost is the endpoint of servers on the panel's own machine while
// the settings name none.
func New(st *store.Store, dep *deploy.Manager, geo *geoip.DB, events event.Recorder, publicHost string) *Service {
	return &Service{
		store:      st,
		deploy:     dep,
		geo:        geo,
		events:     events,
		publicHost: publicHost,
		lookupHost: net.DefaultResolver.LookupHost,
	}
}

// Create checks a new server and saves it; Provision deploys it.
func (s *Service) Create(ctx context.Context, c Change) (*wg.Instance, error) {
	existing, err := s.store.Instances(ctx)
	if err != nil {
		return nil, err
	}

	var nodeID int64
	if c.NodeID != nil {
		nodeID = *c.NodeID
	}
	in, err := s.defaults(ctx, existing, nodeID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, apperr.Field("node_id", "no such node")
	}
	if err != nil {
		return nil, err
	}
	fields := c.apply(&in)
	if c.Country == nil {
		in.Country = s.country(ctx, in.Endpoint)
	}
	if c.MaxDevices != nil {
		switch bits, ok := subnetBitsFor(*c.MaxDevices); {
		case c.Address != nil:
			fields["max_devices"] = "send address or max_devices, not both"
		case !ok:
			fields["max_devices"] = fmt.Sprintf("max_devices must be between 1 and %d", wg.Capacity(wg.MinSubnetBits))
		default:
			in.Address = nextFreeSubnet(existing, bits)
		}
	}
	if c.Address != nil {
		if other := overlapping(existing, in.Address); other != nil {
			fields["address"] = fmt.Sprintf("subnet overlaps with %q (%s)", other.Name, other.Subnet())
		}
	}
	if err := apperr.Fields(in.Validate(), fields); err != nil {
		return nil, err
	}

	created, err := s.store.CreateInstance(ctx, in)
	if err != nil {
		return nil, writeError(err)
	}
	return created, nil
}

// Provision deploys a saved server and takes it away again if that fails,
// so a create either works or changes nothing.
func (s *Service) Provision(ctx context.Context, in *wg.Instance, progress deploy.Progress) error {
	if err := s.deploy.Provision(ctx, in.ID, progress); err != nil {
		s.rollback(context.WithoutCancel(ctx), in.ID)
		e := event.ForInstance("server.deploy_failed", in)
		e.Detail = err.Error()
		s.events.Record(ctx, e)
		return deployError(err)
	}
	s.events.Record(ctx, event.ForInstance("server.created", in))
	return nil
}

func (s *Service) rollback(ctx context.Context, id int64) {
	if err := s.deploy.Remove(ctx, id); err != nil {
		slog.Warn("roll back instance container", "instance", id, "error", err)
	}
	if err := s.store.DeleteInstance(ctx, id); err != nil {
		slog.Error("roll back instance row", "instance", id, "error", err)
	}
}

func (s *Service) Update(ctx context.Context, current *wg.Instance, c Change) (*wg.Instance, error) {
	in := *current
	fields := c.apply(&in)
	if c.NodeID != nil && *c.NodeID != current.NodeID {
		fields["node_id"] = "a server cannot move to another node"
	}
	if in.Address != current.Address {
		fields["address"] = "address cannot be changed once peers may hold configs for it"
	}
	if c.MaxDevices != nil {
		fields["max_devices"] = "max_devices only applies when creating a server"
	}
	if err := apperr.Fields(in.Validate(), fields); err != nil {
		return nil, err
	}

	updated, err := s.store.UpdateInstance(ctx, in)
	if err != nil {
		return nil, writeError(err)
	}

	// Port and MTU are fixed at container creation; the resolver follows its
	// file, like peers.
	s.events.Record(ctx, event.ForInstance("server.updated", updated))
	switch {
	case updated.ListenPort != current.ListenPort || updated.MTU != current.MTU:
		if err := s.deploy.Redeploy(ctx, updated.ID); err != nil {
			return nil, deployError(err)
		}
	case updated.DNSOnServer != current.DNSOnServer || !slices.Equal(updated.DNS, current.DNS):
		if err := s.deploy.Apply(ctx, updated.ID); err != nil {
			return nil, applyError(err)
		}
	}
	return updated, nil
}

// Container first: a leftover row beats a tunnel the panel forgot. An
// offline node drops the orphan itself when it reconnects.
func (s *Service) Delete(ctx context.Context, in *wg.Instance) error {
	if err := s.deploy.Remove(ctx, in.ID); err != nil && !errors.Is(err, host.ErrOffline) {
		return deployError(err)
	}
	if err := s.store.DeleteInstance(ctx, in.ID); err != nil {
		return err
	}
	s.events.Record(ctx, event.ForInstance("server.deleted", in))
	return nil
}

func (s *Service) Start(ctx context.Context, in *wg.Instance) error {
	return s.act(ctx, in, "server.started", s.deploy.Start)
}

func (s *Service) Stop(ctx context.Context, in *wg.Instance) error {
	return s.act(ctx, in, "server.stopped", s.deploy.Stop)
}

func (s *Service) Restart(ctx context.Context, in *wg.Instance) error {
	return s.act(ctx, in, "server.restarted", s.deploy.Restart)
}

func (s *Service) act(ctx context.Context, in *wg.Instance, kind string, action func(context.Context, int64) error) error {
	if err := action(ctx, in.ID); err != nil {
		return deployError(err)
	}
	s.events.Record(ctx, event.ForInstance(kind, in))
	return nil
}

// Logs is the container's output; follow keeps it open for new lines.
func (s *Service) Logs(ctx context.Context, in *wg.Instance, tail int, follow bool) (io.ReadCloser, error) {
	logs, err := s.deploy.Logs(ctx, in.ID, tail, follow)
	if errors.Is(err, deploy.ErrNotDeployed) {
		return nil, apperr.New(apperr.Conflict, "conflict", "this server has no container yet; deploy it first")
	}
	if err != nil {
		return nil, deployError(err)
	}
	return logs, nil
}

func writeError(err error) error {
	var dup *store.DuplicateError
	if errors.As(err, &dup) {
		switch dup.Column {
		case "name":
			return apperr.Field("name", "an instance with this name already exists")
		case "listen_port":
			return apperr.Field("listen_port", "another server on this machine already uses this port")
		}
	}
	return err
}

// Docker's message goes through: "port is already allocated" is what the admin needs.
func deployError(err error) error {
	e := &apperr.Error{Kind: apperr.Upstream, Code: "deploy_failed", Message: err.Error(), Err: err}
	switch {
	case errors.Is(err, store.ErrNotFound):
		e.Kind, e.Code, e.Message = apperr.NotFound, "not_found", "instance not found"
	case errors.Is(err, host.ErrOffline):
		e.Kind, e.Code = apperr.Unavailable, "node_offline"
	case errors.Is(err, docker.ErrUnavailable):
		e.Kind, e.Code, e.Message = apperr.Unavailable, "docker_unavailable", "docker is not reachable: "+err.Error()
	}
	return e
}

// Saved but not live; the next start or restart picks it up. The peer
// package has its own copy: a little copying beats a dependency.
func applyError(err error) error {
	return &apperr.Error{Kind: apperr.Upstream, Code: "apply_failed", Err: err,
		Message: "saved, but the running tunnel could not be updated (restart it to apply): " + err.Error()}
}
