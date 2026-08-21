package agentendpoint

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	serviceendpoints "github.com/futrx-com/remote.futrx.com/internal/service/agentendpoints"
)

// stalledRunner stands in for an agent CLI that a third-party endpoint has
// refused: it prints a line and then never returns, which is what the real
// binary does when it is retrying a rejected key.
type stalledRunner struct {
	partial string
}

func (r stalledRunner) Available() bool { return true }

func (r stalledRunner) Run(ctx context.Context, _ ...string) (string, error) {
	<-ctx.Done()
	return r.partial, ctx.Err()
}

func (r stalledRunner) RunStdin(ctx context.Context, _ io.Reader, _ ...string) (string, error) {
	<-ctx.Done()
	return r.partial, ctx.Err()
}

// TestProbeSurfacesTimeoutSentinel checks the sentinel and the wording through
// the real code path, with a runner that never answers.
func TestProbeSurfacesTimeoutSentinel(t *testing.T) {
	original := probeTimeout
	probeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { probeTimeout = original })

	client := NewClient(stalledRunner{partial: "⚠ connectors are disabled"})
	text, err := client.Probe(context.Background(), "wp-test", serviceendpoints.Probe{
		CLI:    serviceendpoints.CLIClaude,
		Model:  "glm-4.6",
		Prompt: "say ok",
	})

	if !errors.Is(err, ErrProbeTimedOut) {
		t.Fatalf("want ErrProbeTimedOut, got %v", err)
	}
	for _, want := range []string{"rejected the key", "kept retrying"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message should mention %q, got %q", want, err.Error())
		}
	}
	if !strings.Contains(text, "connectors are disabled") {
		t.Errorf("whatever the CLI managed to print should still come back, got %q", text)
	}
}

// TestProbeKeepsCallerCancellationDistinct makes sure an operator navigating
// away is not reported as a rejected key.
func TestProbeKeepsCallerCancellationDistinct(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewClient(stalledRunner{})
	if _, err := client.Probe(ctx, "wp-test", serviceendpoints.Probe{
		CLI: serviceendpoints.CLIClaude,
	}); errors.Is(err, ErrProbeTimedOut) {
		t.Fatalf("a canceled caller is not a probe timeout, got %v", err)
	}
}
