package node

import (
	"context"
	"errors"
	"testing"

	"github.com/kwa0x2/tunploy/internal/event"
	"github.com/kwa0x2/tunploy/internal/store"
)

func TestOnlyAnOnlineNodeIsReportedDown(t *testing.T) {
	var events []store.Event
	p := NewPool(nil, event.Func(func(_ context.Context, e store.Event) { events = append(events, e) }))
	c := &conn{pool: p, node: store.Node{ID: 1, Name: "FRA-1"}, status: Status{State: StateConnecting}}

	c.setOffline(errors.New("no route to host"))
	if len(events) != 0 {
		t.Fatalf("a node that never came up is not news: %+v", events)
	}

	c.status.State = StateOnline
	c.setOffline(errors.New("connection refused"))
	c.setOffline(errors.New("connection refused"))
	if len(events) != 1 || events[0].Kind != "node.offline" || events[0].NodeName != "FRA-1" || events[0].Detail != "connection refused" {
		t.Fatalf("events = %+v", events)
	}
	if !c.reportedDown {
		t.Fatal("the node should come back as news")
	}
}
