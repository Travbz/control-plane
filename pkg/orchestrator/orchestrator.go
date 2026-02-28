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
	"strings"
	"time"

	"control-plane/pkg/config"
	"control-plane/pkg/provisioner"
	"control-plane/pkg/secrets"
)

// Orchestrator coordinates sandbox lifecycle operations.
type Orchestrator struct {
	cfg         *config.Config
	provisioner provisioner.Provisioner
	secrets     secrets.Store
	proxyAddr   string
	logger      *log.Logger
	httpClient  *http.Client
}

// New creates a new Orchestrator.
func New(
	cfg *config.Config,
	prov provisioner.Provisioner,
	secretStore secrets.Store,
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

	// 2. Merge env vars from [env] table and env_file.
	configEnv, err := o.cfg.ResolveEnv("")
	if err != nil {
		return nil, fmt.Errorf("resolving env config: %w", err)
	}
	for k, v := range configEnv {
		env[k] = v
	}

	// 3. Add agent configuration env vars.
	env["AGENT_COMMAND"] = o.cfg.Agent.Command
	if len(o.cfg.Agent.Args) > 0 {
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

	// 4. Set control plane URL so the sandbox knows where the proxy is.
	env["CONTROL_PLANE_URL"] = "http://host.docker.internal" + o.proxyAddr

	// 4b. If network allowlist is configured, set the proxy env vars.
	// The allowlist proxy runs on the host and the sandbox routes all
	// outbound HTTP(S) traffic through it.
	if len(o.cfg.Network.AllowedHosts) > 0 {
		proxyPort := o.cfg.Network.ProxyPort
		if proxyPort == 0 {
			proxyPort = 3128
		}
		proxyURL := fmt.Sprintf("http://host.docker.internal:%d", proxyPort)
		env["HTTP_PROXY"] = proxyURL
		env["HTTPS_PROXY"] = proxyURL
		env["http_proxy"] = proxyURL
		env["https_proxy"] = proxyURL
		// Ensure intra-sandbox traffic (tool sidecars, localhost) bypasses the proxy.
		env["NO_PROXY"] = "localhost,127.0.0.1,.internal"
		env["no_proxy"] = "localhost,127.0.0.1,.internal"
	}

	// 5. Build mounts from shared_dirs config.
	var mounts []provisioner.Mount
	for _, sd := range o.cfg.SharedDirs {
		mounts = append(mounts, provisioner.Mount{
			HostPath:  sd.HostPath,
			GuestPath: sd.GuestPath,
			ReadOnly:  sd.ReadOnly,
		})
	}

	// 6. If tools are configured, create a sandbox network.
	var networkID string
	if len(o.cfg.Tools) > 0 {
		if dp, ok := o.provisioner.(*provisioner.DockerProvisioner); ok {
			netName := fmt.Sprintf("sandbox-%s", name)
			networkID, err = dp.CreateNetwork(ctx, netName)
			if err != nil {
				return nil, fmt.Errorf("creating sandbox network: %w", err)
			}
			o.logger.Printf("created sandbox network %s (id=%s)", netName, networkID)
		}

		// Build TOOL_ENDPOINTS env var for the agent.
		var endpoints []string
		for _, tool := range o.cfg.Tools {
			if tool.Transport == "http" {
				endpoints = append(endpoints, fmt.Sprintf("%s=http://%s:%d", tool.Name, tool.Name, tool.Port))
			} else {
				// stdio tools run in the same container or via a socket.
				endpoints = append(endpoints, fmt.Sprintf("%s=stdio://%s", tool.Name, tool.Name))
			}
		}
		env["TOOL_ENDPOINTS"] = strings.Join(endpoints, ",")
	}

	// 7. Provision the agent sandbox.
	sandbox, err := o.provisioner.Create(ctx, provisioner.CreateOpts{
		Name:      name,
		Image:     o.cfg.Image,
		Env:       env,
		Mounts:    mounts,
		ProxyAddr: o.proxyAddr,
		Memory:    o.cfg.Resources.Memory,
		CPUs:      o.cfg.Resources.CPUs,
		NetworkID: networkID,
	})
	if err != nil {
		o.cleanupNetwork(ctx, networkID)
		return nil, fmt.Errorf("provisioning sandbox: %w", err)
	}
	o.logger.Printf("created sandbox %s (id=%s)", sandbox.Name, sandbox.ID)

	// 8. Start tool sidecar containers on the same network.
	var toolContainerIDs []string
	for _, tool := range o.cfg.Tools {
		toolEnv := o.resolveToolEnv(tool.Env)
		toolSandbox, err := o.provisioner.Create(ctx, provisioner.CreateOpts{
			Name:      fmt.Sprintf("%s-tool-%s", name, tool.Name),
			Image:     tool.Image,
			Env:       toolEnv,
			NetworkID: networkID,
		})
		if err != nil {
			o.logger.Printf("warning: failed to create tool %s: %v", tool.Name, err)
			continue
		}
		if err := o.provisioner.Start(ctx, toolSandbox.ID); err != nil {
			o.logger.Printf("warning: failed to start tool %s: %v", tool.Name, err)
			_ = o.provisioner.Destroy(ctx, toolSandbox.ID)
			continue
		}
		toolContainerIDs = append(toolContainerIDs, toolSandbox.ID)
		o.logger.Printf("started tool sidecar %s (id=%s)", tool.Name, toolSandbox.ID)
	}

	// 9. Register proxy sessions for proxied secrets.
	for secretName, token := range sessionTokens {
		secretCfg := o.cfg.Secrets[secretName]

		realKey, err := o.secrets.Get(secretName)
		if err != nil {
			o.cleanupToolContainers(ctx, toolContainerIDs)
			_ = o.provisioner.Destroy(ctx, sandbox.ID)
			o.cleanupNetwork(ctx, networkID)
			return nil, fmt.Errorf("getting secret %q for proxy registration: %w", secretName, err)
		}

		if err := o.registerProxySession(token, secretCfg.Provider, realKey, secretCfg.UpstreamURL, sandbox.ID); err != nil {
			o.cleanupToolContainers(ctx, toolContainerIDs)
			_ = o.provisioner.Destroy(ctx, sandbox.ID)
			o.cleanupNetwork(ctx, networkID)
			return nil, fmt.Errorf("registering proxy session for %q: %w", secretName, err)
		}
		o.logger.Printf("registered proxy session for %s (provider=%s)", secretName, secretCfg.Provider)
	}

	// 10. Start the agent sandbox.
	if err := o.provisioner.Start(ctx, sandbox.ID); err != nil {
		for _, token := range sessionTokens {
			_ = o.revokeProxySession(token)
		}
		o.cleanupToolContainers(ctx, toolContainerIDs)
		_ = o.provisioner.Destroy(ctx, sandbox.ID)
		o.cleanupNetwork(ctx, networkID)
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

	proxyBaseURL := "http://host.docker.internal" + o.proxyAddr

	for name, secretCfg := range o.cfg.Secrets {
		switch secretCfg.Mode {
		case "inject":
			value, err := o.secrets.Get(name)
			if err != nil {
				return nil, nil, fmt.Errorf("secret %q: %w", name, err)
			}
			env[secretCfg.EnvVar] = value

		case "proxy":
			token, err := secrets.GenerateSessionToken()
			if err != nil {
				return nil, nil, fmt.Errorf("secret %q: generating token: %w", name, err)
			}
			sessionTokens[name] = token
			env[secretCfg.EnvVar] = token

			switch secretCfg.Provider {
			case "anthropic":
				env["ANTHROPIC_BASE_URL"] = proxyBaseURL
			case "openai":
				env["OPENAI_BASE_URL"] = proxyBaseURL
			case "ollama":
				env["OLLAMA_HOST"] = proxyBaseURL
			}

			env["SESSION_TOKEN"] = token
			env["SESSION_ID"] = name
		}
	}

	return env, sessionTokens, nil
}

// resolveToolEnv processes a tool's env map, resolving "inject:secret_name"
// references from the secret store.
func (o *Orchestrator) resolveToolEnv(toolEnv map[string]string) map[string]string {
	resolved := make(map[string]string)
	for k, v := range toolEnv {
		if strings.HasPrefix(v, "inject:") {
			secretName := strings.TrimPrefix(v, "inject:")
			realValue, err := o.secrets.Get(secretName)
			if err != nil {
				o.logger.Printf("warning: tool env %s: secret %q not found, skipping", k, secretName)
				continue
			}
			resolved[k] = realValue
		} else {
			resolved[k] = v
		}
	}
	return resolved
}

// cleanupNetwork removes a Docker network if the provisioner supports it.
func (o *Orchestrator) cleanupNetwork(ctx context.Context, networkID string) {
	if networkID == "" {
		return
	}
	if dp, ok := o.provisioner.(*provisioner.DockerProvisioner); ok {
		if err := dp.RemoveNetwork(ctx, networkID); err != nil {
			o.logger.Printf("warning: cleanup network: %v", err)
		}
	}
}

// cleanupToolContainers destroys tool sidecar containers.
func (o *Orchestrator) cleanupToolContainers(ctx context.Context, ids []string) {
	for _, id := range ids {
		_ = o.provisioner.Stop(ctx, id)
		_ = o.provisioner.Destroy(ctx, id)
	}
}

// registerProxySession registers a session with the LLM proxy.
func (o *Orchestrator) registerProxySession(token, provider, apiKey, upstreamURL, sandboxID string) error {
	body := map[string]string{
		"token":      token,
		"provider":   provider,
		"api_key":    apiKey,
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
