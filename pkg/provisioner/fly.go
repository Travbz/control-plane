package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FlyProvisioner implements the Provisioner interface using Fly Machines API.
// Each sandbox is a Fly Machine (Firecracker VM under the hood), providing
// full hardware-level isolation suitable for multi-tenant production.
type FlyProvisioner struct {
	appName string
	region  string
	size    string
	token   string
	baseURL string
	client  *http.Client
}

// FlyConfig holds Fly-specific configuration.
type FlyConfig struct {
	App    string // Fly app name
	Region string // e.g. "iad", "lhr"
	Size   string // e.g. "shared-cpu-1x", "performance-2x"
	Token  string // Fly API token
}

// NewFlyProvisioner creates a Fly Machines provisioner.
func NewFlyProvisioner(cfg FlyConfig) *FlyProvisioner {
	return &FlyProvisioner{
		appName: cfg.App,
		region:  cfg.Region,
		size:    cfg.Size,
		token:   cfg.Token,
		baseURL: "https://api.machines.dev/v1",
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// flyMachineConfig is the Fly Machines API request body for creating a machine.
type flyMachineConfig struct {
	Name   string              `json:"name,omitempty"`
	Region string              `json:"region,omitempty"`
	Config flyMachineConfigInner `json:"config"`
}

type flyMachineConfigInner struct {
	Image    string            `json:"image"`
	Env      map[string]string `json:"env,omitempty"`
	Guest    *flyGuest         `json:"guest,omitempty"`
	Mounts   []flyMount        `json:"mounts,omitempty"`
	AutoDestroy bool           `json:"auto_destroy"`
}

type flyGuest struct {
	CPUKind string `json:"cpu_kind"`
	CPUs    int    `json:"cpus"`
	MemoryMB int   `json:"memory_mb"`
}

type flyMount struct {
	Volume string `json:"volume"`
	Path   string `json:"path"`
}

type flyMachineResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	PrivateIP  string `json:"private_ip"`
}

func (f *FlyProvisioner) Create(ctx context.Context, opts CreateOpts) (*Sandbox, error) {
	env := make(map[string]string)
	for k, v := range opts.Env {
		env[k] = v
	}

	guest := f.resolveGuest(opts)

	body := flyMachineConfig{
		Name:   opts.Name,
		Region: f.region,
		Config: flyMachineConfigInner{
			Image:       opts.Image,
			Env:         env,
			Guest:       guest,
			AutoDestroy: true,
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling machine config: %w", err)
	}

	url := fmt.Sprintf("%s/apps/%s/machines", f.baseURL, f.appName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	f.setHeaders(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fly create machine: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fly create machine: status %d: %s", resp.StatusCode, errBody)
	}

	var machine flyMachineResponse
	if err := json.NewDecoder(resp.Body).Decode(&machine); err != nil {
		return nil, fmt.Errorf("fly decode response: %w", err)
	}

	return &Sandbox{
		ID:     machine.ID,
		Name:   machine.Name,
		Status: machine.State,
		IP:     machine.PrivateIP,
	}, nil
}

func (f *FlyProvisioner) Start(ctx context.Context, id string) error {
	return f.machineAction(ctx, id, "start")
}

func (f *FlyProvisioner) Stop(ctx context.Context, id string) error {
	return f.machineAction(ctx, id, "stop")
}

func (f *FlyProvisioner) Destroy(ctx context.Context, id string) error {
	url := fmt.Sprintf("%s/apps/%s/machines/%s?force=true", f.baseURL, f.appName, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	f.setHeaders(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("fly destroy machine: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fly destroy machine: status %d: %s", resp.StatusCode, errBody)
	}
	return nil
}

func (f *FlyProvisioner) Status(ctx context.Context, id string) (*Sandbox, error) {
	url := fmt.Sprintf("%s/apps/%s/machines/%s", f.baseURL, f.appName, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	f.setHeaders(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fly get machine: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fly get machine: status %d: %s", resp.StatusCode, errBody)
	}

	var machine flyMachineResponse
	if err := json.NewDecoder(resp.Body).Decode(&machine); err != nil {
		return nil, err
	}

	return &Sandbox{
		ID:     machine.ID,
		Name:   machine.Name,
		Status: machine.State,
		IP:     machine.PrivateIP,
	}, nil
}

func (f *FlyProvisioner) List(ctx context.Context) ([]*Sandbox, error) {
	url := fmt.Sprintf("%s/apps/%s/machines", f.baseURL, f.appName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	f.setHeaders(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fly list machines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fly list machines: status %d: %s", resp.StatusCode, errBody)
	}

	var machines []flyMachineResponse
	if err := json.NewDecoder(resp.Body).Decode(&machines); err != nil {
		return nil, err
	}

	result := make([]*Sandbox, len(machines))
	for i, m := range machines {
		result[i] = &Sandbox{
			ID:     m.ID,
			Name:   m.Name,
			Status: m.State,
			IP:     m.PrivateIP,
		}
	}
	return result, nil
}

func (f *FlyProvisioner) machineAction(ctx context.Context, id, action string) error {
	url := fmt.Sprintf("%s/apps/%s/machines/%s/%s", f.baseURL, f.appName, id, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	f.setHeaders(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("fly %s machine: %w", action, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fly %s machine: status %d: %s", action, resp.StatusCode, errBody)
	}
	return nil
}

func (f *FlyProvisioner) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
}

func (f *FlyProvisioner) resolveGuest(opts CreateOpts) *flyGuest {
	// Map size presets to guest configs.
	switch f.size {
	case "shared-cpu-1x":
		return &flyGuest{CPUKind: "shared", CPUs: 1, MemoryMB: 256}
	case "shared-cpu-2x":
		return &flyGuest{CPUKind: "shared", CPUs: 2, MemoryMB: 512}
	case "performance-1x":
		return &flyGuest{CPUKind: "performance", CPUs: 1, MemoryMB: 2048}
	case "performance-2x":
		return &flyGuest{CPUKind: "performance", CPUs: 2, MemoryMB: 4096}
	default:
		return &flyGuest{CPUKind: "shared", CPUs: 1, MemoryMB: 256}
	}
}
