package drain

import (
	"errors"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

type fixedGate bool

func (g fixedGate) Blocked() bool { return bool(g) }

func TestGateClosesOnDrainAndSaysWhy(t *testing.T) {
	gate := NewGate(fixedGate(false))
	if gate.Blocked() || gate.BlockedReason() != nil {
		t.Fatal("an open gate refused a run")
	}
	gate.Start()
	if !gate.Blocked() || !errors.Is(gate.BlockedReason(), prompt.ErrServerRestarting) {
		t.Fatalf("draining gate: blocked=%v reason=%v", gate.Blocked(), gate.BlockedReason())
	}
}

func TestGateStillHonoursTheMaintenanceWindow(t *testing.T) {
	gate := NewGate(fixedGate(true))
	if !gate.Blocked() {
		t.Fatal("maintenance window ignored")
	}
	if gate.BlockedReason() != nil {
		t.Fatal("maintenance must keep its own message")
	}
	if NewGate(nil).Blocked() {
		t.Fatal("a nil inner gate blocked runs")
	}
}
