package tui

import "testing"

func TestStandardWizardZeroSecrets(t *testing.T) {
	t.Parallel()

	wizard := standardWizard{password: []byte("user-secret"), rootPassword: []byte("root-secret")}
	password := wizard.password
	rootPassword := wizard.rootPassword
	wizard.zeroSecrets()
	for index, value := range password {
		if value != 0 {
			t.Fatalf("password[%d] = %d, want zero", index, value)
		}
	}
	for index, value := range rootPassword {
		if value != 0 {
			t.Fatalf("rootPassword[%d] = %d, want zero", index, value)
		}
	}
	if wizard.password != nil || wizard.rootPassword != nil {
		t.Fatalf("secret slices should be nil after zeroing")
	}
}
