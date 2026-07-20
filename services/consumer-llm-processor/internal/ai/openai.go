package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	Stream   bool          `json:"stream,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// streamChunk is one SSE `data:` frame of a streaming completion.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// Reply sends the conversation history plus the new user message (and an
// optional attached image, sent as a base64 data URL per the OpenAI image
// content-part convention) and returns the model's answer.
func (o *OpenAI) Reply(ctx context.Context, history []store.Message, userMessage string, image *Image) (string, error) {
	messages, err := o.buildMessages(history, userMessage, image)
	if err != nil {
		return "", err
	}

	resp, err := o.post(ctx, chatRequest{Model: o.model, Messages: messages})
	if err != nil {
		return "", err
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

// ReplyStream streams the answer via the OpenAI SSE protocol (stream:true),
// calling emit with each content delta and returning the full text. It
// satisfies StreamProvider.
func (o *OpenAI) ReplyStream(ctx context.Context, history []store.Message, userMessage string, image *Image, emit func(delta string) error) (string, error) {
	messages, err := o.buildMessages(history, userMessage, image)
	if err != nil {
		return "", err
	}

	resp, err := o.post(ctx, chatRequest{Model: o.model, Messages: messages, Stream: true})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read a bounded error body so a 429 falls back like the non-stream path.
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("%s returned status %d: %s", o.name, resp.StatusCode, truncate(string(errBody), 300))
	}

	var full string
	scanner := bufio.NewScanner(resp.Body)
	// A single SSE data line can exceed the default 64KB token cap on long
	// answers; give the scanner room.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		// SSE frames are "data: {json}"; blank lines and ":" comments separate
		// or keep-alive them.
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Skip frames we can't parse rather than aborting a good stream.
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		full += delta
		if err := emit(delta); err != nil {
			return full, err
		}
	}
	if err := scanner.Err(); err != nil {
		return full, fmt.Errorf("read %s stream: %w", o.name, err)
	}
	if full == "" {
		return "", fmt.Errorf("empty response from %s", o.Name())
	}
	return full, nil
}

// buildMessages assembles the OpenAI chat messages from history + the new
// user message (and an optional image), shared by Reply and ReplyStream.
func (o *OpenAI) buildMessages(history []store.Message, userMessage string, image *Image) ([]chatMessage, error) {
	if image != nil && !o.vision {
		return nil, fmt.Errorf("%s does not support image input", o.Name())
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
	return messages, nil
}

// post marshals and sends a chat completion request, returning the raw
// response for the caller to read (whole-body for Reply, streamed for
// ReplyStream).
func (o *OpenAI) post(ctx context.Context, cr chatRequest) (*http.Response, error) {
	body, err := json.Marshal(cr)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", o.name, err)
	}
	return resp, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
