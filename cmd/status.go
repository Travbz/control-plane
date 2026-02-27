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

// Status implements the "status" subcommand: show sandbox status.
func Status(args []string, logger *log.Logger) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := fs.String("config", "sandbox.toml", "Path to sandbox.toml")
	id := fs.String("id", "", "Sandbox ID (if empty, lists all)")
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

	if *id != "" {
		// Show specific sandbox.
		sandbox, err := orch.Status(context.Background(), *id)
		if err != nil {
			return fmt.Errorf("getting status: %w", err)
		}
		fmt.Printf("%-12s %s\n", "ID:", sandbox.ID)
		fmt.Printf("%-12s %s\n", "Name:", sandbox.Name)
		fmt.Printf("%-12s %s\n", "Status:", sandbox.Status)
		if sandbox.IP != "" {
			fmt.Printf("%-12s %s\n", "IP:", sandbox.IP)
		}
	} else {
		// List all sandboxes.
		sandboxes, err := orch.List(context.Background())
		if err != nil {
			return fmt.Errorf("listing sandboxes: %w", err)
		}

		if len(sandboxes) == 0 {
			fmt.Println("No sandboxes found")
			return nil
		}

		fmt.Printf("%-16s %-20s %s\n", "ID", "NAME", "STATUS")
		for _, s := range sandboxes {
			short := s.ID
			if len(short) > 12 {
				short = short[:12]
			}
			fmt.Printf("%-16s %-20s %s\n", short, s.Name, s.Status)
		}
	}

	return nil
}
