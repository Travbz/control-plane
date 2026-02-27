# Secret Store

The secret store is a local, file-backed key-value store for credentials. It holds the real values of all secrets referenced in `sandbox.toml`.

Source: `pkg/secrets/store.go`

## How it works

```mermaid
flowchart LR
    CLI["control-plane secrets add"] -->|Set| Store[Store struct]
    Store -->|persist| File["~/.control-plane/secrets.json"]
    File -->|load on init| Store
    Orch[Orchestrator] -->|Get| Store
```

The store is a Go struct with an in-memory `map[string]string` and a backing JSON file. Every write operation (Set, Delete) persists the full map to disk immediately.

### Directory layout

```
~/.control-plane/
└── secrets.json     # JSON map of name -> value
```

The directory is created with `0700` permissions (owner-only access). The file is written with `0600` permissions.

### File format

```json
{
  "anthropic_key": "sk-ant-api03-real-key-here",
  "github_token": "ghp_abc123def456",
  "ssh_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n..."
}
```

Plain JSON. Keys are secret names (matching `sandbox.toml` sections). Values are the raw secret strings.

## Operations

### NewStore(dir)

Opens or creates the store at the given directory path. If `secrets.json` exists, it's loaded into memory. If not, the store starts empty.

```go
store, err := secrets.NewStore(filepath.Join(home, ".control-plane"))
```

### Set(name, value)

Stores a secret value. Overwrites if the name already exists. Persists to disk immediately.

```go
err := store.Set("anthropic_key", "sk-ant-api03-real-key")
```

### Get(name)

Returns the secret value. Returns an error if the name doesn't exist.

```go
value, err := store.Get("anthropic_key")
// value = "sk-ant-api03-real-key"
```

### Delete(name)

Removes a secret from the store. Persists to disk immediately. Deleting a non-existent key is not an error (it's a no-op that re-persists).

```go
err := store.Delete("anthropic_key")
```

### List()

Returns all secret names (not values).

```go
names := store.List()
// ["anthropic_key", "github_token"]
```

## Thread safety

The store uses a `sync.RWMutex`:
- `Get` and `List` take a read lock
- `Set` and `Delete` take a write lock

This means multiple goroutines can read concurrently, but writes are exclusive. In practice, the CLI is single-threaded, so this is a safeguard rather than a necessity.

## CLI commands

### Add a secret

```bash
control-plane secrets add <name>
```

Reads the value from stdin (or prompts interactively). The `setup.sh` script pipes values in:

```bash
echo "sk-ant-api03-real-key" | control-plane secrets add anthropic_key
```

### List secrets

```bash
control-plane secrets list
```

Prints secret names, one per line. Never prints values.

### Remove a secret

```bash
control-plane secrets remove <name>
```

Removes the secret from the store.

## Future: age encryption

The current implementation stores secrets in plaintext JSON. This is fine for local development where the filesystem is trusted, but not ideal for shared machines or CI environments.

The planned upgrade path:

1. Generate an age keypair at init time, store the private key in the OS keychain
2. Encrypt each value with the age public key before writing to JSON
3. Decrypt on read using the private key from the keychain
4. The file format changes from `{"name": "value"}` to `{"name": "age-encrypted-blob"}`

The `Store` interface won't change. The encryption is transparent to the rest of the codebase.

## How the orchestrator uses it

During `Up`, the orchestrator calls `store.Get(secretName)` for each secret in `sandbox.toml`:

- **inject mode**: The returned value is set directly as an env var in the sandbox
- **proxy mode**: The returned value is sent to the llm-proxy's session registry (never enters the sandbox)

If any `Get` fails (secret not in store), the `Up` command aborts before provisioning anything.
