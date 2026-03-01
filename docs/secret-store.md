# Secret Store

The secret store is a pluggable system for credential management. Multiple backends implement the same `secrets.Store` interface, so the orchestrator doesn't care where secrets come from.

## Interface

```go
type Store interface {
    Get(name string) (string, error)
    Set(name, value string) error
    Delete(name string) error
    List() ([]string, error)
}
```

## Backends

```mermaid
flowchart LR
    Orch[Orchestrator] --> IF{secrets.Store}
    IF -->|local dev| FS[FileStore<br/>JSON file]
    IF -->|CI / containers| ES[EnvStore<br/>env vars]
    IF -->|multi-tenant| DS[DelegatedStore<br/>AWS SM / Vault]

    style IF fill:#333,stroke:#999,color:#fff
```

### FileStore

Source: `pkg/secrets/store.go`

The default for local development. A JSON file in a user-only directory.

```
~/.config/CommandGrid/secrets/
└── secrets.json     # {"name": "value", ...}
```

- Directory: `0700` permissions
- File: `0600` permissions
- Thread-safe (`sync.RWMutex`)
- Persists on every Set/Delete

```go
store, err := secrets.NewFileStore("~/.config/CommandGrid/secrets")
```

### EnvStore

Source: `pkg/secrets/env.go`

Read-only backend that reads secrets from environment variables or a `.env` file. Good for CI pipelines and containerized deployments.

Secret names are uppercased and prefixed. `Get("anthropic_key")` checks `SECRET_ANTHROPIC_KEY`.

```go
store, err := secrets.NewEnvStore("/path/to/.env", "SECRET_")
```

- Env vars take precedence over `.env` file values
- `Set` and `Delete` work in-memory only (not persisted)
- `.env` file loaded once at construction

### DelegatedStore

Source: `pkg/secrets/delegated.go`

For multi-tenant production. Fetches secrets from a customer's own external vault at runtime. CommandGrid never stores customer secrets.

```mermaid
flowchart LR
    CP[CommandGrid] -->|"runtime fetch"| DS[DelegatedStore]
    DS -->|"GetSecretValue"| AWS[AWS Secrets Manager]
    DS -->|"GET /v1/secret/data/*"| Vault[HashiCorp Vault]

    style DS fill:#333,stroke:#999,color:#fff
```

Supported backends:

| Type | Auth | How it works |
|---|---|---|
| `aws_sm` | STS AssumeRole (cross-account) | HTTP API with SigV4 signing |
| `vault` | Token auth | KV v2 HTTP API |

```go
store, err := secrets.NewDelegatedStore(secrets.DelegatedConfig{
    Type:    "aws_sm",
    Region:  "us-east-1",
    RoleARN: "arn:aws:iam::role/customer-secrets",
})
```

Features:
- **Short-lived cache** (30s TTL) to avoid hammering external vaults
- **Read-only** -- `Set`, `Delete`, `List` return errors. Customers manage their own vaults.
- **Customer-scoped** -- each customer's profile can define a different `SecretsProviderConfig`

## CLI commands

```bash
# Add a secret (FileStore only)
CommandGrid secrets add --name anthropic_key --value "sk-ant-..."

# List secret names
CommandGrid secrets list

# Remove a secret
CommandGrid secrets rm --name old_key
```

## How the orchestrator uses it

During `Up`, the orchestrator calls `store.Get(secretName)` for each secret in `sandbox.toml`:

- **inject mode**: returned value is set as an env var in the sandbox
- **proxy mode**: returned value is sent to GhostProxy's session registry (never enters sandbox)

If any `Get` fails, the `up` command aborts before provisioning.

## Future: age encryption

The FileStore currently uses plaintext JSON. The planned upgrade:

1. Generate an `age` keypair at init time
2. Encrypt each value before writing to JSON
3. Decrypt on read
4. Interface stays the same -- encryption is transparent
