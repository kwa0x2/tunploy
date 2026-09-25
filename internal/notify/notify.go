package notify

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	// Events this close together go out as one email, so a restart that drops
	// every device is one message rather than dozens.
	batchWindow = 10 * time.Second
	maxBatch    = 50
	maxPerHour  = 30
	queueSize   = 200
)

type Group struct {
	ID    string   `json:"id"`
	Kinds []string `json:"-"`
}

var Groups = []Group{
	{ID: "servers", Kinds: []string{"server.created", "server.deploy_failed", "server.deleted", "server.down", "server.recovered",
		"node.added", "node.deleted", "node.offline", "node.online"}},
	{ID: "devices", Kinds: []string{"device.created", "device.deleted", "device.enabled", "device.disabled"}},
	{ID: "limits", Kinds: []string{"device.limit_reached", "device.expired", "device.unblocked"}},
	{ID: "failed_logins", Kinds: []string{"auth.login_failed"}},
	{ID: "security", Kinds: []string{"auth.password_changed", "auth.totp_enabled", "auth.totp_disabled",
		"settings.domain_changed", "settings.notifications_changed", "settings.backups_changed",
		"backup.downloaded", "backup.restored", "apikey.created", "apikey.revoked"}},
	{ID: "backups", Kinds: []string{"backup.failed"}},
	{ID: "logins", Kinds: []string{"auth.login"}},
	{ID: "connections", Kinds: []string{"device.connected", "device.disconnected"}},
}

var DefaultGroups = []string{"servers", "devices", "limits", "failed_logins", "security", "backups"}

func ValidGroup(id string) bool {
	return slices.ContainsFunc(Groups, func(g Group) bool { return g.ID == id })
}

type Config struct {
	SMTP
	To     []string
	Groups []string
	// Where links in the email point; empty leaves them out.
	PanelURL string
}

func (c *Config) wants(kind string) bool {
	for _, g := range Groups {
		if slices.Contains(g.Kinds, kind) && slices.Contains(c.Groups, g.ID) {
			return true
		}
	}
	return false
}

type Status struct {
	LastSentAt  *time.Time `json:"last_sent_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
}

type Notifier struct {
	mu     sync.Mutex
	cfg    *Config
	status Status
	sent   []time.Time

	queue  chan store.Event
	window time.Duration
	send   func(context.Context, SMTP, Message) error
}

func New() *Notifier {
	return &Notifier{
		queue:  make(chan store.Event, queueSize),
		window: batchWindow,
		send:   func(ctx context.Context, c SMTP, m Message) error { return c.Send(ctx, m) },
	}
}

// Configure with nil turns email off.
func (n *Notifier) Configure(cfg *Config) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg = cfg
}

func (n *Notifier) Status() Status {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.status
}

// Notify never blocks the request that caused the event.
func (n *Notifier) Notify(e store.Event) {
	n.mu.Lock()
	wanted := n.cfg != nil && n.cfg.wants(e.Kind)
	n.mu.Unlock()
	if !wanted {
		return
	}
	select {
	case n.queue <- e:
	default:
		slog.Warn("email queue is full; dropping event", "kind", e.Kind)
	}
}

func (n *Notifier) Run(ctx context.Context) {
	for {
		var batch []store.Event
		select {
		case <-ctx.Done():
			return
		case e := <-n.queue:
			batch = append(batch, e)
		}

		timer := time.NewTimer(n.window)
	collect:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case e := <-n.queue:
				if len(batch) < maxBatch {
					batch = append(batch, e)
				}
			case <-timer.C:
				break collect
			}
		}
		n.deliver(ctx, batch)
	}
}

func (n *Notifier) deliver(ctx context.Context, batch []store.Event) {
	n.mu.Lock()
	cfg := n.cfg
	now := time.Now()
	n.sent = slices.DeleteFunc(n.sent, func(t time.Time) bool { return now.Sub(t) > time.Hour })
	capped := len(n.sent) >= maxPerHour
	if cfg != nil && !capped {
		n.sent = append(n.sent, now)
	}
	n.mu.Unlock()

	if cfg == nil {
		return
	}
	if capped {
		slog.Warn("email limit reached for this hour; skipping", "events", len(batch))
		return
	}
	err := n.send(ctx, cfg.SMTP, compose(batch, cfg.To, cfg.PanelURL))
	n.record(err)
	if err != nil {
		slog.Error("send notification email", "events", len(batch), "error", err)
	}
}

func (n *Notifier) record(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now().UTC()
	if err != nil {
		n.status.LastError, n.status.LastErrorAt = err.Error(), &now
		return
	}
	n.status.LastSentAt = &now
	n.status.LastError, n.status.LastErrorAt = "", nil
}

// Test sends right away with settings that may not be saved yet.
func (n *Notifier) Test(ctx context.Context, cfg Config) error {
	body := "This is a test email from Tunploy.\n\n" +
		"If you can read this, notifications will reach " + strings.Join(cfg.To, ", ") + ".\n"
	if cfg.PanelURL != "" {
		body += "\nPanel: " + cfg.PanelURL + "\n"
	}
	err := n.send(ctx, cfg.SMTP, Message{To: cfg.To, Subject: "[Tunploy] Test email", Body: body,
		HTML: renderHTML(testView(cfg.To, cfg.PanelURL))})
	n.record(err)
	return err
}

func compose(batch []store.Event, to []string, panelURL string) Message {
	var b strings.Builder
	subject := "[Tunploy] " + Describe(batch[0])
	if len(batch) == 1 {
		writeEvent(&b, batch[0])
	} else {
		subject = fmt.Sprintf("[Tunploy] %d new events: %s", len(batch), Describe(batch[0]))
		for i, e := range batch {
			if i > 0 {
				b.WriteString("\n")
			}
			writeEvent(&b, e)
		}
	}
	if panelURL != "" {
		b.WriteString("\nActivity log: " + strings.TrimSuffix(panelURL, "/") + "/activity\n")
	}
	b.WriteString("\n--\nSent by Tunploy. Choose which events send email under Settings → Notifications.\n")
	return Message{To: to, Subject: subject, Body: b.String(), HTML: renderHTML(eventsView(batch, panelURL))}
}

func writeEvent(b *strings.Builder, e store.Event) {
	b.WriteString(Describe(e) + "\n")
	at := e.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(b, "  %-9s %s\n", label+":", value)
		}
	}
	line("Time", at.In(time.Local).Format("2 Jan 2006 15:04:05 MST"))
	line("Node", e.NodeName)
	line("Server", e.InstanceName)
	line("Device", e.PeerName)
	ip := e.IP
	if ip != "" && e.Country != "" {
		ip += " (" + e.Country + ")"
	}
	line("IP", ip)
	line("Details", e.Detail)
	line("By", actorText(e.Actor))
}

func actorText(actor string) string {
	if name, ok := strings.CutPrefix(actor, "api:"); ok {
		return "API key " + name
	}
	return actor
}

func Describe(e store.Event) string {
	device, server := quote(e.PeerName), quote(e.InstanceName)
	switch e.Kind {
	case "device.connected":
		return device + " connected to " + server
	case "device.disconnected":
		return device + " disconnected from " + server
	case "device.created":
		return device + " was added to " + server
	case "device.deleted":
		return device + " was removed from " + server
	case "device.enabled":
		return device + " was enabled on " + server
	case "device.disabled":
		return device + " was disabled on " + server
	case "device.renamed":
		return "A device on " + server + " was renamed to " + device
	case "device.limits_changed":
		return "Limits changed for " + device + " on " + server
	case "device.limit_reached":
		return device + " reached its data limit on " + server
	case "device.expired":
		return "Access for " + device + " on " + server + " ended"
	case "device.unblocked":
		return device + " can connect to " + server + " again"
	case "server.created":
		return "Server " + server + " was created"
	case "server.deploy_failed":
		return "Deploying server " + server + " failed"
	case "server.updated":
		return "Server " + server + " settings changed"
	case "server.deleted":
		return "Server " + server + " was deleted"
	case "server.started":
		return "Server " + server + " was started"
	case "server.stopped":
		return "Server " + server + " was stopped"
	case "server.restarted":
		return "Server " + server + " was restarted"
	case "server.down":
		return "Server " + server + " is down"
	case "server.recovered":
		return "Server " + server + " is running again"
	case "node.added":
		return "Node " + quote(e.NodeName) + " was added"
	case "node.renamed":
		return "A node was renamed to " + quote(e.NodeName)
	case "node.deleted":
		return "Node " + quote(e.NodeName) + " was removed"
	case "node.offline":
		return "Node " + quote(e.NodeName) + " is offline"
	case "node.online":
		return "Node " + quote(e.NodeName) + " is back online"
	case "settings.updated":
		return "Panel settings changed"
	case "settings.domain_changed":
		if e.Detail == "" {
			return "Panel domain removed"
		}
		return "Panel domain set"
	case "settings.notifications_changed":
		return "Email notification settings changed"
	case "settings.backups_changed":
		if e.Detail == "disconnected" {
			return "Backup bucket disconnected"
		}
		return "Backup settings changed"
	case "backup.created":
		return "Backup created"
	case "backup.failed":
		return "Scheduled backup failed"
	case "backup.downloaded":
		return "A backup was downloaded"
	case "backup.deleted":
		return "A backup was deleted"
	case "backup.restored":
		return "The panel was restored from a backup"
	case "auth.login":
		return "Someone signed in to the panel"
	case "auth.login_failed":
		return "Failed sign-in attempt on the panel"
	case "auth.password_changed":
		return "The admin password was changed"
	case "auth.totp_enabled":
		return "Two-factor authentication was turned on"
	case "auth.totp_disabled":
		return "Two-factor authentication was turned off"
	case "apikey.created":
		return "An API key was created"
	case "apikey.revoked":
		return "An API key was revoked"
	}
	return e.Kind
}

func quote(name string) string {
	if name == "" {
		return "?"
	}
	return name
}
