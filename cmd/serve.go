package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"control-plane/pkg/config"
	"control-plane/pkg/orchestrator"
	"control-plane/pkg/secrets"
)

// Serve implements the "serve" subcommand: run the control plane as an HTTP server.
// This mode exposes the orchestrator over HTTP so external services (like the api-gateway)
// can provision and manage sandboxes programmatically.
func Serve(args []string, logger *log.Logger) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8091", "HTTP listen address")
	configPath := fs.String("config", "sandbox.toml", "Path to sandbox.toml")
	secretsDir := fs.String("secrets-dir", "", "Path to secrets directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	sDir := *secretsDir
	if sDir == "" {
		home, _ := userHomeDir()
		sDir = home + "/.config/control-plane/secrets"
	}

	store, err := secrets.NewFileStore(sDir)
	if err != nil {
		return fmt.Errorf("opening secret store: %w", err)
	}

	prov := resolveProvisioner(cfg)

	proxyAddr := cfg.Proxy.Addr
	if proxyAddr == "" {
		proxyAddr = ":8090"
	}

	orch := orchestrator.New(cfg, prov, store, proxyAddr, logger)

	mux := http.NewServeMux()

	// Health check.
	mux.HandleFunc("GET /internal/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Create and start a sandbox.
	mux.HandleFunc("POST /internal/v1/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			req.Name = "sandbox"
		}

		sandbox, err := orch.Up(context.Background(), req.Name)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(sandbox)
	})

	// Destroy a sandbox.
	mux.HandleFunc("DELETE /internal/v1/sandboxes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
			return
		}

		if err := orch.Down(context.Background(), id); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "destroyed"})
	})

	// Get sandbox status.
	mux.HandleFunc("GET /internal/v1/sandboxes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
			return
		}

		sandbox, err := orch.Status(context.Background(), id)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sandbox)
	})

	// List all sandboxes.
	mux.HandleFunc("GET /internal/v1/sandboxes", func(w http.ResponseWriter, _ *http.Request) {
		sandboxes, err := orch.List(context.Background())
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sandboxes)
	})

	logger.Printf("control-plane HTTP server listening on %s", *addr)
	return http.ListenAndServe(*addr, mux)
}
