package runtime

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
)

type noOpParser struct{}

func (noOpParser) ParseLine([]byte) ([]agent.Event, error) { return nil, nil }

func TestRunProcessReturnsCapturedStderr(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo no rollout found for thread >&2; exit 1")
	err := RunProcess(context.Background(), cmd, noOpParser{}, nil, ProcessOptions{Name: "test"})
	if err == nil || !strings.Contains(ErrorStderr(err), "no rollout found") {
		t.Fatalf("error = %v, stderr = %q", err, ErrorStderr(err))
	}
	var processErr *ProcessError
	if !errors.As(err, &processErr) {
		t.Fatalf("error type = %T, want ProcessError", err)
	}
}

func TestRunProcessHandsStderrToACleanExit(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo stopped: permission denied >&2; exit 0")
	var lines []string
	err := RunProcess(context.Background(), cmd, noOpParser{}, nil, ProcessOptions{
		Name:     "test",
		OnStderr: func(line string) { lines = append(lines, line) },
	})
	if err != nil {
		t.Fatalf("clean exit returned %v", err)
	}
	if len(lines) != 1 || lines[0] != "stopped: permission denied" {
		t.Fatalf("stderr lines = %q", lines)
	}
}
