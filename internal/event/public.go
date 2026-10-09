package event

import (
	"slices"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
)

// Public is an event as /api/v1/events lists it and webhooks send it, so a
// receiver can use either.
type Public struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	CreatedAt  time.Time `json:"created_at"`
	ServerID   int64     `json:"server_id,omitempty"`
	ServerName string    `json:"server_name,omitempty"`
	DeviceID   int64     `json:"device_id,omitempty"`
	DeviceName string    `json:"device_name,omitempty"`
	NodeName   string    `json:"node_name,omitempty"`
	IP         string    `json:"ip,omitempty"`
	Country    string    `json:"country,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	Actor      string    `json:"actor,omitempty"`
}

func ToPublic(e store.Event) Public {
	return Public{
		ID: e.ID, Kind: e.Kind, CreatedAt: e.CreatedAt.UTC().Truncate(time.Second),
		ServerID: e.InstanceID, ServerName: e.InstanceName, DeviceID: e.PeerID, DeviceName: e.PeerName,
		NodeName: e.NodeName, IP: e.IP, Country: e.Country, Detail: e.Detail, Actor: e.Actor,
	}
}

// Sign-ins and settings stay out: an API key reaches devices, not the panel.
var PublicFamilies = []string{"device", "server", "node"}

// PublicKinds is every kind a webhook can subscribe to.
var PublicKinds = []string{
	"device.created", "device.deleted", "device.renamed", "device.moved", "device.enabled", "device.disabled",
	"device.limits_changed", "device.limit_reached", "device.expired", "device.unblocked", "device.usage_reset",
	"device.shared", "device.unshared", "device.connected", "device.disconnected",
	"server.created", "server.deploy_failed", "server.updated", "server.deleted", "server.started",
	"server.stopped", "server.restarted", "server.down", "server.recovered",
	"node.added", "node.renamed", "node.deleted", "node.offline", "node.online",
}

func IsPublic(kind string) bool {
	family, _, _ := strings.Cut(kind, ".")
	return slices.Contains(PublicFamilies, family)
}
