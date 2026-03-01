# Configuration Reference

All sandbox configuration lives in a single `sandbox.toml` file. CommandGrid reads this on every `up` command.

## Full schema

```toml
# Required. Selects the provisioner backend.
sandbox_mode = "docker"    # "docker", "fly", or "unikraft"

# Required. Container image or VM image reference.
image = "RootFS:latest"

# LLM proxy configuration.
[proxy]
addr = ":8090"

# Agent configuration.
[agent]
command = "my-agent"
args = ["--flag", "value"]
user = "agent"
workdir = "/workspace"

# Plain environment variables (no secret management).
[env]
LOG_LEVEL = "debug"
NODE_ENV = "production"

# Load env vars from a file (lower precedence than [env]).
env_file = ".env"

# Secrets. Each key maps to a name in the secret store.
[secrets.my_llm_key]
mode = "proxy"
env_var = "ANTHROPIC_API_KEY"
provider = "anthropic"

[secrets.my_token]
mode = "inject"
env_var = "GITHUB_TOKEN"

# Shared directories (bind mounts).
[[shared_dirs]]
host_path = "./workspace"
guest_path = "/workspace"
read_only = false

# Container resource limits.
[resources]
memory = "512m"
cpus = "1"

# MCP tool sidecars.
[[tools]]
name = "echo"
image = "ghcr.io/yourorg/tool-echo:latest"
transport = "http"
port = 8080

[tools.env]
API_KEY = "inject:some_secret"

# Network restrictions.
[network]
allowed_hosts = ["api.anthropic.com", "*.github.com"]
proxy_port = 3128
```

## Field reference

### Top-level

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `sandbox_mode` | string | yes | -- | `"docker"`, `"fly"`, or `"unikraft"` |
| `image` | string | yes | -- | Container or VM image reference |
| `env_file` | string | no | -- | Path to a `.env` file (KEY=VALUE format) |

### [proxy]

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `addr` | string | no | `":8090"` | Listen address for GhostProxy |

### [agent]

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `command` | string | yes | -- | Binary or script to exec inside the sandbox |
| `args` | string[] | no | `[]` | Arguments for the agent command |
| `user` | string | no | `"agent"` | Unix user the agent runs as |
| `workdir` | string | no | `"/workspace"` | Working directory inside the sandbox |

### [env]

A flat map of environment variables injected directly into the sandbox. No secret management -- these are plain key-value pairs.

```toml
[env]
LOG_LEVEL = "debug"
DATABASE_URL = "postgres://localhost/mydb"
```

Values from `env_file` are loaded first, then `[env]` values are overlaid (higher precedence).

### [secrets.*]

Each secret is a named section under `[secrets]`. The key (e.g., `anthropic_key` in `[secrets.anthropic_key]`) is the lookup name in the secret store.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `mode` | string | yes | -- | `"proxy"` or `"inject"` |
| `env_var` | string | yes | -- | Environment variable name in the sandbox |
| `provider` | string | proxy only | -- | LLM provider: `"anthropic"`, `"openai"`, `"ollama"` |
| `upstream_url` | string | no | -- | Override the provider's default upstream URL |

### [[shared_dirs]]

Each entry is a host-to-guest bind mount.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host_path` | string | yes | -- | Path on the host (relative paths resolved to config dir) |
| `guest_path` | string | yes | -- | Mount point inside the sandbox |
| `read_only` | bool | no | `false` | Read-only mount |

### [resources]

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `memory` | string | no | -- | Memory limit (e.g. `"512m"`, `"1g"`) |
| `cpus` | string | no | -- | CPU limit (e.g. `"0.5"`, `"2"`) |

### [[tools]]

Each entry defines an MCP tool sidecar container started on the sandbox network.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `name` | string | yes | -- | Tool identifier (used as container name) |
| `image` | string | yes | -- | Docker image for the tool |
| `transport` | string | yes | -- | `"http"` or `"stdio"` |
| `port` | int | http only | -- | Port the tool listens on |
| `env` | map | no | -- | Tool-specific env vars. `"inject:name"` resolves from secret store. |

### [network]

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `allowed_hosts` | string[] | no | `[]` | Hosts the sandbox may reach. Empty = unrestricted. |
| `proxy_port` | int | no | `3128` | Allowlist proxy listen port |

Supports exact match (`"api.anthropic.com"`) and wildcard subdomains (`"*.github.com"`).

## Validation rules

The config parser enforces:

1. `sandbox_mode` must be `"docker"`, `"fly"`, or `"unikraft"`
2. `image` cannot be empty
3. `agent.command` cannot be empty
4. Every secret must have `mode` set to `"proxy"` or `"inject"`
5. Every secret must have `env_var` set
6. Secrets with `mode = "proxy"` must have `provider` set

If validation fails, the `up` command exits before provisioning.

## Example configurations

### Minimal (hello world)

```toml
sandbox_mode = "docker"
image = "RootFS:latest"

[proxy]
addr = ":8090"

[agent]
command = "bash"
args = ["/workspace/agent.sh"]

[secrets.anthropic_key]
mode = "proxy"
env_var = "ANTHROPIC_API_KEY"
provider = "anthropic"

[[shared_dirs]]
host_path = "."
guest_path = "/workspace"
```

### Full production config

```toml
sandbox_mode = "fly"
image = "ghcr.io/yourorg/RootFS:latest"

[proxy]
addr = ":8090"

[agent]
command = "/usr/local/bin/agent"
user = "agent"
workdir = "/workspace"

[env]
LOG_LEVEL = "info"
NODE_ENV = "production"

env_file = ".env.production"

[secrets.anthropic_key]
mode = "proxy"
env_var = "ANTHROPIC_API_KEY"
provider = "anthropic"

[secrets.openai_key]
mode = "proxy"
env_var = "OPENAI_API_KEY"
provider = "openai"

[secrets.github_token]
mode = "inject"
env_var = "GITHUB_TOKEN"

[resources]
memory = "1g"
cpus = "2"

[[tools]]
name = "gmail"
image = "ghcr.io/yourorg/tool-gmail:latest"
transport = "http"
port = 8080

[tools.env]
GOOGLE_API_KEY = "inject:google_api_key"

[[tools]]
name = "discord"
image = "ghcr.io/yourorg/tool-discord:latest"
transport = "http"
port = 8080

[tools.env]
DISCORD_TOKEN = "inject:discord_token"

[network]
allowed_hosts = [
    "api.anthropic.com",
    "api.openai.com",
    "*.googleapis.com",
    "discord.com",
    "*.discord.com"
]
proxy_port = 3128
```
