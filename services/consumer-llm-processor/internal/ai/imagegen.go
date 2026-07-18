package ai

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // Nano Banana returns PNG; register decoder for re-encoding

	"google.golang.org/genai"
)

// ImageGenerator turns a text prompt into a picture. The router calls it for
// requests the classifier labels "image".
type ImageGenerator interface {
	Name() string
	Generate(ctx context.Context, prompt string) (data []byte, mimeType string, caption string, err error)
}

// GeminiImage generates images with a Gemini image model (Nano Banana line,
// e.g. gemini-2.5-flash-image), sharing the chat client's connection and the
// same free-tier API key.
type GeminiImage struct {
	client *genai.Client
	model  string
}

// NewGeminiImage derives an image generator from an existing Gemini chat
// client.
func NewGeminiImage(g *Gemini, model string) *GeminiImage {
	return &GeminiImage{client: g.client, model: model}
}

func (g *GeminiImage) Name() string { return "gemini-image/" + g.model }

// Generate asks the model for an image (plus any accompanying text, returned
// as the caption). The image is re-encoded as JPEG: LINE's preview URL caps
// out at 1MB and the model's native PNGs can exceed that.
func (g *GeminiImage) Generate(ctx context.Context, prompt string) ([]byte, string, string, error) {
	contents := []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)}
	resp, err := g.client.Models.GenerateContent(ctx, g.model, contents, &genai.GenerateContentConfig{
		ResponseModalities: []string{"IMAGE", "TEXT"},
	})
	if err != nil {
		return nil, "", "", fmt.Errorf("generate image: %w", err)
	}

	var data []byte
	var mime, caption string
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			switch {
			case part.InlineData != nil && data == nil:
				data = part.InlineData.Data
				mime = part.InlineData.MIMEType
			case part.Text != "":
				caption += part.Text
			}
		}
	}
	if data == nil {
		return nil, "", "", fmt.Errorf("model %s returned no image", g.model)
	}

	if jpg, err := reencodeJPEG(data); err == nil {
		data, mime = jpg, "image/jpeg"
	}
	return data, mime, caption, nil
}

func reencodeJPEG(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
