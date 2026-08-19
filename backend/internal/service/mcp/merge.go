package mcp

// Merging the two registry layers, and resolving the vault placeholders one
// project's material carries.

import (
	"sort"
	"strings"
)

// Available returns everything one project can use: platform entries whose
// scope covers it, plus that project's own additions. A project entry of the
// same name shadows the platform one outright — the project is the more
// specific statement, exactly as a project-local skill shadows a global one.
//
// The result is sorted by name so two equal registries render equally.
func Available(platform []Server, settings ProjectSettings, projectID string) []Entry {
	disabled := settings.DisabledSet()
	byName := make(map[string]Entry, len(platform)+len(settings.Servers))

	for _, server := range platform {
		if !server.Scope.Includes(projectID) {
			continue
		}
		byName[server.Name] = Entry{
			Server:  server,
			Source:  SourcePlatform,
			Enabled: !disabled[server.Name],
		}
	}
	for _, server := range settings.Servers {
		// A project-only entry has no scope of its own; carrying one over
		// from a shadowed platform entry would be a lie.
		server.Scope = Scope{}
		byName[server.Name] = Entry{
			Server:  server,
			Source:  SourceProject,
			Enabled: !disabled[server.Name],
		}
	}

	entries := make([]Entry, 0, len(byName))
	for _, entry := range byName {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// SecretKeys lists every vault key the enabled entries of one project need,
// sorted and de-duplicated. It is what the service asks the vault for, so a
// project's materialization reads exactly the keys it uses and no others.
func SecretKeys(entries []Entry) []string {
	seen := map[string]bool{}
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		for _, ref := range entry.SecretRefs {
			seen[ref] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Resolution is the outcome of substituting vault values into the enabled
// entries of one project.
type Resolution struct {
	// Servers are ready to render: every ${KEY} is gone.
	Servers []Server
	// Skipped names the entries left out because a vault key they reference
	// is missing or out of this project's scope. They are reported, never
	// written with a literal placeholder.
	Skipped []string
}

// Resolve substitutes values into the enabled entries. Keys absent from
// values cause their entry to be skipped rather than half-configured.
func Resolve(entries []Entry, values map[string]string) Resolution {
	resolution := Resolution{}
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		resolved, ok := resolveServer(entry.Server, values)
		if !ok {
			resolution.Skipped = append(resolution.Skipped, entry.Name)
			continue
		}
		resolution.Servers = append(resolution.Servers, resolved)
	}
	sort.Strings(resolution.Skipped)
	return resolution
}

func resolveServer(server Server, values map[string]string) (Server, bool) {
	for _, key := range Placeholders(server) {
		if _, ok := values[key]; !ok {
			return Server{}, false
		}
	}
	out := server
	out.URL = substitute(server.URL, values)
	if len(server.Args) > 0 {
		args := make([]string, 0, len(server.Args))
		for _, arg := range server.Args {
			args = append(args, substitute(arg, values))
		}
		out.Args = args
	}
	out.Env = substituteMap(server.Env, values)
	out.Headers = substituteMap(server.Headers, values)
	return out, true
}

func substituteMap(source, values map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = substitute(value, values)
	}
	return out
}

// substitute replaces every ${KEY} whose key is present in values. A
// placeholder with no value is left alone; resolveServer has already refused
// such an entry, so this only ever runs on a fully resolvable one.
func substitute(value string, values map[string]string) string {
	if value == "" || !strings.Contains(value, "${") {
		return value
	}
	return placeholderRegex.ReplaceAllStringFunc(value, func(match string) string {
		key := match[2 : len(match)-1]
		if replacement, ok := values[key]; ok {
			return replacement
		}
		return match
	})
}

// ForProvider keeps only the entries written for one CLI.
func ForProvider(servers []Server, provider string) []Server {
	out := make([]Server, 0, len(servers))
	for _, server := range servers {
		if server.Targets(provider) {
			out = append(out, server)
		}
	}
	return out
}

// Names lists the entry names, sorted, for the manifest and the project panel.
func Names(servers []Server) []string {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	sort.Strings(names)
	return names
}

// UnsupportedNamed reports the providers an entry names that this platform
// cannot configure. Normalize rejects them on the write path; this exists for
// documents edited by hand under DATA_DIR.
func UnsupportedNamed(server Server) []string {
	supported := map[string]bool{}
	for _, provider := range SupportedProviders() {
		supported[provider] = true
	}
	var out []string
	for _, provider := range server.Providers {
		if !supported[provider] {
			out = append(out, provider)
		}
	}
	sort.Strings(out)
	return out
}
