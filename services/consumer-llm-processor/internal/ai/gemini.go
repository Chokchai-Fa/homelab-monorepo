package ai

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"consumer-llm-processor/internal/store"
)

const systemInstruction = `You are a helpful assistant chatting with users on the LINE messaging app.
Always answer in the same language the user writes in (Thai, English, or any other language).
Keep replies concise and chat-friendly: plain text only, no markdown formatting.`

// Gemini generates chat replies using the Gemini API.
type Gemini struct {
	client *genai.Client
	model  string
}

// New creates a Gemini client for the given model (e.g. gemini-3.5-flash).
func New(ctx context.Context, apiKey, model string) (*Gemini, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}
	return &Gemini{client: client, model: model}, nil
}

// Reply sends the conversation history plus the new user message and returns
// the model's answer.
func (g *Gemini) Reply(ctx context.Context, history []store.Message, userMessage string) (string, error) {
	contents := make([]*genai.Content, 0, len(history)+1)
	for _, m := range history {
		contents = append(contents, genai.NewContentFromText(m.Content, genai.Role(m.Role)))
	}
	contents = append(contents, genai.NewContentFromText(userMessage, genai.RoleUser))

	resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemInstruction, genai.RoleUser),
		// Gemini 3.5 guidance: steer latency with thinking level, leave
		// temperature/top_p at their defaults.
		ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow},
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
