package cmd

import (
	"context"
	"flag"
	"fmt"
	"log"

	"control-plane/pkg/config"
	"control-plane/pkg/orchestrator"
	"control-plane/pkg/secrets"
)

// Down implements the "down" subcommand: stop and destroy a sandbox.
func Down(args []string, logger *log.Logger) error {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	configPath := fs.String("config", "sandbox.toml", "Path to sandbox.toml")
	id := fs.String("id", "", "Sandbox ID to stop")
	secretsDir := fs.String("secrets-dir", "", "Path to secrets directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *id == "" {
		return fmt.Errorf("--id is required")
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

	store, err := secrets.NewStore(sDir)
	if err != nil {
		return fmt.Errorf("opening secret store: %w", err)
	}

	prov := resolveProvisioner(cfg)

	proxyAddr := cfg.Proxy.Addr
	if proxyAddr == "" {
		proxyAddr = ":8090"
	}

	orch := orchestrator.New(cfg, prov, store, proxyAddr, logger)

	if err := orch.Down(context.Background(), *id); err != nil {
		return fmt.Errorf("stopping sandbox: %w", err)
	}

	fmt.Printf("Sandbox %s destroyed\n", *id)
	return nil
}
