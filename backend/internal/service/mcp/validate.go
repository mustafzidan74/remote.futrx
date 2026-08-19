package mcp

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Errors the transport maps onto status codes. Every message is about shape,
// never about a value.
var (
	ErrUnavailable   = errors.New("MCP registry is unavailable on this server")
	ErrNotFound      = errors.New("MCP server not found")
	ErrExists        = errors.New("an MCP server with that name already exists")
	ErrInvalidName   = errors.New("name must start with a letter or digit and use only letters, digits, '-' or '_' (max 48)")
	ErrInvalidKind   = errors.New("transport must be \"stdio\" or \"http\"")
	ErrNoCommand     = errors.New("a stdio server needs a command")
	ErrNoURL         = errors.New("an http server needs an absolute http(s) URL")
	ErrInvalidArg    = errors.New("an argument may not contain a line break or a null byte")
	ErrInvalidEnv    = errors.New("environment names must match [A-Za-z_][A-Za-z0-9_]* and values may not span lines")
	ErrInvalidHeader = errors.New("header names must match [A-Za-z0-9-]+ and values may not span lines")
	ErrInvalidScope  = errors.New("scope must be all projects or at least one project id")
	ErrTooLarge      = errors.New("entry is too large")
	ErrProvider      = errors.New("enabledForProviders may only name providers that support MCP")
	ErrSecretRef     = errors.New("secretRefs must be Secrets-vault keys matching [A-Za-z_][A-Za-z0-9_]*")
	ErrNoProject     = errors.New("a project id is required")
	ErrProbeFailed   = errors.New("the probe could not be run")
)

// ErrUnresolvedSecret names a placeholder whose vault key is missing or out
// of scope for the project being materialized. The entry is skipped rather
// than written with a literal ${KEY} that would confuse the MCP server.
type ErrUnresolvedSecret struct {
	Server string
	Key    string
}

func (e ErrUnresolvedSecret) Error() string {
	return fmt.Sprintf("MCP server %q references vault key %q, which this project cannot read", e.Server, e.Key)
}

// Size ceilings. They exist to keep one hand-edited registry file from
// producing a container config nothing can parse, not to be exact policy.
const (
	maxArgs        = 32
	maxArgBytes    = 1024
	maxEnvEntries  = 32
	maxHeaders     = 16
	maxValueBytes  = 4096
	maxDescription = 500
	maxSecretRefs  = 16
	maxNameBytes   = 48
)

var (
	namePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	envNamePattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	headerPattern    = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	placeholderRegex = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// ValidName reports whether a name is usable as an MCP server name. It is
// deliberately narrower than either CLI allows: the name becomes part of a
// tool identifier the model sees and a TOML table key, and neither benefits
// from dots or spaces.
func ValidName(name string) bool {
	return len(name) > 0 && len(name) <= maxNameBytes && namePattern.MatchString(name)
}

// ValidTransport reports whether a transport is one this platform renders.
func ValidTransport(transport Transport) bool {
	return transport == TransportStdio || transport == TransportHTTP
}

// Placeholders returns every ${KEY} referenced anywhere in one entry, sorted
// and de-duplicated.
func Placeholders(server Server) []string {
	seen := map[string]bool{}
	collect := func(value string) {
		for _, match := range placeholderRegex.FindAllStringSubmatch(value, -1) {
			seen[match[1]] = true
		}
	}
	collect(server.URL)
	for _, arg := range server.Args {
		collect(arg)
	}
	for _, value := range server.Env {
		collect(value)
	}
	for _, value := range server.Headers {
		collect(value)
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Normalize validates one entry and returns the canonical form that gets
// stored: trimmed strings, a normalized scope, sorted secret refs, and the
// fields the other transport uses dropped so a stdio entry cannot smuggle a
// URL past the renderer.
//
// platform is false for a project-only entry, whose scope is implicit.
func Normalize(server Server, platform bool) (Server, error) {
	name := strings.TrimSpace(server.Name)
	if !ValidName(name) {
		return Server{}, ErrInvalidName
	}
	if !ValidTransport(server.Transport) {
		return Server{}, ErrInvalidKind
	}

	out := Server{
		Name:        name,
		Transport:   server.Transport,
		Description: truncate(strings.TrimSpace(server.Description), maxDescription),
	}

	providers, err := normalizeProviders(server.Providers)
	if err != nil {
		return Server{}, err
	}
	out.Providers = providers

	refs, err := normalizeSecretRefs(server.SecretRefs)
	if err != nil {
		return Server{}, err
	}
	out.SecretRefs = refs

	switch server.Transport {
	case TransportStdio:
		command := strings.TrimSpace(server.Command)
		if command == "" || strings.ContainsAny(command, "\r\n\x00") || len(command) > maxValueBytes {
			return Server{}, ErrNoCommand
		}
		out.Command = command
		if len(server.Args) > maxArgs {
			return Server{}, ErrTooLarge
		}
		for _, arg := range server.Args {
			if strings.ContainsAny(arg, "\r\n\x00") || len(arg) > maxArgBytes {
				return Server{}, ErrInvalidArg
			}
			out.Args = append(out.Args, arg)
		}
	case TransportHTTP:
		endpoint := strings.TrimSpace(server.URL)
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Server{}, ErrNoURL
		}
		out.URL = endpoint
	}

	if env, err := normalizeEnv(server.Env); err != nil {
		return Server{}, err
	} else {
		out.Env = env
	}
	if headers, err := normalizeHeaders(server.Headers); err != nil {
		return Server{}, err
	} else {
		out.Headers = headers
	}
	// Headers only travel over HTTP; an env block is meaningless for a remote
	// endpoint. Dropping them keeps the stored document honest about what the
	// entry actually does.
	if server.Transport == TransportStdio {
		out.Headers = nil
	} else {
		out.Env = nil
	}

	if platform {
		out.Scope = server.Scope.Normalize()
		if !out.Scope.All && len(out.Scope.ProjectIDs) == 0 {
			return Server{}, ErrInvalidScope
		}
	}

	// A placeholder with no matching secretRef would be written literally
	// into the container config, which is never what anyone meant.
	declared := make(map[string]bool, len(out.SecretRefs))
	for _, ref := range out.SecretRefs {
		declared[ref] = true
	}
	for _, key := range Placeholders(out) {
		if !declared[key] {
			return Server{}, fmt.Errorf("%w: ${%s} is not listed in secretRefs", ErrSecretRef, key)
		}
	}
	return out, nil
}

func normalizeProviders(providers []string) ([]string, error) {
	if len(providers) == 0 {
		return nil, nil
	}
	supported := map[string]bool{}
	for _, provider := range SupportedProviders() {
		supported[provider] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(providers))
	for _, provider := range providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" || seen[provider] {
			continue
		}
		if !supported[provider] {
			return nil, fmt.Errorf("%w: %q", ErrProvider, provider)
		}
		seen[provider] = true
		out = append(out, provider)
	}
	if len(out) == 0 {
		return nil, nil
	}
	// Stored in the platform's own order, not the caller's, so two equal
	// selections serialize equally.
	ordered := make([]string, 0, len(out))
	for _, provider := range SupportedProviders() {
		if seen[provider] {
			ordered = append(ordered, provider)
		}
	}
	return ordered, nil
}

func normalizeSecretRefs(refs []string) ([]string, error) {
	if len(refs) > maxSecretRefs {
		return nil, ErrTooLarge
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		if !envNamePattern.MatchString(ref) {
			return nil, ErrSecretRef
		}
		seen[ref] = true
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil, nil
	}
	sort.Strings(out)
	return out, nil
}

func normalizeEnv(env map[string]string) (map[string]string, error) {
	if len(env) == 0 {
		return nil, nil
	}
	if len(env) > maxEnvEntries {
		return nil, ErrTooLarge
	}
	out := make(map[string]string, len(env))
	for name, value := range env {
		name = strings.TrimSpace(name)
		if !envNamePattern.MatchString(name) ||
			strings.ContainsAny(value, "\r\n\x00") || len(value) > maxValueBytes {
			return nil, ErrInvalidEnv
		}
		out[name] = value
	}
	return out, nil
}

func normalizeHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	if len(headers) > maxHeaders {
		return nil, ErrTooLarge
	}
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if !headerPattern.MatchString(name) ||
			strings.ContainsAny(value, "\r\n\x00") || len(value) > maxValueBytes {
			return nil, ErrInvalidHeader
		}
		out[name] = value
	}
	return out, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
