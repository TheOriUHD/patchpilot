package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/core"
)

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

func TestRenderWizardBootstrapClampsProgress(t *testing.T) {
	t.Parallel()

	model := newModel(context.Background(), "/tmp/patchpilot.sock")
	model.wizard = newStandardWizard()
	model.wizard.step = wizardStepBootstrapping
	for index := 0; index < 50; index++ {
		message := fmt.Sprintf("step %02d", index)
		if index == 20 {
			message = "Supported modules: " + strings.Repeat("apt-solver ", 80)
		}
		model.wizard.events = append(model.wizard.events, core.BootstrapEvent{Step: fmt.Sprintf("Resolving command path %02d", index), Status: core.BootstrapStepRunning, Message: message})
	}
	model.wizard.events = append(model.wizard.events, core.BootstrapEvent{Step: "Verifying NOPASSWD sudo", Status: core.BootstrapStepSuccess, Message: "done"})

	rendered := model.renderWizardBootstrap(18)
	if height := lipgloss.Height(rendered); height > 18 {
		t.Fatalf("bootstrap modal height = %d, want <= 18\n%s", height, rendered)
	}
	if !strings.Contains(rendered, "Verifying NOPASSWD sudo") {
		t.Fatalf("bootstrap modal should auto-tail latest event: %s", rendered)
	}
	if strings.Contains(rendered, "apt-solver apt-solver apt-solver apt-solver apt-solver apt-solver apt-solver apt-solver apt-solver") {
		t.Fatalf("bootstrap modal leaked long apt dump: %s", rendered)
	}
}
