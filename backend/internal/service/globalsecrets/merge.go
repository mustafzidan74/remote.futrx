package globalsecrets

// Merge decides what one project's container gets from the vault: which
// entries are in scope, which `env` entries the project shadows with a secret
// of its own, and what that leaves to materialize.

import (
	"sort"
	"strconv"
)

// forProject returns the in-scope entries in key order.
func forProject(secrets []Secret, projectID string) []Secret {
	scoped := make([]Secret, 0, len(secrets))
	for _, secret := range secrets {
		if secret.Scope.Includes(projectID) {
			scoped = append(scoped, secret)
		}
	}
	sort.Slice(scoped, func(i, j int) bool { return scoped[i].Key < scoped[j].Key })
	return scoped
}

func keySet(keys []string) map[string]bool {
	set := make(map[string]bool, len(keys))
	for _, key := range keys {
		set[key] = true
	}
	return set
}

// InheritedFor lists what the vault contributes to one project, marking the
// `env` entries the project's own secrets override. It is what a member sees
// above their project's own secrets, so they can tell what the container will
// actually have.
func InheritedFor(secrets []Secret, projectID string, ownKeys []string) []Inherited {
	own := keySet(ownKeys)
	scoped := forProject(secrets, projectID)
	list := make([]Inherited, 0, len(scoped))
	for _, secret := range scoped {
		list = append(list, Inherited{
			Key:         secret.Key,
			Kind:        secret.Kind,
			Source:      SourceGlobal,
			Shadowed:    secret.Kind == KindEnv && own[secret.Key],
			Path:        secret.Path,
			Description: secret.Description,
		})
	}
	return list
}

// EnvFor returns the environment variables the vault publishes to one
// project. An `env` entry whose key the project also defines is dropped: the
// project-level secret wins, and the shadowing is reported through
// InheritedFor rather than fought over at apply time.
//
// SSH targets contribute the documented SSH_TARGET_<NAME>_* trio so a skill
// can read connection details without parsing ~/.ssh/config.
func EnvFor(secrets []Secret, projectID string, ownKeys []string) map[string]string {
	own := keySet(ownKeys)
	env := make(map[string]string)
	for _, secret := range forProject(secrets, projectID) {
		switch secret.Kind {
		case KindEnv:
			if own[secret.Key] || secret.Value == "" {
				continue
			}
			env[secret.Key] = secret.Value
		case KindSSH:
			if secret.SSH == nil {
				continue
			}
			host, user, port := EnvNamesForSSH(secret.SSH.Name)
			env[host] = secret.SSH.Host
			env[user] = secret.SSH.User
			env[port] = strconv.Itoa(secret.SSH.EffectivePort())
		}
	}
	return env
}

// MaterialFor renders the file and SSH material one project's container
// should carry, plus the environment names the vault owns there.
func MaterialFor(secrets []Secret, projectID string, ownKeys []string) Material {
	scoped := forProject(secrets, projectID)
	own := keySet(ownKeys)

	material := Material{}
	targets := make([]SSHTarget, 0, len(scoped))
	for _, secret := range scoped {
		switch secret.Kind {
		case KindEnv:
			if own[secret.Key] || secret.Value == "" {
				continue
			}
			material.EnvKeys = append(material.EnvKeys, secret.Key)
		case KindFile:
			if secret.Path == "" || secret.Value == "" {
				continue
			}
			material.Files = append(material.Files, MaterialFile{
				Path:    secret.Path,
				Content: secret.Value,
			})
		case KindSSH:
			if secret.SSH == nil || secret.SSH.PrivateKey == "" {
				continue
			}
			target := *secret.SSH
			targets = append(targets, target)
			material.Files = append(material.Files, MaterialFile{
				Path:    ContainerKeyPath(target.Name),
				Content: ensureTrailingNewline(target.PrivateKey),
			})
			material.SSHNames = append(material.SSHNames, target.Name)
			host, user, port := EnvNamesForSSH(target.Name)
			material.EnvKeys = append(material.EnvKeys, host, user, port)
		}
	}
	material.SSHConfig = RenderSSHConfig(targets)
	material.KnownHosts = RenderKnownHosts(targets)
	sort.Strings(material.EnvKeys)
	sort.Slice(material.Files, func(i, j int) bool { return material.Files[i].Path < material.Files[j].Path })
	sort.Strings(material.SSHNames)
	return material
}

// ensureTrailingNewline is what makes a pasted private key usable: OpenSSH
// refuses a key file whose final line has no terminator.
func ensureTrailingNewline(value string) string {
	if value == "" || value[len(value)-1] == '\n' {
		return value
	}
	return value + "\n"
}
