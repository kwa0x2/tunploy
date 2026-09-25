package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/notify/notifytest"
)

type notificationsJSON struct {
	Enabled     bool     `json:"enabled"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Security    string   `json:"security"`
	Username    string   `json:"username"`
	Password    *string  `json:"password"`
	PasswordSet bool     `json:"password_set"`
	From        string   `json:"from"`
	To          []string `json:"to"`
	Events      []string `json:"events"`
}

func TestNotificationSettings(t *testing.T) {
	p := newPanel(t)

	var got notificationsJSON
	p.want(p.do("GET", "/api/settings/notifications", nil), http.StatusOK, &got)
	if got.Enabled || got.Port != 587 || got.Security != "starttls" || len(got.To) != 0 ||
		strings.Join(got.Events, ",") != "servers,devices,limits,failed_logins,security,backups" {
		t.Fatalf("defaults = %+v", got)
	}

	e := p.wantError(p.do("PUT", "/api/settings/notifications", map[string]any{
		"enabled": true, "host": "smtp.example.com:587", "from": "nope", "to": []string{"a@example.com", "bad"},
		"events": []string{"servers", "gossip"},
	}), http.StatusUnprocessableEntity, "validation_failed")
	for _, f := range []string{"host", "from", "to", "events"} {
		if e.Error.Fields[f] == "" {
			t.Errorf("no error for %s: %+v", f, e.Error.Fields)
		}
	}
	e = p.wantError(p.do("PUT", "/api/settings/notifications", map[string]any{
		"enabled": true, "host": "smtp.example.com", "security": "none", "username": "me", "password": "pw",
		"from": "a@example.com", "to": []string{"a@example.com"},
	}), http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["security"] == "" {
		t.Errorf("a password must not travel unencrypted: %+v", e.Error.Fields)
	}

	body := map[string]any{
		"enabled": true, "host": "smtp.example.com", "port": 465, "security": "tls",
		"username": "tunploy@example.com", "password": "s3cret", "from": "Tunploy <tunploy@example.com>",
		"to": []string{" Me@Example.com ", "me@example.com", "ops@example.com"}, "events": []string{"servers", "limits"},
	}
	rec := p.do("PUT", "/api/settings/notifications", body)
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatal("the SMTP password must never be sent back")
	}
	got = notificationsJSON{}
	p.want(rec, http.StatusOK, &got)
	if !got.Enabled || !got.PasswordSet || got.Port != 465 || strings.Join(got.To, ",") != "Me@Example.com,ops@example.com" ||
		got.From != "Tunploy <tunploy@example.com>" {
		t.Fatalf("saved = %+v", got)
	}

	delete(body, "password")
	body["port"] = 587
	body["security"] = "starttls"
	got = notificationsJSON{}
	p.want(p.do("PUT", "/api/settings/notifications", body), http.StatusOK, &got)
	if !got.PasswordSet {
		t.Fatal("leaving the password out must keep the saved one")
	}

	var events []eventJSON
	p.want(p.do("GET", "/api/events?category=change", nil), http.StatusOK, &events)
	if events[0].Kind != "settings.notifications_changed" || events[0].Detail != "to Me@Example.com, ops@example.com" {
		t.Fatalf("events = %+v", events)
	}
}

func TestNotificationTestEmail(t *testing.T) {
	p := newPanel(t)
	srv := notifytest.New(t)
	body := map[string]any{
		"host": srv.Host, "port": srv.Port, "security": "none",
		"from": "tunploy@example.com", "to": []string{"me@example.com"},
	}

	p.want(p.do("POST", "/api/settings/notifications/test", body), http.StatusNoContent, nil)
	mails := srv.Mails()
	if len(mails) != 1 || mails[0].To[0] != "me@example.com" || !strings.Contains(mails[0].Data, "test email from Tunploy") {
		t.Fatalf("mails = %+v", mails)
	}

	srv.Reject = []string{"me@example.com"}
	e := p.wantError(p.do("POST", "/api/settings/notifications/test", body), http.StatusBadGateway, "email_failed")
	if !strings.Contains(e.Error.Message, "no such user") {
		t.Fatalf("the server's reason should reach the form: %s", e.Error.Message)
	}

	var got notificationsJSON
	p.want(p.do("GET", "/api/settings/notifications", nil), http.StatusOK, &got)
	if got.Host != "" {
		t.Fatal("a test must not save the settings")
	}

	delete(body, "to")
	p.wantError(p.do("POST", "/api/settings/notifications/test", body), http.StatusUnprocessableEntity, "validation_failed")
}

func TestServerHealthIsRecorded(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})

	p.s.serverHealth(deploy.ServerHealth{InstanceID: in.ID, Down: true, Reason: "exited with code 1"})
	p.s.serverHealth(deploy.ServerHealth{InstanceID: in.ID})

	var events []eventJSON
	p.want(p.do("GET", "/api/events?category=change", nil), http.StatusOK, &events)
	if events[0].Kind != "server.recovered" || events[1].Kind != "server.down" || events[1].Detail != "exited with code 1" {
		t.Fatalf("events = %+v", events)
	}
}
