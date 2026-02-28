package main

import (
	"fmt"
	"log"
	"os"

	"control-plane/cmd"
)

func main() {
	logger := log.New(os.Stderr, "[control-plane] ", log.LstdFlags)

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	subcommand := os.Args[1]
	args := os.Args[2:]

	var err error
	switch subcommand {
	case "up":
		err = cmd.Up(args, logger)
	case "down":
		err = cmd.Down(args, logger)
	case "status":
		err = cmd.Status(args, logger)
	case "secrets":
		err = cmd.Secrets(args, logger)
	case "serve":
		err = cmd.Serve(args, logger)
	case "help", "--help", "-h":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", subcommand)
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `control-plane - Agent sandbox orchestrator

Usage:
  control-plane <command> [options]

Commands:
  up        Start a sandbox
  down      Stop and destroy a sandbox
  status    Show sandbox status
  secrets   Manage the secret store (add, rm, list)
  serve     Run as an HTTP server (for api-gateway integration)
  help      Show this help message

Run 'control-plane <command> --help' for command-specific options.
`)
}
