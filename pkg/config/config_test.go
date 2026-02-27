package config

import (
	"os"
	"path/filepath"
	"testing"
)

const validConfig = `
sandbox_mode = "docker"
image = "ghcr.io/myorg/sandbox-image:latest"

[proxy]
addr = ":8090"

[agent]
command = "claude"
args = ["--model", "sonnet"]
user = "agent"
workdir = "/workspace"

[secrets.anthropic_key]
mode = "proxy"
env_var = "ANTHROPIC_API_KEY"
provider = "anthropic"

[secrets.github_token]
mode = "inject"
env_var = "GITHUB_TOKEN"

[[shared_dirs]]
host_path = "./workspace"
guest_path = "/workspace"

[[shared_dirs]]
host_path = "./data"
guest_path = "/data"
read_only = true
`

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTemp(t, validConfig)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.SandboxMode != "docker" {
		t.Errorf("SandboxMode = %q, want %q", cfg.SandboxMode, "docker")
	}
	if cfg.Image != "ghcr.io/myorg/sandbox-image:latest" {
		t.Errorf("Image = %q", cfg.Image)
	}
	if cfg.Agent.Command != "claude" {
		t.Errorf("Agent.Command = %q", cfg.Agent.Command)
	}
	if len(cfg.Secrets) != 2 {
		t.Errorf("len(Secrets) = %d, want 2", len(cfg.Secrets))
	}
	if cfg.Secrets["anthropic_key"].Mode != "proxy" {
		t.Errorf("anthropic_key mode = %q", cfg.Secrets["anthropic_key"].Mode)
	}
	if cfg.Secrets["github_token"].Mode != "inject" {
		t.Errorf("github_token mode = %q", cfg.Secrets["github_token"].Mode)
	}
	if len(cfg.SharedDirs) != 2 {
		t.Errorf("len(SharedDirs) = %d, want 2", len(cfg.SharedDirs))
	}
}

func TestLoad_MissingSandboxMode(t *testing.T) {
	path := writeTemp(t, `
image = "foo"
[agent]
command = "bar"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing sandbox_mode")
	}
}

func TestLoad_InvalidSandboxMode(t *testing.T) {
	path := writeTemp(t, `
sandbox_mode = "vmware"
image = "foo"
[agent]
command = "bar"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid sandbox_mode")
	}
}

func TestLoad_ProxySecretMissingProvider(t *testing.T) {
	path := writeTemp(t, `
sandbox_mode = "docker"
image = "foo"
[agent]
command = "bar"
[secrets.my_key]
mode = "proxy"
env_var = "MY_KEY"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for proxy secret without provider")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/sandbox.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sandbox.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}
