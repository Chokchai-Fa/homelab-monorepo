package consumer

import (
	"sync"
	"testing"
	"time"
)

type flushRecorder struct {
	mu     sync.Mutex
	events []RequestEvent
}

func (f *flushRecorder) flush(e RequestEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *flushRecorder) get() []RequestEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RequestEvent(nil), f.events...)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestDebouncerMergesBurst(t *testing.T) {
	rec := &flushRecorder{}
	d := NewDebouncer(30*time.Millisecond, time.Second, rec.flush)

	d.Add(RequestEvent{UserID: "u1", ReplyToken: "t1", Text: "hey"})
	d.Add(RequestEvent{UserID: "u1", ReplyToken: "t2", Text: "quick question"})
	d.Add(RequestEvent{UserID: "u1", ReplyToken: "t3", Text: "how do I deploy?"})

	waitFor(t, func() bool { return len(rec.get()) == 1 })
	got := rec.get()[0]
	if got.Text != "hey\nquick question\nhow do I deploy?" {
		t.Errorf("merged text = %q", got.Text)
	}
	if got.ReplyToken != "t3" {
		t.Errorf("reply token = %q, want latest t3", got.ReplyToken)
	}
}

func TestDebouncerSeparatesUsers(t *testing.T) {
	rec := &flushRecorder{}
	d := NewDebouncer(20*time.Millisecond, time.Second, rec.flush)

	d.Add(RequestEvent{UserID: "u1", Text: "from u1"})
	d.Add(RequestEvent{UserID: "u2", Text: "from u2"})

	waitFor(t, func() bool { return len(rec.get()) == 2 })
	texts := map[string]string{}
	for _, e := range rec.get() {
		texts[e.UserID] = e.Text
	}
	if texts["u1"] != "from u1" || texts["u2"] != "from u2" {
		t.Errorf("per-user texts wrong: %v", texts)
	}
}

func TestDebouncerMaxWaitFlushesNonstopTypist(t *testing.T) {
	rec := &flushRecorder{}
	// window longer than the typing gap so only maxWait can trigger.
	d := NewDebouncer(50*time.Millisecond, 120*time.Millisecond, rec.flush)

	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) && len(rec.get()) == 0 {
		d.Add(RequestEvent{UserID: "u1", Text: "still typing"})
		time.Sleep(20 * time.Millisecond)
	}
	if len(rec.get()) == 0 {
		t.Fatal("maxWait never flushed while user kept typing")
	}
}

func TestDebouncerMergesImageWithCaption(t *testing.T) {
	rec := &flushRecorder{}
	d := NewDebouncer(30*time.Millisecond, time.Second, rec.flush)

	d.Add(RequestEvent{UserID: "u1", ImageKey: "img-1", ImageMime: "image/jpeg"})
	d.Add(RequestEvent{UserID: "u1", Text: "what plant is this?"})

	waitFor(t, func() bool { return len(rec.get()) == 1 })
	got := rec.get()[0]
	if got.ImageKey != "img-1" || got.Text != "what plant is this?" {
		t.Errorf("merged event = %+v", got)
	}
}

func TestDebouncerSecondImageFlushesFirst(t *testing.T) {
	rec := &flushRecorder{}
	d := NewDebouncer(50*time.Millisecond, time.Second, rec.flush)

	d.Add(RequestEvent{UserID: "u1", ImageKey: "img-1", ImageMime: "image/jpeg"})
	d.Add(RequestEvent{UserID: "u1", ImageKey: "img-2", ImageMime: "image/png"})

	waitFor(t, func() bool { return len(rec.get()) == 2 })
	keys := []string{rec.get()[0].ImageKey, rec.get()[1].ImageKey}
	if keys[0] != "img-1" || keys[1] != "img-2" {
		t.Errorf("image flush order = %v", keys)
	}
}

func TestDebouncerBypassesWindowForResetCommand(t *testing.T) {
	rec := &flushRecorder{}
	// window long enough that a normal message would never flush in time.
	d := NewDebouncer(10*time.Second, time.Minute, rec.flush)

	d.Add(RequestEvent{UserID: "u1", Text: "/reset"})

	waitFor(t, func() bool { return len(rec.get()) == 1 })
	if got := rec.get()[0]; got.Text != "/reset" {
		t.Errorf("dispatched event = %+v", got)
	}
}

func TestDebouncerResetFlushesPriorBurstFirst(t *testing.T) {
	rec := &flushRecorder{}
	d := NewDebouncer(10*time.Second, time.Minute, rec.flush)

	d.Add(RequestEvent{UserID: "u1", Text: "hey"})
	d.Add(RequestEvent{UserID: "u1", Text: "quick question"})
	d.Add(RequestEvent{UserID: "u1", Text: "/reset"})

	waitFor(t, func() bool { return len(rec.get()) == 2 })
	got := rec.get()
	if got[0].Text != "hey\nquick question" {
		t.Errorf("first flush = %q, want prior burst merged", got[0].Text)
	}
	if got[1].Text != "/reset" {
		t.Errorf("second flush = %q, want /reset", got[1].Text)
	}
}

func TestDebouncerFlushAllWaits(t *testing.T) {
	rec := &flushRecorder{}
	d := NewDebouncer(10*time.Second, time.Minute, rec.flush) // never fires on its own

	d.Add(RequestEvent{UserID: "u1", Text: "buffered"})
	d.FlushAll()

	if got := rec.get(); len(got) != 1 || got[0].Text != "buffered" {
		t.Errorf("FlushAll result = %+v", got)
	}
}
