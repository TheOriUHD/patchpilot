package secret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	// KeySize is the AES-256 key size in bytes.
	KeySize = 32
	// NonceSize is the GCM nonce size in bytes.
	NonceSize = 12
)

// Manager loads the daemon secret key and encrypts/decrypts host API tokens.
type Manager struct {
	path string

	mu  sync.Mutex
	key []byte
}

// NewManager creates a secret manager backed by path.
func NewManager(path string) *Manager {
	return &Manager{path: path}
}

// Ensure creates the key on first use and returns a copy of the loaded key.
func (m *Manager) Ensure(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.key) == KeySize {
		return append([]byte(nil), m.key...), nil
	}
	key, err := loadOrCreateKey(m.path)
	if err != nil {
		return nil, err
	}
	m.key = key
	return append([]byte(nil), key...), nil
}

// EncryptString encrypts plaintext and returns base64 ciphertext and nonce.
func (m *Manager) EncryptString(ctx context.Context, plaintext string) (string, string, error) {
	key, err := m.Ensure(ctx)
	if err != nil {
		return "", "", err
	}
	ciphertext, nonce, err := Encrypt([]byte(plaintext), key)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), base64.StdEncoding.EncodeToString(nonce), nil
}

// DecryptString decrypts base64 ciphertext and nonce.
func (m *Manager) DecryptString(ctx context.Context, encodedCiphertext string, encodedNonce string) (string, error) {
	key, err := m.Ensure(ctx)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(encodedNonce)
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	plaintext, err := Decrypt(ciphertext, nonce, key)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// Encrypt encrypts plaintext with AES-256-GCM and a random nonce.
func Encrypt(plaintext []byte, key []byte) ([]byte, []byte, error) {
	if len(key) != KeySize {
		return nil, nil, fmt.Errorf("secret key must be %d bytes", KeySize)
	}
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, NonceSize)
	if err != nil {
		return nil, nil, fmt.Errorf("create gcm: %w", err)
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nonce, nil
}

// Decrypt decrypts ciphertext with AES-256-GCM.
func Decrypt(ciphertext []byte, nonce []byte, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("secret key must be %d bytes", KeySize)
	}
	if len(nonce) != NonceSize {
		return nil, fmt.Errorf("nonce must be %d bytes", NonceSize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, NonceSize)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}
	return plaintext, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		if len(raw) != KeySize {
			return nil, fmt.Errorf("secret key %s must be %d bytes", path, KeySize)
		}
		return append([]byte(nil), raw...), nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read secret key %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create secret key directory: %w", err)
	}
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return loadOrCreateKey(path)
		}
		return nil, fmt.Errorf("create secret key %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Write(key); err != nil {
		return nil, fmt.Errorf("write secret key %s: %w", path, err)
	}
	return key, nil
}
