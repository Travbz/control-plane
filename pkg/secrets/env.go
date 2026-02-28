package secrets

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// EnvStore is a read-only secret store that reads secrets from environment
// variables or a .env file. Useful for CI pipelines and simple deployments
// where secrets are injected by the runtime environment.
//
// Secret names are uppercased and prefixed with SECRET_ when looking up env
// vars. For example, Get("anthropic_key") checks SECRET_ANTHROPIC_KEY.
//
// If an env file path is provided, it is loaded once at construction time.
// The env file does NOT override existing environment variables.
type EnvStore struct {
	mu     sync.RWMutex
	data   map[string]string
	prefix string
}

// NewEnvStore creates an EnvStore. If envFile is non-empty, key-value pairs
// from that file are loaded (KEY=VALUE format, one per line, # comments).
// The prefix is prepended to secret names when looking up env vars (default: "SECRET_").
func NewEnvStore(envFile string, prefix string) (*EnvStore, error) {
	if prefix == "" {
		prefix = "SECRET_"
	}

	s := &EnvStore{
		data:   make(map[string]string),
		prefix: prefix,
	}

	// Load env file if provided.
	if envFile != "" {
		if err := s.loadEnvFile(envFile); err != nil {
			return nil, fmt.Errorf("loading env file: %w", err)
		}
	}

	return s, nil
}

// Get retrieves a secret. Checks the process environment first (prefixed),
// then falls back to values loaded from the env file.
func (s *EnvStore) Get(name string) (string, error) {
	// Check env var first: SECRET_ANTHROPIC_KEY for name "anthropic_key".
	envKey := s.prefix + strings.ToUpper(name)
	if v := os.Getenv(envKey); v != "" {
		return v, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[name]
	if !ok {
		return "", fmt.Errorf("secret not found: %s (checked env %s and env file)", name, envKey)
	}
	return v, nil
}

// Set stores a secret in the in-memory map. EnvStore does not persist to disk.
func (s *EnvStore) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[name] = value
	return nil
}

// Delete removes a secret from the in-memory map.
func (s *EnvStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, name)
	return nil
}

// List returns all known secret names from both the env file and any that
// were Set() at runtime.
func (s *EnvStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.data))
	for name := range s.data {
		names = append(names, name)
	}
	return names, nil
}

// loadEnvFile reads a KEY=VALUE file into the data map.
func (s *EnvStore) loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Strip surrounding quotes if present.
		value = strings.Trim(value, `"'`)

		s.data[key] = value
	}

	return scanner.Err()
}
