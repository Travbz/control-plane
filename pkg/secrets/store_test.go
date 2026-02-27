package secrets

import (
	"strings"
	"testing"
)

func TestStore_SetAndGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if err := store.Set("anthropic_key", "sk-ant-123"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err := store.Get("anthropic_key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "sk-ant-123" {
		t.Errorf("Get() = %q, want %q", got, "sk-ant-123")
	}
}

func TestStore_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.Get("nonexistent")
	if err == nil {
		t.Fatal("Get() expected error for nonexistent secret")
	}
}

func TestStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_ = store.Set("key", "value")
	_ = store.Delete("key")

	_, err = store.Get("key")
	if err == nil {
		t.Fatal("Get() expected error after Delete()")
	}
}

func TestStore_List(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_ = store.Set("a", "1")
	_ = store.Set("b", "2")

	names := store.List()
	if len(names) != 2 {
		t.Fatalf("List() returned %d names, want 2", len(names))
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Write a secret.
	store1, _ := NewStore(dir)
	_ = store1.Set("persistent", "value123")

	// Reopen the store and verify.
	store2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() reopen error = %v", err)
	}

	got, err := store2.Get("persistent")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "value123" {
		t.Errorf("Get() = %q, want %q", got, "value123")
	}
}

func TestGenerateSessionToken(t *testing.T) {
	token, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken() error = %v", err)
	}

	if !strings.HasPrefix(token, "session-") {
		t.Errorf("token %q does not have session- prefix", token)
	}

	// session- (8) + 64 hex chars = 72
	if len(token) != 72 {
		t.Errorf("token length = %d, want 72", len(token))
	}

	// Two tokens should be different.
	token2, _ := GenerateSessionToken()
	if token == token2 {
		t.Error("two generated tokens are identical")
	}
}
