// Package secrets defines the Store interface for secret management and
// provides concrete implementations for different backends.
package secrets

// Store defines the interface for secret management. Implementations handle
// the underlying storage mechanism (file, environment, cloud vault, etc.).
type Store interface {
	// Get retrieves a secret value by name. Returns an error if not found.
	Get(name string) (string, error)

	// Set stores a secret value. Creates it if it doesn't exist, overwrites if it does.
	Set(name, value string) error

	// Delete removes a secret by name.
	Delete(name string) error

	// List returns all secret names (not values).
	List() ([]string, error)
}
