package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeEmbedder returns a deterministic vector per text and records calls.
type fakeEmbedder struct {
	dim       int
	docCalls  int
	queryText string
	err       error
}

func (f *fakeEmbedder) Dim() int { return f.dim }

func (f *fakeEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	f.docCalls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, f.dim)
	}
	return out, nil
}

func (f *fakeEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	f.queryText = text
	if f.err != nil {
		return nil, f.err
	}
	return make([]float32, f.dim), nil
}

// fakeStore is an in-memory VectorStore recording upserts and returning canned
// search results.
type fakeStore struct {
	hashes      map[string]string
	upserted    []StoredDoc
	searchOut   []Match
	upsertErr   error
	searchErr   error
	hashesErr   error
	searchTopK  int
	searchCalls int
}

func (s *fakeStore) Hashes(context.Context) (map[string]string, error) {
	if s.hashesErr != nil {
		return nil, s.hashesErr
	}
	if s.hashes == nil {
		return map[string]string{}, nil
	}
	return s.hashes, nil
}

func (s *fakeStore) Upsert(_ context.Context, docs []StoredDoc) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserted = append(s.upserted, docs...)
	return nil
}

func (s *fakeStore) Search(_ context.Context, _ []float32, topK int) ([]Match, error) {
	s.searchCalls++
	s.searchTopK = topK
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.searchOut, nil
}

func TestDocumentsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Documents() {
		if d.ID == "" || d.Source == "" || d.Title == "" || d.Content == "" {
			t.Errorf("document %q has an empty field: %+v", d.ID, d)
		}
		if seen[d.ID] {
			t.Errorf("duplicate document ID %q", d.ID)
		}
		seen[d.ID] = true
	}
	// The identity chunk must carry the exact Thai name and the research chunk
	// the paper - the two facts visitors complained about.
	joined := ""
	for _, d := range Documents() {
		joined += d.Content
	}
	for _, want := range []string{"โชคชัย ฟ้ารุ่งสาง", "GitCoFL", "InCIT 2025"} {
		if !strings.Contains(joined, want) {
			t.Errorf("corpus missing required fact %q", want)
		}
	}
}

func TestIngestOnlyEmbedsChangedDocs(t *testing.T) {
	docs := Documents()
	// Pretend every doc but one is already stored with the current hash.
	existing := map[string]string{}
	for _, d := range docs[1:] {
		existing[d.ID] = d.Hash()
	}
	store := &fakeStore{hashes: existing}
	emb := &fakeEmbedder{dim: 8}
	r := NewRetriever(emb, store, 4, 0.6)

	n, err := r.Ingest(context.Background(), docs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 changed doc, got %d", n)
	}
	if len(store.upserted) != 1 || store.upserted[0].ID != docs[0].ID {
		t.Fatalf("expected only %q upserted, got %+v", docs[0].ID, store.upserted)
	}
}

func TestIngestNoChangesSkipsEmbedding(t *testing.T) {
	docs := Documents()
	existing := map[string]string{}
	for _, d := range docs {
		existing[d.ID] = d.Hash()
	}
	emb := &fakeEmbedder{dim: 8}
	r := NewRetriever(emb, &fakeStore{hashes: existing}, 4, 0.6)

	n, err := r.Ingest(context.Background(), docs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || emb.docCalls != 0 {
		t.Fatalf("expected no work, got n=%d docCalls=%d", n, emb.docCalls)
	}
}

func TestRetrieveFiltersByDistance(t *testing.T) {
	store := &fakeStore{searchOut: []Match{
		{Document: Document{ID: "a", Source: "résumé", Content: "close"}, Distance: 0.2},
		{Document: Document{ID: "b", Source: "résumé", Content: "far"}, Distance: 0.9},
	}}
	r := NewRetriever(&fakeEmbedder{dim: 8}, store, 4, 0.6)

	matches, err := r.Retrieve(context.Background(), "where does he work?")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].ID != "a" {
		t.Fatalf("expected only the close match, got %+v", matches)
	}
	if store.searchTopK != 4 {
		t.Fatalf("expected topK 4, got %d", store.searchTopK)
	}
}

func TestRetrieveContextFormatsWithCitations(t *testing.T) {
	store := &fakeStore{searchOut: []Match{
		{Document: Document{Source: "InCIT 2025 paper", Content: "GitCoFL is a federated learning framework."}, Distance: 0.1},
	}}
	r := NewRetriever(&fakeEmbedder{dim: 8}, store, 4, 0.6)

	ctxBlock := r.RetrieveContext(context.Background(), "what research has he done?")
	if !strings.Contains(ctxBlock, "[source: InCIT 2025 paper]") {
		t.Fatalf("expected source citation, got %q", ctxBlock)
	}
	if !strings.Contains(ctxBlock, "GitCoFL") {
		t.Fatalf("expected content, got %q", ctxBlock)
	}
}

func TestRetrieveContextEmptyOnErrorOrNoMatch(t *testing.T) {
	// Embedding error → best-effort empty context, not a failure.
	r1 := NewRetriever(&fakeEmbedder{dim: 8, err: errors.New("boom")}, &fakeStore{}, 4, 0.6)
	if got := r1.RetrieveContext(context.Background(), "hi"); got != "" {
		t.Fatalf("expected empty context on error, got %q", got)
	}
	// No matches → empty context.
	r2 := NewRetriever(&fakeEmbedder{dim: 8}, &fakeStore{searchOut: nil}, 4, 0.6)
	if got := r2.RetrieveContext(context.Background(), "hi"); got != "" {
		t.Fatalf("expected empty context on no match, got %q", got)
	}
}

func TestRetrieveEmptyQuery(t *testing.T) {
	emb := &fakeEmbedder{dim: 8}
	r := NewRetriever(emb, &fakeStore{}, 4, 0.6)
	matches, err := r.Retrieve(context.Background(), "   ")
	if err != nil || matches != nil {
		t.Fatalf("expected nil matches for blank query, got %+v / %v", matches, err)
	}
	if emb.queryText != "" {
		t.Fatal("must not embed a blank query")
	}
}

func TestVectorLiteral(t *testing.T) {
	got := vectorLiteral([]float32{0.5, -1, 0.25})
	if got != "[0.5,-1,0.25]" {
		t.Fatalf("unexpected literal %q", got)
	}
}
