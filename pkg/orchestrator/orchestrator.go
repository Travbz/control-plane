// Package orchestrator implements the sandbox boot sequence: resolve secrets,
// provision sandbox, register proxy sessions, inject env vars, start sandbox.
package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"control-plane/pkg/config"
	"control-plane/pkg/provisioner"
	"control-plane/pkg/secrets"
)

// Orchestrator coordinates sandbox lifecycle operations.
type Orchestrator struct {
	cfg         *config.Config
	provisioner provisioner.Provisioner
	secrets     *secrets.Store
	proxyAddr   string
	logger      *log.Logger
	httpClient  *http.Client
}

// New creates a new Orchestrator.
func New(
	cfg *config.Config,
	prov provisioner.Provisioner,
	secretStore *secrets.Store,
	proxyAddr string,
	logger *log.Logger,
) *Orchestrator {
	return &Orchestrator{
		cfg:         cfg,
		provisioner: prov,
		secrets:     secretStore,
		proxyAddr:   proxyAddr,
		logger:      logger,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Up executes the full boot sequence for a sandbox.
func (o *Orchestrator) Up(ctx context.Context, name string) (*provisioner.Sandbox, error) {
	o.logger.Printf("starting sandbox %q", name)

	// 1. Resolve secrets and build the env var map.
	env, sessionTokens, err := o.resolveSecrets()
	if err != nil {
		return nil, fmt.Errorf("resolving secrets: %w", err)
	}

	// 2. Add agent configuration env vars.
	env["AGENT_COMMAND"] = o.cfg.Agent.Command
	if len(o.cfg.Agent.Args) > 0 {
		// Join args with spaces — the entrypoint splits on whitespace.
		argsStr := ""
		for i, a := range o.cfg.Agent.Args {
			if i > 0 {
				argsStr += " "
			}
			argsStr += a
		}
		env["AGENT_ARGS"] = argsStr
	}
	if o.cfg.Agent.User != "" {
		env["AGENT_USER"] = o.cfg.Agent.User
	}
	if o.cfg.Agent.Workdir != "" {
		env["AGENT_WORKDIR"] = o.cfg.Agent.Workdir
	}

	// 3. Set control plane URL so the sandbox knows where the proxy is.
	env["CONTROL_PLANE_URL"] = "http://host.docker.internal" + o.proxyAddr

	// 4. Build mounts from shared_dirs config.
	var mounts []provisioner.Mount
	for _, sd := range o.cfg.SharedDirs {
		mounts = append(mounts, provisioner.Mount{
			HostPath:  sd.HostPath,
			GuestPath: sd.GuestPath,
			ReadOnly:  sd.ReadOnly,
		})
	}

	// 5. Provision the sandbox.
	sandbox, err := o.provisioner.Create(ctx, provisioner.CreateOpts{
		Name:      name,
		Image:     o.cfg.Image,
		Env:       env,
		Mounts:    mounts,
		ProxyAddr: o.proxyAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("provisioning sandbox: %w", err)
	}
	o.logger.Printf("created sandbox %s (id=%s)", sandbox.Name, sandbox.ID)

	// 6. Register proxy sessions for proxied secrets.
	for secretName, token := range sessionTokens {
		secretCfg := o.cfg.Secrets[secretName]

		realKey, err := o.secrets.Get(secretName)
		if err != nil {
			// Clean up the created sandbox.
			_ = o.provisioner.Destroy(ctx, sandbox.ID)
			return nil, fmt.Errorf("getting secret %q for proxy registration: %w", secretName, err)
		}

		if err := o.registerProxySession(token, secretCfg.Provider, realKey, secretCfg.UpstreamURL, sandbox.ID); err != nil {
			_ = o.provisioner.Destroy(ctx, sandbox.ID)
			return nil, fmt.Errorf("registering proxy session for %q: %w", secretName, err)
		}
		o.logger.Printf("registered proxy session for %s (provider=%s)", secretName, secretCfg.Provider)
	}

	// 7. Start the sandbox.
	if err := o.provisioner.Start(ctx, sandbox.ID); err != nil {
		// Clean up proxy sessions.
		for _, token := range sessionTokens {
			_ = o.revokeProxySession(token)
		}
		_ = o.provisioner.Destroy(ctx, sandbox.ID)
		return nil, fmt.Errorf("starting sandbox: %w", err)
	}

	o.logger.Printf("sandbox %s is running", sandbox.Name)
	return sandbox, nil
}

// Down stops and destroys a sandbox by ID, revoking any proxy sessions.
func (o *Orchestrator) Down(ctx context.Context, id string) error {
	o.logger.Printf("stopping sandbox %s", id)

	if err := o.provisioner.Stop(ctx, id); err != nil {
		o.logger.Printf("warning: stop sandbox: %v", err)
	}

	if err := o.provisioner.Destroy(ctx, id); err != nil {
		return fmt.Errorf("destroying sandbox: %w", err)
	}

	o.logger.Printf("sandbox %s destroyed", id)
	return nil
}

// Status returns the status of a sandbox.
func (o *Orchestrator) Status(ctx context.Context, id string) (*provisioner.Sandbox, error) {
	return o.provisioner.Status(ctx, id)
}

// List returns all managed sandboxes.
func (o *Orchestrator) List(ctx context.Context) ([]*provisioner.Sandbox, error) {
	return o.provisioner.List(ctx)
}

// resolveSecrets processes the config's secret definitions and returns:
// - env: the environment variables to inject into the sandbox
// - sessionTokens: map of secret_name -> session_token (for proxy mode secrets)
func (o *Orchestrator) resolveSecrets() (env map[string]string, sessionTokens map[string]string, err error) {
	env = make(map[string]string)
	sessionTokens = make(map[string]string)

	for name, secretCfg := range o.cfg.Secrets {
		switch secretCfg.Mode {
		case "inject":
			// Direct injection: resolve the real secret value and set as env var.
			value, err := o.secrets.Get(name)
			if err != nil {
				return nil, nil, fmt.Errorf("secret %q: %w", name, err)
			}
			env[secretCfg.EnvVar] = value

		case "proxy":
			// Proxy mode: generate a session token.
			token, err := secrets.GenerateSessionToken()
			if err != nil {
				return nil, nil, fmt.Errorf("secret %q: generating token: %w", name, err)
			}
			sessionTokens[name] = token

			// The sandbox gets the session token as its "API key".
			env[secretCfg.EnvVar] = token

			// Also set SESSION_TOKEN and SESSION_ID for the entrypoint.
			env["SESSION_TOKEN"] = token
			env["SESSION_ID"] = name
		}
	}

	return env, sessionTokens, nil
}

// registerProxySession registers a session with the LLM proxy.
func (o *Orchestrator) registerProxySession(token, provider, apiKey, upstreamURL, sandboxID string) error {
	body := map[string]string{
		"token":     token,
		"provider":  provider,
		"api_key":   apiKey,
		"sandbox_id": sandboxID,
	}
	if upstreamURL != "" {
		body["upstream_url"] = upstreamURL
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling session: %w", err)
	}

	url := fmt.Sprintf("http://localhost%s/v1/sessions", o.proxyAddr)
	resp, err := o.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("posting to proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("proxy returned status %d", resp.StatusCode)
	}

	return nil
}

// revokeProxySession revokes a session from the LLM proxy.
func (o *Orchestrator) revokeProxySession(token string) error {
	url := fmt.Sprintf("http://localhost%s/v1/sessions/%s", o.proxyAddr, token)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	defer resp.Body.Close()

	return nil
}
