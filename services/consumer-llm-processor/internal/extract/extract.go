// Package extract pulls a reminder message and time out of natural language
// (Thai or English) with one small LLM call. It deliberately avoids the full
// provider framework in consumer-llm-processor: a single HTTP POST to Groq's
// OpenAI-compatible endpoint, or Gemini's REST API as fallback, is enough.
package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Result is what the LLM found. A zero RemindAt means no time was given;
// an empty Message means no reminder text was given. Callers re-ask the
// user for whatever is missing.
type Result struct {
	Message  string
	RemindAt time.Time
}

// Extractor extracts reminder details from a chat message.
type Extractor interface {
	Extract(ctx context.Context, text string, now time.Time) (Result, error)
}

const requestTimeout = 10 * time.Second

func instruction(now time.Time) string {
	return fmt.Sprintf(`You extract reminder details from a chat message (Thai or English).
Current date-time: %s (%s).
Reply with ONLY a JSON object, no markdown, no explanation:
{"message": "<what to remind, in the user's own words, or null>",
 "remind_at": "<ISO 8601 with +07:00 offset, or null if no time found>"}
Resolve relative times against the current date-time ("พรุ่งนี้ 9 โมง" = tomorrow 09:00,
"ตอนเย็น" = 18:00, "9 โมง" with no meridiem = 09:00). If the time already passed today
and no date was given, assume tomorrow.`,
		now.Format(time.RFC3339), now.Weekday())
}

// parse turns the LLM's raw reply into a Result. Fences and surrounding
// prose are tolerated; a null/absent field comes back zero-valued.
func parse(raw string) (Result, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	// Some models wrap the JSON in prose; cut to the outermost braces.
	if start := strings.Index(cleaned, "{"); start >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end > start {
			cleaned = cleaned[start : end+1]
		}
	}

	var payload struct {
		Message  *string `json:"message"`
		RemindAt *string `json:"remind_at"`
	}
	if err := json.Unmarshal([]byte(cleaned), &payload); err != nil {
		return Result{}, fmt.Errorf("parse extraction reply %q: %w", raw, err)
	}

	var result Result
	if payload.Message != nil {
		result.Message = strings.TrimSpace(*payload.Message)
	}
	if payload.RemindAt != nil && strings.TrimSpace(*payload.RemindAt) != "" {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(*payload.RemindAt))
		if err != nil {
			return Result{}, fmt.Errorf("parse remind_at %q: %w", *payload.RemindAt, err)
		}
		result.RemindAt = at
	}
	return result, nil
}

// Groq calls Groq's OpenAI-compatible chat completions endpoint.
type Groq struct {
	APIKey string
	Model  string
	Client *http.Client
}

func (g *Groq) Extract(ctx context.Context, text string, now time.Time) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"model":       g.Model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": instruction(now)},
			{"role": "user", "content": text},
		},
	})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.APIKey)

	resp, err := g.client().Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("groq extract: status %d: %s", resp.StatusCode, truncate(raw))
	}

	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return Result{}, fmt.Errorf("groq extract: decode: %w", err)
	}
	if len(reply.Choices) == 0 {
		return Result{}, fmt.Errorf("groq extract: empty choices")
	}
	return parse(reply.Choices[0].Message.Content)
}

func (g *Groq) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

// Gemini calls the generateContent REST endpoint (no SDK needed for one
// call).
type Gemini struct {
	APIKey string
	Model  string
	Client *http.Client
}

func (g *Gemini) Extract(ctx context.Context, text string, now time.Time) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"system_instruction": map[string]any{
			"parts": []map[string]string{{"text": instruction(now)}},
		},
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": text}}},
		},
		"generationConfig": map[string]any{"temperature": 0},
	})
	if err != nil {
		return Result{}, err
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", g.Model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey)

	resp, err := g.client().Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("gemini extract: status %d: %s", resp.StatusCode, truncate(raw))
	}

	var reply struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return Result{}, fmt.Errorf("gemini extract: decode: %w", err)
	}
	if len(reply.Candidates) == 0 || len(reply.Candidates[0].Content.Parts) == 0 {
		return Result{}, fmt.Errorf("gemini extract: empty candidates")
	}
	return parse(reply.Candidates[0].Content.Parts[0].Text)
}

func (g *Gemini) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

func truncate(b []byte) string {
	const max = 300
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}
