package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"consumer-llm-processor/internal/store"
)

// OpenAI calls any OpenAI-compatible chat completions API (Groq, OpenRouter,
// Mistral, Cerebras, ...) selected by base URL.
type OpenAI struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	system  string
	vision  bool
	client  *http.Client
}

// NewOpenAI creates a client for an OpenAI-compatible provider. baseURL is
// the prefix before /chat/completions, e.g. https://api.groq.com/openai/v1.
// vision must only be true if model actually accepts image inputs.
func NewOpenAI(name, baseURL, apiKey, model, system string, vision bool) *OpenAI {
	return &OpenAI{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		system:  system,
		vision:  vision,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (o *OpenAI) Name() string { return o.name + "/" + o.model }

// chatMessage's Content is either a plain string or, for a multimodal
// request, a []contentPart - hence the any and the custom marshaling below.
type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Reply sends the conversation history plus the new user message (and an
// optional attached image, sent as a base64 data URL per the OpenAI image
// content-part convention) and returns the model's answer.
func (o *OpenAI) Reply(ctx context.Context, history []store.Message, userMessage string, image *Image) (string, error) {
	if image != nil && !o.vision {
		return "", fmt.Errorf("%s does not support image input", o.Name())
	}

	messages := make([]chatMessage, 0, len(history)+2)
	messages = append(messages, chatMessage{Role: "system", Content: o.system})
	for _, m := range history {
		role := m.Role
		if role == store.RoleModel {
			role = "assistant"
		}
		messages = append(messages, chatMessage{Role: role, Content: m.Content})
	}

	if image != nil {
		parts := []contentPart{{
			Type:     "image_url",
			ImageURL: &imageURL{URL: fmt.Sprintf("data:%s;base64,%s", image.MimeType, base64.StdEncoding.EncodeToString(image.Data))},
		}}
		if userMessage != "" {
			parts = append(parts, contentPart{Type: "text", Text: userMessage})
		}
		messages = append(messages, chatMessage{Role: "user", Content: parts})
	} else {
		messages = append(messages, chatMessage{Role: "user", Content: userMessage})
	}

	body, err := json.Marshal(chatRequest{Model: o.model, Messages: messages})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call %s: %w", o.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read %s response: %w", o.name, err)
	}
	// Surface the status code so rate limits (429) are visible in logs and
	// trigger the router's fallback to the next provider.
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned status %d: %s", o.name, resp.StatusCode, truncate(string(respBody), 300))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode %s response: %w", o.name, err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty response from %s", o.Name())
	}
	return parsed.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
