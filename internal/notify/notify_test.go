package notify

import (
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kwa0x2/tunploy/internal/notify/notifytest"
	"github.com/kwa0x2/tunploy/internal/store"
)

func TestSend(t *testing.T) {
	srv := notifytest.New(t)
	c := SMTP{Host: srv.Host, Port: srv.Port, Security: SecurityNone, From: "alerts@example.com"}

	err := c.Send(context.Background(), Message{
		To:      []string{"me@example.com", "you@example.com"},
		Subject: "[Tunploy] Ayşe'nin telefonu connected",
		Body:    "line one\nline two with ünicode and a very long line " + strings.Repeat("x", 120) + "\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	mails := srv.Mails()
	if len(mails) != 1 || mails[0].From != "alerts@example.com" || len(mails[0].To) != 2 {
		t.Fatalf("mails = %+v", mails)
	}
	msg, err := mail.ReadMessage(strings.NewReader(mails[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "[Tunploy] Ayşe'nin telefonu connected" {
		t.Errorf("subject = %q", subject)
	}
	if got := msg.Header.Get("From"); got != `"Tunploy" <alerts@example.com>` {
		t.Errorf("from = %q", got)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if !strings.Contains(string(body), "line two with ünicode") || !strings.Contains(string(body), strings.Repeat("x", 120)) {
		t.Errorf("body = %q", body)
	}
}

func TestSendReportsServerErrors(t *testing.T) {
	srv := notifytest.New(t)
	srv.Reject = []string{"nobody@example.com"}
	c := SMTP{Host: srv.Host, Port: srv.Port, Security: SecurityNone, From: "alerts@example.com"}

	err := c.Send(context.Background(), Message{To: []string{"nobody@example.com"}, Subject: "x"})
	if err == nil || !strings.Contains(err.Error(), "no such user") {
		t.Fatalf("err = %v", err)
	}

	c.Security = SecurityStartTLS
	err = c.Send(context.Background(), Message{To: []string{"me@example.com"}, Subject: "x"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("a server without STARTTLS must not be used in the clear: %v", err)
	}
}

type outbox struct {
	mu   sync.Mutex
	sent []Message
}

func (o *outbox) send(_ context.Context, _ SMTP, m Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, m)
	return nil
}

func (o *outbox) messages() []Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Message(nil), o.sent...)
}

func TestNotifierBatchesWantedEvents(t *testing.T) {
	var box outbox
	n := New()
	n.window = 50 * time.Millisecond
	n.send = box.send
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	n.Notify(store.Event{Kind: "server.down", InstanceName: "Home"}) // not configured yet
	n.Configure(&Config{To: []string{"me@example.com"}, Groups: []string{"servers", "failed_logins"},
		PanelURL: "https://vpn.example.com"})
	n.Notify(store.Event{Kind: "auth.login_failed", IP: "203.0.113.9", Country: "DE", Detail: "wrong password"})
	n.Notify(store.Event{Kind: "device.connected", PeerName: "phone"}) // group not chosen
	n.Notify(store.Event{Kind: "server.down", InstanceName: "Home", Detail: "exited with code 1"})

	deadline := time.Now().Add(2 * time.Second)
	for len(box.messages()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	msgs := box.messages()
	if len(msgs) != 1 {
		t.Fatalf("want one batched email, got %d", len(msgs))
	}
	m := msgs[0]
	if m.Subject != "[Tunploy] 2 new events: Failed sign-in attempt on the panel" {
		t.Errorf("subject = %q", m.Subject)
	}
	for _, want := range []string{"203.0.113.9 (DE)", "Server Home is down", "exited with code 1",
		"https://vpn.example.com/activity"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("body missing %q:\n%s", want, m.Body)
		}
	}
	if strings.Contains(m.Body, "phone") {
		t.Errorf("an unchosen event was sent:\n%s", m.Body)
	}
	if st := n.Status(); st.LastSentAt == nil || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
}

func TestNotifierHourlyCap(t *testing.T) {
	var box outbox
	n := New()
	n.send = box.send
	n.Configure(&Config{To: []string{"me@example.com"}, Groups: []string{"servers"}})
	for range maxPerHour + 5 {
		n.deliver(context.Background(), []store.Event{{Kind: "server.down"}})
	}
	if got := len(box.messages()); got != maxPerHour {
		t.Fatalf("sent %d emails, want %d", got, maxPerHour)
	}
}

func TestEveryGroupedKindHasText(t *testing.T) {
	for _, g := range Groups {
		for _, k := range g.Kinds {
			if Describe(store.Event{Kind: k}) == k {
				t.Errorf("%s has no description", k)
			}
		}
	}
}
