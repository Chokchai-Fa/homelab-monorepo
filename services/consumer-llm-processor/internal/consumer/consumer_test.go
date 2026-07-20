package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

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
}

func (f *fakeHistoryStore) GetRecent(_ context.Context, _ string) ([]store.Message, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.history, nil
}

func (f *fakeHistoryStore) Append(_ context.Context, _, role, content string) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appended = append(f.appended, store.Message{Role: role, Content: content})
	return nil
}

func (f *fakeHistoryStore) Clear(_ context.Context, _ string) error {
	f.clearCalls++
	return f.clearErr
}

func (f *fakeHistoryStore) Close() {}

// fakeResponder is a hand-rolled Responder fake.
type fakeResponder struct {
	result      ai.Result
	err         error
	calls       int
	lastMessage string
	lastImage   *ai.Image
	lastHistory []store.Message
}

func (f *fakeResponder) Route(_ context.Context, history []store.Message, userMessage string, image *ai.Image) (ai.Result, error) {
	f.calls++
	f.lastMessage = userMessage
	f.lastImage = image
	f.lastHistory = history
	return f.result, f.err
}

// fakeImageStore is a hand-rolled ImageStore fake.
type fakeImageStore struct {
	takeData    []byte
	takeErr     error
	putErr      error
	putCalls    int
	lastPutData []byte
	lastPutTTL  time.Duration
}

func (f *fakeImageStore) Take(_ context.Context, _ string) ([]byte, error) {
	if f.takeErr != nil {
		return nil, f.takeErr
	}
	return f.takeData, nil
}

func (f *fakeImageStore) PutGenerated(_ context.Context, _ string, data []byte, ttl time.Duration) error {
	f.putCalls++
	f.lastPutData = data
	f.lastPutTTL = ttl
	if f.putErr != nil {
		return f.putErr
	}
	return nil
}

// fakeFlowChecker is a hand-rolled FlowChecker fake.
type fakeFlowChecker struct {
	active bool
}

func (f *fakeFlowChecker) Active(_ context.Context, _ string) bool { return f.active }

func TestRespondEmptyQueryReturnsUsageHint(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{}
	c := &Consumer{store: fs, ai: fr}

	text, imageKey, reminder := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "   "})

	if imageKey != "" || reminder {
		t.Fatalf("imageKey=%q reminder=%v, want empty/false", imageKey, reminder)
	}
	if text == "" {
		t.Error("expected a non-empty usage hint")
	}
	if fr.calls != 0 {
		t.Errorf("responder called %d times, want 0 for empty query", fr.calls)
	}
}

func TestRespondResetCommandClearsHistory(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{}
	c := &Consumer{store: fs, ai: fr}

	text, _, reminder := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "/reset"})

	if fs.clearCalls != 1 {
		t.Errorf("Clear called %d times, want 1", fs.clearCalls)
	}
	if reminder {
		t.Error("reset command should not be treated as reminder")
	}
	if text != "Conversation history cleared. เริ่มบทสนทนาใหม่ได้เลย!" {
		t.Errorf("text = %q", text)
	}
}

func TestRespondResetCommandClearErrorFallsBack(t *testing.T) {
	fs := &fakeHistoryStore{clearErr: errors.New("redis down")}
	c := &Consumer{store: fs, ai: &fakeResponder{}}

	text, _, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "ล้าง"})

	if text != "Sorry, I couldn't reset the conversation. Please try again." {
		t.Errorf("text = %q", text)
	}
}

func TestRespondNormalTextStoresBothTurns(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{result: ai.Result{Text: "the answer"}}
	c := &Consumer{store: fs, ai: fr}

	text, imageKey, reminder := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "what's up"})

	if text != "the answer" || imageKey != "" || reminder {
		t.Errorf("got (%q, %q, %v)", text, imageKey, reminder)
	}
	if fr.calls != 1 || fr.lastMessage != "what's up" {
		t.Errorf("responder call = %d, lastMessage = %q", fr.calls, fr.lastMessage)
	}
	if len(fs.appended) != 2 {
		t.Fatalf("appended %d messages, want 2", len(fs.appended))
	}
	if fs.appended[0].Role != store.RoleUser || fs.appended[0].Content != "what's up" {
		t.Errorf("first appended = %+v", fs.appended[0])
	}
	if fs.appended[1].Role != store.RoleModel || fs.appended[1].Content != "the answer" {
		t.Errorf("second appended = %+v", fs.appended[1])
	}
}

func TestRespondAIErrorReturnsFallbackText(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{err: errors.New("all providers failed")}
	c := &Consumer{store: fs, ai: fr}

	text, _, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "hi"})

	if text != "Sorry, the AI is unavailable right now. Please try again later.\nขออภัย ตอนนี้ AI ไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง" {
		t.Errorf("text = %q", text)
	}
}

func TestRespondAIErrorWithImageReturnsImageFallback(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{err: errors.New("vision down")}
	fis := &fakeImageStore{takeData: []byte("bytes")}
	c := &Consumer{store: fs, ai: fr, images: fis}

	text, _, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", ImageKey: "img1", ImageMime: "image/jpeg"})

	if text != "Sorry, I can't view images right now. Please try again later.\nขออภัย ตอนนี้ฉันดูรูปไม่ได้ กรุณาลองใหม่ภายหลัง" {
		t.Errorf("text = %q", text)
	}
}

func TestRespondImageRequestWithoutImageStoreConfigured(t *testing.T) {
	c := &Consumer{store: &fakeHistoryStore{}, ai: &fakeResponder{}}

	text, imageKey, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", ImageKey: "img1"})

	if imageKey != "" {
		t.Errorf("imageKey = %q, want empty", imageKey)
	}
	if text != "Sorry, I can't view images right now. Please try again later." {
		t.Errorf("text = %q", text)
	}
}

func TestRespondImageTakeErrorReturnsFallback(t *testing.T) {
	fis := &fakeImageStore{takeErr: errors.New("expired")}
	c := &Consumer{store: &fakeHistoryStore{}, ai: &fakeResponder{}, images: fis}

	text, _, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", ImageKey: "img1"})

	if text != "Sorry, I couldn't retrieve that image in time. Please send it again." {
		t.Errorf("text = %q", text)
	}
}

func TestRespondImageRequestUsesDefaultPromptAndTagsHistory(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{result: ai.Result{Text: "it's a cat"}}
	fis := &fakeImageStore{takeData: []byte("img-bytes")}
	c := &Consumer{store: fs, ai: fr, images: fis}

	text, _, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", ImageKey: "img1", ImageMime: "image/jpeg"})

	if text != "it's a cat" {
		t.Errorf("text = %q", text)
	}
	if fr.lastMessage != defaultImagePrompt {
		t.Errorf("responder query = %q, want default prompt", fr.lastMessage)
	}
	if fr.lastImage == nil || string(fr.lastImage.Data) != "img-bytes" || fr.lastImage.MimeType != "image/jpeg" {
		t.Errorf("responder image = %+v", fr.lastImage)
	}
	if len(fs.appended) != 2 || fs.appended[0].Content != "[user sent an image] "+defaultImagePrompt {
		t.Errorf("appended user turn = %+v", fs.appended)
	}
}

func TestRespondReminderIntentShortCircuits(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{result: ai.Result{ReminderIntent: true}}
	c := &Consumer{store: fs, ai: fr}

	text, imageKey, reminder := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "remind me to sleep"})

	if !reminder {
		t.Fatal("expected reminder=true")
	}
	if text != "" || imageKey != "" {
		t.Errorf("got (%q, %q), want both empty on reminder handoff", text, imageKey)
	}
	if len(fs.appended) != 0 {
		t.Errorf("history should not be touched on reminder handoff, got %+v", fs.appended)
	}
}

func TestRespondGeneratedImageStashedAndHistoryTagged(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{result: ai.Result{Text: "a cat", ImageData: []byte("jpeg-bytes"), ImageMime: "image/jpeg"}}
	fis := &fakeImageStore{}
	c := &Consumer{store: fs, ai: fr, images: fis}

	text, imageKey, reminder := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "draw a cat"})

	if reminder {
		t.Fatal("unexpected reminder=true")
	}
	if text != "a cat" {
		t.Errorf("text = %q", text)
	}
	if imageKey == "" {
		t.Fatal("expected a generated image key")
	}
	if fis.putCalls != 1 || string(fis.lastPutData) != "jpeg-bytes" {
		t.Errorf("PutGenerated calls=%d data=%q", fis.putCalls, fis.lastPutData)
	}
	if len(fs.appended) != 2 || fs.appended[1].Content != "[generated an image] a cat" {
		t.Errorf("model history = %+v", fs.appended)
	}
}

func TestRespondGeneratedImageWithoutImageStoreFallsBack(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{result: ai.Result{Text: "a cat", ImageData: []byte("jpeg-bytes")}}
	c := &Consumer{store: fs, ai: fr} // no images configured

	text, imageKey, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "draw a cat"})

	if imageKey != "" {
		t.Errorf("imageKey = %q, want empty", imageKey)
	}
	if text != "Sorry, I drew your picture but couldn't deliver it. Please try again.\nขออภัย วาดรูปเสร็จแล้วแต่ส่งไม่ได้ ลองใหม่อีกครั้งน้า~" {
		t.Errorf("text = %q", text)
	}
}

func TestRespondGeneratedImagePutErrorFallsBack(t *testing.T) {
	fs := &fakeHistoryStore{}
	fr := &fakeResponder{result: ai.Result{Text: "a cat", ImageData: []byte("jpeg-bytes")}}
	fis := &fakeImageStore{putErr: errors.New("redis down")}
	c := &Consumer{store: fs, ai: fr, images: fis}

	text, imageKey, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "draw a cat"})

	if imageKey != "" {
		t.Errorf("imageKey = %q, want empty", imageKey)
	}
	if text == "" || fis.putCalls != 1 {
		t.Errorf("text=%q putCalls=%d", text, fis.putCalls)
	}
}

func TestRespondDegradesWhenHistoryLoadFails(t *testing.T) {
	fs := &fakeHistoryStore{getErr: errors.New("db down")}
	fr := &fakeResponder{result: ai.Result{Text: "ok"}}
	c := &Consumer{store: fs, ai: fr}

	text, _, _ := c.respond(context.Background(), RequestEvent{UserID: "u1", Text: "hi"})

	if text != "ok" {
		t.Errorf("text = %q, want the responder's answer despite history load failure", text)
	}
	if fr.lastHistory != nil {
		t.Errorf("history passed to responder = %v, want nil on load failure", fr.lastHistory)
	}
}

func TestIsReminderPath(t *testing.T) {
	cases := []struct {
		name  string
		event RequestEvent
		flows FlowChecker
		want  bool
	}{
		{
			name:  "image request never counts as reminder",
			event: RequestEvent{ImageKey: "img1", Text: "/reminder"},
			want:  false,
		},
		{
			name:  "trigger keyword without flow checker",
			event: RequestEvent{Text: "/reminder buy milk"},
			want:  true,
		},
		{
			name:  "list command counts as reminder path",
			event: RequestEvent{Text: "/reminders"},
			want:  true,
		},
		{
			name:  "ordinary text with nil flow checker",
			event: RequestEvent{Text: "hello there"},
			flows: nil,
			want:  false,
		},
		{
			name:  "ordinary text with inactive flow",
			event: RequestEvent{Text: "hello there"},
			flows: &fakeFlowChecker{active: false},
			want:  false,
		},
		{
			name:  "ordinary text with active flow",
			event: RequestEvent{Text: "9am"},
			flows: &fakeFlowChecker{active: true},
			want:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Consumer{flows: tc.flows}
			if got := c.isReminderPath(tc.event); got != tc.want {
				t.Errorf("isReminderPath(%+v) = %v, want %v", tc.event, got, tc.want)
			}
		})
	}
}

func TestIsResetCommand(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"/reset", true},
		{"  /reset  ", true},
		{"ล้าง", true},
		{"เริ่มใหม่", true},
		{"/RESET", true},
		{"reset", false},
		{"", false},
		{"/reset now", false},
	}
	for _, tc := range cases {
		if got := isResetCommand(tc.text); got != tc.want {
			t.Errorf("isResetCommand(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsReminderCommand(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"/reminder", true},
		{"/reminder buy milk at 9am", true},
		{"ตั้งเตือนกินยา", true},
		{"/reminders", true},
		{"ดูเตือนทั้งหมด", true},
		{"รายการเตือน", true},
		{"hello", false},
		{"/reminderish", false}, // no space, must be exact or "/reminder "
		{"", false},
	}
	for _, tc := range cases {
		if got := isReminderCommand(tc.text); got != tc.want {
			t.Errorf("isReminderCommand(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsReminderListCommand(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"/reminders", true},
		{"ดูเตือน", true},
		{"ดูเตือนวันนี้", true},
		{"รายการเตือน", true},
		{"/reminder", false},
		{"hello", false},
	}
	for _, tc := range cases {
		if got := isReminderListCommand(tc.text); got != tc.want {
			t.Errorf("isReminderListCommand(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsCancelText(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"ยกเลิก", true},
		{"cancel", true},
		{"CANCEL", true},
		{"/cancel", true},
		{"  cancel  ", true},
		{"nevermind", false},
	}
	for _, tc := range cases {
		if got := isCancelText(tc.text); got != tc.want {
			t.Errorf("isCancelText(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestStripReminderTrigger(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"/reminder buy milk", "buy milk"},
		{"/reminder", ""},
		{"ตั้งเตือนกินยา 9 โมง", "กินยา 9 โมง"},
		{"no trigger here", "no trigger here"},
	}
	for _, tc := range cases {
		if got := stripReminderTrigger(tc.text); got != tc.want {
			t.Errorf("stripReminderTrigger(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestRandomID(t *testing.T) {
	id1, err := randomID()
	if err != nil {
		t.Fatalf("randomID: %v", err)
	}
	if len(id1) != 32 { // 16 bytes hex-encoded
		t.Errorf("len(id) = %d, want 32", len(id1))
	}
	id2, err := randomID()
	if err != nil {
		t.Fatalf("randomID: %v", err)
	}
	if id1 == id2 {
		t.Error("expected two random IDs to differ")
	}
}
