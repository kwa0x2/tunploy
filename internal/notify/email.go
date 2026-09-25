package notify

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/store"
)

//go:embed email.html
var emailHTML string

var emailTemplate = template.Must(template.New("email").Parse(emailHTML))

type tone struct{ Label, Dot, Tint, Text string }

var (
	toneAlert    = tone{"Alert", "#dc2626", "#fef2f2", "#b91c1c"}
	toneResolved = tone{"Resolved", "#16a34a", "#f0fdf4", "#15803d"}
	toneLimit    = tone{"Limit", "#d97706", "#fffbeb", "#b45309"}
	toneSecurity = tone{"Security", "#7c3aed", "#f5f3ff", "#6d28d9"}
	toneActivity = tone{"Activity", "#71717a", "#f4f4f5", "#3f3f46"}
)

func toneOf(kind string) tone {
	switch kind {
	case "server.down", "server.deploy_failed", "node.offline", "backup.failed", "auth.login_failed":
		return toneAlert
	case "server.recovered", "node.online", "device.unblocked":
		return toneResolved
	case "device.limit_reached", "device.expired":
		return toneLimit
	}
	if strings.HasPrefix(kind, "auth.") || strings.HasPrefix(kind, "settings.") ||
		kind == "backup.downloaded" || kind == "backup.restored" {
		return toneSecurity
	}
	return toneActivity
}

type row struct{ Label, Value string }

type eventView struct {
	Title string
	Time  string
	Tone  tone
	Rows  []row
}

type link struct{ Label, URL string }

type emailView struct {
	Heading    string
	Preheader  string
	Host       string
	Single     bool
	Events     []eventView
	Paragraphs []string
	Button     *link
	// Empty without a panel URL, as a link to nowhere is worse than none.
	SettingsURL string
}

func eventTime(e store.Event) time.Time {
	if e.CreatedAt.IsZero() {
		return time.Now()
	}
	return e.CreatedAt
}

func eventRows(e store.Event) []row {
	var rows []row
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, row{label, value})
		}
	}
	add("Node", e.NodeName)
	add("Server", e.InstanceName)
	add("Device", e.PeerName)
	ip := e.IP
	if ip != "" && e.Country != "" {
		ip += " (" + e.Country + ")"
	}
	add("IP", ip)
	add("Details", e.Detail)
	return rows
}

func newView(heading, panelURL string) emailView {
	v := emailView{Heading: heading, Preheader: heading}
	if base := strings.TrimSuffix(panelURL, "/"); base != "" {
		if u, err := url.Parse(base); err == nil {
			v.Host = u.Host
		}
		v.SettingsURL = base + "/settings/notifications"
	}
	return v
}

func eventsView(batch []store.Event, panelURL string) emailView {
	heading := Describe(batch[0])
	if len(batch) > 1 {
		heading = fmt.Sprintf("%d new events on your panel", len(batch))
	}
	v := newView(heading, panelURL)
	v.Single = len(batch) == 1
	for _, e := range batch {
		v.Events = append(v.Events, eventView{
			Title: Describe(e),
			Time:  eventTime(e).In(time.Local).Format("2 Jan 2006, 15:04 MST"),
			Tone:  toneOf(e.Kind),
			Rows:  eventRows(e),
		})
	}
	if len(batch) > 1 {
		v.Preheader = Describe(batch[0]) + fmt.Sprintf(", and %d more", len(batch)-1)
	}
	if base := strings.TrimSuffix(panelURL, "/"); base != "" {
		v.Button = &link{"Open the activity log", base + "/activity"}
		// A deleted server has no page left to open.
		if e := batch[0]; v.Single && e.InstanceID != 0 && e.Kind != "server.deleted" {
			v.Button = &link{"Open " + quote(e.InstanceName), fmt.Sprintf("%s/servers/%d", base, e.InstanceID)}
		}
	}
	return v
}

func testView(to []string, panelURL string) emailView {
	v := newView("Email notifications work", panelURL)
	v.Paragraphs = []string{
		"This is a test email from Tunploy. If you can read it, notifications will reach " + strings.Join(to, ", ") + ".",
	}
	if base := strings.TrimSuffix(panelURL, "/"); base != "" {
		v.Button = &link{"Open the panel", base}
	}
	return v
}

func renderHTML(v emailView) string {
	var b strings.Builder
	if err := emailTemplate.Execute(&b, v); err != nil {
		// The template is fixed at build time; the plain-text part still goes out.
		return ""
	}
	return b.String()
}
