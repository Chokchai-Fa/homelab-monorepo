package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// Retriever ties an Embedder to a VectorStore: it ingests the corpus once at
// startup and, per query, returns a context block of the most relevant chunks
// to prepend to the prompt.
type Retriever struct {
	embedder Embedder
	store    VectorStore
	topK     int
	maxDist  float64
}

// NewRetriever builds a retriever. topK caps how many chunks are injected;
// maxDist drops chunks whose cosine distance exceeds it (<= 0 disables the
// filter) so an off-topic question doesn't pull in irrelevant facts.
func NewRetriever(embedder Embedder, store VectorStore, topK int, maxDist float64) *Retriever {
	if topK <= 0 {
		topK = 4
	}
	return &Retriever{embedder: embedder, store: store, topK: topK, maxDist: maxDist}
}

// Ingest embeds and upserts any documents whose content changed since the last
// run (idempotent via content hash). It returns how many were (re)embedded.
func (r *Retriever) Ingest(ctx context.Context, docs []Document) (int, error) {
	existing, err := r.store.Hashes(ctx)
	if err != nil {
		return 0, fmt.Errorf("load existing hashes: %w", err)
	}
	var changed []Document
	for _, d := range docs {
		if existing[d.ID] != d.Hash() {
			changed = append(changed, d)
		}
	}
	if len(changed) == 0 {
		return 0, nil
	}
	texts := make([]string, len(changed))
	for i, d := range changed {
		texts[i] = embedText(d)
	}
	vecs, err := r.embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("embed documents: %w", err)
	}
	if len(vecs) != len(changed) {
		return 0, fmt.Errorf("embed documents: got %d vectors for %d docs", len(vecs), len(changed))
	}
	stored := make([]StoredDoc, len(changed))
	for i, d := range changed {
		stored[i] = StoredDoc{Document: d, Hash: d.Hash(), Embedding: vecs[i]}
	}
	if err := r.store.Upsert(ctx, stored); err != nil {
		return 0, fmt.Errorf("upsert documents: %w", err)
	}
	return len(changed), nil
}

// Retrieve returns the relevant chunks for a query, closest first, filtered by
// the distance threshold.
func (r *Retriever) Retrieve(ctx context.Context, query string) ([]Match, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	vec, err := r.embedder.EmbedQuery(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	matches, err := r.store.Search(ctx, vec, r.topK)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if r.maxDist <= 0 {
		return matches, nil
	}
	kept := matches[:0]
	for _, m := range matches {
		if m.Distance <= r.maxDist {
			kept = append(kept, m)
		}
	}
	return kept, nil
}

// RetrieveContext returns a prompt context block for the query, or "" when
// nothing relevant is found. Retrieval is best-effort: any error degrades to
// no context (the persona's own facts still answer) rather than failing the
// chat. It satisfies webchat.ContextRetriever.
func (r *Retriever) RetrieveContext(ctx context.Context, query string) string {
	matches, err := r.Retrieve(ctx, query)
	if err != nil {
		log.Warn().Err(err).Msg("rag: retrieval failed - answering without retrieved context")
		return ""
	}
	if len(matches) == 0 {
		return ""
	}
	return formatContext(matches)
}

// embedText is the text embedded for a document: title plus content, so the
// heading contributes to the match.
func embedText(d Document) string {
	return d.Title + "\n" + d.Content
}

// formatContext renders retrieved chunks into a prompt preamble that asks the
// model to ground its answer and cite the source.
func formatContext(matches []Match) string {
	var b strings.Builder
	b.WriteString("Relevant facts about Chokchai (use them to answer and cite the source in parentheses, e.g. \"(source: résumé)\", when you use one):\n")
	for _, m := range matches {
		fmt.Fprintf(&b, "- [source: %s] %s\n", m.Source, m.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}
