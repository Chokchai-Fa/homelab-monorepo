package knowledge

import (
	"context"
	"fmt"

	"google.golang.org/genai"
)

// Task labels for the embedding model. Query and document embeddings are asked
// for with different task types so the model places a question near the
// passages that answer it.
const (
	taskDocument = "RETRIEVAL_DOCUMENT"
	taskQuery    = "RETRIEVAL_QUERY"
)

// Embedder turns text into vectors. EmbedDocuments and EmbedQuery are split so
// each can pass the right task type to the model.
type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	// Dim is the output dimensionality of the vectors, used to size the
	// pgvector column.
	Dim() int
}

// GeminiEmbedder embeds text with a Gemini embedding model.
type GeminiEmbedder struct {
	client *genai.Client
	model  string
	dim    int
}

// NewGeminiEmbedder creates an embedder. dim truncates the output embedding
// (Gemini embedding models support reduced dimensionality); it must match the
// pgvector column width.
func NewGeminiEmbedder(ctx context.Context, apiKey, model string, dim int) (*GeminiEmbedder, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create embedder client: %w", err)
	}
	return &GeminiEmbedder{client: client, model: model, dim: dim}, nil
}

func (e *GeminiEmbedder) Dim() int { return e.dim }

func (e *GeminiEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, texts, taskDocument)
}

func (e *GeminiEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.embed(ctx, []string{text}, taskQuery)
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embed query: expected 1 vector, got %d", len(vecs))
	}
	return vecs[0], nil
}

func (e *GeminiEmbedder) embed(ctx context.Context, texts []string, taskType string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	contents := make([]*genai.Content, 0, len(texts))
	for _, t := range texts {
		contents = append(contents, genai.NewContentFromText(t, genai.RoleUser))
	}
	dim := int32(e.dim)
	resp, err := e.client.Models.EmbedContent(ctx, e.model, contents, &genai.EmbedContentConfig{
		TaskType:             taskType,
		OutputDimensionality: &dim,
	})
	if err != nil {
		return nil, fmt.Errorf("embed content: %w", err)
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed content: expected %d embeddings, got %d", len(texts), len(resp.Embeddings))
	}
	out := make([][]float32, len(resp.Embeddings))
	for i, emb := range resp.Embeddings {
		if emb == nil || len(emb.Values) == 0 {
			return nil, fmt.Errorf("embed content: empty embedding at index %d", i)
		}
		out[i] = emb.Values
	}
	return out, nil
}
