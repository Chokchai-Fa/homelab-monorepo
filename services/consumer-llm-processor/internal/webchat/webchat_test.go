package webchat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"consumer-llm-processor/internal/ai"
	"consumer-llm-processor/internal/store"
)

// fakeHistoryStore is a hand-rolled store.Store fake, tracking calls so
// respond()'s side effects can be asserted.
type fakeHistoryStore struct {
	history    []store.Message
	getErr     error
	clearErr   error
	appendErr  error
	getCalls   int
	clearCalls int
	appended   []store.Message
	lastUserID string
}

func (f *fakeHistoryStore) GetRecent(_ context.Context, userID string) ([]store.Message, error) {
	f.getCalls++
	f.lastUserID = userID
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.history, nil
}

func (f *fakeHistoryStore) Append(_ context.Context, userID, role, content string) error {
	f.lastUserID = userID
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appended = append(f.appended, store.Message{Role: role, Content: content})
	return nil
}

func (f *fakeHistoryStore) Clear(_ context.Context, userID string) error {
	f.clearCalls++
	f.lastUserID = userID
	return f.clearErr
}

func (f *fakeHistoryStore) Close() {}

// fakeResponder is a hand-rolled Responder fake.
type fakeResponder struct {
	result      ai.Result
	err         error
	calls       int
	lastMessage string
	lastHistory []store.Message
}

func (f *fakeResponder) Route(_ context.Context, history []store.Message, userMessage string, _ *ai.Image) (ai.Result, error) {
	f.calls++
	f.lastMessage = userMessage
	f.lastHistory = history
	return f.result, f.err
}

func request(t *testing.T, sessionID, message string) []byte {
	t.Helper()
	data, err := json.Marshal(Request{SessionID: sessionID, Message: message})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRespondInvalidJSONReturnsError(t *testing.T) {
	c := New(&fakeHistoryStore{}, &fakeResponder{}, nil)
	resp := c.respond(context.Background(), []byte("{not json"))
	if resp.Error == "" {
		t.Fatalf("expected error, got %+v", resp)
	}
}

func TestRespondMissingSessionIDReturnsError(t *testing.T) {
	c := New(&fakeHistoryStore{}, &fakeResponder{}, nil)
	resp := c.respond(context.Background(), request(t, "  ", "hello"))
	if resp.Error != "missing session_id" {
		t.Fatalf("expected missing session_id error, got %+v", resp)
	}
}

func TestRespondEmptyMessageReturnsUsageHint(t *testing.T) {
	r := &fakeResponder{}
	c := New(&fakeHistoryStore{}, r, nil)
	resp := c.respond(context.Background(), request(t, "s1", "   "))
	if resp.Text != usageHint {
		t.Fatalf("expected usage hint, got %+v", resp)
	}
	if r.calls != 0 {
		t.Fatal("LLM must not be called for empty messages")
	}
}

func TestRespondOversizeMessageReturnsError(t *testing.T) {
	r := &fakeResponder{}
	c := New(&fakeHistoryStore{}, r, nil)
	resp := c.respond(context.Background(), request(t, "s1", strings.Repeat("ก", maxMessageChars+1)))
	if resp.Error != "message too long" {
		t.Fatalf("expected size error, got %+v", resp)
	}
	if r.calls != 0 {
		t.Fatal("LLM must not be called for oversize messages")
	}
}

func TestRespondResetClearsHistory(t *testing.T) {
	s := &fakeHistoryStore{}
	c := New(s, &fakeResponder{}, nil)
	resp := c.respond(context.Background(), request(t, "s1", "/reset"))
	if resp.Text != resetDone {
		t.Fatalf("expected reset confirmation, got %+v", resp)
	}
	if s.clearCalls != 1 {
		t.Fatalf("expected 1 clear call, got %d", s.clearCalls)
	}
	if s.lastUserID != "web:s1" {
		t.Fatalf("expected web: prefixed user id, got %q", s.lastUserID)
	}
}

func TestRespondResetClearErrorFallsBack(t *testing.T) {
	s := &fakeHistoryStore{clearErr: errors.New("boom")}
	c := New(s, &fakeResponder{}, nil)
	resp := c.respond(context.Background(), request(t, "s1", "/reset"))
	if resp.Text == resetDone || resp.Text == "" {
		t.Fatalf("expected failure text, got %+v", resp)
	}
}

func TestRespondNormalTextAnswersAndStoresBothTurns(t *testing.T) {
	s := &fakeHistoryStore{history: []store.Message{{Role: store.RoleUser, Content: "earlier"}}}
	r := &fakeResponder{result: ai.Result{Text: "he works at LINE"}}
	c := New(s, r, nil)

	resp := c.respond(context.Background(), request(t, "abc-123", "where does he work?"))
	if resp.Text != "he works at LINE" || resp.Error != "" {
		t.Fatalf("unexpected response %+v", resp)
	}
	if r.lastMessage != "where does he work?" {
		t.Fatalf("unexpected message %q", r.lastMessage)
	}
	if len(r.lastHistory) != 1 {
		t.Fatalf("expected history passed through, got %d", len(r.lastHistory))
	}
	if len(s.appended) != 2 || s.appended[0].Role != store.RoleUser || s.appended[1].Role != store.RoleModel {
		t.Fatalf("expected both turns stored, got %+v", s.appended)
	}
	if s.lastUserID != "web:abc-123" {
		t.Fatalf("expected web: prefixed user id, got %q", s.lastUserID)
	}
}

func TestRespondLLMErrorReturnsFriendlyText(t *testing.T) {
	s := &fakeHistoryStore{}
	c := New(s, &fakeResponder{err: errors.New("all providers failed")}, nil)
	resp := c.respond(context.Background(), request(t, "s1", "hi"))
	if resp.Text != unavailableText || resp.Error != "" {
		t.Fatalf("expected friendly failure text, got %+v", resp)
	}
	if len(s.appended) != 0 {
		t.Fatal("failed turns must not be stored")
	}
}

func TestRespondHistoryLoadErrorStillAnswers(t *testing.T) {
	s := &fakeHistoryStore{getErr: errors.New("db down")}
	r := &fakeResponder{result: ai.Result{Text: "answer"}}
	c := New(s, r, nil)
	resp := c.respond(context.Background(), request(t, "s1", "hi"))
	if resp.Text != "answer" {
		t.Fatalf("expected answer despite history failure, got %+v", resp)
	}
	if len(r.lastHistory) != 0 {
		t.Fatal("expected empty history after load failure")
	}
}

func TestRespondReminderIntentAnswersOffTopic(t *testing.T) {
	s := &fakeHistoryStore{}
	c := New(s, &fakeResponder{result: ai.Result{ReminderIntent: true}}, nil)
	resp := c.respond(context.Background(), request(t, "s1", "remind me tomorrow"))
	if resp.Text != offTopicText {
		t.Fatalf("expected off-topic text, got %+v", resp)
	}
	if len(s.appended) != 0 {
		t.Fatal("off-topic turns must not be stored")
	}
}

func TestRespondGeneratedImageAnswersOffTopic(t *testing.T) {
	c := New(&fakeHistoryStore{}, &fakeResponder{result: ai.Result{ImageData: []byte{1}}}, nil)
	resp := c.respond(context.Background(), request(t, "s1", "draw a cat"))
	if resp.Text != offTopicText {
		t.Fatalf("expected off-topic text, got %+v", resp)
	}
}

func TestIsResetCommand(t *testing.T) {
	for _, yes := range []string{"/reset", " /RESET ", "ล้าง", "เริ่มใหม่"} {
		if !isResetCommand(yes) {
			t.Errorf("expected %q to be a reset command", yes)
		}
	}
	for _, no := range []string{"reset please", "", "clear"} {
		if isResetCommand(no) {
			t.Errorf("expected %q not to be a reset command", no)
		}
	}
}
