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

// Reply sends the conversation history plus the new user message (and an
// optional attached image, which Gemini models handle natively) and returns
// the model's answer.
func (g *Gemini) Reply(ctx context.Context, history []store.Message, userMessage string, image *Image) (string, error) {
	contents := g.buildContents(history, userMessage, image)

	resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, g.genConfig())
	if err != nil {
		return "", fmt.Errorf("generate content: %w", err)
	}

	text := resp.Text()
	if text == "" {
		return "", fmt.Errorf("empty response from model")
	}
	return text, nil
}

// ReplyStream streams the answer token-by-token, calling emit with each
// delta, and returns the full concatenated text. It satisfies StreamProvider.
func (g *Gemini) ReplyStream(ctx context.Context, history []store.Message, userMessage string, image *Image, emit func(delta string) error) (string, error) {
	contents := g.buildContents(history, userMessage, image)

	var full string
	for resp, err := range g.client.Models.GenerateContentStream(ctx, g.model, contents, g.genConfig()) {
		if err != nil {
			// Fail with what we have so the router can decide whether it is
			// safe to fall back (only when nothing was emitted yet).
			return full, fmt.Errorf("generate content stream: %w", err)
		}
		delta := resp.Text()
		if delta == "" {
			continue
		}
		full += delta
		if err := emit(delta); err != nil {
			return full, err
		}
	}
	if full == "" {
		return "", fmt.Errorf("empty response from model")
	}
	return full, nil
}

// buildContents assembles the genai contents from history + the new message
// (plus an optional image), shared by Reply and ReplyStream.
func (g *Gemini) buildContents(history []store.Message, userMessage string, image *Image) []*genai.Content {
	contents := make([]*genai.Content, 0, len(history)+1)
	for _, m := range history {
		contents = append(contents, genai.NewContentFromText(m.Content, genai.Role(m.Role)))
	}
	if image != nil {
		parts := []*genai.Part{genai.NewPartFromBytes(image.Data, image.MimeType)}
		if userMessage != "" {
			parts = append(parts, genai.NewPartFromText(userMessage))
		}
		contents = append(contents, genai.NewContentFromParts(parts, genai.RoleUser))
	} else {
		contents = append(contents, genai.NewContentFromText(userMessage, genai.RoleUser))
	}
	return contents
}

func (g *Gemini) genConfig() *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(g.system, genai.RoleUser),
		// Gemini 3.5 guidance: steer latency with thinking level, leave
		// temperature/top_p at their defaults.
		ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: g.thinking},
	}
}
