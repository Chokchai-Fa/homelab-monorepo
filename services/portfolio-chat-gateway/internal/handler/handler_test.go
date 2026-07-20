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

// fakeRequester is a hand-rolled Requester fake.
type fakeRequester struct {
	response []byte
	err      error
	calls    int
	lastData []byte
}

func (f *fakeRequester) Request(_ string, data []byte, _ time.Duration) (*nats.Msg, error) {
	f.calls++
	f.lastData = data
	if f.err != nil {
		return nil, f.err
	}
	return &nats.Msg{Data: f.response}, nil
}

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
	f := &fakeRequester{response: []byte(`{"text":"he works at LINE"}`)}
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
	f := &fakeRequester{}
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
	f := &fakeRequester{}
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
	h := New(&fakeRequester{err: nats.ErrTimeout}, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", rec.Code)
	}
}

func TestChatNatsErrorReturns503(t *testing.T) {
	h := New(&fakeRequester{err: errors.New("no responders")}, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestChatUpstreamErrorReturns400(t *testing.T) {
	h := New(&fakeRequester{response: []byte(`{"error":"message too long"}`)}, time.Second, 1000)
	rec := doChat(t, h, `{"session_id":"`+validSession+`","message":"hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestChatMalformedUpstreamReturns502(t *testing.T) {
	h := New(&fakeRequester{response: []byte("not json")}, time.Second, 1000)
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
