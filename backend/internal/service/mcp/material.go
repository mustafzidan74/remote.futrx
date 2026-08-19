package mcp

// Material is the registry's desired in-container state for one project, plus
// the manifest that makes removing a deleted entry exact.
//
// Without the manifest a config file whose last entry was deleted would sit
// in the container forever, still advertising a tool the platform no longer
// knows about. The manifest records every path written and the signature of
// what was written, so the next pass can both prune and skip.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// MaterialFile is one config file the platform owns outright.
type MaterialFile struct {
	Path    string
	Content string
}

// Material is the complete desired state for one container.
type Material struct {
	// Files are the platform-owned config files. Empty content is never
	// carried here: a provider with no servers contributes no file and the
	// previous one becomes stale instead.
	Files []MaterialFile
	// CodexRegion is the body of the managed region inside
	// /root/.codex/config.toml. Empty removes the region.
	CodexRegion string
	// ClaudeConfigPath is the file to hand `claude --mcp-config`, empty when
	// Claude Code has no servers for this project.
	ClaudeConfigPath string
	// Names are the entries materialized for any provider, sorted.
	Names []string
	// Skipped names entries left out because a referenced vault key was not
	// readable by this project.
	Skipped []string
}

// Empty reports whether this project ends up with no MCP configuration at all.
func (m Material) Empty() bool {
	return len(m.Files) == 0 && m.CodexRegion == ""
}

// FilePaths lists every owned path, sorted, for the manifest.
func (m Material) FilePaths() []string {
	paths := make([]string, 0, len(m.Files))
	for _, file := range m.Files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
}

// Signature fingerprints everything a container would receive. Equal
// signatures mean the container already holds this exact configuration, which
// is what lets the per-run hook cost one `cat` instead of three pushes.
func (m Material) Signature() string {
	digest := sha256.New()
	fmt.Fprintf(digest, "v%d\x00", ManifestVersion)
	for _, file := range m.Files {
		fmt.Fprintf(digest, "%s\x00%d\x00%s\x00", file.Path, len(file.Content), file.Content)
	}
	fmt.Fprintf(digest, "codex\x00%d\x00%s\x00", len(m.CodexRegion), m.CodexRegion)
	return hex.EncodeToString(digest.Sum(nil))
}

// MaterialFor renders the desired state from already-resolved entries.
func MaterialFor(resolution Resolution) Material {
	material := Material{
		Names:   Names(resolution.Servers),
		Skipped: append([]string(nil), resolution.Skipped...),
	}
	if claude := RenderClaudeConfig(ForProvider(resolution.Servers, ProviderClaude)); claude != "" {
		material.Files = append(material.Files, MaterialFile{Path: ClaudeConfigPath, Content: claude})
		material.ClaudeConfigPath = ClaudeConfigPath
	}
	material.CodexRegion = RenderCodexRegion(ForProvider(resolution.Servers, ProviderCodex))
	sort.Slice(material.Files, func(i, j int) bool { return material.Files[i].Path < material.Files[j].Path })
	return material
}

// Manifest is the record written to ManifestPath inside the container.
type Manifest struct {
	Version   int      `json:"version"`
	Signature string   `json:"signature,omitempty"`
	Files     []string `json:"files,omitempty"`
	Names     []string `json:"names,omitempty"`
	// ClaudeConfig is the path the claude provider should pass to
	// --mcp-config, so a skipped pass still knows what to add to the run.
	ClaudeConfig string `json:"claudeConfig,omitempty"`
}

// ManifestFor renders the manifest describing this material.
func ManifestFor(material Material) Manifest {
	return Manifest{
		Version:      ManifestVersion,
		Signature:    material.Signature(),
		Files:        material.FilePaths(),
		Names:        append([]string(nil), material.Names...),
		ClaudeConfig: material.ClaudeConfigPath,
	}
}

// StaleFiles returns the paths the previous manifest owned that this material
// no longer does. The codex region is not a file and is never pruned here: it
// is merged out of the shared config by the apply script instead.
func StaleFiles(previous Manifest, material Material) []string {
	if len(previous.Files) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(material.Files))
	for _, file := range material.Files {
		wanted[file.Path] = true
	}
	stale := make([]string, 0, len(previous.Files))
	for _, path := range previous.Files {
		if path == "" || wanted[path] {
			continue
		}
		stale = append(stale, path)
	}
	sort.Strings(stale)
	return stale
}
