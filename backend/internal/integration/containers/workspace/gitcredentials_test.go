package workspace

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/assets"
)

func TestGitCredentialHelperIsWrittenOnceThenLeftAlone(t *testing.T) {
	runner := &stubRunner{markers: map[string]string{}}
	provisioner := newTestProvisioner(runner, t.TempDir())

	if err := provisioner.EnsureGitCredentialHelper(context.Background(), "c1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runner.pushes, "c1"+gitSystemConfigPath) {
		t.Fatalf("system gitconfig not published: %v", runner.pushes)
	}

	// With the marker current, an operator's later edit is not overwritten.
	runner.pushes = nil
	runner.markers[gitSystemConfigHash] = assets.Hash([]byte(gitSystemConfig))
	if err := provisioner.EnsureGitCredentialHelper(context.Background(), "c1"); err != nil {
		t.Fatal(err)
	}
	if len(runner.pushes) != 0 {
		t.Fatalf("republished over a current marker: %v", runner.pushes)
	}
}

func TestGitCredentialHelperRoutesOnlyGitHubThroughGh(t *testing.T) {
	for _, want := range []string{
		`[credential "https://github.com"]`,
		"helper = !/usr/bin/gh auth git-credential",
	} {
		if !strings.Contains(gitSystemConfig, want) {
			t.Fatalf("gitconfig is missing %q", want)
		}
	}
	if strings.Contains(gitSystemConfig, "[credential]\n") {
		t.Fatal("helper must be scoped to GitHub hosts, not every remote")
	}
}
