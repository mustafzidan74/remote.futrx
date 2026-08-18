// Package globalsecrets is the platform-level secrets vault: one place for
// the tokens, licence keys, credential files, and SSH targets that every
// project container should have without an operator re-entering them per
// project.
//
// Three kinds of entry live in the same document:
//
//   - env: an environment variable pushed into the container's LXD
//     environment.* configuration, so any `lxc exec` session inherits it.
//   - file: a file materialized inside the container at a declared path
//     (mode 0600) — `/root/.npmrc`, `/root/.composer/auth.json`, and friends.
//   - ssh: an SSH target materialized as a private key plus a `Host` block in
//     `/root/.ssh/config`, so an agent can simply run `ssh <name>`.
//
// A project-level secret with the same key always wins over an `env` entry
// from the vault; the shadowed entry is reported rather than silently
// dropped. Values are write-only over the API: reads return a mask.
package globalsecrets

import (
	"path"
	"sort"
	"strings"
)

// Kind enumerates how one vault entry reaches a container.
type Kind string

const (
	KindEnv  Kind = "env"
	KindFile Kind = "file"
	KindSSH  Kind = "ssh"
)

// SourceGlobal labels an inherited entry's origin in the project API, leaving
// room for other sources later without changing the field's shape.
const SourceGlobal = "global"

// DefaultSSHPort is used when an SSH target declares no port.
const DefaultSSHPort = 22

// FileRoots are the two container directories a `file` entry may be written
// into. Anything else is rejected: the vault must not be a way to overwrite
// arbitrary paths in a container's rootfs.
var FileRoots = []string{"/root/", "/workspace/.secrets/"}

// Scope decides which projects an entry reaches. All wins over ProjectIDs.
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

// Normalize sorts and de-duplicates the explicit project list and drops it
// entirely for an all-projects scope, so two equal scopes serialize equally.
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

// SSHTarget is the connection detail of a `ssh` entry. PrivateKey is the only
// secret field and never leaves the backend.
type SSHTarget struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	PrivateKey     string `json:"privateKey,omitempty"`
	KnownHostsLine string `json:"knownHostsLine,omitempty"`
}

// EffectivePort resolves the port an ssh command should dial.
func (t SSHTarget) EffectivePort() int {
	if t.Port <= 0 {
		return DefaultSSHPort
	}
	return t.Port
}

// Secret is one stored vault entry. Value carries the environment value for
// `env` and the file body for `file`; an `ssh` entry keeps its key material
// in SSH.PrivateKey instead.
type Secret struct {
	Key         string     `json:"key"`
	Kind        Kind       `json:"kind"`
	Value       string     `json:"value,omitempty"`
	Path        string     `json:"path,omitempty"`
	SSH         *SSHTarget `json:"ssh,omitempty"`
	Scope       Scope      `json:"scope"`
	Description string     `json:"description,omitempty"`
	UpdatedAt   int64      `json:"updatedAt"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
}

// secretValue returns the field that holds this entry's confidential material,
// which is what masking and "blank keeps the stored value" reason about.
func (s Secret) secretValue() string {
	if s.Kind == KindSSH {
		if s.SSH == nil {
			return ""
		}
		return s.SSH.PrivateKey
	}
	return s.Value
}

// View is the masked, API-safe projection of one entry.
type View struct {
	Key         string         `json:"key"`
	Kind        Kind           `json:"kind"`
	Path        string         `json:"path,omitempty"`
	SSH         *SSHTargetView `json:"ssh,omitempty"`
	Scope       Scope          `json:"scope"`
	Description string         `json:"description,omitempty"`
	UpdatedAt   int64          `json:"updatedAt"`
	UpdatedBy   string         `json:"updatedBy,omitempty"`
	// Masked is the only thing a reader ever learns about the value.
	Masked string `json:"masked"`
	// HasValue is false for an entry whose value was cleared, which is the
	// state that makes a container drop the material on the next sync.
	HasValue bool `json:"hasValue"`
	// ShadowedIn lists projects whose own secret of the same key overrides
	// this `env` entry. Empty for the other kinds.
	ShadowedIn []string `json:"shadowedIn,omitempty"`
	// EnvVars documents the environment contract an `ssh` entry publishes.
	EnvVars []string `json:"envVars,omitempty"`
}

// SSHTargetView is an SSH target without its key material.
type SSHTargetView struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	KnownHostsLine string `json:"knownHostsLine,omitempty"`
}

// Inherited describes, for one project, what the vault contributes to its
// container. It is what the project Secrets tab renders above the project's
// own entries.
type Inherited struct {
	Key         string `json:"key"`
	Kind        Kind   `json:"kind"`
	Source      string `json:"source"`
	Shadowed    bool   `json:"shadowed"`
	Path        string `json:"path,omitempty"`
	Description string `json:"description,omitempty"`
}

// TestResult is the outcome of probing one SSH target.
type TestResult struct {
	OK        bool   `json:"ok"`
	Output    string `json:"output"`
	LatencyMS int64  `json:"latencyMs"`
}

// Mask renders the tail of a value the way the API exposes it: four visible
// characters at most, and nothing at all for a short or absent value.
func Mask(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("•", 8)
	}
	return strings.Repeat("•", 8) + value[len(value)-4:]
}

// EnvNamesForSSH is the documented environment contract of an ssh target: a
// skill that deploys to `hestia` reads SSH_TARGET_HESTIA_HOST and friends
// instead of parsing ~/.ssh/config.
func EnvNamesForSSH(name string) (host, user, port string) {
	upper := sshEnvSegment(name)
	return "SSH_TARGET_" + upper + "_HOST",
		"SSH_TARGET_" + upper + "_USER",
		"SSH_TARGET_" + upper + "_PORT"
}

// sshEnvSegment uppercases a target name into the environment-variable
// segment, mapping every character an env name may not carry to '_'.
func sshEnvSegment(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToUpper(name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ContainerKeyPath is where an ssh target's private key is materialized.
func ContainerKeyPath(name string) string {
	return path.Join(SSHDir, name+"_key")
}
