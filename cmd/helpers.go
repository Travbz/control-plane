package cmd

import "os"

// userHomeDir returns the user's home directory.
func userHomeDir() (string, error) {
	return os.UserHomeDir()
}
