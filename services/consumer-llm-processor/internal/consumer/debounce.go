package consumer

import (
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Debouncer buffers a user's rapid-fire chat messages and flushes them as
// one merged request after a quiet gap, the way people actually type in
// chat: "hey" / "quick question" / "how do I ...". Without it every
// fragment fires its own LLM call and produces overlapping replies.
//
// State is in-memory, which assumes a single consumer replica (replicas: 1
// today). More replicas would need per-user affinity or shared state.
type Debouncer struct {
	mu      sync.Mutex
	pending map[string]*pendingRequest
	window  time.Duration // quiet gap that triggers a flush
	maxWait time.Duration // cap so a nonstop typist still gets an answer
	flush   func(RequestEvent)
	wg      sync.WaitGroup // in-flight flush callbacks
}

type pendingRequest struct {
	texts     []string
	latest    RequestEvent // most recent event: freshest reply token wins
	imageKey  string
	imageMime string
	firstAt   time.Time
	timer     *time.Timer
}

// NewDebouncer creates a debouncer that calls flush with the merged event.
// window is the quiet gap; maxWait caps total buffering time per burst.
func NewDebouncer(window, maxWait time.Duration, flush func(RequestEvent)) *Debouncer {
	return &Debouncer{
		pending: make(map[string]*pendingRequest),
		window:  window,
		maxWait: maxWait,
		flush:   flush,
	}
}

// Add buffers one event and (re)arms the user's flush timer. Control
// commands (e.g. "/reset") skip the buffer entirely: they run immediately,
// after first answering whatever chat fragments were already waiting for
// that user - in order, on the same goroutine, so e.g. a reset can't race
// ahead of storing the prior burst's answer and then have it linger in the
// "cleared" history - since a command shouldn't sit out the debounce window
// like an ordinary message.
func (d *Debouncer) Add(event RequestEvent) {
	if isResetCommand(event.Text) {
		d.mu.Lock()
		p, hadBurst := d.pending[event.UserID]
		if hadBurst {
			delete(d.pending, event.UserID)
			if p.timer != nil {
				p.timer.Stop()
			}
		}
		d.mu.Unlock()

		log.Info().Str("user_id", event.UserID).Msg("debounce: command - bypassing debounce window")
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			if hadBurst {
				merged := mergeEvent(p)
				if len(p.texts) > 1 {
					log.Info().Str("user_id", event.UserID).Int("messages", len(p.texts)).Msg("debounce: merged burst into one request")
				}
				d.flush(merged)
			}
			d.flush(event)
		}()
		return
	}

	d.mu.Lock()

	p, ok := d.pending[event.UserID]
	// A second image in the same burst would overwrite the first (only one
	// image rides per request), so flush what's buffered and start fresh.
	if ok && p.imageKey != "" && event.ImageKey != "" {
		d.flushLocked(event.UserID)
		p, ok = nil, false
	}
	if !ok {
		p = &pendingRequest{firstAt: time.Now()}
		d.pending[event.UserID] = p
	}

	if text := strings.TrimSpace(event.Text); text != "" {
		p.texts = append(p.texts, text)
	}
	if event.ImageKey != "" {
		p.imageKey = event.ImageKey
		p.imageMime = event.ImageMime
	}
	p.latest = event

	if p.timer != nil {
		p.timer.Stop()
	}
	if time.Since(p.firstAt) >= d.maxWait {
		log.Info().Str("user_id", event.UserID).Int("messages", len(p.texts)).Msg("debounce: max wait reached - flushing")
		d.flushLocked(event.UserID)
		d.mu.Unlock()
		return
	}
	userID := event.UserID
	p.timer = time.AfterFunc(d.window, func() {
		d.mu.Lock()
		d.flushLocked(userID)
		d.mu.Unlock()
	})
	d.mu.Unlock()
}

// FlushAll flushes every pending buffer and waits for the dispatched
// handlers to finish, so shutdown doesn't drop buffered messages or close
// NATS before their replies publish.
func (d *Debouncer) FlushAll() {
	d.mu.Lock()
	for userID := range d.pending {
		d.flushLocked(userID)
	}
	d.mu.Unlock()
	d.wg.Wait()
}

// flushLocked merges and dispatches the user's buffer. Caller holds d.mu;
// the flush callback itself runs on a fresh goroutine so a slow LLM call
// never blocks buffering for other users.
func (d *Debouncer) flushLocked(userID string) {
	p, ok := d.pending[userID]
	if !ok {
		return
	}
	delete(d.pending, userID)
	if p.timer != nil {
		p.timer.Stop()
	}

	merged := mergeEvent(p)
	if len(p.texts) > 1 {
		log.Info().Str("user_id", userID).Int("messages", len(p.texts)).Msg("debounce: merged burst into one request")
	}
	d.dispatch(merged)
}

// mergeEvent folds a buffered burst into the single event that gets sent
// for it: all fragment texts joined in order, latest reply token/image.
func mergeEvent(p *pendingRequest) RequestEvent {
	merged := p.latest
	merged.Text = strings.Join(p.texts, "\n")
	merged.ImageKey = p.imageKey
	merged.ImageMime = p.imageMime
	return merged
}

// dispatch runs the flush callback on its own goroutine, tracked so
// FlushAll can wait for it, so a slow LLM call never blocks buffering for
// other users.
func (d *Debouncer) dispatch(event RequestEvent) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.flush(event)
	}()
}
