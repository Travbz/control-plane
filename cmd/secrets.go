package cmd

import (
	"flag"
	"fmt"
	"log"

	"control-plane/pkg/secrets"
)

// Secrets implements the "secrets" subcommand: manage the secret store.
func Secrets(args []string, logger *log.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: secrets <add|rm|list> [options]")
	}

	subCmd := args[0]
	subArgs := args[1:]

	switch subCmd {
	case "add":
		return secretsAdd(subArgs, logger)
	case "rm":
		return secretsRm(subArgs, logger)
	case "list":
		return secretsList(subArgs, logger)
	default:
		return fmt.Errorf("unknown secrets subcommand: %s (use add, rm, or list)", subCmd)
	}
}

func secretsAdd(args []string, logger *log.Logger) error {
	fs := flag.NewFlagSet("secrets add", flag.ExitOnError)
	secretsDir := fs.String("secrets-dir", "", "Path to secrets directory")
	name := fs.String("name", "", "Secret name")
	value := fs.String("value", "", "Secret value")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *name == "" || *value == "" {
		return fmt.Errorf("--name and --value are required")
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

	if err := store.Set(*name, *value); err != nil {
		return fmt.Errorf("setting secret: %w", err)
	}

	logger.Printf("secret %q stored", *name)
	fmt.Printf("Secret %q added\n", *name)
	return nil
}

func secretsRm(args []string, logger *log.Logger) error {
	fs := flag.NewFlagSet("secrets rm", flag.ExitOnError)
	secretsDir := fs.String("secrets-dir", "", "Path to secrets directory")
	name := fs.String("name", "", "Secret name to remove")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *name == "" {
		return fmt.Errorf("--name is required")
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

	if err := store.Delete(*name); err != nil {
		return fmt.Errorf("removing secret: %w", err)
	}

	logger.Printf("secret %q removed", *name)
	fmt.Printf("Secret %q removed\n", *name)
	return nil
}

func secretsList(args []string, _ *log.Logger) error {
	fs := flag.NewFlagSet("secrets list", flag.ExitOnError)
	secretsDir := fs.String("secrets-dir", "", "Path to secrets directory")
	if err := fs.Parse(args); err != nil {
		return err
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

	names, err := store.List()
	if err != nil {
		return fmt.Errorf("listing secrets: %w", err)
	}
	if len(names) == 0 {
		fmt.Println("No secrets stored")
		return nil
	}

	for _, name := range names {
		fmt.Println(name)
	}
	return nil
}
