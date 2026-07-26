package knowledge

import "context"

// StoredDoc is a document plus its embedding, ready to upsert.
type StoredDoc struct {
	Document
	Hash      string
	Embedding []float32
}

// Match is one retrieval hit: the document and its cosine distance to the
// query (0 = identical, 2 = opposite). Smaller is more relevant.
type Match struct {
	Document
	Distance float64
}

// VectorStore persists document embeddings and does nearest-neighbour search.
// The pgvector implementation is the production one; tests use a fake.
type VectorStore interface {
	// Hashes returns the stored content hash per document ID, so ingestion can
	// skip documents that haven't changed.
	Hashes(ctx context.Context) (map[string]string, error)
	// Upsert inserts or updates documents (with their embeddings).
	Upsert(ctx context.Context, docs []StoredDoc) error
	// Search returns the nearest documents to queryVec, closest first.
	Search(ctx context.Context, queryVec []float32, topK int) ([]Match, error)
}
