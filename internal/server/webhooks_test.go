package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type webhookJSON struct {
	ID      int64    `json:"id"`
	URL     string   `json:"url"`
	Events  []string `json:"events"`
	Enabled bool     `json:"enabled"`
	Secret  string   `json:"secret"`
}

type deliveryJSON struct {
	ID      int64           `json:"id"`
	EventID int64           `json:"event_id"`
	Kind    string          `json:"kind"`
	State   string          `json:"state"`
	Error   string          `json:"error"`
	Payload json.RawMessage `json:"payload"`
}

func TestAPIWebhooks(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Frankfurt"})
	devices := p.apiKey("billing", "devices:write")
	token := p.apiKey("hooks", "webhooks:write")

	p.wantError(p.api(devices, "GET", "/api/v1/webhooks", nil), http.StatusForbidden, "missing_scope")
	for _, bad := range []map[string]any{
		{"url": "ftp://example.com"},
		{"url": "https://user:pass@example.com/"},
		{"url": "https://example.com/", "events": []string{"auth.login"}},
		{"url": "https://example.com/", "events": []string{}},
	} {
		p.wantError(p.api(token, "POST", "/api/v1/webhooks", bad), http.StatusUnprocessableEntity, "validation_failed")
	}

	var hook webhookJSON
	p.want(p.api(token, "POST", "/api/v1/webhooks", map[string]any{
		"url": "https://billing.example.com/hooks/tunploy?token=abc", "events": []string{"device.usage_reset", "device.created"},
	}), http.StatusCreated, &hook)
	if !strings.HasPrefix(hook.Secret, "whsec_") || strings.Join(hook.Events, ",") != "device.created,device.usage_reset" {
		t.Fatalf("created = %+v", hook)
	}
	var again webhookJSON
	p.want(p.api(token, "GET", fmt.Sprintf("/api/v1/webhooks/%d", hook.ID), nil), http.StatusOK, &again)
	if again.Secret != "" {
		t.Fatal("the secret was shown a second time")
	}

	// Only subscribed kinds queue; the activity log keeps the host, not the token.
	var d deviceJSON
	p.want(p.api(devices, "POST", "/api/v1/devices", map[string]any{"server_id": in.ID, "name": "phone"}),
		http.StatusCreated, &d)
	p.want(p.api(devices, "DELETE", fmt.Sprintf("/api/v1/devices/%d", d.ID), nil), http.StatusNoContent, nil)

	var list pageJSON[deliveryJSON]
	path := fmt.Sprintf("/api/v1/webhooks/%d/deliveries", hook.ID)
	p.want(p.api(token, "GET", path, nil), http.StatusOK, &list)
	if len(list.Data) != 1 || list.Data[0].Kind != "device.created" || list.Data[0].State != "pending" {
		t.Fatalf("deliveries = %+v", list.Data)
	}
	var payload struct {
		ID         int64  `json:"id"`
		Kind       string `json:"kind"`
		DeviceID   int64  `json:"device_id"`
		DeviceName string `json:"device_name"`
		Actor      string `json:"actor"`
	}
	if err := json.Unmarshal(list.Data[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID == 0 || payload.ID != list.Data[0].EventID || payload.DeviceID != d.ID ||
		payload.DeviceName != "phone" || payload.Actor != "api:billing" {
		t.Fatalf("payload = %+v", payload)
	}

	var ping deliveryJSON
	p.want(p.api(token, "POST", path[:len(path)-len("/deliveries")]+"/ping", nil), http.StatusAccepted, &ping)
	if ping.Kind != "ping" {
		t.Fatalf("ping = %+v", ping)
	}

	// Turning it off gives up on what was waiting.
	p.want(p.api(token, "PATCH", fmt.Sprintf("/api/v1/webhooks/%d", hook.ID), map[string]any{"enabled": false}),
		http.StatusOK, &again)
	p.want(p.api(token, "GET", path, nil), http.StatusOK, &list)
	for _, dl := range list.Data {
		if dl.State != "failed" || dl.Error == "" {
			t.Fatalf("after turning off: %+v", list.Data)
		}
	}
	p.wantError(p.api(token, "POST", fmt.Sprintf("%s/%d/retry", path, ping.ID), nil), http.StatusConflict, "conflict")

	var events []eventJSON
	p.want(p.do("GET", "/api/events?category=change", nil), http.StatusOK, &events)
	var created string
	for _, e := range events {
		if e.Kind == "webhook.created" {
			created = e.Detail
		}
	}
	if created != "billing.example.com (device.created, device.usage_reset)" {
		t.Fatalf("webhook.created detail = %q", created)
	}

	p.want(p.api(token, "DELETE", fmt.Sprintf("/api/v1/webhooks/%d", hook.ID), nil), http.StatusNoContent, nil)
	p.wantError(p.api(token, "GET", path, nil), http.StatusNotFound, "not_found")
}
