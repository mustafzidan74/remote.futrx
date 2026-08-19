package mcp

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
)

// fakeRunner records every lxc invocation. For a `file push` it reads the
// staged host file while it still exists, which is the only way a test can
// see what would land inside the container.
type fakeRunner struct {
	available bool
	calls     [][]string
	pushed    map[string]string
	execOut   map[string]string
	execErr   error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{available: true, pushed: map[string]string{}, execOut: map[string]string{}}
}

func (r *fakeRunner) Available() bool { return r.available }

func (r *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) >= 2 && args[0] == "file" && args[1] == "push" {
		source := args[len(args)-2]
		destination := args[len(args)-1]
		data, err := os.ReadFile(source)
		if err != nil {
			return "", err
		}
		r.pushed[destination] = string(data)
		return "", nil
	}
	if len(args) >= 5 && args[0] == "exec" && args[2] == "--" && args[3] == "cat" {
		out, present := r.execOut[args[4]]
		if !present {
			return "", errors.New("exit status 1")
		}
		return out, nil
	}
	return "", r.execErr
}

func (r *fakeRunner) RunStdin(ctx context.Context, _ io.Reader, args ...string) (string, error) {
	return r.Run(ctx, args...)
}

// script returns the `sh -c` body of the last exec that carried one.
func (r *fakeRunner) script() string {
	script := ""
	for _, call := range r.calls {
		for index := 0; index+1 < len(call); index++ {
			if call[index] == "sh" && call[index+1] == "-c" && index+2 < len(call) {
				script = call[index+2]
			}
		}
	}
	return script
}

func sampleMaterial() servicemcp.Material {
	return servicemcp.MaterialFor(servicemcp.Resolution{Servers: []servicemcp.Server{
		{
			Name:      "fetch",
			Transport: servicemcp.TransportStdio,
			Command:   "uvx",
			Args:      []string{"mcp-server-fetch"},
		},
	}})
}

func TestApplyPushesBothConfigsAndTheManifest(t *testing.T) {
	runner := newFakeRunner()
	client := NewClient(runner)

	if err := client.Apply(context.Background(), "proj-p1", sampleMaterial(), nil); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	claude, ok := runner.pushed["proj-p1"+servicemcp.ClaudeConfigPath]
	if !ok || !strings.Contains(claude, `"mcpServers"`) {
		t.Fatalf("claude config = %q", claude)
	}
	region, ok := runner.pushed["proj-p1"+regionFile]
	if !ok || !strings.Contains(region, `[mcp_servers."fetch"]`) {
		t.Fatalf("codex region = %q", region)
	}
	manifest, ok := runner.pushed["proj-p1"+servicemcp.ManifestPath]
	if !ok || !strings.Contains(manifest, `"signature"`) {
		t.Fatalf("manifest = %q", manifest)
	}

	// Every push must be 0600: a rendered config can carry a resolved value.
	for _, call := range runner.calls {
		if len(call) >= 2 && call[0] == "file" && call[1] == "push" && call[2] != "--mode=0600" {
			t.Fatalf("push without 0600: %v", call)
		}
	}
}

func TestApplyScriptMergesTheManagedRegionAndPrunesStaleFiles(t *testing.T) {
	runner := newFakeRunner()
	client := NewClient(runner)

	stale := []string{"/root/.claude/old-mcp.json"}
	if err := client.Apply(context.Background(), "proj-p1", sampleMaterial(), stale); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	script := runner.script()
	for _, want := range []string{
		"BEGIN_MARK='" + servicemcp.ManagedBegin + "'",
		"END_MARK='" + servicemcp.ManagedEnd + "'",
		"TARGET='" + servicemcp.CodexConfigPath + "'",
		`rm -f '/root/.claude/old-mcp.json'`,
		`chmod 600 "$TARGET"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	// The region is stripped before it is re-appended, which is what makes a
	// second identical pass byte-identical.
	if !strings.Contains(script, "awk -v b=\"$BEGIN_MARK\"") {
		t.Fatalf("script does not strip the previous region:\n%s", script)
	}
}

func TestManifestReadsWhatThePreviousPassLeft(t *testing.T) {
	runner := newFakeRunner()
	runner.execOut[servicemcp.ManifestPath] =
		`{"version":1,"signature":"abc","files":["` + servicemcp.ClaudeConfigPath + `"],"claudeConfig":"` +
			servicemcp.ClaudeConfigPath + `"}`
	client := NewClient(runner)

	manifest, err := client.Manifest(context.Background(), "proj-p1")
	if err != nil {
		t.Fatalf("Manifest() error = %v", err)
	}
	if manifest.Signature != "abc" || manifest.ClaudeConfig != servicemcp.ClaudeConfigPath {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestManifestTreatsAMissingOrCorruptRecordAsAbsent(t *testing.T) {
	tests := []struct {
		name    string
		content *string
	}{
		{name: "missing", content: nil},
		{name: "corrupt", content: strPtr("{not json")},
		{name: "empty", content: strPtr("   ")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newFakeRunner()
			if tt.content != nil {
				runner.execOut[servicemcp.ManifestPath] = *tt.content
			}
			manifest, err := NewClient(runner).Manifest(context.Background(), "proj-p1")
			if err != nil {
				t.Fatalf("Manifest() error = %v", err)
			}
			if manifest.Version != 0 || manifest.Signature != "" {
				t.Fatalf("manifest = %#v, want the zero value", manifest)
			}
		})
	}
}

func TestApplyReportsAnUnavailableRuntime(t *testing.T) {
	runner := newFakeRunner()
	runner.available = false
	if err := NewClient(runner).Apply(context.Background(), "proj-p1", sampleMaterial(), nil); !errors.Is(err, command.ErrUnavailable) {
		t.Fatalf("Apply() error = %v, want ErrUnavailable", err)
	}
}

func TestProbeScriptQuotesEverythingItInterpolates(t *testing.T) {
	tests := []struct {
		name     string
		server   servicemcp.Server
		contains []string
		absent   []string
	}{
		{
			name: "stdio quotes the command and every argument",
			server: servicemcp.Server{
				Transport: servicemcp.TransportStdio,
				Command:   "npx",
				Args:      []string{"-y", "server; rm -rf /"},
			},
			contains: []string{`'npx' '-y' 'server; rm -rf /'`, "timeout 30"},
			absent:   []string{"| rm -rf"},
		},
		{
			name: "stdio survives a single quote in an argument",
			server: servicemcp.Server{
				Transport: servicemcp.TransportStdio,
				Command:   "sh",
				Args:      []string{"it's fine"},
			},
			contains: []string{`'it'\''s fine'`},
		},
		{
			name: "http quotes the url and every header",
			server: servicemcp.Server{
				Transport: servicemcp.TransportHTTP,
				URL:       "https://jira.example.com/mcp",
				Headers:   map[string]string{"Authorization": "Bearer tok"},
			},
			contains: []string{
				`-H 'Authorization: Bearer tok'`,
				`'https://jira.example.com/mcp'`,
				"curl",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := probeScript(tt.server)
			for _, want := range tt.contains {
				if !strings.Contains(script, want) {
					t.Fatalf("script missing %q:\n%s", want, script)
				}
			}
			for _, unwanted := range tt.absent {
				if strings.Contains(script, unwanted) {
					t.Fatalf("script contains %q:\n%s", unwanted, script)
				}
			}
		})
	}
}

func TestProbePassesEnvironmentThroughLXCRatherThanTheScript(t *testing.T) {
	runner := newFakeRunner()
	_, _ = NewClient(runner).Probe(context.Background(), "proj-p1", servicemcp.Server{
		Transport: servicemcp.TransportStdio,
		Command:   "npx",
		Env:       map[string]string{"PGPASSWORD": "s3cr3t"},
	})

	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	call := runner.calls[0]
	joined := strings.Join(call, " ")
	if !strings.Contains(joined, "--env PGPASSWORD=s3cr3t") {
		t.Fatalf("the value did not travel as --env: %v", call)
	}
	if strings.Contains(call[len(call)-1], "s3cr3t") {
		t.Fatalf("the value leaked into the script body: %s", call[len(call)-1])
	}
}

func strPtr(value string) *string { return &value }
