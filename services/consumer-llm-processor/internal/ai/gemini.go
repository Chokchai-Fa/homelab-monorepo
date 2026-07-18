package ai

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"consumer-llm-processor/internal/store"
)

// Gemini generates chat replies using the Gemini API.
type Gemini struct {
	client   *genai.Client
	name     string
	model    string
	system   string
	thinking genai.ThinkingLevel
}

// New creates a Gemini client for the given model (e.g. gemini-3.1-flash-lite)
// with the Mio persona and low thinking for chat latency.
func New(ctx context.Context, apiKey, model string) (*Gemini, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}
	return &Gemini{
		client:   client,
		name:     "gemini/" + model,
		model:    model,
		system:   PersonaInstruction,
		thinking: genai.ThinkingLevelLow,
	}, nil
}

// Derive returns a copy with a different name, system instruction and
// thinking depth, sharing the underlying API client. Used for the deep
// thinking technical tier and the tier classifier.
func (g *Gemini) Derive(name, system string, deepThinking bool) *Gemini {
	thinking := genai.ThinkingLevelLow
	if deepThinking {
		thinking = genai.ThinkingLevelHigh
	}
	return &Gemini{
		client:   g.client,
		name:     name,
		model:    g.model,
		system:   system,
		thinking: thinking,
	}
}

func (g *Gemini) Name() string { return g.name }

// Reply sends the conversation history plus the new user message and returns
// the model's answer.
func (g *Gemini) Reply(ctx context.Context, history []store.Message, userMessage string) (string, error) {
	contents := make([]*genai.Content, 0, len(history)+1)
	for _, m := range history {
		contents = append(contents, genai.NewContentFromText(m.Content, genai.Role(m.Role)))
	}
	contents = append(contents, genai.NewContentFromText(userMessage, genai.RoleUser))

	resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(g.system, genai.RoleUser),
		// Gemini 3.5 guidance: steer latency with thinking level, leave
		// temperature/top_p at their defaults.
		ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: g.thinking},
	})
	if err != nil {
		return "", fmt.Errorf("generate content: %w", err)
	}

	text := resp.Text()
	if text == "" {
		return "", fmt.Errorf("empty response from model")
	}
	return text, nil
}
