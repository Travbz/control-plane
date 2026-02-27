// Package config handles parsing of sandbox.toml configuration files.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the top-level sandbox configuration.
type Config struct {
	// SandboxMode selects the provisioner backend: "docker" or "unikraft".
	SandboxMode string `toml:"sandbox_mode"`

	// Image is the sandbox container/VM image reference.
	Image string `toml:"image"`

	// Proxy configures the LLM proxy.
	Proxy ProxyConfig `toml:"proxy"`

	// Agent configures the agent running inside the sandbox.
	Agent AgentConfig `toml:"agent"`

	// Secrets defines credentials with their injection mode.
	Secrets map[string]SecretConfig `toml:"secrets"`

	// SharedDirs defines host directories to mount into the sandbox.
	SharedDirs []SharedDir `toml:"shared_dirs"`
}

// ProxyConfig configures the LLM proxy.
type ProxyConfig struct {
	// Addr is the listen address for the LLM proxy (default: ":8090").
	Addr string `toml:"addr"`
}

// AgentConfig configures the agent inside the sandbox.
type AgentConfig struct {
	// Command is the agent binary to exec (e.g. "claude", "opencode").
	Command string `toml:"command"`

	// Args are additional arguments passed to the agent command.
	Args []string `toml:"args"`

	// User is the unprivileged user the agent runs as (default: "agent").
	User string `toml:"user"`

	// Workdir is the working directory inside the sandbox (default: "/workspace").
	Workdir string `toml:"workdir"`
}

// SecretConfig defines a single secret and how it's injected.
type SecretConfig struct {
	// Mode is "proxy" or "inject".
	// - "proxy": control plane proxies requests via llm-proxy with a session token.
	// - "inject": real value is injected as an env var into the sandbox.
	Mode string `toml:"mode"`

	// EnvVar is the environment variable name inside the sandbox.
	// For "proxy" mode, this receives a session token (or base URL).
	// For "inject" mode, this receives the real secret value.
	EnvVar string `toml:"env_var"`

	// Provider is the LLM provider name (only for mode="proxy").
	// One of: "anthropic", "openai", "ollama".
	Provider string `toml:"provider,omitempty"`

	// UpstreamURL is an optional override for the provider API URL.
	UpstreamURL string `toml:"upstream_url,omitempty"`
}

// SharedDir defines a host directory to mount into the sandbox.
type SharedDir struct {
	// HostPath is the path on the host.
	HostPath string `toml:"host_path"`

	// GuestPath is the mount point inside the sandbox.
	GuestPath string `toml:"guest_path"`

	// ReadOnly makes the mount read-only (default: false).
	ReadOnly bool `toml:"read_only,omitempty"`
}

// Load reads and parses a sandbox.toml configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// validate checks the configuration for required fields and valid values.
func (c *Config) validate() error {
	switch c.SandboxMode {
	case "docker", "unikraft":
		// valid
	case "":
		return fmt.Errorf("sandbox_mode is required (docker or unikraft)")
	default:
		return fmt.Errorf("unknown sandbox_mode: %q (must be docker or unikraft)", c.SandboxMode)
	}

	if c.Image == "" {
		return fmt.Errorf("image is required")
	}

	if c.Agent.Command == "" {
		return fmt.Errorf("agent.command is required")
	}

	for name, secret := range c.Secrets {
		switch secret.Mode {
		case "proxy", "inject":
			// valid
		case "":
			return fmt.Errorf("secret %q: mode is required (proxy or inject)", name)
		default:
			return fmt.Errorf("secret %q: unknown mode %q (must be proxy or inject)", name, secret.Mode)
		}

		if secret.EnvVar == "" {
			return fmt.Errorf("secret %q: env_var is required", name)
		}

		if secret.Mode == "proxy" && secret.Provider == "" {
			return fmt.Errorf("secret %q: provider is required for mode=proxy", name)
		}
	}

	return nil
}
