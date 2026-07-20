package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/line/line-bot-sdk-go/v7/linebot"

	"line-webhook/internal/publisher"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func newWebhookRequest(t *testing.T, secret string, body []byte, badSignature bool) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if badSignature {
		req.Header.Set("X-Line-Signature", "not-valid-base64-signature")
	} else {
		req.Header.Set("X-Line-Signature", sign(secret, body))
	}
	rec := httptest.NewRecorder()
	e := echo.New()
	return e.NewContext(req, rec), rec
}

func TestWebhookInvalidSignature(t *testing.T) {
	cfg := &Config{ChannelSecret: "secret", AIPrefix: "/ai"}
	h := &LineHandler{cfg: cfg}
	body := []byte(`{"events":[]}`)

	c, rec := newWebhookRequest(t, "secret", body, true)
	err := h.Webhook(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("Webhook() error = %v (%T), want *echo.HTTPError", err, err)
	}
	if httpErr.Code != http.StatusUnauthorized {
		t.Errorf("Webhook() status = %d, want %d", httpErr.Code, http.StatusUnauthorized)
	}
	_ = rec
}

func TestWebhookMalformedBody(t *testing.T) {
	cfg := &Config{ChannelSecret: "secret", AIPrefix: "/ai"}
	h := &LineHandler{cfg: cfg}
	body := []byte(`not-json`)

	c, _ := newWebhookRequest(t, "secret", body, false)
	err := h.Webhook(c)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("Webhook() error = %v (%T), want *echo.HTTPError", err, err)
	}
	if httpErr.Code != http.StatusBadRequest {
		t.Errorf("Webhook() status = %d, want %d", httpErr.Code, http.StatusBadRequest)
	}
}

func TestWebhookHappyPath(t *testing.T) {
	cfg := &Config{ChannelSecret: "secret", AIPrefix: "/ai"}
	pub := &fakePublisher{}
	h := &LineHandler{cfg: cfg, pub: pub}

	body := []byte(`{"events":[{"type":"message","replyToken":"tok","timestamp":1690000000000,"source":{"type":"user","userId":"u1"},"message":{"type":"text","id":"m1","text":"random message"}}]}`)
	c, rec := newWebhookRequest(t, "secret", body, false)

	if err := h.Webhook(c); err != nil {
		t.Fatalf("Webhook() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Webhook() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(pub.replies) != 1 || pub.replies[0].Text != "You said: random message" {
		t.Errorf("expected echo reply, got %+v", pub.replies)
	}
}

func TestMarkAsReadGuards(t *testing.T) {
	h := &LineHandler{cfg: &Config{}}

	// No channel token configured: markAsRead must no-op (no HTTP attempted).
	if err := h.markAsRead([]byte(`{"events":[{}]}`), 0); err != nil {
		t.Errorf("markAsRead() with no token error = %v, want nil", err)
	}

	h2 := &LineHandler{cfg: &Config{ChannelToken: "tok"}}
	if err := h2.markAsRead(nil, 0); err != nil {
		t.Errorf("markAsRead() with empty body error = %v, want nil", err)
	}
	if err := h2.markAsRead([]byte(`{"events":[{}]}`), -1); err != nil {
		t.Errorf("markAsRead() with negative index error = %v, want nil", err)
	}

	// Nil cfg must also short-circuit.
	h3 := &LineHandler{}
	if err := h3.markAsRead([]byte(`{"events":[{}]}`), 0); err != nil {
		t.Errorf("markAsRead() with nil cfg error = %v, want nil", err)
	}

	// A body whose event has no markAsReadToken: token extraction succeeds
	// with an empty string, which is itself a no-op (LINE may not be
	// sending the token, per the doc comment).
	if err := h2.markAsRead([]byte(`{"events":[{"message":{}}]}`), 0); err != nil {
		t.Errorf("markAsRead() with empty token error = %v, want nil", err)
	}
}

func TestHandleEventDispatch(t *testing.T) {
	pub := &fakePublisher{}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub}

	// Unfollow just logs; must return nil without publishing anything.
	unfollow := &linebot.Event{
		Type:      linebot.EventTypeUnfollow,
		Source:    &linebot.EventSource{UserID: "u1"},
		Timestamp: time.Now(),
	}
	if err := h.handleEvent(unfollow); err != nil {
		t.Fatalf("handleEvent(unfollow) error = %v", err)
	}
	if len(pub.replies) != 0 || len(pub.aiRequests) != 0 {
		t.Errorf("unfollow should not publish anything, got replies=%+v aiRequests=%+v", pub.replies, pub.aiRequests)
	}

	// A follow event replies with the welcome message.
	follow := &linebot.Event{
		Type:       linebot.EventTypeFollow,
		Source:     &linebot.EventSource{UserID: "u1"},
		ReplyToken: "tok",
		Timestamp:  time.Now(),
	}
	if err := h.handleEvent(follow); err != nil {
		t.Fatalf("handleEvent(follow) error = %v", err)
	}
	if len(pub.replies) != 1 || !strings.Contains(pub.replies[0].Text, "Welcome") {
		t.Errorf("expected welcome reply, got %+v", pub.replies)
	}
}

// fakeProfileGate implements ProfileGate for ensureProfile tests. Release
// may run on ensureProfile's background goroutine concurrently with the
// test reading `released`, so both are guarded by a mutex.
type fakeProfileGate struct {
	claim bool

	mu       sync.Mutex
	released []string
}

func (f *fakeProfileGate) TryClaim(_ context.Context, _ string) bool { return f.claim }
func (f *fakeProfileGate) Release(_ context.Context, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, userID)
}

func (f *fakeProfileGate) releasedSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.released))
	copy(out, f.released)
	return out
}

func newFakeLineClient(t *testing.T, handler http.Handler) *linebot.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	bot, err := linebot.New("secret", "token", linebot.WithEndpointBase(srv.URL), linebot.WithEndpointBaseData(srv.URL))
	if err != nil {
		t.Fatalf("linebot.New() error = %v", err)
	}
	return bot
}

func TestEnsureProfilePublishesOnSuccess(t *testing.T) {
	bot := newFakeLineClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"displayName":"Alice","userId":"u1"}`))
	}))
	pub := &fakePublisher{}
	gate := &fakeProfileGate{claim: true}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, profiles: gate, bot: bot}

	h.ensureProfile("u1")
	// ensureProfile fetches in a background goroutine; wait for it to land.
	deadline := time.After(2 * time.Second)
	var profiles []publisher.ProfileEvent
	for len(profiles) == 0 {
		profiles = pub.profilesSnapshot()
		if len(profiles) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for profile publish")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if profiles[0].UserID != "u1" || profiles[0].DisplayName != "Alice" {
		t.Errorf("unexpected profile event: %+v", profiles[0])
	}
	if released := gate.releasedSnapshot(); len(released) != 0 {
		t.Errorf("gate should not be released on success, got %+v", released)
	}
}

func TestEnsureProfileReleasesGateOnFetchError(t *testing.T) {
	bot := newFakeLineClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	pub := &fakePublisher{}
	gate := &fakeProfileGate{claim: true}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, profiles: gate, bot: bot}

	h.ensureProfile("u1")
	deadline := time.After(2 * time.Second)
	var released []string
	for len(released) == 0 {
		released = gate.releasedSnapshot()
		if len(released) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for gate release")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if released[0] != "u1" {
		t.Errorf("released = %+v, want [u1]", released)
	}
	if profiles := pub.profilesSnapshot(); len(profiles) != 0 {
		t.Errorf("no profile should be published on fetch error, got %+v", profiles)
	}
}

func TestEnsureProfileSkipsWhenGateDenies(t *testing.T) {
	pub := &fakePublisher{}
	gate := &fakeProfileGate{claim: false}
	// bot/pub present but the gate denies the claim, so GetProfile must not
	// even be attempted - use a nil bot to prove it's never dereferenced.
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, profiles: gate, bot: nil}

	h.ensureProfile("u1")
	time.Sleep(20 * time.Millisecond)
	if profiles := pub.profilesSnapshot(); len(profiles) != 0 {
		t.Errorf("expected no profile publish, got %+v", profiles)
	}
}

func TestEnsureProfileNoopWithoutDependencies(t *testing.T) {
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}}
	// No profiles/bot/pub configured and no user ID: must not panic.
	h.ensureProfile("")
	h.ensureProfile("u1")
}

func TestHandleImageMessageRequiresActiveSession(t *testing.T) {
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{}, flows: map[string]bool{}}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, sessions: sessions}

	event := &linebot.Event{
		ReplyToken: "tok",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
	}
	if err := h.handleImageMessage(event, &linebot.ImageMessage{ID: "m1"}); err != nil {
		t.Fatalf("handleImageMessage() error = %v", err)
	}
	if len(pub.replies) != 1 || !strings.Contains(pub.replies[0].Text, "Start an AI session") {
		t.Fatalf("expected prompt to start a session, got %+v", pub.replies)
	}
}

func TestHandleImageMessageMissingDependencies(t *testing.T) {
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{"u1": true}, flows: map[string]bool{}}
	// Active session but no bot/images configured.
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, sessions: sessions}

	event := &linebot.Event{
		ReplyToken: "tok",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
	}
	if err := h.handleImageMessage(event, &linebot.ImageMessage{ID: "m1"}); err != nil {
		t.Fatalf("handleImageMessage() error = %v", err)
	}
	if len(pub.replies) != 1 || !strings.Contains(pub.replies[0].Text, "can't process images") {
		t.Fatalf("expected can't-process reply, got %+v", pub.replies)
	}
}

// fakeImageStore implements ImageStore for handleImageMessage tests.
type fakeImageStore struct {
	fail  bool
	saved map[string][]byte
}

func (f *fakeImageStore) Put(_ context.Context, messageID string, data []byte, _ time.Duration) error {
	if f.fail {
		return io.ErrClosedPipe
	}
	if f.saved == nil {
		f.saved = map[string][]byte{}
	}
	f.saved[messageID] = data
	return nil
}

func TestHandleImageMessageDownloadsAndForwards(t *testing.T) {
	imageBytes := []byte("fake-jpeg-bytes")
	bot := newFakeLineClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(imageBytes)
	}))
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{"u1": true}, flows: map[string]bool{}}
	images := &fakeImageStore{}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, sessions: sessions, images: images, bot: bot}

	event := &linebot.Event{
		ReplyToken: "tok",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
	}
	if err := h.handleImageMessage(event, &linebot.ImageMessage{ID: "m1"}); err != nil {
		t.Fatalf("handleImageMessage() error = %v", err)
	}
	if string(images.saved["m1"]) != string(imageBytes) {
		t.Errorf("stored image = %q, want %q", images.saved["m1"], imageBytes)
	}
	if len(pub.aiRequests) != 1 || pub.aiRequests[0].ImageKey != "m1" || pub.aiRequests[0].ImageMime != "image/jpeg" {
		t.Fatalf("expected AI request carrying the image, got %+v", pub.aiRequests)
	}
}

func TestHandleImageMessageTooLarge(t *testing.T) {
	bot := newFakeLineClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(make([]byte, 100))
	}))
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{"u1": true}, flows: map[string]bool{}}
	images := &fakeImageStore{}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai", MaxImageBytes: 10}, pub: pub, sessions: sessions, images: images, bot: bot}

	event := &linebot.Event{
		ReplyToken: "tok",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
	}
	if err := h.handleImageMessage(event, &linebot.ImageMessage{ID: "m1"}); err != nil {
		t.Fatalf("handleImageMessage() error = %v", err)
	}
	if len(pub.replies) != 1 || !strings.Contains(pub.replies[0].Text, "too large") {
		t.Fatalf("expected too-large reply, got %+v", pub.replies)
	}
	if len(pub.aiRequests) != 0 {
		t.Errorf("oversized image must not be forwarded, got %+v", pub.aiRequests)
	}
}

func TestHandleImageMessageStoreFailure(t *testing.T) {
	bot := newFakeLineClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("bytes"))
	}))
	pub := &fakePublisher{}
	sessions := &fakeSessions{active: map[string]bool{"u1": true}, flows: map[string]bool{}}
	images := &fakeImageStore{fail: true}
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: pub, sessions: sessions, images: images, bot: bot}

	event := &linebot.Event{
		ReplyToken: "tok",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
	}
	if err := h.handleImageMessage(event, &linebot.ImageMessage{ID: "m1"}); err != nil {
		t.Fatalf("handleImageMessage() error = %v", err)
	}
	if len(pub.replies) != 1 || !strings.Contains(pub.replies[0].Text, "couldn't process") {
		t.Fatalf("expected stash-failure reply, got %+v", pub.replies)
	}
}

func TestReplyWithoutPublisherIsNoop(t *testing.T) {
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}}
	event := &linebot.Event{ReplyToken: "tok", Source: &linebot.EventSource{UserID: "u1"}}
	if err := h.reply(event, "hi"); err != nil {
		t.Fatalf("reply() with nil publisher error = %v, want nil", err)
	}
}

func TestPublishAIRequestWithoutPublisherIsNoop(t *testing.T) {
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}}
	event := &linebot.Event{ReplyToken: "tok", Source: &linebot.EventSource{UserID: "u1"}, Timestamp: time.Now()}
	if err := h.publishAIRequest(event, "hi"); err != nil {
		t.Fatalf("publishAIRequest() with nil publisher error = %v, want nil", err)
	}
}

func TestHandlePostbackWithoutPublisherIsNoop(t *testing.T) {
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}}
	event := &linebot.Event{
		ReplyToken: "tok",
		Timestamp:  time.Now(),
		Source:     &linebot.EventSource{UserID: "u1"},
		Postback:   &linebot.Postback{Data: "flow=rem"},
	}
	if err := h.handlePostbackEvent(event); err != nil {
		t.Fatalf("handlePostbackEvent() with nil publisher error = %v, want nil", err)
	}
}

// failingPublisher returns errors from every publish method, to exercise
// the error-logging branches of reply/publishAIRequest/handlePostbackEvent.
type failingPublisher struct{}

func (failingPublisher) PublishAIRequest(publisher.AIRequestEvent) error { return io.ErrClosedPipe }
func (failingPublisher) PublishReply(publisher.ReplyEvent) error         { return io.ErrClosedPipe }
func (failingPublisher) PublishPostback(publisher.PostbackEvent) error   { return io.ErrClosedPipe }
func (failingPublisher) PublishProfile(publisher.ProfileEvent) error     { return io.ErrClosedPipe }

func TestPublishFailuresPropagate(t *testing.T) {
	h := &LineHandler{cfg: &Config{AIPrefix: "/ai"}, pub: failingPublisher{}}
	event := &linebot.Event{ReplyToken: "tok", Timestamp: time.Now(), Source: &linebot.EventSource{UserID: "u1"}}

	if err := h.reply(event, "hi"); err == nil {
		t.Error("reply() with failing publisher error = nil, want non-nil")
	}
	// publishAIRequest swallows the publish error and instead tries to send
	// an "unavailable" reply, which itself fails and surfaces that error.
	if err := h.publishAIRequest(event, "hi"); err == nil {
		t.Error("publishAIRequest() with failing publisher error = nil, want non-nil")
	}
	event.Postback = &linebot.Postback{Data: "flow=rem"}
	if err := h.handlePostbackEvent(event); err == nil {
		t.Error("handlePostbackEvent() with failing publisher error = nil, want non-nil")
	}
}
