// Package config handles parsing of sandbox.toml configuration files.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the top-level sandbox configuration.
type Config struct {
	// SandboxMode selects the provisioner backend: "docker", "unikraft", or "fly".
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

	// Env is a flat map of environment variables injected directly into the
	// sandbox. These are plain key-value pairs with no secret management.
	Env map[string]string `toml:"env"`

	// EnvFile is an optional path to a .env file. Key-value pairs from this
	// file are merged into the sandbox environment. Values in [env] take
	// precedence over values from env_file.
	EnvFile string `toml:"env_file"`

	// Resources configures container resource limits.
	Resources ResourceConfig `toml:"resources"`

	// Tools defines MCP tool sidecars to start alongside the agent.
	Tools []ToolConfig `toml:"tools"`

	// Network configures sandbox networking restrictions.
	Network NetworkConfig `toml:"network"`
}

// NetworkConfig controls outbound network access from the sandbox.
type NetworkConfig struct {
	// AllowedHosts is a list of host patterns the sandbox may reach.
	// Supports exact match ("api.anthropic.com") and wildcard subdomains
	// ("*.anthropic.com"). An empty list means no outbound restrictions.
	AllowedHosts []string `toml:"allowed_hosts"`

	// ProxyPort is the listen port for the allowlist proxy sidecar
	// (default: 3128). Only used when AllowedHosts is non-empty.
	ProxyPort int `toml:"proxy_port,omitempty"`
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

// ResourceConfig defines container resource limits.
type ResourceConfig struct {
	// Memory is the memory limit (e.g. "512m", "1g"). Empty means no limit.
	Memory string `toml:"memory,omitempty"`

	// CPUs is the CPU limit (e.g. "0.5", "2"). Empty means no limit.
	CPUs string `toml:"cpus,omitempty"`
}

// ToolConfig defines an MCP tool sidecar container.
type ToolConfig struct {
	// Name is the tool's identifier, used as the container name on the sandbox network.
	Name string `toml:"name"`

	// Image is the Docker image for the tool.
	Image string `toml:"image"`

	// Transport is the MCP transport: "stdio" or "http".
	Transport string `toml:"transport"`

	// Port is the port the tool listens on (only for http transport).
	Port int `toml:"port,omitempty"`

	// Env is tool-specific environment variables. Values prefixed with
	// "inject:" are resolved from the secret store.
	Env map[string]string `toml:"env"`
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
	case "docker", "unikraft", "fly":
		// valid
	case "":
		return fmt.Errorf("sandbox_mode is required (docker, unikraft, or fly)")
	default:
		return fmt.Errorf("unknown sandbox_mode: %q (must be docker, unikraft, or fly)", c.SandboxMode)
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

// ResolveEnv merges environment variables from env_file and [env] into
// a single map. Values in [env] take precedence over values from env_file.
func (c *Config) ResolveEnv(configDir string) (map[string]string, error) {
	env := make(map[string]string)

	// Load from env_file first (lower precedence).
	if c.EnvFile != "" {
		path := c.EnvFile
		if !strings.HasPrefix(path, "/") {
			path = configDir + "/" + path
		}
		fileEnv, err := loadEnvFile(path)
		if err != nil {
			return nil, fmt.Errorf("loading env_file %q: %w", c.EnvFile, err)
		}
		for k, v := range fileEnv {
			env[k] = v
		}
	}

	// Overlay [env] table (higher precedence).
	for k, v := range c.Env {
		env[k] = v
	}

	return env, nil
}

// loadEnvFile parses a KEY=VALUE file, skipping comments and empty lines.
func loadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		data[key] = value
	}
	return data, scanner.Err()
}
