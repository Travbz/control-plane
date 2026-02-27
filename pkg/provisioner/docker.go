package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// DockerProvisioner implements Provisioner using the Docker Engine API.
// It communicates with the Docker daemon over a Unix socket.
type DockerProvisioner struct {
	client *http.Client
	host   string // e.g. "unix:///var/run/docker.sock"
}

// NewDockerProvisioner creates a new Docker provisioner.
// The host defaults to the local Docker socket.
func NewDockerProvisioner(host string) *DockerProvisioner {
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}

	socketPath := strings.TrimPrefix(host, "unix://")

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}

	return &DockerProvisioner{
		client: client,
		host:   host,
	}
}

// Create creates a new Docker container.
func (d *DockerProvisioner) Create(ctx context.Context, opts CreateOpts) (*Sandbox, error) {
	// Build environment variables list.
	var env []string
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}

	// Build bind mounts.
	var binds []string
	for _, m := range opts.Mounts {
		bind := m.HostPath + ":" + m.GuestPath
		if m.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}

	// Docker Engine API create container payload.
	body := map[string]any{
		"Image": opts.Image,
		"Env":   env,
		"HostConfig": map[string]any{
			"Binds":       binds,
			"NetworkMode": "bridge",
			// Add host.docker.internal so sandbox can reach the proxy.
			"ExtraHosts": []string{"host.docker.internal:host-gateway"},
		},
		"Labels": map[string]string{
			"managed-by": "control-plane",
		},
	}

	containerName := opts.Name
	if containerName == "" {
		containerName = "sandbox"
	}

	resp, err := d.apiPost(ctx, fmt.Sprintf("/containers/create?name=%s", containerName), body)
	if err != nil {
		return nil, fmt.Errorf("docker create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("docker create: status %d: %s", resp.StatusCode, errBody)
	}

	var result struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("docker create: decode response: %w", err)
	}

	return &Sandbox{
		ID:     result.ID,
		Name:   containerName,
		Status: "created",
	}, nil
}

// Start starts a Docker container.
func (d *DockerProvisioner) Start(ctx context.Context, id string) error {
	resp, err := d.apiPost(ctx, fmt.Sprintf("/containers/%s/start", id), nil)
	if err != nil {
		return fmt.Errorf("docker start: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker start: status %d: %s", resp.StatusCode, errBody)
	}

	return nil
}

// Stop stops a Docker container.
func (d *DockerProvisioner) Stop(ctx context.Context, id string) error {
	resp, err := d.apiPost(ctx, fmt.Sprintf("/containers/%s/stop", id), nil)
	if err != nil {
		return fmt.Errorf("docker stop: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker stop: status %d: %s", resp.StatusCode, errBody)
	}

	return nil
}

// Destroy removes a Docker container.
func (d *DockerProvisioner) Destroy(ctx context.Context, id string) error {
	resp, err := d.apiDelete(ctx, fmt.Sprintf("/containers/%s?force=true", id))
	if err != nil {
		return fmt.Errorf("docker destroy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker destroy: status %d: %s", resp.StatusCode, errBody)
	}

	return nil
}

// Status returns the current status of a Docker container.
func (d *DockerProvisioner) Status(ctx context.Context, id string) (*Sandbox, error) {
	resp, err := d.apiGet(ctx, fmt.Sprintf("/containers/%s/json", id))
	if err != nil {
		return nil, fmt.Errorf("docker inspect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("docker inspect: status %d: %s", resp.StatusCode, errBody)
	}

	var result struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		NetworkSettings struct {
			IPAddress string `json:"IPAddress"`
		} `json:"NetworkSettings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("docker inspect: decode: %w", err)
	}

	return &Sandbox{
		ID:     result.ID,
		Name:   strings.TrimPrefix(result.Name, "/"),
		Status: result.State.Status,
		IP:     result.NetworkSettings.IPAddress,
	}, nil
}

// List returns all containers with the managed-by=control-plane label.
func (d *DockerProvisioner) List(ctx context.Context) ([]*Sandbox, error) {
	resp, err := d.apiGet(ctx, `/containers/json?all=true&filters={"label":["managed-by=control-plane"]}`)
	if err != nil {
		return nil, fmt.Errorf("docker list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("docker list: status %d: %s", resp.StatusCode, errBody)
	}

	var containers []struct {
		ID     string            `json:"Id"`
		Names  []string          `json:"Names"`
		State  string            `json:"State"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, fmt.Errorf("docker list: decode: %w", err)
	}

	sandboxes := make([]*Sandbox, len(containers))
	for i, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		sandboxes[i] = &Sandbox{
			ID:     c.ID,
			Name:   name,
			Status: c.State,
		}
	}

	return sandboxes, nil
}

// apiGet performs a GET request to the Docker Engine API.
func (d *DockerProvisioner) apiGet(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	return d.client.Do(req)
}

// apiPost performs a POST request to the Docker Engine API.
func (d *DockerProvisioner) apiPost(ctx context.Context, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker"+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return d.client.Do(req)
}

// apiDelete performs a DELETE request to the Docker Engine API.
func (d *DockerProvisioner) apiDelete(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	return d.client.Do(req)
}
