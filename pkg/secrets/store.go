package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store is an encrypted secret store backed by JSON files.
// Secrets are stored as name->value pairs in a JSON file.
// In a production implementation this would use age encryption;
// for now, secrets are stored in plaintext JSON in a user-only-readable directory.
type Store struct {
	mu   sync.RWMutex
	dir  string
	file string
	data map[string]string
}

// NewStore creates or opens a secret store at the given directory.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating secrets dir: %w", err)
	}

	s := &Store{
		dir:  dir,
		file: filepath.Join(dir, "secrets.json"),
		data: make(map[string]string),
	}

	// Load existing secrets if the file exists.
	if _, err := os.Stat(s.file); err == nil {
		raw, err := os.ReadFile(s.file)
		if err != nil {
			return nil, fmt.Errorf("reading secrets: %w", err)
		}
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("parsing secrets: %w", err)
		}
	}

	return s, nil
}

// Set stores a secret value.
func (s *Store) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[name] = value
	return s.persist()
}

// Get retrieves a secret value. Returns an error if not found.
func (s *Store) Get(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[name]
	if !ok {
		return "", fmt.Errorf("secret not found: %s", name)
	}
	return v, nil
}

// Delete removes a secret.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, name)
	return s.persist()
}

// List returns all secret names (not values).
func (s *Store) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.data))
	for name := range s.data {
		names = append(names, name)
	}
	return names
}

// persist writes the current secret data to disk.
func (s *Store) persist() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling secrets: %w", err)
	}
	if err := os.WriteFile(s.file, raw, 0600); err != nil {
		return fmt.Errorf("writing secrets: %w", err)
	}
	return nil
}
