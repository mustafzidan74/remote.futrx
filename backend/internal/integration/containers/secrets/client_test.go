package secrets

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
)

// fakeRunner records every lxc invocation. For a `file push` it also reads the
// staged host file while it still exists, which is the only way a test can see
// what would land inside the container.
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
	if len(args) >= 4 && args[0] == "exec" && args[2] == "--" && args[3] == "cat" {
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

func (r *fakeRunner) script() string {
	for _, call := range r.calls {
		if len(call) >= 5 && call[0] == "exec" && call[3] == "sh" && call[4] == "-c" {
			return call[5]
		}
	}
	return ""
}

func sampleMaterial() serviceglobalsecrets.Material {
	return serviceglobalsecrets.Material{
		EnvKeys: []string{"GITHUB_TOKEN"},
		Files: []serviceglobalsecrets.MaterialFile{
			{Path: "/root/.npmrc", Content: "//registry.npmjs.org/:_authToken=tok\n"},
			{Path: "/root/.ssh/hestia_key", Content: "-----BEGIN OPENSSH PRIVATE KEY-----\n"},
		},
		SSHConfig:  "Host hestia\n    HostName 203.0.113.10\n",
		KnownHosts: "203.0.113.10 ssh-ed25519 AAAA\n",
		SSHNames:   []string{"hestia"},
	}
}

func TestApplyPushesEveryFileAtModeZeroSixHundred(t *testing.T) {
	runner := newFakeRunner()
	client := NewClient(runner)

	if err := client.Apply(context.Background(), "proj", sampleMaterial(), nil); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"proj/root/.npmrc":                         "//registry.npmjs.org/:_authToken=tok\n",
		"proj/root/.ssh/hestia_key":                "-----BEGIN OPENSSH PRIVATE KEY-----\n",
		"proj/root/.ssh/.remote-vault-config":      "Host hestia\n    HostName 203.0.113.10\n",
		"proj/root/.ssh/.remote-vault-known-hosts": "203.0.113.10 ssh-ed25519 AAAA\n",
	}
	for destination, content := range want {
		if runner.pushed[destination] != content {
			t.Fatalf("pushed[%s] = %q, want %q", destination, runner.pushed[destination], content)
		}
	}
	for _, call := range runner.calls {
		if len(call) >= 2 && call[0] == "file" && call[1] == "push" && call[2] != "--mode=0600" {
			t.Fatalf("push without a private mode: %v", call)
		}
	}
}

func TestApplyWritesTheManifestDescribingWhatItPushed(t *testing.T) {
	runner := newFakeRunner()
	client := NewClient(runner)

	if err := client.Apply(context.Background(), "proj", sampleMaterial(), nil); err != nil {
		t.Fatal(err)
	}

	manifest := runner.pushed["proj"+serviceglobalsecrets.ManifestPath]
	for _, fragment := range []string{
		`"envKeys":["GITHUB_TOKEN"]`,
		`"/root/.npmrc"`,
		`"/root/.ssh/hestia_key"`,
		`"ssh":["hestia"]`,
	} {
		if !strings.Contains(manifest, fragment) {
			t.Fatalf("manifest %q missing %q", manifest, fragment)
		}
	}
	if strings.Contains(manifest, "authToken") || strings.Contains(manifest, "PRIVATE KEY") {
		t.Fatalf("the manifest must record paths, never values: %q", manifest)
	}
}

func TestApplyRemovesExactlyTheStalePathsItIsGiven(t *testing.T) {
	runner := newFakeRunner()
	client := NewClient(runner)

	stale := []string{"/root/.composer/auth.json", "/root/.ssh/old_key"}
	if err := client.Apply(context.Background(), "proj", sampleMaterial(), stale); err != nil {
		t.Fatal(err)
	}

	script := runner.script()
	for _, path := range stale {
		if !strings.Contains(script, "rm -f '"+path+"'") {
			t.Fatalf("script does not remove %s:\n%s", path, script)
		}
	}
	if strings.Contains(script, "rm -f '/root/.npmrc'") {
		t.Fatal("a live file must never be removed")
	}
}

func TestApplyMergesOnlyItsOwnRegionOfTheSharedSSHFiles(t *testing.T) {
	runner := newFakeRunner()
	client := NewClient(runner)

	if err := client.Apply(context.Background(), "proj", sampleMaterial(), nil); err != nil {
		t.Fatal(err)
	}

	script := runner.script()
	for _, fragment := range []string{
		serviceglobalsecrets.ManagedBegin,
		serviceglobalsecrets.ManagedEnd,
		"merge_region '" + serviceglobalsecrets.SSHConfigPath + "'",
		"merge_region '" + serviceglobalsecrets.KnownHostsPath + "'",
		"chmod 700 '" + serviceglobalsecrets.SSHDir + "'",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("script missing %q:\n%s", fragment, script)
		}
	}
	// The staged region files are consumed, so no key material is left in a
	// world-readable scratch path inside the container.
	if !strings.Contains(script, `rm -f "$region"`) {
		t.Fatalf("script leaves the staged region behind:\n%s", script)
	}
}

func TestApplyCreatesEveryParentDirectoryBeforePushing(t *testing.T) {
	runner := newFakeRunner()
	client := NewClient(runner)

	material := sampleMaterial()
	material.Files = append(material.Files, serviceglobalsecrets.MaterialFile{
		Path: "/workspace/.secrets/deploy.json", Content: "{}",
	})
	if err := client.Apply(context.Background(), "proj", material, nil); err != nil {
		t.Fatal(err)
	}

	var install []string
	for _, call := range runner.calls {
		if len(call) >= 5 && call[3] == "install" {
			install = call
			break
		}
	}
	if install == nil {
		t.Fatalf("no directory creation call: %v", runner.calls)
	}
	joined := strings.Join(install, " ")
	for _, directory := range []string{"/root", "/root/.ssh", "/workspace/.secrets"} {
		if !strings.Contains(joined, directory) {
			t.Fatalf("install call %q does not create %s", joined, directory)
		}
	}
	if !strings.Contains(joined, "-m 700") {
		t.Fatalf("directories must be private: %q", joined)
	}
}

func TestManifestReadsBackWhatAPreviousApplyStored(t *testing.T) {
	runner := newFakeRunner()
	runner.execOut[serviceglobalsecrets.ManifestPath] =
		`{"version":1,"envKeys":["A"],"files":["/root/.npmrc"],"ssh":["hestia"]}`
	client := NewClient(runner)

	manifest, err := client.Manifest(context.Background(), "proj")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || len(manifest.Files) != 1 || manifest.Files[0] != "/root/.npmrc" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if len(manifest.EnvKeys) != 1 || manifest.EnvKeys[0] != "A" {
		t.Fatalf("env keys = %v", manifest.EnvKeys)
	}
}

func TestManifestTreatsAMissingOrCorruptRecordAsAbsent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		present bool
	}{
		{name: "never synced", present: false},
		{name: "empty file", content: "  ", present: true},
		{name: "corrupt json", content: "{not json", present: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := newFakeRunner()
			if test.present {
				runner.execOut[serviceglobalsecrets.ManifestPath] = test.content
			}
			manifest, err := NewClient(runner).Manifest(context.Background(), "proj")
			if err != nil {
				t.Fatalf("a missing manifest must not fail a sync: %v", err)
			}
			if manifest.Version != 0 || len(manifest.Files) != 0 {
				t.Fatalf("manifest = %+v", manifest)
			}
		})
	}
}

func TestApplyRefusesToRunWithoutTheContainerRuntime(t *testing.T) {
	runner := newFakeRunner()
	runner.available = false
	client := NewClient(runner)

	if err := client.Apply(context.Background(), "proj", sampleMaterial(), nil); !errors.Is(err, command.ErrUnavailable) {
		t.Fatalf("Apply() error = %v, want ErrUnavailable", err)
	}
	if _, err := client.Manifest(context.Background(), "proj"); !errors.Is(err, command.ErrUnavailable) {
		t.Fatalf("Manifest() error = %v, want ErrUnavailable", err)
	}
}

func TestApplyErrorNeverEchoesAValue(t *testing.T) {
	const secret = "//registry.npmjs.org/:_authToken=super-secret"
	runner := newFakeRunner()
	runner.execErr = errors.New("exit status 1")
	client := NewClient(runner)

	material := serviceglobalsecrets.Material{
		Files: []serviceglobalsecrets.MaterialFile{{Path: "/root/.npmrc", Content: secret}},
	}
	err := client.Apply(context.Background(), "proj", material, nil)
	if err == nil {
		t.Fatal("expected the failing exec to surface")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked a value: %v", err)
	}
}
