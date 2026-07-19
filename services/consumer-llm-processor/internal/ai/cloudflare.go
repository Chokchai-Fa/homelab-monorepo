package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CloudflareImage generates images with a Cloudflare Workers AI text-to-image
// model (e.g. @cf/black-forest-labs/flux-1-schnell). Workers AI has its own
// free tier (10k neurons/day, ~230 flux-1-schnell images), independent of the
// Gemini key.
type CloudflareImage struct {
	accountID string
	apiToken  string
	model     string
	client    *http.Client
}

func NewCloudflareImage(accountID, apiToken, model string) *CloudflareImage {
	return &CloudflareImage{
		accountID: accountID,
		apiToken:  apiToken,
		model:     model,
		client:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *CloudflareImage) Name() string { return "cloudflare-image/" + c.model }

type cfImageResponse struct {
	Result struct {
		Image string `json:"image"`
	} `json:"result"`
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Generate asks Workers AI for an image. flux-1-schnell answers JSON with a
// base64 image; older models (stable diffusion) answer raw image bytes - both
// are handled. Like GeminiImage, the result is re-encoded as JPEG for LINE's
// 1MB preview cap. Workers AI models return no caption.
func (c *CloudflareImage) Generate(ctx context.Context, prompt string) ([]byte, string, string, error) {
	// flux-1-schnell rejects prompts over 2048 chars.
	if len(prompt) > 2048 {
		prompt = prompt[:2048]
	}
	body, err := json.Marshal(map[string]string{"prompt": prompt})
	if err != nil {
		return nil, "", "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", c.accountID, c.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("call cloudflare: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "", "", fmt.Errorf("read cloudflare response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("cloudflare returned status %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}

	var data []byte
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "image/") {
		data = respBody
	} else {
		var parsed cfImageResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return nil, "", "", fmt.Errorf("decode cloudflare response: %w", err)
		}
		if !parsed.Success || parsed.Result.Image == "" {
			msg := "no image in response"
			if len(parsed.Errors) > 0 {
				msg = parsed.Errors[0].Message
			}
			return nil, "", "", fmt.Errorf("cloudflare image generation failed: %s", msg)
		}
		data, err = base64.StdEncoding.DecodeString(parsed.Result.Image)
		if err != nil {
			return nil, "", "", fmt.Errorf("decode cloudflare image: %w", err)
		}
	}

	if jpg, err := reencodeJPEG(data); err == nil {
		return jpg, "image/jpeg", "", nil
	}
	return data, http.DetectContentType(data), "", nil
}
