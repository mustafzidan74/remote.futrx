package agentendpoints

// Validation and normalization of one stored profile.
//
// Two of these rules are load-bearing rather than cosmetic:
//
//   - the id becomes a `model_providers.<id>` key path on the codex command
//     line, so it is restricted to characters that are simultaneously a legal
//     bare TOML key and a token that needs no shell quoting;
//   - a header name becomes a `http_headers.<name>` key path for the same
//     reason, so it is restricted to the header-token characters.
//
// Everything else is a length or shape bound that keeps a hand-edited
// document from producing a command line the CLI refuses to parse.

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	ErrInvalidID     = errors.New("endpoint id must be 1-40 characters of a-z, 0-9, hyphen or underscore")
	ErrInvalidLabel  = errors.New("endpoint label is required")
	ErrInvalidCLI    = errors.New("endpoint cli must be claude or codex")
	ErrInvalidURL    = errors.New("endpoint base URL must be an absolute http(s) URL")
	ErrInvalidKeyRef = errors.New("endpoint API key reference must name a Secrets vault key")
	ErrInvalidModel  = errors.New("endpoint model ids must be non-empty and free of whitespace")
	ErrInvalidHeader = errors.New("endpoint header names must be header tokens (letters, digits, hyphen)")
	ErrTooLarge      = errors.New("endpoint profile is too large")
	ErrNotFound      = errors.New("agent endpoint not found")
	ErrExists        = errors.New("an agent endpoint with that id already exists")
	ErrDisabled      = errors.New("this agent endpoint is disabled")
	ErrUnavailable   = errors.New("agent endpoints are unavailable on this deployment")
	ErrNoProject     = errors.New("a project is required")
	ErrProbeFailed   = errors.New("the endpoint test could not be run on this deployment")
)

// ErrKeyUnresolved reports that a profile's vault key holds no value. It is
// the error an operator sees most often — a profile enabled before its key
// was added — so it names both halves of the problem.
type ErrKeyUnresolved struct {
	Endpoint string
	Key      string
}

func (e ErrKeyUnresolved) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("agent endpoint %q has no API key reference", e.Endpoint)
	}
	return fmt.Sprintf(
		"agent endpoint %q needs Secrets vault key %q, which is not set for all projects",
		e.Endpoint, e.Key,
	)
}

// ValidID reports whether a string may name a profile. The character set is
// the intersection of "legal bare TOML key" and "needs no shell quoting".
func ValidID(id string) bool {
	if id == "" || len(id) > MaxIDLength {
		return false
	}
	for index := 0; index < len(id); index++ {
		char := id[index]
		switch {
		case char >= 'a' && char <= 'z',
			char >= '0' && char <= '9':
		case char == '-' || char == '_':
			// A leading separator would render a table key that reads as an
			// empty first segment.
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ValidHeaderName reports whether a header name may become a `http_headers.*`
// key path segment.
func ValidHeaderName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for index := 0; index < len(name); index++ {
		char := name[index]
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9':
		case char == '-' || char == '_':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// NormalizeCLI collapses anything unrecognized onto the empty CLI, so an
// unknown value is rejected by Normalize rather than silently treated as one
// of the two supported command lines.
func NormalizeCLI(cli CLI) CLI {
	switch CLI(strings.ToLower(strings.TrimSpace(string(cli)))) {
	case CLIClaude:
		return CLIClaude
	case CLICodex:
		return CLICodex
	default:
		return ""
	}
}

// Normalize validates and canonicalizes one profile. It runs on the way in
// from the API and again on the way out of the store, so a document written
// by an earlier version — or edited by hand — still yields something the
// renderers can be trusted with.
func Normalize(endpoint Endpoint) (Endpoint, error) {
	endpoint.ID = strings.ToLower(strings.TrimSpace(endpoint.ID))
	if !ValidID(endpoint.ID) {
		return Endpoint{}, ErrInvalidID
	}
	endpoint.Label = strings.TrimSpace(endpoint.Label)
	if endpoint.Label == "" {
		return Endpoint{}, ErrInvalidLabel
	}
	if len(endpoint.Label) > MaxLabelLength {
		return Endpoint{}, ErrTooLarge
	}

	endpoint.CLI = NormalizeCLI(endpoint.CLI)
	if endpoint.CLI == "" {
		return Endpoint{}, ErrInvalidCLI
	}

	endpoint.BaseURL = strings.TrimSpace(endpoint.BaseURL)
	if len(endpoint.BaseURL) > MaxURLLength {
		return Endpoint{}, ErrTooLarge
	}
	if !validBaseURL(endpoint.BaseURL) {
		return Endpoint{}, ErrInvalidURL
	}

	endpoint.APIKeyRef = strings.TrimSpace(endpoint.APIKeyRef)
	if len(endpoint.APIKeyRef) > MaxKeyRefLength {
		return Endpoint{}, ErrTooLarge
	}
	// A disabled template may not name a key yet: seeding one that does would
	// mean inventing a vault key nobody created. An enabled profile must.
	if endpoint.Enabled && !validKeyRef(endpoint.APIKeyRef) {
		return Endpoint{}, ErrInvalidKeyRef
	}
	if endpoint.APIKeyRef != "" && !validKeyRef(endpoint.APIKeyRef) {
		return Endpoint{}, ErrInvalidKeyRef
	}

	models, err := normalizeModels(endpoint.Models)
	if err != nil {
		return Endpoint{}, err
	}
	endpoint.Models = models

	headers, err := normalizeHeaders(endpoint.Headers)
	if err != nil {
		return Endpoint{}, err
	}
	endpoint.Headers = headers

	if endpoint.CLI == CLICodex {
		endpoint.WireAPI = NormalizeWireAPI(endpoint.WireAPI)
	} else {
		// The claude CLI has no wire selection; keeping a value would suggest
		// it did.
		endpoint.WireAPI = ""
	}

	endpoint.Notes = strings.TrimSpace(endpoint.Notes)
	if len(endpoint.Notes) > MaxNotesLength {
		return Endpoint{}, ErrTooLarge
	}
	endpoint.UpdatedBy = strings.ToLower(strings.TrimSpace(endpoint.UpdatedBy))
	return endpoint, nil
}

// validBaseURL insists on an absolute http(s) URL with a host. A relative or
// scheme-less value would be accepted by the CLI and then fail at request
// time with a message that says nothing about the profile.
func validBaseURL(raw string) bool {
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	// A URL carrying credentials or a query would be a sign the operator
	// pasted something other than a base.
	return parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

// validKeyRef accepts what the Secrets vault accepts as an environment key.
func validKeyRef(key string) bool {
	if key == "" {
		return false
	}
	for index := 0; index < len(key); index++ {
		char := key[index]
		switch {
		case char >= 'A' && char <= 'Z', char == '_':
		case char >= '0' && char <= '9':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func normalizeModels(models []Model) ([]Model, error) {
	if len(models) > MaxModels {
		return nil, ErrTooLarge
	}
	seen := make(map[string]bool, len(models))
	out := make([]Model, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if len(id) > MaxModelIDLength || strings.ContainsAny(id, " \t\r\n") {
			return nil, ErrInvalidModel
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(model.Label)
		if len(label) > MaxLabelLength {
			return nil, ErrTooLarge
		}
		out = append(out, Model{ID: id, Label: label})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizeHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	if len(headers) > MaxHeaders {
		return nil, ErrTooLarge
	}
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" && value == "" {
			continue
		}
		if !ValidHeaderName(name) {
			return nil, ErrInvalidHeader
		}
		if len(value) > MaxHeaderLength || strings.ContainsAny(value, "\r\n") {
			return nil, ErrTooLarge
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
