// Package secrets manages the encrypted secret store and session token
// generation for the control plane.
package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateSessionToken creates a cryptographically random session token.
// The token is 32 bytes (64 hex characters) prefixed with "session-".
func GenerateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}
	return "session-" + hex.EncodeToString(b), nil
}
