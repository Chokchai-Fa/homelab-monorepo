package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nats-io/nats.go"
)

// fakeBackend is a hand-rolled Backend fake covering unary + streaming.
type fakeBackend struct {
	response     []byte
	err          error
	calls        int
	lastData     []byte
	connected    bool
	streamFrames [][]byte
	streamErr    error
}

func (f *fakeBackend) Request(_ string, data []byte, _ time.Duration) (*nats.Msg, error) {
	f.calls++
	f.lastData = data
	if f.err != nil {
		return nil, f.err
	}
	return &nats.Msg{Data: f.response}, nil
}

func (f *fakeBackend) Connected() bool { return f.connected }

func (f *fakeBackend) OpenStream(_ string, data []byte) (StreamSub, error) {
	f.calls++
	f.lastData = data
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	return &fakeStreamSub{frames: f.streamFrames}, nil
}

// fakeStreamSub replays queued frames, then reports a timeout (as a real
// subscription would when the consumer sends nothing more).
type fakeStreamSub struct {
	frames [][]byte
	idx    int
	closed bool
}

func (s *fakeStreamSub) Next(_ time.Duration) ([]byte, error) {
	if s.idx >= len(s.frames) {
		return nil, nats.ErrTimeout
	}
	f := s.frames[s.idx]
	s.idx++
	return f, nil
}

func (s *fakeStreamSub) Close() error { s.closed = true; return nil }

func doChat(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := h.Chat(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	return rec
}

const validSession = "3f2b8a70-1234-4cde-9f00-abcdef012345"

func TestChatSuccess(t *testing.T) {
	f := &fakeBackend{response: []byte(`{"text":"he works at LINE"}`)}
	h := New(f, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"where does he work?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Text != "he works at LINE" {
		t.Fatalf("unexpected text %q", resp.Text)
	}
	var sent map[string]any
	if err := json.Unmarshal(f.lastData, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["session_id"] != validSession || sent["message"] != "where does he work?" {
		t.Fatalf("unexpected payload %s", f.lastData)
	}
}

func TestChatRejectsBadSessionID(t *testing.T) {
	f := &fakeBackend{}
	h := New(f, time.Second, 1000)
	for _, id := range []string{"", "short", "has space in it padding", "web:injected-prefix-x"} {
		rec := doChat(t, h, `{"session_id":"`+id+`","message":"hi"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("session %q: expected 400, got %d", id, rec.Code)
		}
	}
	if f.calls != 0 {
		t.Fatal("NATS must not be called for invalid sessions")
	}
}

func TestChatRejectsEmptyAndOversizeMessage(t *testing.T) {
	f := &fakeBackend{}
	h := New(f, time.Second, 10)
	if rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"   "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty: expected 400, got %d", rec.Code)
	}
	if rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"`+strings.Repeat("x", 11)+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("oversize: expected 400, got %d", rec.Code)
	}
	if f.calls != 0 {
		t.Fatal("NATS must not be called for invalid messages")
	}
}

func TestChatNilRequesterReturns503(t *testing.T) {
	h := New(nil, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestChatTimeoutReturns504(t *testing.T) {
	h := New(&fakeBackend{err: nats.ErrTimeout}, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", rec.Code)
	}
}

func TestChatNatsErrorReturns503(t *testing.T) {
	h := New(&fakeBackend{err: errors.New("no responders")}, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestChatUpstreamErrorReturns400(t *testing.T) {
	h := New(&fakeBackend{response: []byte(`{"error":"message too long"}`)}, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestChatMalformedUpstreamReturns502(t *testing.T) {
	h := New(&fakeBackend{response: []byte("not json")}, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	if err := New(nil, time.Second, 1000).Healthz(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func doStream(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/chat/stream", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := h.ChatStream(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestChatStreamForwardsFramesUntilDone(t *testing.T) {
	f := &fakeBackend{streamFrames: [][]byte{
		[]byte(`{"delta":"He "}`),
		[]byte(`{"delta":"works "}`),
		[]byte(`{"delta":"at LINE."}`),
		[]byte(`{"done":true}`),
	}}
	h := New(f, time.Second, 1000)
	rec := doStream(t, h, `{"session_id":"`+validSession+`","message":"where?"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected SSE content-type, got %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"data: {\"delta\":\"He \"}", "data: {\"delta\":\"at LINE.\"}", "data: {\"done\":true}"} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q; got:\n%s", want, body)
		}
	}
	// The forwarded payload carried the real message.
	var sent map[string]any
	if err := json.Unmarshal(f.lastData, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["message"] != "where?" {
		t.Fatalf("unexpected payload %s", f.lastData)
	}
}

func TestChatStreamRejectsBadSessionBeforeStreaming(t *testing.T) {
	f := &fakeBackend{}
	h := New(f, time.Second, 1000)
	rec := doStream(t, h, `{"session_id":"x","message":"hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if f.calls != 0 {
		t.Fatal("must not open a stream for an invalid session")
	}
}

func TestChatStreamNilBackendReturns503(t *testing.T) {
	h := New(nil, time.Second, 1000)
	rec := doStream(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestStatusReportsNatsState(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	if err := New(&fakeBackend{connected: true}, time.Second, 1000).Status(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" || !resp.NATS {
		t.Fatalf("expected ok + nats up, got %+v", resp)
	}

	// nil backend → NATS reported down, but the endpoint still answers 200.
	rec2 := httptest.NewRecorder()
	if err := New(nil, time.Second, 1000).Status(e.NewContext(httptest.NewRequest(http.MethodGet, "/status", nil), rec2)); err != nil {
		t.Fatal(err)
	}
	var resp2 StatusResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if resp2.NATS {
		t.Fatal("expected nats down for nil backend")
	}
}
