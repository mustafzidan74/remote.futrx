package mcp

import (
	"reflect"
	"strings"
	"testing"
)

func TestMaterialForSplitsEntriesAcrossTheTwoProviderConfigs(t *testing.T) {
	material := MaterialFor(Resolution{Servers: []Server{
		{Name: "claude-only", Transport: TransportStdio, Command: "a", Providers: []string{ProviderClaude}},
		{Name: "codex-only", Transport: TransportStdio, Command: "b", Providers: []string{ProviderCodex}},
	}})

	if material.ClaudeConfigPath != ClaudeConfigPath {
		t.Fatalf("claude config path = %q", material.ClaudeConfigPath)
	}
	if len(material.Files) != 1 || material.Files[0].Path != ClaudeConfigPath {
		t.Fatalf("files = %#v", material.Files)
	}
	if strings.Contains(material.Files[0].Content, "codex-only") {
		t.Fatalf("a codex-only entry reached the claude config: %s", material.Files[0].Content)
	}
	if strings.Contains(material.CodexRegion, "claude-only") {
		t.Fatalf("a claude-only entry reached the codex region: %s", material.CodexRegion)
	}
	if !strings.Contains(material.CodexRegion, `[mcp_servers."codex-only"]`) {
		t.Fatalf("codex region = %s", material.CodexRegion)
	}
	if !reflect.DeepEqual(material.Names, []string{"claude-only", "codex-only"}) {
		t.Fatalf("names = %v", material.Names)
	}
}

func TestMaterialForProducesNoClaudeFileWhenClaudeHasNoServers(t *testing.T) {
	material := MaterialFor(Resolution{Servers: []Server{
		{Name: "codex-only", Transport: TransportStdio, Command: "b", Providers: []string{ProviderCodex}},
	}})
	if material.ClaudeConfigPath != "" || len(material.Files) != 0 {
		t.Fatalf("material = %#v", material)
	}
	if material.Empty() {
		t.Fatalf("a codex-only project still has material")
	}
}

func TestEmptyMaterialIsEmpty(t *testing.T) {
	if !MaterialFor(Resolution{}).Empty() {
		t.Fatalf("a project with no enabled entries should have empty material")
	}
}

func TestSignatureIsStableAcrossRendersAndChangesWithContent(t *testing.T) {
	base := MaterialFor(Resolution{Servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx", Args: []string{"mcp-server-fetch"}},
	}})
	same := MaterialFor(Resolution{Servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx", Args: []string{"mcp-server-fetch"}},
	}})
	if base.Signature() != same.Signature() {
		t.Fatalf("identical input produced different signatures")
	}

	changed := MaterialFor(Resolution{Servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx", Args: []string{"mcp-server-fetch", "--full"}},
	}})
	if base.Signature() == changed.Signature() {
		t.Fatalf("a changed argument did not change the signature")
	}

	// A resolved secret is part of the signature, so rotating a vault value
	// re-materializes the container instead of leaving the old one in place.
	withSecret := MaterialFor(Resolution{Servers: []Server{
		{Name: "pg", Transport: TransportStdio, Command: "npx", Env: map[string]string{"PGPASSWORD": "one"}},
	}})
	rotated := MaterialFor(Resolution{Servers: []Server{
		{Name: "pg", Transport: TransportStdio, Command: "npx", Env: map[string]string{"PGPASSWORD": "two"}},
	}})
	if withSecret.Signature() == rotated.Signature() {
		t.Fatalf("a rotated value did not change the signature")
	}
}

func TestManifestForRecordsWhatWasWritten(t *testing.T) {
	material := MaterialFor(Resolution{Servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx"},
	}})
	manifest := ManifestFor(material)

	if manifest.Version != ManifestVersion {
		t.Errorf("version = %d", manifest.Version)
	}
	if manifest.Signature != material.Signature() {
		t.Errorf("signature = %q", manifest.Signature)
	}
	if !reflect.DeepEqual(manifest.Files, []string{ClaudeConfigPath}) {
		t.Errorf("files = %v", manifest.Files)
	}
	if !reflect.DeepEqual(manifest.Names, []string{"fetch"}) {
		t.Errorf("names = %v", manifest.Names)
	}
	if manifest.ClaudeConfig != ClaudeConfigPath {
		t.Errorf("claude config = %q", manifest.ClaudeConfig)
	}
}

func TestStaleFiles(t *testing.T) {
	tests := []struct {
		name     string
		previous Manifest
		material Material
		want     []string
	}{
		{
			name:     "a container that never had a config has nothing to prune",
			previous: Manifest{},
			material: Material{Files: []MaterialFile{{Path: ClaudeConfigPath}}},
			want:     nil,
		},
		{
			name:     "a file still wanted is not pruned",
			previous: Manifest{Files: []string{ClaudeConfigPath}},
			material: Material{Files: []MaterialFile{{Path: ClaudeConfigPath}}},
			want:     []string{},
		},
		{
			name:     "the last claude entry leaving removes its config",
			previous: Manifest{Files: []string{ClaudeConfigPath}},
			material: Material{},
			want:     []string{ClaudeConfigPath},
		},
		{
			name:     "a path from an older layout is pruned too",
			previous: Manifest{Files: []string{"/root/.claude/old-mcp.json", ClaudeConfigPath}},
			material: Material{Files: []MaterialFile{{Path: ClaudeConfigPath}}},
			want:     []string{"/root/.claude/old-mcp.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StaleFiles(tt.previous, tt.material)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("StaleFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}
