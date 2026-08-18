package globalsecrets

// Material is the vault's desired in-container state for one project. The
// service decides what it should contain; the container adapter only writes
// it and prunes whatever the previous manifest listed and this one does not.

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// SSHDir is where SSH material is materialized inside a container.
	SSHDir = "/root/.ssh"
	// SSHConfigPath and KnownHostsPath are shared files: the vault owns only
	// the region between its markers so anything an agent added by hand
	// survives a re-sync.
	SSHConfigPath  = SSHDir + "/config"
	KnownHostsPath = SSHDir + "/known_hosts"
	// ManifestPath records exactly what the vault materialized, so removing
	// an entry removes its material rather than leaving it behind forever.
	ManifestPath = "/root/.remote-secrets.json"

	// ManagedBegin and ManagedEnd delimit the vault-owned region.
	ManagedBegin = "# BEGIN remote.futrx managed secrets vault"
	ManagedEnd   = "# END remote.futrx managed secrets vault"

	// SecretsFileMode is the mode every materialized file carries.
	SecretsFileMode = "0600"

	// ManifestVersion is bumped only if the manifest's shape changes.
	ManifestVersion = 1
)

// MaterialFile is one file the vault owns outright.
type MaterialFile struct {
	Path    string
	Content string
}

// Material is the complete desired state for one container.
type Material struct {
	// EnvKeys are the environment names the vault owns here. Values travel
	// through the container environment port; the names are tracked so a
	// removed entry can be unset exactly.
	EnvKeys []string
	// Files are the vault-owned files, SSH private keys included.
	Files []MaterialFile
	// SSHConfig is the managed region of /root/.ssh/config, empty when the
	// project inherits no SSH target.
	SSHConfig string
	// KnownHosts is the managed region of /root/.ssh/known_hosts.
	KnownHosts string
	// SSHNames lists the live target names, for the manifest and for logs.
	SSHNames []string
}

// Empty reports whether nothing at all is materialized.
func (m Material) Empty() bool {
	return len(m.EnvKeys) == 0 && len(m.Files) == 0 && m.SSHConfig == "" && m.KnownHosts == ""
}

// FilePaths lists every owned file path, sorted, for the manifest.
func (m Material) FilePaths() []string {
	paths := make([]string, 0, len(m.Files))
	for _, file := range m.Files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
}

// Manifest is the record written to ManifestPath inside the container. It is
// the only thing that makes cleanup exact: without it, a `file` entry renamed
// to another path would leave the old file behind.
type Manifest struct {
	Version int      `json:"version"`
	EnvKeys []string `json:"envKeys,omitempty"`
	Files   []string `json:"files,omitempty"`
	SSH     []string `json:"ssh,omitempty"`
}

// ManifestFor renders the manifest describing this material.
func ManifestFor(material Material) Manifest {
	envKeys := append([]string(nil), material.EnvKeys...)
	sort.Strings(envKeys)
	names := append([]string(nil), material.SSHNames...)
	sort.Strings(names)
	return Manifest{
		Version: ManifestVersion,
		EnvKeys: envKeys,
		Files:   material.FilePaths(),
		SSH:     names,
	}
}

// StaleFiles returns the paths the previous manifest owned that the new
// material no longer does. The manifest also records the managed shared files
// implicitly — they are regions, not owned files, so they are never pruned.
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

// StaleEnvKeys returns the environment names the previous manifest owned and
// the new material does not, which is what the caller unsets.
func StaleEnvKeys(previous Manifest, material Material) []string {
	if len(previous.EnvKeys) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(material.EnvKeys))
	for _, key := range material.EnvKeys {
		wanted[key] = true
	}
	stale := make([]string, 0, len(previous.EnvKeys))
	for _, key := range previous.EnvKeys {
		if key == "" || wanted[key] {
			continue
		}
		stale = append(stale, key)
	}
	sort.Strings(stale)
	return stale
}

// RenderSSHConfig renders one `Host` block per target, in name order, so
// regenerating from unchanged input produces a byte-identical region. Empty
// input renders nothing, which is how the managed region disappears when the
// last target leaves a project's scope.
func RenderSSHConfig(targets []SSHTarget) string {
	if len(targets) == 0 {
		return ""
	}
	ordered := append([]SSHTarget(nil), targets...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })

	var out strings.Builder
	for index, target := range ordered {
		if index > 0 {
			out.WriteByte('\n')
		}
		// A target with a pinned host key gets strict checking; without one,
		// accept-new is the only setting that lets an unattended agent
		// connect at all without disabling host verification afterwards.
		checking := "accept-new"
		if target.KnownHostsLine != "" {
			checking = "yes"
		}
		fmt.Fprintf(&out, "Host %s\n", sshConfigValue(target.Name))
		fmt.Fprintf(&out, "    HostName %s\n", sshConfigValue(target.Host))
		fmt.Fprintf(&out, "    User %s\n", sshConfigValue(target.User))
		fmt.Fprintf(&out, "    Port %d\n", target.EffectivePort())
		fmt.Fprintf(&out, "    IdentityFile %s\n", sshConfigValue(ContainerKeyPath(target.Name)))
		out.WriteString("    IdentitiesOnly yes\n")
		fmt.Fprintf(&out, "    StrictHostKeyChecking %s\n", checking)
	}
	return out.String()
}

// RenderKnownHosts renders the pinned host keys, one per line, in name order.
func RenderKnownHosts(targets []SSHTarget) string {
	lines := make([]string, 0, len(targets))
	ordered := append([]SSHTarget(nil), targets...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	for _, target := range ordered {
		line := strings.TrimSpace(target.KnownHostsLine)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// sshConfigValue quotes an ssh_config argument only when it needs it. The
// validated alphabet never does, so this is a guard against a future rule
// change silently producing a config OpenSSH would misparse.
func sshConfigValue(value string) string {
	safe := value != ""
	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case strings.IndexByte("._-/@:", char) >= 0:
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return value
	}
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
