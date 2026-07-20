package consumer

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"

	"consumer-reminder/internal/events"
)

// fakeFlow records the calls the consumer routes to it, standing in for
// *flow.Flow so the message handlers can be tested without Redis/Postgres.
type fakeFlow struct {
	requests  []events.ReminderRequestEvent
	postbacks []events.PostbackEvent
}

func (f *fakeFlow) HandleRequest(_ context.Context, ev events.ReminderRequestEvent) {
	f.requests = append(f.requests, ev)
}

func (f *fakeFlow) HandlePostback(_ context.Context, ev events.PostbackEvent) {
	f.postbacks = append(f.postbacks, ev)
}

type fakeUserStore struct {
	upserts []events.ProfileEvent
	err     error
}

func (f *fakeUserStore) UpsertUser(_ context.Context, userID, displayName string) error {
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, events.ProfileEvent{UserID: userID, DisplayName: displayName})
	return nil
}

func newTestConsumer() (*Consumer, *fakeFlow, *fakeUserStore) {
	fl := &fakeFlow{}
	us := &fakeUserStore{}
	return &Consumer{flow: fl, users: us}, fl, us
}

func TestHandleReminderRequestMsg(t *testing.T) {
	t.Run("valid message routes to flow", func(t *testing.T) {
		c, fl, _ := newTestConsumer()
		c.handleReminderRequestMsg(&nats.Msg{Data: []byte(`{"user_id":"u1","reply_token":"tok","text":"/reminder"}`)})
		if len(fl.requests) != 1 || fl.requests[0].UserID != "u1" || fl.requests[0].Text != "/reminder" {
			t.Fatalf("requests = %+v", fl.requests)
		}
	})

	t.Run("bad json is dropped", func(t *testing.T) {
		c, fl, _ := newTestConsumer()
		c.handleReminderRequestMsg(&nats.Msg{Data: []byte(`not json`)})
		if len(fl.requests) != 0 {
			t.Fatalf("expected no requests routed, got %+v", fl.requests)
		}
	})

	t.Run("missing user_id is dropped", func(t *testing.T) {
		c, fl, _ := newTestConsumer()
		c.handleReminderRequestMsg(&nats.Msg{Data: []byte(`{"text":"/reminder"}`)})
		if len(fl.requests) != 0 {
			t.Fatalf("expected no requests routed, got %+v", fl.requests)
		}
	})
}

func TestHandlePostbackMsg(t *testing.T) {
	t.Run("valid postback routes to flow", func(t *testing.T) {
		c, fl, _ := newTestConsumer()
		c.handlePostbackMsg(&nats.Msg{Data: []byte(`{"user_id":"u1","reply_token":"tok","data":"flow=rem&a=confirm"}`)})
		if len(fl.postbacks) != 1 || fl.postbacks[0].UserID != "u1" || fl.postbacks[0].Data != "flow=rem&a=confirm" {
			t.Fatalf("postbacks = %+v", fl.postbacks)
		}
	})

	t.Run("bad json is dropped", func(t *testing.T) {
		c, fl, _ := newTestConsumer()
		c.handlePostbackMsg(&nats.Msg{Data: []byte(`{`)})
		if len(fl.postbacks) != 0 {
			t.Fatalf("expected no postbacks routed, got %+v", fl.postbacks)
		}
	})

	t.Run("missing user_id is dropped", func(t *testing.T) {
		c, fl, _ := newTestConsumer()
		c.handlePostbackMsg(&nats.Msg{Data: []byte(`{"data":"flow=rem&a=confirm"}`)})
		if len(fl.postbacks) != 0 {
			t.Fatalf("expected no postbacks routed, got %+v", fl.postbacks)
		}
	})
}

func TestHandleProfileMsg(t *testing.T) {
	t.Run("valid profile upserts", func(t *testing.T) {
		c, _, us := newTestConsumer()
		c.handleProfileMsg(&nats.Msg{Data: []byte(`{"user_id":"u1","display_name":"Meow"}`)})
		if len(us.upserts) != 1 || us.upserts[0].UserID != "u1" || us.upserts[0].DisplayName != "Meow" {
			t.Fatalf("upserts = %+v", us.upserts)
		}
	})

	t.Run("bad json is dropped", func(t *testing.T) {
		c, _, us := newTestConsumer()
		c.handleProfileMsg(&nats.Msg{Data: []byte(`not json`)})
		if len(us.upserts) != 0 {
			t.Fatalf("expected no upserts, got %+v", us.upserts)
		}
	})

	t.Run("missing user_id is dropped", func(t *testing.T) {
		c, _, us := newTestConsumer()
		c.handleProfileMsg(&nats.Msg{Data: []byte(`{"display_name":"Meow"}`)})
		if len(us.upserts) != 0 {
			t.Fatalf("expected no upserts, got %+v", us.upserts)
		}
	})

	t.Run("missing display_name is dropped", func(t *testing.T) {
		c, _, us := newTestConsumer()
		c.handleProfileMsg(&nats.Msg{Data: []byte(`{"user_id":"u1"}`)})
		if len(us.upserts) != 0 {
			t.Fatalf("expected no upserts, got %+v", us.upserts)
		}
	})

	t.Run("store error does not panic", func(t *testing.T) {
		c, _, us := newTestConsumer()
		us.err = context.DeadlineExceeded
		c.handleProfileMsg(&nats.Msg{Data: []byte(`{"user_id":"u1","display_name":"Meow"}`)})
		if len(us.upserts) != 0 {
			t.Fatalf("expected no upserts recorded on error, got %+v", us.upserts)
		}
	})
}

func TestNewWrapsFlow(t *testing.T) {
	// New must accept a *flow.Flow and store it behind the Flow interface;
	// this just guards the constructor signature/wiring, not flow behavior.
	if c := New(nil, &fakeUserStore{}); c == nil {
		t.Fatal("New returned nil")
	}
}
