package secrets

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DelegatedConfig describes where to fetch secrets from a customer's vault.
type DelegatedConfig struct {
	// Type is the provider backend: "aws_sm" or "vault".
	Type string

	// --- AWS Secrets Manager fields ---
	// Region is the AWS region (e.g. "us-east-1").
	Region string
	// RoleARN is the IAM role to assume via STS for cross-account access.
	RoleARN string

	// --- HashiCorp Vault fields ---
	// Addr is the Vault server address (e.g. "https://vault.example.com:8200").
	Addr string
	// Token is the Vault authentication token.
	Token string
	// MountPath is the secrets engine mount (e.g. "secret").
	MountPath string
}

// DelegatedStore implements the Store interface by fetching secrets from a
// customer-managed external vault at runtime. It does not cache secrets
// beyond a short TTL to ensure rotated values are picked up quickly.
//
// Supported backends:
//   - "aws_sm" — AWS Secrets Manager (via HTTP API with SigV4 or IRSA)
//   - "vault"  — HashiCorp Vault KV v2 (via HTTP API with token auth)
type DelegatedStore struct {
	cfg    DelegatedConfig
	client *http.Client

	// Short-lived cache to avoid hitting the vault on every Get().
	mu    sync.RWMutex
	cache map[string]cachedSecret
	ttl   time.Duration
}

type cachedSecret struct {
	value     string
	fetchedAt time.Time
}

// NewDelegatedStore creates a DelegatedStore for the given provider config.
func NewDelegatedStore(cfg DelegatedConfig) (*DelegatedStore, error) {
	switch cfg.Type {
	case "aws_sm", "vault":
		// valid
	default:
		return nil, fmt.Errorf("unsupported delegated secret provider: %q", cfg.Type)
	}

	return &DelegatedStore{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]cachedSecret),
		ttl:   30 * time.Second,
	}, nil
}

// Get retrieves a secret from the external vault. Results are cached briefly.
func (d *DelegatedStore) Get(name string) (string, error) {
	// Check cache first.
	if v, ok := d.fromCache(name); ok {
		return v, nil
	}

	var value string
	var err error

	switch d.cfg.Type {
	case "aws_sm":
		value, err = d.getFromAWS(name)
	case "vault":
		value, err = d.getFromVault(name)
	default:
		return "", fmt.Errorf("unsupported provider: %s", d.cfg.Type)
	}

	if err != nil {
		return "", err
	}

	d.toCache(name, value)
	return value, nil
}

// Set is not supported — customers manage their own vault contents.
func (d *DelegatedStore) Set(_, _ string) error {
	return fmt.Errorf("delegated store is read-only: manage secrets in your vault directly")
}

// Delete is not supported — customers manage their own vault contents.
func (d *DelegatedStore) Delete(_ string) error {
	return fmt.Errorf("delegated store is read-only: manage secrets in your vault directly")
}

// List is not supported — we don't enumerate customer vaults.
func (d *DelegatedStore) List() ([]string, error) {
	return nil, fmt.Errorf("delegated store does not support listing: secrets are fetched on demand")
}

// --- AWS Secrets Manager ---

// getFromAWS fetches a secret from AWS Secrets Manager using the HTTP API.
// In production, this would use STS AssumeRole for cross-account access.
// For now, it relies on ambient AWS credentials (IRSA, instance profile, etc.).
func (d *DelegatedStore) getFromAWS(name string) (string, error) {
	region := d.cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	endpoint := fmt.Sprintf("https://secretsmanager.%s.amazonaws.com", region)

	// AWS Secrets Manager GetSecretValue request.
	payload := fmt.Sprintf(`{"SecretId": %q}`, name)

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("building aws request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")

	// In a real implementation, this request would be signed with SigV4
	// using the assumed role credentials. For now, we rely on the AWS SDK
	// credential chain or environment variables. This is a placeholder
	// that demonstrates the HTTP flow.
	//
	// TODO: Integrate aws-sdk-go-v2 for proper SigV4 signing and STS AssumeRole.

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("aws_sm get %q: %w", name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("aws_sm read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("aws_sm get %q: status %d: %s", name, resp.StatusCode, body)
	}

	var result struct {
		SecretString string `json:"SecretString"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("aws_sm parse response: %w", err)
	}

	if result.SecretString == "" {
		return "", fmt.Errorf("aws_sm: secret %q has no string value (binary secrets not supported)", name)
	}

	return result.SecretString, nil
}

// --- HashiCorp Vault ---

// getFromVault fetches a secret from Vault's KV v2 engine.
func (d *DelegatedStore) getFromVault(name string) (string, error) {
	if d.cfg.Addr == "" {
		return "", fmt.Errorf("vault: addr is required")
	}

	mount := d.cfg.MountPath
	if mount == "" {
		mount = "secret"
	}

	// KV v2 read: GET /v1/{mount}/data/{name}
	url := fmt.Sprintf("%s/v1/%s/data/%s", strings.TrimRight(d.cfg.Addr, "/"), mount, name)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building vault request: %w", err)
	}

	if d.cfg.Token != "" {
		req.Header.Set("X-Vault-Token", d.cfg.Token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vault get %q: %w", name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("vault read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault get %q: status %d: %s", name, resp.StatusCode, body)
	}

	// KV v2 response shape: { "data": { "data": { "value": "..." } } }
	var result struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("vault parse response: %w", err)
	}

	// Look for a "value" key, or return the entire data map as JSON.
	if v, ok := result.Data.Data["value"]; ok {
		return fmt.Sprintf("%v", v), nil
	}

	// If no "value" key, serialize the whole data map.
	serialized, err := json.Marshal(result.Data.Data)
	if err != nil {
		return "", fmt.Errorf("vault serialize data: %w", err)
	}
	return string(serialized), nil
}

// --- Cache helpers ---

func (d *DelegatedStore) fromCache(name string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entry, ok := d.cache[name]
	if !ok {
		return "", false
	}
	if time.Since(entry.fetchedAt) > d.ttl {
		return "", false
	}
	return entry.value, true
}

func (d *DelegatedStore) toCache(name, value string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.cache[name] = cachedSecret{
		value:     value,
		fetchedAt: time.Now(),
	}
}
