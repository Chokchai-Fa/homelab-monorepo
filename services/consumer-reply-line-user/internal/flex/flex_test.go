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
