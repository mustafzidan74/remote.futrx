// Package drain lets the server finish the agent runs already in flight
// before it stops.
//
// A restart used to be immediate: systemd sent SIGTERM and the backend died
// on the spot. The agents themselves survived inside their containers
// (KillMode=process), but nothing was left reading their output, so a run
// that was halfway through a change lost the rest of its transcript and
// never recorded how it ended. Every deploy had to be timed by hand around
// whoever was working.
package drain

import (
	"sync/atomic"

	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// Gate closes the prompt service to new runs once draining starts, on top of
// whatever gate it wraps (the maintenance window).
type Gate struct {
	inner    prompt.StartGate
	draining atomic.Bool
}

// NewGate wraps inner, which may be nil.
func NewGate(inner prompt.StartGate) *Gate {
	return &Gate{inner: inner}
}

// Start closes the gate. It cannot be reopened: draining ends in an exit.
func (g *Gate) Start() {
	g.draining.Store(true)
}

// Draining reports whether Start has been called.
func (g *Gate) Draining() bool {
	return g.draining.Load()
}

// Blocked implements prompt.StartGate.
func (g *Gate) Blocked() bool {
	if g.draining.Load() {
		return true
	}
	return g.inner != nil && g.inner.Blocked()
}

// BlockedReason tells the chat why its prompt was refused, so a restart does
// not read as the maintenance window.
func (g *Gate) BlockedReason() error {
	if g.draining.Load() {
		return prompt.ErrServerRestarting
	}
	return nil
}
