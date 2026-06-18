package core

import (
	"context"
	"strings"
	"testing"
)

func TestSetAPIHostCredentialAuditRedactsToken(t *testing.T) {
	t.Parallel()

	store := &credentialHostStore{host: Host{ID: 1, Name: "nas", Type: HostTypeTrueNAS}}
	audit := &recordingAuditStore{}
	service := NewHostService(store, audit, nil, nil, nil, fakeSecretCipher{})
	const secretToken = "dummy-secret-token"
	host, err := service.SetAPIHostCredential(context.Background(), 1, APIHostCredentialInput{APIToken: []byte(secretToken), APIUsername: "root"})
	if err != nil {
		t.Fatalf("SetAPIHostCredential() error: %v", err)
	}
	if host.APIUsername != "root" {
		t.Fatalf("APIUsername = %q, want root", host.APIUsername)
	}
	if strings.Contains(audit.details, secretToken) {
		t.Fatalf("audit details leaked token: %s", audit.details)
	}
	if strings.Contains(store.token.Ciphertext, secretToken) || strings.Contains(store.token.Nonce, secretToken) {
		t.Fatalf("stored fake credential leaked token: %#v", store.token)
	}
}

type credentialHostStore struct {
	host  Host
	token EncryptedSecret
}

func (s *credentialHostStore) List(context.Context) ([]Host, error)             { return []Host{s.host}, nil }
func (s *credentialHostStore) Get(context.Context, int64) (Host, error)         { return s.host, nil }
func (s *credentialHostStore) Create(context.Context, HostCreate) (Host, error) { return Host{}, nil }
func (s *credentialHostStore) Update(context.Context, int64, HostUpdate) (Host, error) {
	return Host{}, nil
}
func (s *credentialHostStore) SetEnabled(context.Context, int64, bool) (Host, error) {
	return Host{}, nil
}
func (s *credentialHostStore) Delete(context.Context, int64) error { return nil }
func (s *credentialHostStore) SetAPICredential(_ context.Context, _ int64, token EncryptedSecret, apiUsername string) (Host, error) {
	s.token = token
	s.host.APIUsername = apiUsername
	return s.host, nil
}

type recordingAuditStore struct{ details string }

func (s *recordingAuditStore) Create(_ context.Context, event AuditEvent) error {
	s.details = event.Details
	return nil
}

type fakeSecretCipher struct{}

func (fakeSecretCipher) EncryptString(context.Context, string) (string, string, error) {
	return "ciphertext", "nonce", nil
}

func (fakeSecretCipher) DecryptString(context.Context, string, string) (string, error) {
	return "", nil
}
