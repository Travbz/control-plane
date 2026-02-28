// Package memory defines the interface for agent memory persistence.
// Memory allows agents to retain context across jobs for a given customer.
package memory

import "time"

// Message represents a single conversational message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Conversation is a recorded exchange from a previous job.
type Conversation struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customer_id"`
	JobID      string    `json:"job_id"`
	Messages   []Message `json:"messages"`
	CreatedAt  time.Time `json:"created_at"`
}

// Fact is an extracted piece of knowledge with a vector embedding.
type Fact struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customer_id"`
	Content    string    `json:"content"`
	Embedding  []float32 `json:"-"` // not serialized in JSON responses
	CreatedAt  time.Time `json:"created_at"`
}

// Store defines the interface for memory persistence. Implementations
// handle the underlying storage (SQLite for dev/Pi, Postgres for cloud).
type Store interface {
	// SaveConversation persists a conversation from a completed job.
	SaveConversation(customerID, jobID string, messages []Message) error

	// GetConversations retrieves recent conversations for a customer.
	GetConversations(customerID string, limit int) ([]Conversation, error)

	// SaveFact stores an extracted fact with its embedding vector.
	SaveFact(customerID, content string, embedding []float32) error

	// SearchFacts performs a semantic similarity search over stored facts.
	SearchFacts(customerID string, queryEmbedding []float32, topK int) ([]Fact, error)

	// Close releases any resources held by the store.
	Close() error
}
