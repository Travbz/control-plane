// Package customer manages customer profiles for personalization and
// per-customer configuration (secret providers, default tools, system prompts).
package customer

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Profile defines per-customer configuration.
type Profile struct {
	// CustomerID is the unique identifier for this customer.
	CustomerID string `json:"customer_id"`

	// SystemPromptAdditions is appended to the base system prompt for this
	// customer's jobs. Used for personalization (e.g. "Always respond in Spanish").
	SystemPromptAdditions string `json:"system_prompt_additions,omitempty"`

	// DefaultTools is the list of tool names this customer's jobs get by default.
	DefaultTools []string `json:"default_tools,omitempty"`

	// MemoryEnabled controls whether the agent retrieves memories for this customer.
	MemoryEnabled bool `json:"memory_enabled"`

	// SecretsProvider configures where this customer's secrets are stored.
	SecretsProvider *SecretsProviderConfig `json:"secrets_provider,omitempty"`

	// MaxConcurrentJobs limits how many jobs can run simultaneously. 0 = unlimited.
	MaxConcurrentJobs int `json:"max_concurrent_jobs,omitempty"`

	// TokenBudget is the monthly token budget. 0 = unlimited.
	TokenBudget int64 `json:"token_budget,omitempty"`
}

// SecretsProviderConfig tells the control plane how to fetch this
// customer's secrets at job time.
type SecretsProviderConfig struct {
	// Type is the provider type: "aws_sm", "vault", "env".
	Type string `json:"type"`

	// Region is the AWS region (for aws_sm).
	Region string `json:"region,omitempty"`

	// RoleARN is the IAM role to assume (for aws_sm).
	RoleARN string `json:"role_arn,omitempty"`

	// Addr is the Vault address (for vault).
	Addr string `json:"addr,omitempty"`

	// MountPath is the Vault secrets mount (for vault).
	MountPath string `json:"mount_path,omitempty"`
}

// Store manages customer profiles. Thread-safe.
type Store struct {
	mu       sync.RWMutex
	profiles map[string]*Profile
	filePath string
}

// NewStore creates a profile store. If filePath is provided, profiles are
// persisted to disk as JSON. Otherwise, they're in-memory only.
func NewStore(filePath string) (*Store, error) {
	s := &Store{
		profiles: make(map[string]*Profile),
		filePath: filePath,
	}

	if filePath != "" {
		if err := s.load(); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// Get retrieves a customer profile.
func (s *Store) Get(customerID string) (*Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.profiles[customerID]
	if !ok {
		return nil, fmt.Errorf("customer not found: %s", customerID)
	}
	return p, nil
}

// Set creates or updates a customer profile.
func (s *Store) Set(p *Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.profiles[p.CustomerID] = p
	return s.persist()
}

// Delete removes a customer profile.
func (s *Store) Delete(customerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.profiles, customerID)
	return s.persist()
}

// List returns all customer profiles.
func (s *Store) List() []*Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		result = append(result, p)
	}
	return result
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if os.IsNotExist(err) {
		return nil // No file yet, start empty.
	}
	if err != nil {
		return fmt.Errorf("reading profiles: %w", err)
	}

	var profiles []*Profile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return fmt.Errorf("parsing profiles: %w", err)
	}

	for _, p := range profiles {
		s.profiles[p.CustomerID] = p
	}
	return nil
}

func (s *Store) persist() error {
	if s.filePath == "" {
		return nil
	}

	profiles := make([]*Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		profiles = append(profiles, p)
	}

	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling profiles: %w", err)
	}

	return os.WriteFile(s.filePath, data, 0600)
}
