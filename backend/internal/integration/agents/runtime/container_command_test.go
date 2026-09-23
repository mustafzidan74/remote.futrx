package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
)

func stubPush(t *testing.T, push func(ctx context.Context, container, path string, content []byte) error) {
	t.Helper()
	original := pushEnvironment
	pushEnvironment = push
	t.Cleanup(func() { pushEnvironment = original })
}

// When the environment file cannot be pushed, the run still starts with the
// environment on the command line, in the same order as before.
func TestBuildContainerCommandPreservesEnvironmentOrderAndPrecedence(t *testing.T) {
	stubPush(t, func(context.Context, string, string, []byte) error { return errors.New("no lxc") })
	command := BuildContainerCommand(context.Background(), ContainerCommandSpec{
		ContainerName:     "project-container",
		PrefixEnvironment: []string{"HOME=/root", "PROVIDER_HOME=/root/.provider"},
		Secrets: []agent.ProjectSecret{
			{Key: "FIRST_SECRET", Value: "first"},
			{Key: "EXCLUDED_SECRET", Value: "excluded"},
			{Key: "RUNTIME_VALUE", Value: "attacker"},
			{Key: "invalid-name", Value: "masked"},
		},
		ExcludedSecrets:   []string{"EXCLUDED_SECRET"},
		SuffixEnvironment: []string{"EXCLUDED_SECRET="},
		RuntimeEnvironment: map[string]string{
			"RUNTIME_VALUE": "trusted",
			"ALPHA_VALUE":   "sorted-first",
			"invalid-name":  "discarded",
		},
		Binary:    "provider-cli",
		Arguments: []string{"run", "--json"},
	})

	want := []string{
		"lxc", "exec", "--cwd", "/workspace",
		"--env", "HOME=/root",
		"--env", "PROVIDER_HOME=/root/.provider",
		"--env", "FIRST_SECRET=first",
		"--env", "EXCLUDED_SECRET=",
		"--env", "ALPHA_VALUE=sorted-first",
		"--env", "RUNTIME_VALUE=trusted",
		"project-container", "--", "provider-cli", "run", "--json",
	}
	if !slices.Equal(command.Args, want) {
		t.Fatalf("container command\n got: %#v\nwant: %#v", command.Args, want)
	}
}

func TestBuildContainerCommandKeepsTheEnvironmentOffTheCommandLine(t *testing.T) {
	var pushedTo, pushedPath string
	var pushed []byte
	stubPush(t, func(_ context.Context, container, path string, content []byte) error {
		pushedTo, pushedPath, pushed = container, path, content
		return nil
	})
	command := BuildContainerCommand(context.Background(), ContainerCommandSpec{
		ContainerName:     "project-container",
		PrefixEnvironment: []string{"HOME=/root"},
		Secrets: []agent.ProjectSecret{
			{Key: "WP_ADMIN_PASSWORD", Value: "s3cret-value"},
			{Key: "RUNTIME_VALUE", Value: "attacker"},
		},
		RuntimeEnvironment: map[string]string{"RUNTIME_VALUE": "trusted"},
		FinalEnvironment:   []string{"HOME=/root/final"},
		Binary:             "provider-cli",
		Arguments:          []string{"run"},
	})

	joined := strings.Join(command.Args, " ")
	if strings.Contains(joined, "s3cret-value") || strings.Contains(joined, "--env") {
		t.Fatalf("environment reached the command line: %#v", command.Args)
	}
	if pushedTo != "project-container" || !strings.HasPrefix(pushedPath, environmentDir+"/env-") {
		t.Fatalf("pushed to %q at %q", pushedTo, pushedPath)
	}
	want := []string{"lxc", "exec", "--cwd", "/workspace", "project-container", "--",
		"sh", "-c", sourceAndExec, pushedPath, "provider-cli", "run"}
	if !slices.Equal(command.Args, want) {
		t.Fatalf("container command\n got: %#v\nwant: %#v", command.Args, want)
	}
	wantFile := "HOME='/root'\nWP_ADMIN_PASSWORD='s3cret-value'\nRUNTIME_VALUE='trusted'\nHOME='/root/final'\n"
	if string(pushed) != wantFile {
		t.Fatalf("environment file\n got: %q\nwant: %q", pushed, wantFile)
	}
}

// The file is sourced by a real shell, so values must arrive byte for byte —
// quotes, dollars, backticks and newlines included — and the file must be gone
// before the program runs.
func TestEnvironmentFileSurvivesAShellByteForByte(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this machine")
	}
	values := map[string]string{
		"QUOTED":    `it's "quoted"`,
		"EXPANDING": "$HOME `id` $(id) \\n",
		"PEM":       "-----BEGIN KEY-----\nline two\n-----END KEY-----",
	}
	entries := []string{"QUOTED=" + values["QUOTED"], "EXPANDING=" + values["EXPANDING"],
		"PEM=" + values["PEM"], "not-a-name=dropped"}
	file := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(file, environmentFile(entries), 0o600); err != nil {
		t.Fatal(err)
	}
	for key, want := range values {
		out, err := exec.Command(sh, "-c", sourceAndExec, file, "printenv", key).Output()
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if got := strings.TrimSuffix(string(out), "\n"); got != want {
			t.Fatalf("%s\n got: %q\nwant: %q", key, got, want)
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("environment file still exists after %s", key)
		}
		if err := os.WriteFile(file, environmentFile(entries), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
