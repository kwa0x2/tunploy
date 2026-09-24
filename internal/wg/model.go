package wg

import (
	"net/netip"
	"time"
)

const (
	DefaultListenPort = 51820
	DefaultKeepalive  = 25
	DefaultMTU        = 1420
)

var DefaultAddress = netip.MustParsePrefix("10.8.0.1/24")

// Instance is one WireGuard interface, run as its own container.
type Instance struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Address is the server's own address; its prefix length sets the subnet
	// peers are allocated from.
	Address    netip.Prefix `json:"address"`
	ListenPort int          `json:"listen_port"`
	PrivateKey Key          `json:"-"`
	PublicKey  Key          `json:"public_key"`
	// Endpoint is the host clients dial; the port always comes from ListenPort.
	Endpoint            string         `json:"endpoint"`
	DNS                 []netip.Addr   `json:"dns"`
	MTU                 int            `json:"mtu"`
	PersistentKeepalive int            `json:"persistent_keepalive"`
	ClientAllowedIPs    []netip.Prefix `json:"client_allowed_ips"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

// Peer is a client of an instance. Its private key is kept so the panel can
// hand out the config and QR code again later.
type Peer struct {
	ID           int64      `json:"id"`
	InstanceID   int64      `json:"instance_id"`
	Name         string     `json:"name"`
	Address      netip.Addr `json:"address"`
	PrivateKey   Key        `json:"-"`
	PublicKey    Key        `json:"public_key"`
	PresharedKey Key        `json:"-"`
	Enabled      bool       `json:"enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// NewInstance returns an instance with fresh keys and full-tunnel defaults.
func NewInstance(name, endpoint string) Instance {
	priv := GeneratePrivateKey()
	return Instance{
		Name:                name,
		Address:             DefaultAddress,
		ListenPort:          DefaultListenPort,
		PrivateKey:          priv,
		PublicKey:           priv.PublicKey(),
		Endpoint:            endpoint,
		DNS:                 []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("1.0.0.1")},
		PersistentKeepalive: DefaultKeepalive,
		ClientAllowedIPs:    []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
	}
}

// NewPeer returns an enabled peer with fresh keys; the store assigns its address.
func NewPeer(instanceID int64, name string) Peer {
	priv := GeneratePrivateKey()
	return Peer{
		InstanceID:   instanceID,
		Name:         name,
		PrivateKey:   priv,
		PublicKey:    priv.PublicKey(),
		PresharedKey: GeneratePresharedKey(),
		Enabled:      true,
	}
}

func (in Instance) Subnet() netip.Prefix { return in.Address.Masked() }
