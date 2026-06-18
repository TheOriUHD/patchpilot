package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheOriUHD/patchpilot/internal/core"
	"github.com/TheOriUHD/patchpilot/internal/stream"
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

func TestWizardDiagnoseUsesSuBootstrapPath(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), "/tmp/patchpilot.sock")
	m.wizard = newStandardWizard()
	m.wizard.step = wizardStepDiagnose
	m.wizard.hostname = "192.168.10.231"
	m.wizard.sshUser = "database"
	m.wizard.password = []byte("user-secret")
	m.wizard.rootPassword = []byte("root-secret")
	m.wizard.facts = core.HostFacts{SuPresent: true, SudoPresent: false}
	m.wizard.diagnosis = &core.PermissionDiagnosis{Kind: "sudo_missing", CanAutoFix: true}

	updated, cmd := m.handleWizardKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("su-capable diagnosis should start bootstrap command")
	}
	if m.wizard.step != wizardStepBootstrapping {
		t.Fatalf("wizard step = %d, want bootstrapping", m.wizard.step)
	}
}

func TestNextBootstrapEventPrefersBufferedCompleteOverClosedErrors(t *testing.T) {
	t.Parallel()

	summary := core.BootstrapSummary{Facts: core.HostFacts{Hostname: "mc-database"}}
	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	events := make(chan stream.Event, 1)
	errors := make(chan error)
	events <- stream.Event{Type: "bootstrap.complete", Data: payload}
	close(errors)

	msg := nextBootstrapEventCmd(events, errors)().(bootstrapEventMsg)
	if msg.done {
		t.Fatal("reader returned done while a complete event was buffered")
	}
	if msg.event.Type != "bootstrap.complete" {
		t.Fatalf("event type = %q, want bootstrap.complete", msg.event.Type)
	}

	m := newModel(context.Background(), "/tmp/patchpilot.sock")
	m.wizard = newStandardWizard()
	m.wizard.step = wizardStepBootstrapping
	updated, _ := m.handleBootstrapEvent(msg)
	m = updated
	if m.wizard.step != wizardStepReview {
		t.Fatalf("wizard step = %d, want review", m.wizard.step)
	}
}

func TestBootstrapCompleteUnmarshalErrorIsVisible(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), "/tmp/patchpilot.sock")
	m.wizard = newStandardWizard()
	m.wizard.step = wizardStepBootstrapping
	updated, _ := m.handleBootstrapEvent(bootstrapEventMsg{event: stream.Event{Type: "bootstrap.complete", Data: []byte("{")}})
	m = updated
	if m.wizard.step != wizardStepError {
		t.Fatalf("wizard step = %d, want error", m.wizard.step)
	}
	if !strings.Contains(m.wizard.errorMessage, "could not read the completion summary") {
		t.Fatalf("error message = %q", m.wizard.errorMessage)
	}
}
