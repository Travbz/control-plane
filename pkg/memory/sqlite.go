package memory

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// SQLiteStore implements the Store interface using SQLite.
// Good for development and Raspberry Pi deployments where running
// a separate database server is overkill.
//
// Semantic search is brute-force cosine similarity — fine for small
// fact counts per customer (< 10k). For production scale, use
// PostgresStore with pgvector.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates a SQLite-backed memory store.
// The dsn is a file path (e.g. "memory.db") or ":memory:" for testing.
func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrating memory tables: %w", err)
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			id          TEXT PRIMARY KEY,
			customer_id TEXT NOT NULL,
			job_id      TEXT NOT NULL,
			messages    TEXT NOT NULL,
			created_at  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_conversations_customer ON conversations(customer_id);

		CREATE TABLE IF NOT EXISTS facts (
			id          TEXT PRIMARY KEY,
			customer_id TEXT NOT NULL,
			content     TEXT NOT NULL,
			embedding   TEXT NOT NULL,
			created_at  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_facts_customer ON facts(customer_id);
	`)
	return err
}

func (s *SQLiteStore) SaveConversation(customerID, jobID string, messages []Message) error {
	id := generateID()
	msgJSON, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("marshaling messages: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO conversations (id, customer_id, job_id, messages, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, customerID, jobID, string(msgJSON), time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) GetConversations(customerID string, limit int) ([]Conversation, error) {
	rows, err := s.db.Query(
		`SELECT id, customer_id, job_id, messages, created_at FROM conversations WHERE customer_id = ? ORDER BY created_at DESC LIMIT ?`,
		customerID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Conversation
	for rows.Next() {
		var c Conversation
		var msgJSON, createdAt string
		if err := rows.Scan(&c.ID, &c.CustomerID, &c.JobID, &msgJSON, &createdAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(msgJSON), &c.Messages)
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) SaveFact(customerID, content string, embedding []float32) error {
	id := generateID()
	embJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshaling embedding: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO facts (id, customer_id, content, embedding, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, customerID, content, string(embJSON), time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) SearchFacts(customerID string, queryEmbedding []float32, topK int) ([]Fact, error) {
	rows, err := s.db.Query(
		`SELECT id, customer_id, content, embedding, created_at FROM facts WHERE customer_id = ?`,
		customerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		fact  Fact
		score float64
	}

	var all []scored
	for rows.Next() {
		var f Fact
		var embJSON, createdAt string
		if err := rows.Scan(&f.ID, &f.CustomerID, &f.Content, &embJSON, &createdAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(embJSON), &f.Embedding)
		f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

		score := cosineSimilarity(queryEmbedding, f.Embedding)
		all = append(all, scored{fact: f, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending.
	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})

	if topK > len(all) {
		topK = len(all)
	}

	result := make([]Fact, topK)
	for i := 0; i < topK; i++ {
		result[i] = all[i].fact
	}
	return result, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
