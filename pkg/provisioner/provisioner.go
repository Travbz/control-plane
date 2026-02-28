// Package provisioner defines the interface for sandbox provisioning backends
// and common types shared between Docker and Unikraft implementations.
package provisioner

import "context"

// Sandbox represents a running sandbox instance.
type Sandbox struct {
	// ID is the sandbox identifier (container ID or VM ID).
	ID string

	// Name is the human-readable sandbox name.
	Name string

	// Status is the current sandbox status (running, stopped, etc.).
	Status string

	// IP is the sandbox's IP address (if available).
	IP string
}

// CreateOpts configures sandbox creation.
type CreateOpts struct {
	// Name is the desired sandbox name.
	Name string

	// Image is the container/VM image reference.
	Image string

	// Env is a map of environment variables to inject.
	Env map[string]string

	// Mounts is a list of bind mounts (host:guest).
	Mounts []Mount

	// ProxyAddr is the address of the LLM proxy the sandbox should reach.
	// The provisioner ensures this is reachable from inside the sandbox.
	ProxyAddr string

	// Memory is the container memory limit (e.g. "512m", "1g"). Empty = no limit.
	Memory string

	// CPUs is the CPU limit (e.g. "0.5", "2"). Empty = no limit.
	CPUs string

	// NetworkID is a pre-created Docker network to attach to.
	// If empty, the container uses bridge mode.
	NetworkID string
}

// Mount defines a bind mount.
type Mount struct {
	HostPath  string
	GuestPath string
	ReadOnly  bool
}

// Provisioner is the interface that all sandbox backends implement.
type Provisioner interface {
	// Create provisions a new sandbox but does not start it.
	Create(ctx context.Context, opts CreateOpts) (*Sandbox, error)

	// Start starts a previously created sandbox.
	Start(ctx context.Context, id string) error

	// Stop stops a running sandbox.
	Stop(ctx context.Context, id string) error

	// Destroy removes a sandbox entirely.
	Destroy(ctx context.Context, id string) error

	// Status returns the current status of a sandbox.
	Status(ctx context.Context, id string) (*Sandbox, error)

	// List returns all sandboxes managed by this provisioner.
	List(ctx context.Context) ([]*Sandbox, error)
}
