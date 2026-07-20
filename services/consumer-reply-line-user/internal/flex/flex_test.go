package flex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildProducesValidJSONWithEscapedText(t *testing.T) {
	remindAt := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	raw, err := Build(`say "hi" & <bye>`, "Meow", remindAt)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Build output is not valid JSON: %v\n%s", err, raw)
	}
	if parsed["type"] != "bubble" {
		t.Errorf("type = %v, want bubble", parsed["type"])
	}
	// The raw JSON must contain the real quote character (properly escaped
	// as \"), not a broken/truncated structure from string concatenation.
	if !strings.Contains(string(raw), `say \"hi\"`) {
		t.Errorf("message not properly escaped: %s", raw)
	}
	if !strings.Contains(string(raw), "Meow") {
		t.Errorf("display name missing: %s", raw)
	}
}

func TestBuildFallsBackToSomeoneWhenNameEmpty(t *testing.T) {
	raw, err := Build("กินยา", "", time.Now())
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(string(raw), "someone") {
		t.Errorf("expected fallback name, got: %s", raw)
	}
}

func TestBuildFormatsTimeInBangkok(t *testing.T) {
	// 2026-07-20T02:00:00Z is 2026-07-20 09:00 in Asia/Bangkok (+07:00).
	remindAt := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	raw, err := Build("test", "Meow", remindAt)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(string(raw), "20/07/2026 09:00") {
		t.Errorf("expected Bangkok-local time in output: %s", raw)
	}
}

func TestBuildFormatsTimeAcrossBangkokDayBoundary(t *testing.T) {
	// 2026-07-19T23:30:00Z is 2026-07-20 06:30 in Asia/Bangkok: the +07:00
	// offset rolls the date forward, which a naive same-day formatter would
	// get wrong.
	remindAt := time.Date(2026, 7, 19, 23, 30, 0, 0, time.UTC)
	raw, err := Build("test", "Meow", remindAt)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(string(raw), "20/07/2026 06:30") {
		t.Errorf("expected day-rolled Bangkok-local time in output: %s", raw)
	}
	if strings.Contains(string(raw), "19/07/2026") {
		t.Errorf("output kept the UTC date instead of rolling to Bangkok's: %s", raw)
	}
}

func TestBuildEmptyMessage(t *testing.T) {
	raw, err := Build("", "Meow", time.Now())
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Build output is not valid JSON: %v\n%s", err, raw)
	}
}

func TestBuildBothMessageAndNameEmpty(t *testing.T) {
	raw, err := Build("", "", time.Now())
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(string(raw), "someone") {
		t.Errorf("expected fallback name even with empty message, got: %s", raw)
	}
}

func TestBuildLongMessageIsNotTruncated(t *testing.T) {
	// Unlike the consumer package's flex alt-text, Build itself applies no
	// length limit to the bubble body text.
	long := strings.Repeat("a", 2000)
	raw, err := Build(long, "Meow", time.Now())
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(string(raw), long) {
		t.Error("long message was altered or truncated")
	}
}

func TestBuildStructure(t *testing.T) {
	// Walk the full JSON structure (not just substring checks) to pin the
	// bubble's shape: colored header, wrapped body text, a separator, and a
	// footer box with both the "from" and "time" lines.
	remindAt := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	raw, err := Build("take medicine", "Alice", remindAt)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}

	var parsed struct {
		Type   string `json:"type"`
		Header struct {
			Type            string           `json:"type"`
			Layout          string           `json:"layout"`
			BackgroundColor string           `json:"backgroundColor"`
			Contents        []map[string]any `json:"contents"`
		} `json:"header"`
		Body struct {
			Type     string           `json:"type"`
			Layout   string           `json:"layout"`
			Spacing  string           `json:"spacing"`
			Contents []map[string]any `json:"contents"`
		} `json:"body"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}

	if parsed.Type != "bubble" {
		t.Errorf("type = %q, want bubble", parsed.Type)
	}
	if parsed.Header.BackgroundColor != "#7D5FFF" {
		t.Errorf("header backgroundColor = %q", parsed.Header.BackgroundColor)
	}
	if len(parsed.Header.Contents) != 1 || parsed.Header.Contents[0]["text"] != "⏰ แจ้งเตือน" {
		t.Errorf("header contents = %v", parsed.Header.Contents)
	}

	if len(parsed.Body.Contents) != 3 {
		t.Fatalf("body has %d contents, want 3 (message text, separator, footer box)", len(parsed.Body.Contents))
	}
	if parsed.Body.Contents[0]["text"] != "take medicine" || parsed.Body.Contents[0]["wrap"] != true {
		t.Errorf("body message content = %v", parsed.Body.Contents[0])
	}
	if parsed.Body.Contents[1]["type"] != "separator" {
		t.Errorf("body second content = %v, want a separator", parsed.Body.Contents[1])
	}
	footer, ok := parsed.Body.Contents[2]["contents"].([]any)
	if !ok || len(footer) != 2 {
		t.Fatalf("footer box contents = %v, want 2 text lines", parsed.Body.Contents[2]["contents"])
	}
	fromLine, ok := footer[0].(map[string]any)
	if !ok || fromLine["text"] != "จาก: Alice" {
		t.Errorf("footer 'from' line = %v", footer[0])
	}
	timeLine, ok := footer[1].(map[string]any)
	if !ok || timeLine["text"] != "เวลา: 20/07/2026 09:00" {
		t.Errorf("footer 'time' line = %v", footer[1])
	}
}
