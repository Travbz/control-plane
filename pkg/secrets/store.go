package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore is a secret store backed by a plaintext JSON file in a
// user-only-readable directory. Implements the Store interface.
// In a production deployment, consider using EnvStore, AWSStore, or
// DelegatedStore instead.
type FileStore struct {
	mu   sync.RWMutex
	dir  string
	file string
	data map[string]string
}

// NewFileStore creates or opens a file-backed secret store at the given directory.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating secrets dir: %w", err)
	}

	s := &FileStore{
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

// Get retrieves a secret value. Returns an error if not found.
func (s *FileStore) Get(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[name]
	if !ok {
		return "", fmt.Errorf("secret not found: %s", name)
	}
	return v, nil
}

// Set stores a secret value.
func (s *FileStore) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[name] = value
	return s.persist()
}

// Delete removes a secret.
func (s *FileStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, name)
	return s.persist()
}

// List returns all secret names (not values).
func (s *FileStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.data))
	for name := range s.data {
		names = append(names, name)
	}
	return names, nil
}

// persist writes the current secret data to disk.
func (s *FileStore) persist() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling secrets: %w", err)
	}
	if err := os.WriteFile(s.file, raw, 0600); err != nil {
		return fmt.Errorf("writing secrets: %w", err)
	}
	return nil
}
