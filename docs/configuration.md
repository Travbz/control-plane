# Configuration Reference

All sandbox configuration lives in a single `sandbox.toml` file. The control plane reads this file on every `up` command.

## Full schema

```toml
# Required. Selects the provisioner backend.
# "docker" - Docker Engine API (local dev, Raspberry Pi)
# "unikraft" - kraft.cloud REST API (macOS, cloud)
sandbox_mode = "docker"

# Required. Container image or Unikraft image reference.
image = "sandbox-image:latest"

# LLM proxy configuration.
[proxy]
addr = ":8090"    # Listen address for the proxy (default: ":8090")

# Agent configuration.
[agent]
command = "my-agent"       # Required. Binary or script to execute.
args = ["--flag", "value"] # Optional. Arguments passed to the command.
user = "agent"             # Optional. Unix user to run as (default: "agent").
workdir = "/workspace"     # Optional. Working directory (default: "/workspace").

# Secrets. Each key under [secrets] is the secret name in the store.
[secrets.my_secret]
mode = "proxy"              # Required. "proxy" or "inject".
env_var = "ANTHROPIC_API_KEY" # Required. Env var name in the sandbox.
provider = "anthropic"      # Required for proxy mode. Provider name.
upstream_url = ""           # Optional. Override provider default URL.

# Shared directories. Each [[shared_dirs]] entry is a bind mount.
[[shared_dirs]]
host_path = "./workspace"   # Required. Path on the host (relative or absolute).
guest_path = "/workspace"   # Required. Mount point inside the sandbox.
read_only = false           # Optional. Default: false.
```

## Field reference

### Top-level

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `sandbox_mode` | string | yes | -- | `"docker"` or `"unikraft"` |
| `image` | string | yes | -- | Container or VM image reference |

### [proxy]

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `addr` | string | no | `":8090"` | Listen address for the llm-proxy |

### [agent]

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `command` | string | yes | -- | Binary or script to exec inside the sandbox |
| `args` | string[] | no | `[]` | Arguments for the agent command |
| `user` | string | no | `"agent"` | Unix user the agent runs as |
| `workdir` | string | no | `"/workspace"` | Working directory inside the sandbox |

### [secrets.*]

Each secret is a named section under `[secrets]`. The key (e.g., `anthropic_key` in `[secrets.anthropic_key]`) is used to look up the value in the secret store.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `mode` | string | yes | -- | `"proxy"` or `"inject"` |
| `env_var` | string | yes | -- | Environment variable name in the sandbox |
| `provider` | string | proxy only | -- | LLM provider: `"anthropic"`, `"openai"`, `"ollama"` |
| `upstream_url` | string | no | -- | Override the provider's default upstream URL |

### [[shared_dirs]]

Each `[[shared_dirs]]` entry defines a host-to-guest directory mount.

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host_path` | string | yes | -- | Path on the host machine |
| `guest_path` | string | yes | -- | Mount point inside the sandbox |
| `read_only` | bool | no | `false` | Whether the mount is read-only |

## Validation rules

The config parser (`pkg/config/config.go`) enforces:

1. `sandbox_mode` must be `"docker"` or `"unikraft"`. Cannot be empty.
2. `image` cannot be empty.
3. `agent.command` cannot be empty.
4. Every secret must have `mode` set to `"proxy"` or `"inject"`.
5. Every secret must have `env_var` set.
6. Secrets with `mode = "proxy"` must have `provider` set.

If validation fails, the `up` command exits with an error before any provisioning happens.

## Example configurations

### Anthropic only (proxy mode)

```toml
sandbox_mode = "docker"
image = "sandbox-image:latest"

[proxy]
addr = ":8090"

[agent]
command = "python3"
args = ["agent.py"]

[secrets.anthropic_key]
mode = "proxy"
env_var = "ANTHROPIC_API_KEY"
provider = "anthropic"

[[shared_dirs]]
host_path = "./workspace"
guest_path = "/workspace"
```

### Multi-provider

```toml
sandbox_mode = "docker"
image = "sandbox-image:latest"

[proxy]
addr = ":8090"

[agent]
command = "my-agent"

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

[[shared_dirs]]
host_path = "./workspace"
guest_path = "/workspace"
```

### Ollama local

```toml
sandbox_mode = "docker"
image = "sandbox-image:latest"

[proxy]
addr = ":8090"

[agent]
command = "python3"
args = ["local_agent.py"]

[secrets.ollama]
mode = "proxy"
env_var = "OLLAMA_API_KEY"
provider = "ollama"

[[shared_dirs]]
host_path = "./workspace"
guest_path = "/workspace"
```

For Ollama, the proxy doesn't inject any auth header (Ollama doesn't need one). The value of `env_var` gets a session token, but the agent doesn't actually use it for auth -- the `OLLAMA_HOST` env var is what matters, and the control plane sets that automatically.

### Inject only (no proxy)

```toml
sandbox_mode = "docker"
image = "sandbox-image:latest"

[agent]
command = "my-script.sh"

[secrets.api_key]
mode = "inject"
env_var = "MY_API_KEY"

[secrets.db_password]
mode = "inject"
env_var = "DATABASE_URL"

[[shared_dirs]]
host_path = "./workspace"
guest_path = "/workspace"
```

No `[proxy]` section needed if you're not using proxy mode. The llm-proxy won't be started.

### Unikraft (cloud deployment)

```toml
sandbox_mode = "unikraft"
image = "my-registry.com/sandbox-image:latest"

[proxy]
addr = ":8090"

[agent]
command = "agent"

[secrets.anthropic_key]
mode = "proxy"
env_var = "ANTHROPIC_API_KEY"
provider = "anthropic"
```

Requires `UKC_TOKEN` to be set in the host environment for kraft.cloud API auth. Shared directories are not supported in Unikraft mode (VMs don't have bind mounts).
