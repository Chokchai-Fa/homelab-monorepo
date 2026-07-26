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

func (f *fakeResponder) RouteStream(_ context.Context, history []store.Message, userMessage string, emit func(string) error) (ai.Result, error) {
	f.calls++
	f.lastMessage = userMessage
	f.lastHistory = history
	if f.err == nil && f.result.Text != "" {
		if e := emit(f.result.Text); e != nil {
			return f.result, e
		}
	}
	return f.result, f.err
}

// fakeRetriever returns a fixed context block and records the query it saw.
type fakeRetriever struct {
	block     string
	lastQuery string
}

func (f *fakeRetriever) RetrieveContext(_ context.Context, query string) string {
	f.lastQuery = query
	return f.block
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
	c := New(&fakeHistoryStore{}, &fakeResponder{}, nil, nil)
	resp := c.respond(context.Background(), []byte("{not json"))
	if resp.Error == "" {
		t.Fatalf("expected error, got %+v", resp)
	}
}

func TestRespondMissingSessionIDReturnsError(t *testing.T) {
	c := New(&fakeHistoryStore{}, &fakeResponder{}, nil, nil)
	resp := c.respond(context.Background(), request(t, "  ", "hello"))
	if resp.Error != "missing session_id" {
		t.Fatalf("expected missing session_id error, got %+v", resp)
	}
}

func TestRespondEmptyMessageReturnsUsageHint(t *testing.T) {
	r := &fakeResponder{}
	c := New(&fakeHistoryStore{}, r, nil, nil)
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
	c := New(&fakeHistoryStore{}, r, nil, nil)
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
	c := New(s, &fakeResponder{}, nil, nil)
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
	c := New(s, &fakeResponder{}, nil, nil)
	resp := c.respond(context.Background(), request(t, "s1", "/reset"))
	if resp.Text == resetDone || resp.Text == "" {
		t.Fatalf("expected failure text, got %+v", resp)
	}
}

func TestRespondNormalTextAnswersAndStoresBothTurns(t *testing.T) {
	s := &fakeHistoryStore{history: []store.Message{{Role: store.RoleUser, Content: "earlier"}}}
	r := &fakeResponder{result: ai.Result{Text: "he works at LINE"}}
	c := New(s, r, nil, nil)

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
	c := New(s, &fakeResponder{err: errors.New("all providers failed")}, nil, nil)
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
	c := New(s, r, nil, nil)
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
	c := New(s, &fakeResponder{result: ai.Result{ReminderIntent: true}}, nil, nil)
	resp := c.respond(context.Background(), request(t, "s1", "remind me tomorrow"))
	if resp.Text != offTopicText {
		t.Fatalf("expected off-topic text, got %+v", resp)
	}
	if len(s.appended) != 0 {
		t.Fatal("off-topic turns must not be stored")
	}
}

func TestRespondGeneratedImageAnswersOffTopic(t *testing.T) {
	c := New(&fakeHistoryStore{}, &fakeResponder{result: ai.Result{ImageData: []byte{1}}}, nil, nil)
	resp := c.respond(context.Background(), request(t, "s1", "draw a cat"))
	if resp.Text != offTopicText {
		t.Fatalf("expected off-topic text, got %+v", resp)
	}
}

func TestRespondAugmentsRoutedMessageButStoresRawQuery(t *testing.T) {
	s := &fakeHistoryStore{}
	r := &fakeResponder{result: ai.Result{Text: "he presented GitCoFL at InCIT 2025 (source: InCIT 2025 paper)"}}
	ret := &fakeRetriever{block: "Relevant facts about Chokchai:\n- [source: InCIT 2025 paper] GitCoFL ..."}
	c := New(s, r, nil, ret)

	resp := c.respond(context.Background(), request(t, "s1", "what research has he done?"))
	if resp.Error != "" {
		t.Fatalf("unexpected error %+v", resp)
	}
	// The router saw the retrieved context prepended to the question...
	if !strings.Contains(r.lastMessage, "[source: InCIT 2025 paper]") ||
		!strings.Contains(r.lastMessage, "Question: what research has he done?") {
		t.Fatalf("router did not receive augmented message: %q", r.lastMessage)
	}
	if ret.lastQuery != "what research has he done?" {
		t.Fatalf("retriever saw %q", ret.lastQuery)
	}
	// ...but the stored history keeps the raw question, not the RAG preamble.
	if len(s.appended) != 2 || s.appended[0].Content != "what research has he done?" {
		t.Fatalf("expected raw question stored, got %+v", s.appended)
	}
}

func TestRespondNoRetrieverLeavesMessageUnchanged(t *testing.T) {
	r := &fakeResponder{result: ai.Result{Text: "ok"}}
	c := New(&fakeHistoryStore{}, r, nil, nil) // no retriever
	c.respond(context.Background(), request(t, "s1", "hello"))
	if r.lastMessage != "hello" {
		t.Fatalf("expected unaugmented message, got %q", r.lastMessage)
	}
}

func TestRespondEmptyContextLeavesMessageUnchanged(t *testing.T) {
	r := &fakeResponder{result: ai.Result{Text: "ok"}}
	c := New(&fakeHistoryStore{}, r, nil, &fakeRetriever{block: ""}) // retrieval found nothing
	c.respond(context.Background(), request(t, "s1", "hello"))
	if r.lastMessage != "hello" {
		t.Fatalf("expected unaugmented message when no context, got %q", r.lastMessage)
	}
}

// streamCollector captures the frames streamRespond emits, mirroring what the
// gateway would forward to the browser.
type streamCollector struct {
	deltas   []string
	doneErr  string
	doneRuns int
}

func (s *streamCollector) emit(delta string) error { s.deltas = append(s.deltas, delta); return nil }
func (s *streamCollector) done(errText string)     { s.doneErr = errText; s.doneRuns++ }
func (s *streamCollector) text() string {
	out := ""
	for _, d := range s.deltas {
		out += d
	}
	return out
}

func TestStreamRespondEmitsDeltasStoresAndDonesOnce(t *testing.T) {
	store := &fakeHistoryStore{}
	// fakeResponder emits result.Text as one delta via RouteStream.
	c := New(store, &fakeResponder{result: ai.Result{Text: "He works at LINE."}}, nil, nil)
	col := &streamCollector{}
	c.streamRespond(context.Background(), request(t, "abc-1", "where?"), col.emit, col.done)

	if col.text() != "He works at LINE." {
		t.Fatalf("streamed %q", col.text())
	}
	if col.doneRuns != 1 || col.doneErr != "" {
		t.Fatalf("expected exactly one clean done, got runs=%d err=%q", col.doneRuns, col.doneErr)
	}
	if len(store.appended) != 2 || store.lastUserID != "web:abc-1" {
		t.Fatalf("expected both turns stored under web: key, got %+v / %q", store.appended, store.lastUserID)
	}
}

func TestStreamRespondEmptyMessageHintNoStore(t *testing.T) {
	store := &fakeHistoryStore{}
	c := New(store, &fakeResponder{}, nil, nil)
	col := &streamCollector{}
	c.streamRespond(context.Background(), request(t, "s1", "  "), col.emit, col.done)
	if col.text() != usageHint || col.doneRuns != 1 {
		t.Fatalf("expected usage hint + done, got %q runs=%d", col.text(), col.doneRuns)
	}
	if len(store.appended) != 0 {
		t.Fatal("must not store on empty message")
	}
}

func TestStreamRespondResetClears(t *testing.T) {
	store := &fakeHistoryStore{}
	c := New(store, &fakeResponder{}, nil, nil)
	col := &streamCollector{}
	c.streamRespond(context.Background(), request(t, "s1", "/reset"), col.emit, col.done)
	if col.text() != resetDone || store.clearCalls != 1 {
		t.Fatalf("expected reset, got %q clears=%d", col.text(), store.clearCalls)
	}
}

func TestStreamRespondLLMErrorEmitsFallback(t *testing.T) {
	store := &fakeHistoryStore{}
	c := New(store, &fakeResponder{err: errors.New("boom")}, nil, nil)
	col := &streamCollector{}
	c.streamRespond(context.Background(), request(t, "s1", "hi"), col.emit, col.done)
	if col.text() != unavailableText {
		t.Fatalf("expected fallback text, got %q", col.text())
	}
	if col.doneErr == "" {
		t.Fatal("expected done to carry an error marker")
	}
	if len(store.appended) != 0 {
		t.Fatal("failed answers must not be stored")
	}
}

func TestStreamRespondOffTopicIntent(t *testing.T) {
	c := New(&fakeHistoryStore{}, &fakeResponder{result: ai.Result{ReminderIntent: true}}, nil, nil)
	col := &streamCollector{}
	c.streamRespond(context.Background(), request(t, "s1", "remind me"), col.emit, col.done)
	if col.text() != offTopicText || col.doneRuns != 1 {
		t.Fatalf("expected off-topic redirect, got %q", col.text())
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
