package api

import (
	"fmt"
	"net/http"
	"testing"
)

type eventJSON struct {
	Kind         string `json:"kind"`
	InstanceName string `json:"instance_name"`
	PeerName     string `json:"peer_name"`
	IP           string `json:"ip"`
	Detail       string `json:"detail"`
}

func TestEventsAreRecorded(t *testing.T) {
	p := newPanel(t)
	in := p.createInstance(map[string]any{"name": "Home"})
	peer := p.createPeer(in.ID, "phone")
	p.want(p.do("PATCH", fmt.Sprintf("/api/instances/%d/peers/%d", in.ID, peer.ID),
		map[string]any{"enabled": false}), http.StatusOK, nil)
	p.want(p.do("POST", fmt.Sprintf("/api/instances/%d/stop", in.ID), nil), http.StatusOK, nil)

	var events []eventJSON
	p.want(p.do("GET", "/api/events?category=change", nil), http.StatusOK, &events)
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	want := []string{"server.stopped", "device.disabled", "device.created", "server.created"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	if events[1].PeerName != "phone" || events[1].InstanceName != "Home" {
		t.Errorf("event should name what it is about: %+v", events[1])
	}

	p.want(p.do("GET", "/api/events?category=auth", nil), http.StatusOK, &events)
	if len(events) != 1 || events[0].Kind != "auth.login" || events[0].IP == "" {
		t.Fatalf("sign-in should be logged with its address: %+v", events)
	}

	p.wantError(p.do("GET", "/api/events?limit=0", nil), http.StatusBadRequest, "invalid_request")
	p.wantError(p.do("GET", "/api/events?category=nope", nil), http.StatusBadRequest, "invalid_request")
}

func TestFailedLoginIsRecorded(t *testing.T) {
	p := newPanel(t)
	rec := do(t, p.s, "POST", "/api/auth/login", map[string]string{"email": "nobody@example.com", "password": "wrong-password"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login = %d", rec.Code)
	}
	var events []eventJSON
	p.want(p.do("GET", "/api/events?category=auth", nil), http.StatusOK, &events)
	if len(events) == 0 || events[0].Kind != "auth.login_failed" || events[0].Detail != "nobody@example.com" {
		t.Fatalf("events = %+v", events)
	}
}
