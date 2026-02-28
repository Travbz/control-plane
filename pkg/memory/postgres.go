package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PostgresStore implements the Store interface using PostgreSQL with pgvector
// for efficient semantic similarity search. Recommended for cloud production.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore creates a Postgres-backed memory store.
// The db should be an open connection to a PostgreSQL database with
// the pgvector extension installed.
func NewPostgresStore(db *sql.DB) (*PostgresStore, error) {
	s := &PostgresStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrating memory tables: %w", err)
	}
	return s, nil
}

func (s *PostgresStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE EXTENSION IF NOT EXISTS vector;

		CREATE TABLE IF NOT EXISTS conversations (
			id          TEXT PRIMARY KEY,
			customer_id TEXT NOT NULL,
			job_id      TEXT NOT NULL,
			messages    JSONB NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_conversations_customer ON conversations(customer_id);

		CREATE TABLE IF NOT EXISTS facts (
			id          TEXT PRIMARY KEY,
			customer_id TEXT NOT NULL,
			content     TEXT NOT NULL,
			embedding   vector(1536),
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_facts_customer ON facts(customer_id);
	`)
	return err
}

func (s *PostgresStore) SaveConversation(customerID, jobID string, messages []Message) error {
	id := generateID()
	msgJSON, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("marshaling messages: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO conversations (id, customer_id, job_id, messages) VALUES ($1, $2, $3, $4)`,
		id, customerID, jobID, string(msgJSON),
	)
	return err
}

func (s *PostgresStore) GetConversations(customerID string, limit int) ([]Conversation, error) {
	rows, err := s.db.Query(
		`SELECT id, customer_id, job_id, messages, created_at FROM conversations WHERE customer_id = $1 ORDER BY created_at DESC LIMIT $2`,
		customerID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Conversation
	for rows.Next() {
		var c Conversation
		var msgJSON string
		if err := rows.Scan(&c.ID, &c.CustomerID, &c.JobID, &msgJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(msgJSON), &c.Messages)
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *PostgresStore) SaveFact(customerID, content string, embedding []float32) error {
	id := generateID()
	embStr := vectorToString(embedding)

	_, err := s.db.Exec(
		`INSERT INTO facts (id, customer_id, content, embedding) VALUES ($1, $2, $3, $4::vector)`,
		id, customerID, content, embStr,
	)
	return err
}

func (s *PostgresStore) SearchFacts(customerID string, queryEmbedding []float32, topK int) ([]Fact, error) {
	embStr := vectorToString(queryEmbedding)

	rows, err := s.db.Query(
		`SELECT id, customer_id, content, created_at
		 FROM facts
		 WHERE customer_id = $1
		 ORDER BY embedding <=> $2::vector
		 LIMIT $3`,
		customerID, embStr, topK,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Fact
	for rows.Next() {
		var f Fact
		if err := rows.Scan(&f.ID, &f.CustomerID, &f.Content, &f.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// vectorToString converts a float32 slice to the pgvector string format: "[0.1,0.2,0.3]"
func vectorToString(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// vectorFromString parses a pgvector string "[0.1,0.2,0.3]" to float32 slice.
// Currently unused but available for future use.
func vectorFromString(s string) ([]float32, error) {
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	result := make([]float32, len(parts))
	for i, p := range parts {
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%f", &f); err != nil {
			return nil, fmt.Errorf("parsing vector element %d: %w", i, err)
		}
		result[i] = float32(f)
	}
	return result, nil
}

// Ensure PostgresStore.SaveConversation sets time properly
func init() {
	_ = time.Now // reference time package to avoid unused import
}
