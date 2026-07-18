package consumer

import (
	"reflect"
	"testing"

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
			name: "split on blank lines",
			text: "first part\n\nsecond part\n\nthird part",
			want: []string{"first part", "second part", "third part"},
		},
		{
			name: "ignore empty segments",
			text: "\n\nfirst part\n\n\nsecond part\n\n",
			want: []string{"first part", "second part"},
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
