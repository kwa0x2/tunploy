package server

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/tlscert"
)

type fakeHTTPS struct {
	mu      sync.Mutex
	status  tlscert.Status
	fail    string
	obtains int
}

func (f *fakeHTTPS) Status() tlscert.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeHTTPS) Configure(domain, email string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status.Domain, f.status.Email = domain, email
	f.status.State = tlscert.StatePending
	if domain == "" {
		f.status.State = tlscert.StateOff
	}
}

func (f *fakeHTTPS) Obtain(ctx context.Context) (tlscert.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status.Domain == "" {
		return tlscert.Status{}, tlscert.ErrNoDomain
	}
	f.obtains++
	f.status.State, f.status.Error = tlscert.StateReady, ""
	if f.fail != "" {
		f.status.State, f.status.Error = tlscert.StateFailed, f.fail
	}
	return f.status, nil
}

func fakeCerts(s *Server) *fakeHTTPS { return s.https.(*fakeHTTPS) }

func TestSetDomain(t *testing.T) {
	s := newTestServer(t)
	cookie := loggedIn(t, s)

	rec := do(t, s, "PUT", "/api/settings/domain", map[string]string{
		"domain": " Panel.Example.com. ", "email": "me@example.com",
	}, cookie)
	var st tlscert.Status
	decode(t, rec, &st)
	if rec.Code != http.StatusOK || st.State != tlscert.StateReady || st.Domain != "panel.example.com" {
		t.Fatalf("set domain: %d %+v", rec.Code, st)
	}
	if fakeCerts(s).obtains != 1 {
		t.Fatal("saving a domain must request its certificate")
	}

	var info httpsInfo
	decode(t, do(t, s, "GET", "/api/https", nil), &info)
	if info.URL != "https://panel.example.com" {
		t.Fatalf("public https url = %q", info.URL)
	}

	stored, _ := s.store.Settings(context.Background())
	if stored[settingPanelDomain] != "panel.example.com" || stored[settingACMEEmail] != "me@example.com" {
		t.Fatalf("stored = %v", stored)
	}
	events, _ := s.store.Events(context.Background(), store.EventFilter{Limit: 5, Category: store.CategoryChange})
	if len(events) == 0 || events[0].Kind != "settings.domain_changed" || events[0].Detail != "panel.example.com" {
		t.Fatalf("events = %+v", events)
	}

	rec = do(t, s, "PUT", "/api/settings/domain", map[string]string{"domain": ""}, cookie)
	decode(t, rec, &st)
	if st.State != tlscert.StateOff {
		t.Fatalf("removing the domain: %+v", st)
	}
	info = httpsInfo{}
	decode(t, do(t, s, "GET", "/api/https", nil), &info)
	if info.URL != "" {
		t.Fatal("no https url without a domain")
	}
}

func TestSetDomainRejects(t *testing.T) {
	s := newTestServer(t)
	cookie := loggedIn(t, s)

	for name, tc := range map[string]struct {
		body  map[string]string
		field string
	}{
		"ip":           {map[string]string{"domain": "203.0.113.10"}, "domain"},
		"url":          {map[string]string{"domain": "https://panel.example.com"}, "domain"},
		"port":         {map[string]string{"domain": "panel.example.com:443"}, "domain"},
		"bad email":    {map[string]string{"domain": "panel.example.com", "email": "nope"}, "email"},
		"unresolvable": {map[string]string{"domain": "panel.example.invalid"}, "domain"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, s, "PUT", "/api/settings/domain", tc.body, cookie)
			var e apiError
			decode(t, rec, &e)
			if rec.Code != http.StatusUnprocessableEntity || e.Error.Fields[tc.field] == "" {
				t.Fatalf("want a %s field error, got %d: %s", tc.field, rec.Code, rec.Body)
			}
		})
	}
	if fakeCerts(s).obtains != 0 {
		t.Fatal("a rejected domain must not reach Let's Encrypt")
	}

	fakeCerts(s).status.Enabled = false
	rec := do(t, s, "PUT", "/api/settings/domain", map[string]string{"domain": "panel.example.com"}, cookie)
	if rec.Code != http.StatusConflict {
		t.Fatalf("https off: want 409, got %d", rec.Code)
	}
}

func TestDomainFailureAndRetry(t *testing.T) {
	s := newTestServer(t)
	cookie := loggedIn(t, s)
	fakeCerts(s).fail = "connection refused on port 80"

	var st tlscert.Status
	decode(t, do(t, s, "PUT", "/api/settings/domain", map[string]string{"domain": "panel.example.com"}, cookie), &st)
	if st.State != tlscert.StateFailed || st.Error == "" {
		t.Fatalf("want the failure reported, got %+v", st)
	}

	fakeCerts(s).fail = ""
	rec := do(t, s, "POST", "/api/settings/domain/retry", nil, cookie)
	decode(t, rec, &st)
	if rec.Code != http.StatusOK || st.State != tlscert.StateReady {
		t.Fatalf("retry: %d %+v", rec.Code, st)
	}
}

func TestRestoreDomain(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	if err := s.store.SaveSettings(ctx, map[string]string{settingPanelDomain: "panel.example.com", settingACMEEmail: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreDomain(ctx); err != nil {
		t.Fatal(err)
	}
	if st := fakeCerts(s).Status(); st.Domain != "panel.example.com" || st.Email != "me@example.com" {
		t.Fatalf("restored %+v", st)
	}
}
