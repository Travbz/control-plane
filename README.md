# control-plane

The orchestrator for the agent sandbox system. Reads a `sandbox.toml` config, manages an encrypted secret store, provisions sandboxes (Docker or Unikraft), and coordinates the [llm-proxy](https://github.com/Travbz/llm-proxy) for credential proxying. One command to boot an isolated agent environment with the hybrid credential model.

Part of a three-service system:

| Repo | What it does |
|---|---|
| **[control-plane](https://github.com/Travbz/control-plane)** | This repo -- orchestrator, config, secrets, provisioning |
| **[llm-proxy](https://github.com/Travbz/llm-proxy)** | Credential-injecting LLM reverse proxy |
| **[sandbox-image](https://github.com/Travbz/sandbox-image)** | Container image -- entrypoint, env stripping, privilege drop |

---

## Architecture

```mermaid
flowchart LR
    subgraph Host
        CP[control-plane]
        Proxy[llm-proxy]
        Secrets[(secret store)]
    end

    subgraph Sandbox
        Entry[entrypoint]
        Agent[agent process]
    end

    LLM[LLM Provider]

    CP -->|"read secrets"| Secrets
    CP -->|"register session"| Proxy
    CP -->|"Docker API / Unikraft API"| Entry
    Entry --> Agent
    Agent -->|"HTTP + session token"| Proxy
    Proxy -->|"HTTPS + real API key"| LLM
```

---

## The boot sequence

When you run `control-plane up`, here's what happens:

```mermaid
sequenceDiagram
    participant User
    participant CP as control-plane
    participant Store as secret store
    participant Proxy as llm-proxy
    participant Docker as Docker / Unikraft
    participant Sandbox as sandbox

    User->>CP: control-plane up --name my-agent
    CP->>Store: Resolve secrets from sandbox.toml
    Store-->>CP: Real values for inject secrets, keys for proxy secrets

    CP->>CP: Generate session tokens for proxy secrets
    CP->>Proxy: Register sessions (token -> real key + provider)
    Proxy-->>CP: OK

    CP->>Docker: Create container with env vars
    Note over Docker: Inject secrets: real values<br/>Proxy secrets: session tokens<br/>Agent config: command, args, workdir

    CP->>Docker: Start container
    Docker->>Sandbox: Container boots
    Sandbox->>Sandbox: Entrypoint strips control plane vars, drops privileges
    Sandbox->>Proxy: Agent makes LLM calls with session token
    Proxy->>Proxy: Swaps token for real key, forwards upstream
```

---

## Hybrid credential model

This is the key design decision. Not everything needs to be proxied, and not everything should be injected directly. Each secret in `sandbox.toml` has a mode:

| Mode | What happens | Good for |
|---|---|---|
| `proxy` | Real key stays on host. Sandbox gets a session token. LLM calls go through llm-proxy which injects the real key. | LLM API keys (high value, high risk) |
| `inject` | Real value injected directly as an env var into the sandbox. | SSH keys, registry tokens, git credentials |

```toml
[secrets.anthropic_key]
mode = "proxy"
env_var = "ANTHROPIC_API_KEY"
provider = "anthropic"

[secrets.github_token]
mode = "inject"
env_var = "GITHUB_TOKEN"
```

---

## Config (`sandbox.toml`)

```toml
# Runtime: "docker" for dev/Pi, "unikraft" for Mac/cloud
sandbox_mode = "docker"

# Container image from the sandbox-image repo
image = "sandbox-image:latest"

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
```

See `sandbox.toml.example` for a fully annotated version.

---

## Usage

### Managing secrets

```bash
# add secrets to the encrypted store
control-plane secrets add --name anthropic_key --value "sk-ant-..."
control-plane secrets add --name github_token --value "ghp_..."

# list stored secret names
control-plane secrets list

# remove a secret
control-plane secrets rm --name old_key
```

### Running sandboxes

```bash
# start a sandbox (reads sandbox.toml in current directory)
control-plane up --name my-agent

# check status
control-plane status
control-plane status --id <container-id>

# stop and destroy
control-plane down --id <container-id>
```

---

## Building

Requires Go 1.25+. Use `nix develop` if you have Nix.

```bash
make build    # builds to ./build/control-plane
make test     # runs all tests
make lint     # golangci-lint
make vet      # go vet
```

### End-to-end test

There's an e2e script that tests the full flow -- proxy sessions, secret management, and Docker sandbox creation:

```bash
# prerequisites: docker running, llm-proxy and sandbox-image built
./e2e_test.sh
```

---

## Sandbox runtimes

The provisioner interface abstracts the sandbox backend. A single config switch controls which one is used:

```mermaid
flowchart TD
    Config[sandbox.toml] --> Switch{sandbox_mode}
    Switch -->|docker| Docker[Docker Engine API<br/>Unix socket]
    Switch -->|unikraft| UKC[Unikraft Cloud REST API<br/>kraft.cloud]

    Docker --> Container[Container on local Docker]
    UKC --> MicroVM[Micro-VM on Unikraft Cloud]

    style Docker fill:#066,stroke:#333,color:#fff
    style UKC fill:#606,stroke:#333,color:#fff
```

- **Docker** -- talks to the Docker daemon over the Unix socket. Used for local development and Raspberry Pi deployment. Containers get `host.docker.internal` for reaching the proxy on the host.
- **Unikraft** -- talks to the kraft.cloud REST API. Used for macOS and cloud production. Needs `UKC_TOKEN` env var set.

---

## Project structure

```
control-plane/
├── main.go                              # CLI entry point + subcommand dispatch
├── cmd/
│   ├── up.go                            # start a sandbox
│   ├── down.go                          # stop + destroy a sandbox
│   ├── status.go                        # sandbox status / list
│   ├── secrets.go                       # secrets add/rm/list
│   └── helpers.go
├── pkg/
│   ├── config/
│   │   ├── config.go                    # sandbox.toml parsing + validation
│   │   └── config_test.go
│   ├── secrets/
│   │   ├── store.go                     # encrypted secret store
│   │   ├── session.go                   # session token generation
│   │   └── store_test.go
│   ├── provisioner/
│   │   ├── provisioner.go               # Provisioner interface
│   │   ├── docker.go                    # Docker Engine API backend
│   │   └── unikraft.go                  # Unikraft Cloud API backend
│   └── orchestrator/
│       └── orchestrator.go              # boot sequence coordination
├── sandbox.toml.example
├── e2e_test.sh
├── Makefile
├── go.mod
├── flake.nix
├── .releaserc.yaml
└── .github/workflows/
    ├── ci.yaml                          # lint, test, vet on PRs
    └── release.yaml                     # semantic-release on main
```

---

## Versioning

Automated with [semantic-release](https://github.com/semantic-release/semantic-release) from [conventional commits](https://www.conventionalcommits.org/). No manual version bumps.
