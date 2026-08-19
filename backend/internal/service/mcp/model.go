// Package mcp is the platform's registry of Model Context Protocol servers:
// the external tools an in-container agent may call instead of guessing.
//
// Two layers make up what one project ends up with:
//
//   - platform entries (DATA_DIR/mcpservers.json), created by an admin and
//     scoped to all projects or to an explicit list;
//   - per-project overrides (DATA_DIR/projectmcp/<id>.json), which switch a
//     platform entry off for that project and add project-only entries.
//
// A registry entry never holds a credential. It holds ${KEY} placeholders and
// a secretRefs list naming the Secrets-vault keys behind them; the values are
// read from the vault at materialization time, written into a 0600 config
// file inside the container, and never logged or returned by the API.
package mcp

import (
	"sort"
	"strings"
)

// Transport is how a client reaches one server: a child process speaking
// JSON-RPC over stdio, or a remote HTTP endpoint.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
)

// The agent CLIs this platform can configure. Kimi Code and Antigravity are
// deliberately absent: neither exposes an MCP configuration this platform can
// write, so an entry that named them would silently do nothing.
const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
)

// SupportedProviders is the ordered set an entry may enable.
func SupportedProviders() []string { return []string{ProviderClaude, ProviderCodex} }

// UnsupportedProviders are the CLIs the UI reports as "no MCP support", so an
// operator is told rather than left wondering why nothing happened.
func UnsupportedProviders() []string { return []string{"kimi", "antigravity"} }

// Container paths. Both configs live inside the per-project agent homes,
// which are bind-mounted from the host and survive a container recycle.
const (
	// ClaudeConfigPath is passed to `claude --mcp-config`. A config file is
	// used rather than `claude mcp add` so nothing needs an interactive
	// command inside the container.
	ClaudeConfigPath = "/root/.claude/mcp-servers.json"
	ClaudeConfigDir  = "/root/.claude"
	// CodexConfigPath is read by the codex CLI at startup. The platform owns
	// only the region between the markers below, so anything else in the file
	// survives a re-materialization.
	CodexConfigPath = "/root/.codex/config.toml"
	CodexConfigDir  = "/root/.codex"
	// ManifestPath records what was materialized, which is what makes the
	// removal of a deleted entry exact.
	ManifestPath = "/root/.remote-mcp.json"

	// ManagedBegin and ManagedEnd delimit the platform-owned region of the
	// codex config. They are TOML comments, so a file carrying them stays
	// parseable even if the platform never runs again.
	ManagedBegin = "# BEGIN remote.futrx managed MCP servers"
	ManagedEnd   = "# END remote.futrx managed MCP servers"

	// ConfigFileMode is the mode every materialized config carries: the file
	// can hold a resolved vault value.
	ConfigFileMode = "0600"

	// ManifestVersion is bumped only if the manifest's shape changes.
	ManifestVersion = 1
)

// SourcePlatform and SourceProject label where an available entry came from.
const (
	SourcePlatform = "platform"
	SourceProject  = "project"
)

// Scope decides which projects a platform entry reaches. All wins over
// ProjectIDs. It mirrors the secrets vault's scope so the two admin screens
// behave identically.
type Scope struct {
	All        bool     `json:"all"`
	ProjectIDs []string `json:"projectIds,omitempty"`
}

// Includes reports whether the scope covers one project.
func (s Scope) Includes(projectID string) bool {
	if s.All {
		return true
	}
	for _, id := range s.ProjectIDs {
		if id == projectID {
			return true
		}
	}
	return false
}

// Normalize sorts and de-duplicates the explicit list and drops it entirely
// for an all-projects scope, so two equal scopes serialize equally.
func (s Scope) Normalize() Scope {
	if s.All {
		return Scope{All: true}
	}
	seen := make(map[string]bool, len(s.ProjectIDs))
	ids := make([]string, 0, len(s.ProjectIDs))
	for _, id := range s.ProjectIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return Scope{}
	}
	return Scope{ProjectIDs: ids}
}

// Server is one registry entry. The same struct serves the platform registry
// and a project-only addition; Scope is ignored for the latter, which reaches
// exactly one project by construction.
type Server struct {
	Name      string    `json:"name"`
	Transport Transport `json:"transport"`

	// stdio transport
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// http transport
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	Scope Scope `json:"scope"`
	// Providers are the agent CLIs this entry is written for. Empty means
	// every supported provider.
	Providers   []string `json:"enabledForProviders,omitempty"`
	Description string   `json:"description,omitempty"`
	// SecretRefs are Secrets-vault keys. Their values replace the matching
	// ${KEY} placeholders at materialization time and are never stored here.
	SecretRefs []string `json:"secretRefs,omitempty"`

	UpdatedAt int64  `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy,omitempty"`
}

// EffectiveProviders resolves the empty-means-all rule once, so rendering and
// the API never disagree about which CLIs an entry targets.
func (s Server) EffectiveProviders() []string {
	if len(s.Providers) == 0 {
		return SupportedProviders()
	}
	return append([]string(nil), s.Providers...)
}

// Targets reports whether this entry is written for one provider.
func (s Server) Targets(provider string) bool {
	for _, candidate := range s.EffectiveProviders() {
		if candidate == provider {
			return true
		}
	}
	return false
}

// ProjectSettings is one project's document: which available entries it turned
// off, the entries only it has, and the record of the last materialization.
type ProjectSettings struct {
	Disabled []string `json:"disabled,omitempty"`
	Servers  []Server `json:"servers,omitempty"`
	// MaterializedAt is when the container configs were last (re)written, in
	// milliseconds. Zero means "never, or not since this container was
	// replaced".
	MaterializedAt int64 `json:"materializedAt,omitempty"`
	// MaterializedNames is what that pass wrote, for the project panel.
	MaterializedNames []string `json:"materializedNames,omitempty"`
}

// DisabledSet is the lookup the merge uses.
func (p ProjectSettings) DisabledSet() map[string]bool {
	set := make(map[string]bool, len(p.Disabled))
	for _, name := range p.Disabled {
		if name = strings.TrimSpace(name); name != "" {
			set[name] = true
		}
	}
	return set
}

// Entry is one server a project can use, with where it came from and whether
// this project has it switched on.
type Entry struct {
	Server
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
}

// View is the API projection of a platform entry. It is the stored entry plus
// nothing: no vault value ever reaches it, because none is ever stored.
type View struct {
	Server
	// Unsupported lists providers named on the entry that cannot be
	// configured, so the admin table can say so instead of pretending.
	Unsupported []string `json:"unsupportedProviders,omitempty"`
}

// ProjectView is what the project MCP panel renders.
type ProjectView struct {
	Available            []Entry  `json:"available"`
	MaterializedAt       int64    `json:"materializedAt,omitempty"`
	MaterializedNames    []string `json:"materializedNames,omitempty"`
	SupportedProviders   []string `json:"supportedProviders"`
	UnsupportedProviders []string `json:"unsupportedProviders"`
}

// ProjectInput is the member-facing write: which available entries are off,
// and the project-only entries.
type ProjectInput struct {
	Disabled []string
	Servers  []Server
}

// TestResult is the outcome of probing one server inside a container. Output
// is raw CLI output with every resolved vault value replaced by a mask.
type TestResult struct {
	OK       bool   `json:"ok"`
	Output   string `json:"output"`
	Duration int64  `json:"durationMs"`
}
