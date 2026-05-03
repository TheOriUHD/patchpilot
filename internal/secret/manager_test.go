package secret

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAESGCMRoundTripAndWrongKey(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{1}, KeySize)
	wrongKey := bytes.Repeat([]byte{2}, KeySize)
	plaintext := []byte("token-value")
	ciphertext, nonce, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}
	decrypted, err := Decrypt(ciphertext, nonce, key)
	if err != nil {
		t.Fatalf("Decrypt() error: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted = %q, want %q", decrypted, plaintext)
	}
	if _, err := Decrypt(ciphertext, nonce, wrongKey); err == nil {
		t.Fatalf("Decrypt() with wrong key succeeded")
	}
}

func TestManagerCreates0600Key(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "secret.key")
	manager := NewManager(path)
	key, err := manager.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	if len(key) != KeySize {
		t.Fatalf("key length = %d, want %d", len(key), KeySize)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o, want 0600", info.Mode().Perm())
	}
}
