package globalsecrets

import (
	"errors"
	"path"
	"strings"
)

var (
	// ErrInvalidKey rejects a key that could not be used as an environment
	// variable name, a URL path segment, or a manifest key.
	ErrInvalidKey = errors.New("secret key must match [A-Za-z_][A-Za-z0-9_]*")
	// ErrInvalidKind rejects anything outside env/file/ssh.
	ErrInvalidKind = errors.New("secret kind must be one of env, file, ssh")
	// ErrInvalidPath rejects a file destination outside the allowed roots.
	ErrInvalidPath = errors.New("file path must be an absolute path under /root or /workspace/.secrets")
	// ErrMultilineEnvValue rejects a multi-line environment value: LXD
	// refuses line breaks in persistent environment.* configuration, so such
	// a value would silently never reach the container.
	ErrMultilineEnvValue = errors.New("environment values cannot contain line breaks; use a file entry instead")
	// ErrInvalidSSHTarget rejects an incomplete or unsafe SSH target.
	ErrInvalidSSHTarget = errors.New("ssh target needs a name, host, user, and a port between 1 and 65535")
	// ErrInvalidKnownHosts rejects a known_hosts line that is not a single line.
	ErrInvalidKnownHosts = errors.New("known_hosts entry must be a single line")
	// ErrNotFound is returned for an unknown key.
	ErrNotFound = errors.New("secret not found")
	// ErrExists is returned when a create would overwrite an existing key.
	ErrExists = errors.New("secret already exists")
	// ErrWrongKind is returned when an operation only applies to another kind.
	ErrWrongKind = errors.New("operation does not apply to this secret kind")
	// ErrNoValue is returned when an operation needs material the entry does
	// not carry (testing an SSH target whose key was cleared, say).
	ErrNoValue = errors.New("secret has no stored value")
	// ErrUnavailable reports a deployment without a vault store.
	ErrUnavailable = errors.New("secrets vault is unavailable")
	// ErrProbeUnavailable reports a deployment that cannot run ssh.
	ErrProbeUnavailable = errors.New("ssh probing is unavailable on this host")
	// ErrInvalidScope rejects a scope that selects no project at all, which
	// is a mis-submitted form rather than an intent.
	ErrInvalidScope = errors.New("scope must be all projects or at least one project")
	// ErrValueTooLarge rejects an oversized value.
	ErrValueTooLarge = errors.New("secret value is too large")
)

// maxValueBytes caps a single stored value. A licence key or a PEM key is
// kilobytes; the limit exists so a mis-paste cannot make the document
// unreadable.
const maxValueBytes = 256 * 1024

const (
	maxKeyLength         = 128
	maxDescriptionLength = 500
	maxSSHNameLength     = 64
	maxHostLength        = 253
	maxUserLength        = 64
)

// ValidKey allows POSIX-style environment variable names. Every kind uses the
// same rule: the key is also the URL path segment and the manifest key, so
// one strict alphabet keeps all three safe.
func ValidKey(key string) bool {
	if key == "" || len(key) > maxKeyLength {
		return false
	}
	for index, r := range key {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r == '_':
		case index > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// ValidKind reports whether kind is one this vault knows how to materialize.
func ValidKind(kind Kind) bool {
	switch kind {
	case KindEnv, KindFile, KindSSH:
		return true
	default:
		return false
	}
}

// NormalizeFilePath cleans a declared destination and confirms it lands under
// an allowed root. It returns the cleaned path so the manifest, the push, and
// the later cleanup all agree on one spelling.
func NormalizeFilePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
		return "", ErrInvalidPath
	}
	if strings.ContainsAny(trimmed, "\x00\n\r") {
		return "", ErrInvalidPath
	}
	cleaned := path.Clean(trimmed)
	for _, root := range FileRoots {
		if strings.HasPrefix(cleaned+"/", root) && cleaned != strings.TrimSuffix(root, "/") {
			return cleaned, nil
		}
	}
	return "", ErrInvalidPath
}

// ValidSSHName constrains a target name to what is safe as a file name, a
// `Host` token in ssh_config, and an environment-variable segment.
func ValidSSHName(name string) bool {
	if name == "" || len(name) > maxSSHNameLength {
		return false
	}
	for index := 0; index < len(name); index++ {
		char := name[index]
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '-' || char == '_' || char == '.':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// normalizeSSHTarget validates and canonicalizes an SSH target.
func normalizeSSHTarget(target SSHTarget) (SSHTarget, error) {
	target.Name = strings.TrimSpace(target.Name)
	target.Host = strings.TrimSpace(target.Host)
	target.User = strings.TrimSpace(target.User)
	target.KnownHostsLine = strings.TrimSpace(target.KnownHostsLine)
	if target.Port == 0 {
		target.Port = DefaultSSHPort
	}
	if !ValidSSHName(target.Name) {
		return SSHTarget{}, ErrInvalidSSHTarget
	}
	if target.Host == "" || len(target.Host) > maxHostLength || strings.ContainsAny(target.Host, " \t\r\n\x00") {
		return SSHTarget{}, ErrInvalidSSHTarget
	}
	if target.User == "" || len(target.User) > maxUserLength || strings.ContainsAny(target.User, " \t\r\n\x00") {
		return SSHTarget{}, ErrInvalidSSHTarget
	}
	if target.Port < 1 || target.Port > 65535 {
		return SSHTarget{}, ErrInvalidSSHTarget
	}
	if strings.ContainsAny(target.KnownHostsLine, "\r\n") {
		return SSHTarget{}, ErrInvalidKnownHosts
	}
	if len(target.PrivateKey) > maxValueBytes {
		return SSHTarget{}, ErrValueTooLarge
	}
	return target, nil
}

// normalizeDescription trims and truncates free text so one pasted essay
// cannot dominate the document.
func normalizeDescription(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if len(text) > maxDescriptionLength {
		text = text[:maxDescriptionLength]
	}
	return text
}
