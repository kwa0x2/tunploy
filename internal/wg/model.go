package wg

import (
	"encoding/json"
	"net/netip"
	"slices"
	"time"
)

const (
	DefaultListenPort = 51820
	DefaultKeepalive  = 25
	DefaultMTU        = 1420
)

var (
	DefaultAddress = netip.MustParsePrefix("10.8.0.1/24")
	DefaultDNS     = []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("1.0.0.1")}
)

type Instance struct {
	ID int64 `json:"id"`
	// 0 is the panel's own machine.
	NodeID int64  `json:"node_id"`
	Name   string `json:"name"`
	// The prefix length sets the subnet peers get addresses from.
	Address    netip.Prefix `json:"address"`
	ListenPort int          `json:"listen_port"`
	PrivateKey Key          `json:"-"`
	PublicKey  Key          `json:"public_key"`
	// Host only; the port comes from ListenPort.
	Endpoint            string         `json:"endpoint"`
	DNS                 []netip.Addr   `json:"dns"`
	MTU                 int            `json:"mtu"`
	PersistentKeepalive int            `json:"persistent_keepalive"`
	ClientAllowedIPs    []netip.Prefix `json:"client_allowed_ips"`
	// ISO 3166 alpha-2, upper case; both may be empty.
	Country   string    `json:"country"`
	City      string    `json:"city"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// The private key is kept so the config and QR code can be shown again,
// unless the client made its own key pair: then PrivateKey is zero.
type Peer struct {
	ID           int64      `json:"id"`
	InstanceID   int64      `json:"instance_id"`
	Name         string     `json:"name"`
	Address      netip.Addr `json:"address"`
	PrivateKey   Key        `json:"-"`
	PublicKey    Key        `json:"public_key"`
	PresharedKey Key        `json:"-"`
	Enabled      bool       `json:"enabled"`
	// Set by API clients to find their own users' devices; not unique.
	ExternalID string `json:"external_id,omitempty"`
	// A JSON object, stored as given.
	Metadata json.RawMessage `json:"metadata,omitempty"`
	// Bytes per LimitPeriod, both directions; 0 means no limit.
	DataLimit   int64       `json:"data_limit"`
	LimitPeriod LimitPeriod `json:"limit_period"`
	// The limit counts from here when it is later than the period's start.
	UsageResetAt  *time.Time `json:"usage_reset_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	LastHandshake *time.Time `json:"last_handshake,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func NewInstance(name, endpoint string) Instance {
	priv := GeneratePrivateKey()
	return Instance{
		Name:                name,
		Address:             DefaultAddress,
		ListenPort:          DefaultListenPort,
		PrivateKey:          priv,
		PublicKey:           priv.PublicKey(),
		Endpoint:            endpoint,
		DNS:                 slices.Clone(DefaultDNS),
		PersistentKeepalive: DefaultKeepalive,
		ClientAllowedIPs:    []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
	}
}

func NewPeer(instanceID int64, name string) Peer {
	priv := GeneratePrivateKey()
	return Peer{
		InstanceID:   instanceID,
		Name:         name,
		PrivateKey:   priv,
		PublicKey:    priv.PublicKey(),
		PresharedKey: GeneratePresharedKey(),
		Enabled:      true,
		LimitPeriod:  PeriodMonthly,
	}
}

// NewClientPeer is a peer whose private key never reaches the panel.
func NewClientPeer(instanceID int64, name string, public Key) Peer {
	return Peer{
		InstanceID:   instanceID,
		Name:         name,
		PublicKey:    public,
		PresharedKey: GeneratePresharedKey(),
		Enabled:      true,
		LimitPeriod:  PeriodMonthly,
	}
}

func (p Peer) KeyOnClient() bool { return p.PrivateKey.IsZero() }

func (in Instance) Subnet() netip.Prefix { return in.Address.Masked() }
