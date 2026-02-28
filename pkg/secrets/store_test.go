package secrets

import (
	"strings"
	"testing"
)

func TestFileStore_SetAndGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
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

func TestFileStore_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	_, err = store.Get("nonexistent")
	if err == nil {
		t.Fatal("Get() expected error for nonexistent secret")
	}
}

func TestFileStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	_ = store.Set("key", "value")
	_ = store.Delete("key")

	_, err = store.Get("key")
	if err == nil {
		t.Fatal("Get() expected error after Delete()")
	}
}

func TestFileStore_List(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	_ = store.Set("a", "1")
	_ = store.Set("b", "2")

	names, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("List() returned %d names, want 2", len(names))
	}
}

func TestFileStore_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Write a secret.
	store1, _ := NewFileStore(dir)
	_ = store1.Set("persistent", "value123")

	// Reopen the store and verify.
	store2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore() reopen error = %v", err)
	}

	got, err := store2.Get("persistent")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "value123" {
		t.Errorf("Get() = %q, want %q", got, "value123")
	}
}

func TestEnvStore_GetFromEnv(t *testing.T) {
	t.Setenv("SECRET_MY_KEY", "env-value")

	store, err := NewEnvStore("", "SECRET_")
	if err != nil {
		t.Fatalf("NewEnvStore() error = %v", err)
	}

	got, err := store.Get("my_key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "env-value" {
		t.Errorf("Get() = %q, want %q", got, "env-value")
	}
}

func TestEnvStore_GetNotFound(t *testing.T) {
	store, err := NewEnvStore("", "SECRET_")
	if err != nil {
		t.Fatalf("NewEnvStore() error = %v", err)
	}

	_, err = store.Get("nonexistent")
	if err == nil {
		t.Fatal("Get() expected error for nonexistent secret")
	}
}

func TestEnvStore_SetAndGet(t *testing.T) {
	store, err := NewEnvStore("", "SECRET_")
	if err != nil {
		t.Fatalf("NewEnvStore() error = %v", err)
	}

	_ = store.Set("runtime_key", "runtime-value")

	got, err := store.Get("runtime_key")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "runtime-value" {
		t.Errorf("Get() = %q, want %q", got, "runtime-value")
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

// TestStoreInterface verifies both implementations satisfy the Store interface.
func TestStoreInterface(t *testing.T) {
	dir := t.TempDir()
	var _ Store = mustFileStore(t, dir)

	envStore, err := NewEnvStore("", "")
	if err != nil {
		t.Fatal(err)
	}
	var _ Store = envStore
}

func mustFileStore(t *testing.T, dir string) *FileStore {
	t.Helper()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
