package knowledge

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGVectorStore stores document embeddings in a pgvector column and does
// cosine-distance nearest-neighbour search. It shares the same Postgres the
// conversation store uses.
type PGVectorStore struct {
	pool *pgxpool.Pool
	dim  int
}

// NewPGVectorStore connects, ensures the pgvector extension + table exist for
// the given embedding dimension, and returns the store. It fails (rather than
// silently degrading) so the caller can log and disable RAG when pgvector is
// unavailable - e.g. the extension isn't installed in the Postgres image.
func NewPGVectorStore(ctx context.Context, databaseURL string, dim int) (*PGVectorStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	// The extension must be available in the image (pgvector/pgvector) and the
	// connecting role must be allowed to create it.
	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create vector extension (is pgvector installed?): %w", err)
	}
	ddl := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS portfolio_knowledge (
	id           text PRIMARY KEY,
	source       text        NOT NULL,
	title        text        NOT NULL,
	content      text        NOT NULL,
	content_hash text        NOT NULL,
	embedding    vector(%d)  NOT NULL,
	updated_at   timestamptz NOT NULL DEFAULT now()
);`, dim)
	if _, err := pool.Exec(ctx, ddl); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensure knowledge table: %w", err)
	}
	return &PGVectorStore{pool: pool, dim: dim}, nil
}

func (s *PGVectorStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *PGVectorStore) Hashes(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, content_hash FROM portfolio_knowledge`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, err
		}
		out[id] = hash
	}
	return out, rows.Err()
}

func (s *PGVectorStore) Upsert(ctx context.Context, docs []StoredDoc) error {
	if len(docs) == 0 {
		return nil
	}
	batch := make([]struct {
		d   StoredDoc
		vec string
	}, 0, len(docs))
	for _, d := range docs {
		if len(d.Embedding) != s.dim {
			return fmt.Errorf("doc %q: embedding dim %d != store dim %d", d.ID, len(d.Embedding), s.dim)
		}
		batch = append(batch, struct {
			d   StoredDoc
			vec string
		}{d, vectorLiteral(d.Embedding)})
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, b := range batch {
		if _, err := tx.Exec(ctx, `
INSERT INTO portfolio_knowledge (id, source, title, content, content_hash, embedding, updated_at)
VALUES ($1, $2, $3, $4, $5, $6::vector, now())
ON CONFLICT (id) DO UPDATE SET
	source = EXCLUDED.source,
	title = EXCLUDED.title,
	content = EXCLUDED.content,
	content_hash = EXCLUDED.content_hash,
	embedding = EXCLUDED.embedding,
	updated_at = now()`,
			b.d.ID, b.d.Source, b.d.Title, b.d.Content, b.d.Hash, b.vec); err != nil {
			return fmt.Errorf("upsert doc %q: %w", b.d.ID, err)
		}
	}
	return tx.Commit(ctx)
}

func (s *PGVectorStore) Search(ctx context.Context, queryVec []float32, topK int) ([]Match, error) {
	rows, err := s.pool.Query(ctx, `
SELECT source, title, content, embedding <=> $1::vector AS distance
FROM portfolio_knowledge
ORDER BY distance ASC
LIMIT $2`, vectorLiteral(queryVec), topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var matches []Match
	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.Source, &m.Title, &m.Content, &m.Distance); err != nil {
			return nil, err
		}
		matches = append(matches, m)
	}
	return matches, rows.Err()
}

// vectorLiteral renders a float slice as a pgvector text literal, e.g.
// "[0.1,0.2,0.3]", to bind as a $n::vector parameter without a pgvector-go
// dependency.
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
