package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	xssh "golang.org/x/crypto/ssh"
)

// KeyPair is the orchestrator SSH key material.
type KeyPair struct {
	PrivatePath string
	PublicPath  string
	PrivatePEM  []byte
	PublicKey   string
	Signer      xssh.Signer
}

// KeyStore manages the orchestrator SSH keypair on disk.
type KeyStore struct {
	dir string
}

// NewKeyStore creates a key store rooted at the supplied data directory.
func NewKeyStore(dataDir string) *KeyStore {
	return &KeyStore{dir: filepath.Join(dataDir, "ssh")}
}

// Ensure returns the current keypair, generating one if missing.
func (s *KeyStore) Ensure(ctx context.Context) (KeyPair, error) {
	if err := ctx.Err(); err != nil {
		return KeyPair{}, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return KeyPair{}, fmt.Errorf("create ssh key directory: %w", err)
	}

	privatePath := filepath.Join(s.dir, "patchpilot_ed25519")
	publicPath := privatePath + ".pub"
	if _, err := os.Stat(privatePath); err != nil {
		if !os.IsNotExist(err) {
			return KeyPair{}, fmt.Errorf("inspect private key: %w", err)
		}
		if err := generateKeypair(privatePath, publicPath); err != nil {
			return KeyPair{}, err
		}
	}

	privateRaw, err := os.ReadFile(privatePath)
	if err != nil {
		return KeyPair{}, fmt.Errorf("read private key: %w", err)
	}
	signer, err := xssh.ParsePrivateKey(privateRaw)
	if err != nil {
		return KeyPair{}, fmt.Errorf("parse private key: %w", err)
	}
	publicRaw, err := os.ReadFile(publicPath)
	if err != nil {
		return KeyPair{}, fmt.Errorf("read public key: %w", err)
	}

	return KeyPair{PrivatePath: privatePath, PublicPath: publicPath, PrivatePEM: append([]byte(nil), privateRaw...), PublicKey: string(publicRaw), Signer: signer}, nil
}

// GenerateInMemory creates a new keypair without writing it to disk.
func GenerateInMemory() (KeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate ed25519 key: %w", err)
	}
	privateBlock, err := xssh.MarshalPrivateKey(privateKey, "patchpilot")
	if err != nil {
		return KeyPair{}, fmt.Errorf("marshal private key: %w", err)
	}
	privatePEM := pem.EncodeToMemory(privateBlock)
	signer, err := xssh.ParsePrivateKey(privatePEM)
	if err != nil {
		return KeyPair{}, fmt.Errorf("parse generated private key: %w", err)
	}
	sshPublic, err := xssh.NewPublicKey(publicKey)
	if err != nil {
		return KeyPair{}, fmt.Errorf("marshal public key: %w", err)
	}
	return KeyPair{PrivatePEM: privatePEM, PublicKey: string(xssh.MarshalAuthorizedKey(sshPublic)), Signer: signer}, nil
}

// Replace writes keypair to disk, replacing the current keypair.
func (s *KeyStore) Replace(ctx context.Context, keypair KeyPair) (KeyPair, error) {
	if err := ctx.Err(); err != nil {
		return KeyPair{}, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return KeyPair{}, fmt.Errorf("create ssh key directory: %w", err)
	}
	privatePath := filepath.Join(s.dir, "patchpilot_ed25519")
	publicPath := privatePath + ".pub"
	if err := os.WriteFile(privatePath, keypair.PrivatePEM, 0o600); err != nil {
		return KeyPair{}, fmt.Errorf("write private key: %w", err)
	}
	if err := os.WriteFile(publicPath, []byte(keypair.PublicKey), 0o644); err != nil {
		return KeyPair{}, fmt.Errorf("write public key: %w", err)
	}
	return s.Ensure(ctx)
}

func generateKeypair(privatePath string, publicPath string) error {
	keypair, err := GenerateInMemory()
	if err != nil {
		return err
	}
	if err := os.WriteFile(privatePath, keypair.PrivatePEM, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if err := os.WriteFile(publicPath, []byte(keypair.PublicKey), 0o644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}
	return nil
}
