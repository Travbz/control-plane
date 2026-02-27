package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// UnikraftProvisioner implements Provisioner using the Unikraft Cloud
// (kraft.cloud) REST API. It creates and manages micro-VM instances.
type UnikraftProvisioner struct {
	apiURL     string
	token      string
	httpClient *http.Client
}

// NewUnikraftProvisioner creates a new Unikraft Cloud provisioner.
// If apiURL is empty, it defaults to https://api.kraft.cloud.
// The API token is read from the UKC_TOKEN environment variable.
func NewUnikraftProvisioner(apiURL string) *UnikraftProvisioner {
	if apiURL == "" {
		apiURL = "https://api.kraft.cloud"
	}
	return &UnikraftProvisioner{
		apiURL: apiURL,
		token:  os.Getenv("UKC_TOKEN"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ukcInstance is the Unikraft Cloud instance representation.
type ukcInstance struct {
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Image  string `json:"image"`
	FQDN   string `json:"fqdn,omitempty"`
	IP     string `json:"private_ip,omitempty"`
	Memory int    `json:"memory_mb,omitempty"`
}

// ukcCreateRequest is the payload for creating a new instance.
type ukcCreateRequest struct {
	Name   string            `json:"name"`
	Image  string            `json:"image"`
	Memory int               `json:"memory_mb,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Args   []string          `json:"args,omitempty"`
	Ports  []ukcPort         `json:"ports,omitempty"`
}

// ukcPort defines a port mapping for a Unikraft instance.
type ukcPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Handler  string `json:"handler,omitempty"`
}

// Create provisions a new Unikraft Cloud instance.
func (u *UnikraftProvisioner) Create(ctx context.Context, opts CreateOpts) (*Sandbox, error) {
	if u.token == "" {
		return nil, fmt.Errorf("UKC_TOKEN environment variable is not set")
	}

	req := ukcCreateRequest{
		Name:   opts.Name,
		Image:  opts.Image,
		Memory: 512, // Default 512MB, can be configurable later.
		Env:    opts.Env,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling create request: %w", err)
	}

	resp, err := u.doRequest(ctx, http.MethodPost, "/instances", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("unikraft create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unikraft create: status %d: %s", resp.StatusCode, errBody)
	}

	var instance ukcInstance
	if err := json.NewDecoder(resp.Body).Decode(&instance); err != nil {
		return nil, fmt.Errorf("unikraft create: decode response: %w", err)
	}

	return &Sandbox{
		ID:     instance.UUID,
		Name:   instance.Name,
		Status: instance.State,
		IP:     instance.IP,
	}, nil
}

// Start starts a Unikraft Cloud instance.
func (u *UnikraftProvisioner) Start(ctx context.Context, id string) error {
	if u.token == "" {
		return fmt.Errorf("UKC_TOKEN environment variable is not set")
	}

	resp, err := u.doRequest(ctx, http.MethodPut, fmt.Sprintf("/instances/%s/start", id), nil)
	if err != nil {
		return fmt.Errorf("unikraft start: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unikraft start: status %d: %s", resp.StatusCode, errBody)
	}

	return nil
}

// Stop stops a running Unikraft Cloud instance.
func (u *UnikraftProvisioner) Stop(ctx context.Context, id string) error {
	if u.token == "" {
		return fmt.Errorf("UKC_TOKEN environment variable is not set")
	}

	resp, err := u.doRequest(ctx, http.MethodPut, fmt.Sprintf("/instances/%s/stop", id), nil)
	if err != nil {
		return fmt.Errorf("unikraft stop: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unikraft stop: status %d: %s", resp.StatusCode, errBody)
	}

	return nil
}

// Destroy removes a Unikraft Cloud instance.
func (u *UnikraftProvisioner) Destroy(ctx context.Context, id string) error {
	if u.token == "" {
		return fmt.Errorf("UKC_TOKEN environment variable is not set")
	}

	resp, err := u.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/instances/%s", id), nil)
	if err != nil {
		return fmt.Errorf("unikraft destroy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unikraft destroy: status %d: %s", resp.StatusCode, errBody)
	}

	return nil
}

// Status returns the current status of a Unikraft Cloud instance.
func (u *UnikraftProvisioner) Status(ctx context.Context, id string) (*Sandbox, error) {
	if u.token == "" {
		return nil, fmt.Errorf("UKC_TOKEN environment variable is not set")
	}

	resp, err := u.doRequest(ctx, http.MethodGet, fmt.Sprintf("/instances/%s", id), nil)
	if err != nil {
		return nil, fmt.Errorf("unikraft status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unikraft status: status %d: %s", resp.StatusCode, errBody)
	}

	var instance ukcInstance
	if err := json.NewDecoder(resp.Body).Decode(&instance); err != nil {
		return nil, fmt.Errorf("unikraft status: decode: %w", err)
	}

	return &Sandbox{
		ID:     instance.UUID,
		Name:   instance.Name,
		Status: instance.State,
		IP:     instance.IP,
	}, nil
}

// List returns all instances from Unikraft Cloud.
func (u *UnikraftProvisioner) List(ctx context.Context) ([]*Sandbox, error) {
	if u.token == "" {
		return nil, fmt.Errorf("UKC_TOKEN environment variable is not set")
	}

	resp, err := u.doRequest(ctx, http.MethodGet, "/instances", nil)
	if err != nil {
		return nil, fmt.Errorf("unikraft list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unikraft list: status %d: %s", resp.StatusCode, errBody)
	}

	var instances []ukcInstance
	if err := json.NewDecoder(resp.Body).Decode(&instances); err != nil {
		return nil, fmt.Errorf("unikraft list: decode: %w", err)
	}

	sandboxes := make([]*Sandbox, len(instances))
	for i, inst := range instances {
		sandboxes[i] = &Sandbox{
			ID:     inst.UUID,
			Name:   inst.Name,
			Status: inst.State,
			IP:     inst.IP,
		}
	}

	return sandboxes, nil
}

// doRequest executes an authenticated HTTP request against the Unikraft Cloud API.
func (u *UnikraftProvisioner) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := u.apiURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return u.httpClient.Do(req)
}
