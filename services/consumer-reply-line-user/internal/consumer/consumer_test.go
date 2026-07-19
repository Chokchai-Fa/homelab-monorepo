package consumer

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

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
