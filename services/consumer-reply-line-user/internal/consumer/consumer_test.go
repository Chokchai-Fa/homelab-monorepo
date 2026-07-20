package consumer

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/line/line-bot-sdk-go/v7/linebot"
)

func TestBuildMessages(t *testing.T) {
	c := New(nil, "https://line-webhook.example.com/")

	t.Run("image then text", func(t *testing.T) {
		msgs := c.buildMessages(ReplyEvent{ImageKey: "abc123", Text: "here you go"})
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
		img, ok := msgs[0].(*linebot.ImageMessage)
		if !ok {
			t.Fatalf("first message is %T, want *linebot.ImageMessage", msgs[0])
		}
		wantURL := "https://line-webhook.example.com/images/abc123"
		if img.OriginalContentURL != wantURL || img.PreviewImageURL != wantURL {
			t.Errorf("image URLs = %q / %q, want %q", img.OriginalContentURL, img.PreviewImageURL, wantURL)
		}
		if txt, ok := msgs[1].(*linebot.TextMessage); !ok || txt.Text != "here you go" {
			t.Errorf("second message = %#v", msgs[1])
		}
	})

	t.Run("image without base URL degrades to text", func(t *testing.T) {
		noBase := New(nil, "")
		msgs := noBase.buildMessages(ReplyEvent{ImageKey: "abc123", Text: "caption"})
		if len(msgs) != 1 {
			t.Fatalf("got %d messages, want 1 text-only", len(msgs))
		}
		if _, ok := msgs[0].(*linebot.TextMessage); !ok {
			t.Errorf("message = %#v, want text", msgs[0])
		}
	})

	t.Run("image with no text", func(t *testing.T) {
		msgs := c.buildMessages(ReplyEvent{ImageKey: "abc123"})
		if len(msgs) != 1 {
			t.Fatalf("got %d messages, want 1", len(msgs))
		}
		if _, ok := msgs[0].(*linebot.ImageMessage); !ok {
			t.Errorf("message = %#v, want image", msgs[0])
		}
	})

	t.Run("reminder flex before text", func(t *testing.T) {
		msgs := c.buildMessages(ReplyEvent{
			Reminder: &ReminderPayload{Message: "กินยา", RemindAt: time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)},
			Text:     "caption",
		})
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
		fm, ok := msgs[0].(*linebot.FlexMessage)
		if !ok {
			t.Fatalf("first message is %T, want *linebot.FlexMessage", msgs[0])
		}
		if fm.AltText != "⏰ กินยา" {
			t.Errorf("alt text = %q, want %q", fm.AltText, "⏰ กินยา")
		}
	})

	t.Run("reminder alone with no text", func(t *testing.T) {
		msgs := c.buildMessages(ReplyEvent{
			Reminder: &ReminderPayload{Message: "test", RemindAt: time.Now()},
		})
		if len(msgs) != 1 {
			t.Fatalf("got %d messages, want 1", len(msgs))
		}
		if _, ok := msgs[0].(*linebot.FlexMessage); !ok {
			t.Errorf("message = %#v, want flex", msgs[0])
		}
	})

	t.Run("alt text truncates to LINE's limit", func(t *testing.T) {
		msgs := c.buildMessages(ReplyEvent{
			Reminder: &ReminderPayload{Message: strings.Repeat("a", maxAltTextRunes+50), RemindAt: time.Now()},
		})
		fm, ok := msgs[0].(*linebot.FlexMessage)
		if !ok {
			t.Fatalf("first message is %T, want *linebot.FlexMessage", msgs[0])
		}
		if got := len([]rune(fm.AltText)); got != maxAltTextRunes {
			t.Errorf("alt text runes = %d, want %d", got, maxAltTextRunes)
		}
	})

	t.Run("quick replies attach to last message", func(t *testing.T) {
		msgs := c.buildMessages(ReplyEvent{
			Reminder: &ReminderPayload{Message: "remind", RemindAt: time.Now()},
			Text:     "pick one",
			QuickReplies: []QuickReply{
				{Label: "Myself", Data: "flow=rem&a=target&v=self"},
				{Label: "Cancel", Data: "flow=rem&a=cancel", DisplayText: "cancel"},
			},
		})
		if len(msgs) != 2 {
			t.Fatalf("got %d messages, want 2", len(msgs))
		}
		// quickReplyItems is unexported, so assert through the wire JSON.
		lastJSON, err := json.Marshal(msgs[len(msgs)-1])
		if err != nil {
			t.Fatalf("marshal last message: %v", err)
		}
		if !strings.Contains(string(lastJSON), `"quickReply"`) {
			t.Fatalf("last message JSON missing quickReply: %s", lastJSON)
		}
		// json.Marshal HTML-escapes the ampersands in postback data, so
		// match on a fragment that has none.
		if !strings.Contains(string(lastJSON), "flow=rem") {
			t.Errorf("last message JSON missing postback data: %s", lastJSON)
		}
		firstJSON, err := json.Marshal(msgs[0])
		if err != nil {
			t.Fatalf("marshal first message: %v", err)
		}
		if strings.Contains(string(firstJSON), `"quickReply"`) {
			t.Error("quick replies also attached to a non-last message")
		}
	})

	t.Run("quick replies with no other content produce nothing", func(t *testing.T) {
		// Regression guard: buildMessages indexes messages[len(messages)-1]
		// when attaching quick replies; if the len(messages) > 0 guard were
		// ever dropped this would panic on an empty event instead of
		// returning an empty slice.
		msgs := c.buildMessages(ReplyEvent{
			QuickReplies: []QuickReply{{Label: "x", Data: "y"}},
		})
		if len(msgs) != 0 {
			t.Fatalf("got %d messages, want 0", len(msgs))
		}
	})

	t.Run("alt text at exactly the limit is not truncated", func(t *testing.T) {
		msgs := c.buildMessages(ReplyEvent{
			Reminder: &ReminderPayload{Message: strings.Repeat("a", maxAltTextRunes-2), RemindAt: time.Now()},
		})
		fm, ok := msgs[0].(*linebot.FlexMessage)
		if !ok {
			t.Fatalf("first message is %T, want *linebot.FlexMessage", msgs[0])
		}
		want := "⏰ " + strings.Repeat("a", maxAltTextRunes-2)
		if fm.AltText != want {
			t.Errorf("alt text = %q, want unmodified %q", fm.AltText, want)
		}
	})

	t.Run("all fields combined: reminder, image, text, quick replies", func(t *testing.T) {
		msgs := c.buildMessages(ReplyEvent{
			Reminder:     &ReminderPayload{Message: "กินยา", RemindAt: time.Now()},
			ImageKey:     "img1",
			Text:         "part one\n\npart two",
			QuickReplies: []QuickReply{{Label: "ok", Data: "d"}},
		})
		if len(msgs) != 3 {
			t.Fatalf("got %d messages, want 3 (flex, image, text)", len(msgs))
		}
		if _, ok := msgs[0].(*linebot.FlexMessage); !ok {
			t.Errorf("message 0 = %T, want flex", msgs[0])
		}
		if _, ok := msgs[1].(*linebot.ImageMessage); !ok {
			t.Errorf("message 1 = %T, want image", msgs[1])
		}
		txt, ok := msgs[2].(*linebot.TextMessage)
		if !ok {
			t.Fatalf("message 2 = %T, want text", msgs[2])
		}
		if txt.Text != "part one\n\npart two" {
			t.Errorf("text = %q", txt.Text)
		}
		lastJSON, err := json.Marshal(msgs[len(msgs)-1])
		if err != nil {
			t.Fatalf("marshal last message: %v", err)
		}
		if !strings.Contains(string(lastJSON), `"quickReply"`) {
			t.Errorf("quick replies not attached to last message: %s", lastJSON)
		}
	})
}

// newTestBot builds a *linebot.Client whose API calls hit a local
// httptest.Server instead of the real LINE API, so deliver()/Handle() tests
// exercise the actual HTTP request/response cycle without any network call.
func newTestBot(t *testing.T, handler http.HandlerFunc) *linebot.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	bot, err := linebot.New("secret", "token", linebot.WithEndpointBase(srv.URL))
	if err != nil {
		t.Fatalf("linebot.New: %v", err)
	}
	return bot
}

// sendBody is the wire shape POSTed to the reply/push endpoints; only the
// message count is needed to verify batching.
type sendBody struct {
	Messages []json.RawMessage `json:"messages"`
}

func messageCount(t *testing.T, r *http.Request) int {
	t.Helper()
	var body sendBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return len(body.Messages)
}

func textMessages(n int) []linebot.SendingMessage {
	msgs := make([]linebot.SendingMessage, n)
	for i := range msgs {
		msgs[i] = linebot.NewTextMessage("part")
	}
	return msgs
}

func TestDeliverUsesReplyTokenFirst(t *testing.T) {
	var calls []string
	bot := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	c := New(bot, "")

	delivered, err := c.deliver(ReplyEvent{ReplyToken: "tok", UserID: "u1"}, textMessages(1))
	if err != nil {
		t.Fatalf("deliver error = %v, want nil", err)
	}
	if !delivered {
		t.Fatal("delivered = false, want true")
	}
	if len(calls) != 1 || calls[0] != linebot.APIEndpointReplyMessage {
		t.Fatalf("calls = %v, want single call to %s", calls, linebot.APIEndpointReplyMessage)
	}
}

func TestDeliverFallsBackToPushWhenReplyTokenFails(t *testing.T) {
	var calls []string
	bot := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Path == linebot.APIEndpointReplyMessage {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"Invalid reply token"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	c := New(bot, "")

	delivered, err := c.deliver(ReplyEvent{ReplyToken: "expired", UserID: "u1"}, textMessages(1))
	if err != nil {
		t.Fatalf("deliver error = %v, want nil (push fallback succeeded)", err)
	}
	if !delivered {
		t.Fatal("delivered = false, want true")
	}
	if len(calls) != 2 || calls[0] != linebot.APIEndpointReplyMessage || calls[1] != linebot.APIEndpointPushMessage {
		t.Fatalf("calls = %v, want [reply, push]", calls)
	}
}

func TestDeliverCannotPushWithoutUserID(t *testing.T) {
	bot := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Invalid reply token"}`))
	})
	c := New(bot, "")

	delivered, err := c.deliver(ReplyEvent{ReplyToken: "expired"}, textMessages(1))
	if delivered {
		t.Error("delivered = true, want false")
	}
	if err == nil || !strings.Contains(err.Error(), "no user_id") {
		t.Fatalf("err = %v, want an error about missing user_id", err)
	}
}

func TestDeliverPushFailureReturnsAPIError(t *testing.T) {
	bot := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"monthly limit reached"}`))
	})
	c := New(bot, "")

	delivered, err := c.deliver(ReplyEvent{UserID: "u1"}, textMessages(1))
	if delivered {
		t.Error("delivered = true, want false")
	}
	var apiErr *linebot.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v (%T), want *linebot.APIError", err, err)
	}
	if apiErr.Code != http.StatusTooManyRequests {
		t.Errorf("APIError.Code = %d, want %d", apiErr.Code, http.StatusTooManyRequests)
	}
}

func TestDeliverBatchesPushOverFiveMessages(t *testing.T) {
	var counts []int
	bot := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		counts = append(counts, messageCount(t, r))
		w.WriteHeader(http.StatusOK)
	})
	c := New(bot, "")

	delivered, err := c.deliver(ReplyEvent{UserID: "u1"}, textMessages(12))
	if err != nil {
		t.Fatalf("deliver error = %v", err)
	}
	if !delivered {
		t.Fatal("delivered = false, want true")
	}
	want := []int{5, 5, 2}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("push batch sizes = %v, want %v", counts, want)
	}
}

func TestDeliverReplyTokenOverflowSpillsToPush(t *testing.T) {
	var replyCounts, pushCounts []int
	bot := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		n := messageCount(t, r)
		if r.URL.Path == linebot.APIEndpointReplyMessage {
			replyCounts = append(replyCounts, n)
		} else {
			pushCounts = append(pushCounts, n)
		}
		w.WriteHeader(http.StatusOK)
	})
	c := New(bot, "")

	delivered, err := c.deliver(ReplyEvent{ReplyToken: "tok", UserID: "u1"}, textMessages(7))
	if err != nil {
		t.Fatalf("deliver error = %v", err)
	}
	if !delivered {
		t.Fatal("delivered = false, want true")
	}
	if !reflect.DeepEqual(replyCounts, []int{5}) {
		t.Fatalf("reply call sizes = %v, want [5]", replyCounts)
	}
	if !reflect.DeepEqual(pushCounts, []int{2}) {
		t.Fatalf("push call sizes = %v, want [2] (the 2 messages beyond the reply's cap)", pushCounts)
	}
}

func TestHandleDropsEventWithoutUserIDOrReplyToken(t *testing.T) {
	// Neither identifier is set, so Handle must return before ever touching
	// c.bot; New(nil, "") makes a real API call panic, turning any regression
	// into a visible test failure.
	c := New(nil, "")
	c.Handle(ReplyEvent{Text: "hello"})
}

func TestHandleWhitespaceOnlyTextYieldsNothingDeliverable(t *testing.T) {
	// Text is non-empty so the empty-reply guard in Handle passes, but it
	// trims away to nothing in buildMessages/splitReplyMessages, so
	// buildMessages returns zero messages and deliver must never be called.
	c := New(nil, "")
	c.Handle(ReplyEvent{UserID: "u1", ReplyToken: "tok", Text: "   \n\n  "})
}

func TestHandleEmptyReplyWithReminderIDDoesNotPanicWithoutNATS(t *testing.T) {
	// ReminderID set but Subscribe was never called, so c.nc is nil;
	// ackDelivery must log and return rather than dereference it.
	c := New(nil, "")
	c.Handle(ReplyEvent{UserID: "u1", ReminderID: 42})
}

func TestHandleDeliversEndToEndViaReplyToken(t *testing.T) {
	var calls []string
	bot := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	c := New(bot, "")

	// ReminderID is set with no NATS connection wired up (Subscribe was
	// never called): Handle must still deliver the message and only fail to
	// publish the (best-effort) delivery ack.
	c.Handle(ReplyEvent{UserID: "u1", ReplyToken: "tok", Text: "hello", ReminderID: 7})

	if len(calls) != 1 || calls[0] != linebot.APIEndpointReplyMessage {
		t.Fatalf("calls = %v, want single call to %s", calls, linebot.APIEndpointReplyMessage)
	}
}

func TestSplitReplyMessagesByteLimitCanSplitMultiByteRune(t *testing.T) {
	// splitReplyMessages compares byte lengths (len(part)) against
	// maxMessageChars, not rune counts. maxMessageChars (5000) is not a
	// multiple of the 3-byte encoding of "ก", so a long run of multi-byte
	// characters gets cut mid-rune. This test pins that existing behavior
	// (each part still concatenates back losslessly) and documents the
	// caveat: an individual part sent as its own LINE message is not
	// guaranteed to be valid UTF-8 on its own.
	text := strings.Repeat("ก", 4000) // 12000 bytes total
	got := splitReplyMessages(text)

	if len(got) != 3 {
		t.Fatalf("got %d parts, want 3", len(got))
	}
	if len(got[0]) != maxMessageChars || len(got[1]) != maxMessageChars {
		t.Fatalf("part byte lengths = %d, %d, want %d, %d", len(got[0]), len(got[1]), maxMessageChars, maxMessageChars)
	}
	if joined := got[0] + got[1] + got[2]; joined != text {
		t.Fatal("parts do not reconstruct the original text byte-for-byte")
	}
	if utf8.ValidString(got[0]) {
		t.Error("expected the byte-boundary split to break rune #1667 of the input (documents a real corner case, not just a hardcoded expectation)")
	}
}

func TestSplitReplyMessages(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "single message",
			text: "hello there",
			want: []string{"hello there"},
		},
		{
			name: "merges short paragraphs into one message",
			text: "first part\n\nsecond part\n\nthird part",
			want: []string{"first part\n\nsecond part\n\nthird part"},
		},
		{
			name: "ignore empty segments",
			text: "\n\nfirst part\n\n\nsecond part\n\n",
			want: []string{"first part\n\nsecond part"},
		},
		{
			name: "splits once packed paragraphs exceed the per-message limit",
			text: strings.Repeat("a", maxMessageChars) + "\n\n" + strings.Repeat("b", maxMessageChars),
			want: []string{strings.Repeat("a", maxMessageChars), strings.Repeat("b", maxMessageChars)},
		},
		{
			name: "splits a single paragraph longer than the per-message limit",
			text: strings.Repeat("a", maxMessageChars+10),
			want: []string{strings.Repeat("a", maxMessageChars), strings.Repeat("a", 10)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitReplyMessages(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("splitReplyMessages(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
