// Package agentendpoints is the platform's register of third-party agent
// endpoints: vendor-published compatibility APIs the operator has their own
// account with, which one chat's coding agent can be pointed at instead of
// the vendor's own default.
//
// The point is cost. A brochure site, a landing page, or a copy edit does not
// need a frontier model, and several vendors publish an endpoint that speaks
// an agent CLI's own protocol precisely so their models can be driven by that
// CLI. Zhipu GLM and Moonshot/Kimi publish Anthropic-compatible endpoints
// documented for use with Claude Code; the Codex CLI documents custom
// OpenAI-compatible providers as `[model_providers.<id>]` blocks in
// ~/.codex/config.toml. Those two sanctioned shapes are the whole feature.
//
// # The rule this package will not bend
//
// Only a vendor's OWN published compatibility endpoint, reached with the
// operator's OWN key, is supported. This platform never impersonates a
// first-party CLI to a vendor that has not published such an endpoint, never
// spoofs a user agent, and never touches cookies or replays a session. A
// vendor with no documented path is simply not supported: there is no profile
// for it, and nothing here could be configured into one.
//
// # What is stored, and what is not
//
// A profile holds the endpoint's base URL, the CLI it is written for, the
// model ids to offer, and the *name* of a Secrets-vault key. It never holds
// the key itself. The value is read from the vault at run time, handed to the
// CLI through the run's own environment, and never logged, never written into
// a chat transcript, and never returned by any API route.
//
// The operator's own Anthropic and ChatGPT credentials are protected in the
// other direction: a run pointed at a third-party endpoint neither seeds them
// into the container nor syncs them back out afterwards, and the rendered
// environment cannot carry them. See render.go, which is the only place an
// endpoint becomes environment.
package agentendpoints

import (
	"sort"
	"strings"
)

// CLI is the agent command line a profile is written for. Only these two are
// supportable: they are the CLIs whose vendors document a compatibility mode
// and whose launch this platform controls end to end. Kimi Code and
// Antigravity are absent because neither documents one.
type CLI string

const (
	CLIClaude CLI = "claude"
	CLICodex  CLI = "codex"
)

// SupportedCLIs is the ordered set a profile may name.
func SupportedCLIs() []string { return []string{string(CLIClaude), string(CLICodex)} }

// UnsupportedCLIs are the agent CLIs the UI reports as having no documented
// third-party mode, so an operator is told rather than left wondering why the
// picker is missing one.
func UnsupportedCLIs() []string { return []string{"kimi", "antigravity"} }

// Wire protocols a codex custom provider may speak. `responses` is the OpenAI
// Responses API, which recent codex builds require; `chat` is the older Chat
// Completions shape most OpenAI-compatible gateways still offer.
const (
	WireResponses = "responses"
	WireChat      = "chat"
)

// WireAPIs is the ordered set the codex form offers.
func WireAPIs() []string { return []string{WireResponses, WireChat} }

// Limits on what one profile may carry, so a hand-edited document cannot turn
// into a command line the CLI refuses to parse.
const (
	MaxIDLength      = 40
	MaxLabelLength   = 60
	MaxURLLength     = 400
	MaxNotesLength   = 600
	MaxKeyRefLength  = 120
	MaxModels        = 24
	MaxModelIDLength = 120
	MaxHeaders       = 8
	MaxHeaderLength  = 300
	MaxEndpoints     = 40
)

// Model is one model id a profile offers, with the name a picker shows.
type Model struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// Name is what a picker renders for this model.
func (m Model) Name() string {
	if label := strings.TrimSpace(m.Label); label != "" {
		return label
	}
	return m.ID
}

// Endpoint is one stored profile.
type Endpoint struct {
	// ID names the profile and, for codex, becomes the `model_providers.<id>`
	// table key on the CLI's command line, so it is restricted to characters
	// that are both a legal bare TOML key and a token needing no quoting.
	ID    string `json:"id"`
	Label string `json:"label"`
	CLI   CLI    `json:"cli"`
	// BaseURL is the vendor's published compatibility endpoint. For the
	// claude CLI it becomes ANTHROPIC_BASE_URL; for codex it becomes the
	// provider table's base_url.
	BaseURL string `json:"baseUrl"`
	// APIKeyRef names a Secrets-vault `env` entry scoped to all projects. Its
	// value is resolved at run time and is never stored here.
	APIKeyRef string  `json:"apiKeyRef"`
	Models    []Model `json:"models,omitempty"`
	// Headers are extra request headers the vendor asks for — OpenRouter's
	// attribution pair, for instance. Names are restricted to header-safe
	// characters because they become part of a config key path.
	Headers map[string]string `json:"headers,omitempty"`
	// WireAPI is the codex `wire_api` value. Ignored for the claude CLI,
	// whose compatibility endpoints speak the Messages API by definition.
	WireAPI string `json:"wireApi,omitempty"`
	// Notes is the operator's own memo, shown in the admin table. It is also
	// where a seeded template records what its values were taken from.
	Notes string `json:"notes,omitempty"`
	// Enabled gates whether the profile appears in the composer and whether a
	// run may use it. Every seeded template ships disabled.
	Enabled   bool        `json:"enabled"`
	UpdatedAt int64       `json:"updatedAt,omitempty"`
	UpdatedBy string      `json:"updatedBy,omitempty"`
	LastTest  *TestRecord `json:"lastTest,omitempty"`
}

// Clone returns a deep copy, so a caller mutating one profile cannot reach
// into the slice a store handed out.
func (e Endpoint) Clone() Endpoint {
	out := e
	out.Models = append([]Model(nil), e.Models...)
	if len(e.Headers) > 0 {
		out.Headers = make(map[string]string, len(e.Headers))
		for name, value := range e.Headers {
			out.Headers[name] = value
		}
	}
	if e.LastTest != nil {
		record := *e.LastTest
		out.LastTest = &record
	}
	return out
}

// ProviderID is the agent-provider vocabulary this profile's CLI corresponds
// to. It is the same string on both sides, named here so callers do not have
// to know that.
func (e Endpoint) ProviderID() string { return string(e.CLI) }

// ModelIDs lists the model ids this profile offers, in stored order.
func (e Endpoint) ModelIDs() []string {
	ids := make([]string, 0, len(e.Models))
	for _, model := range e.Models {
		ids = append(ids, model.ID)
	}
	return ids
}

// DefaultModel is what a run uses when the chat names no model of its own.
// Empty when the profile lists none, which leaves the CLI on whatever the
// endpoint itself defaults to.
func (e Endpoint) DefaultModel() string {
	if len(e.Models) == 0 {
		return ""
	}
	return e.Models[0].ID
}

// Offers reports whether this profile lists one model id.
func (e Endpoint) Offers(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, candidate := range e.Models {
		if candidate.ID == model {
			return true
		}
	}
	return false
}

// TestRecord is the outcome of the last Test, kept on the profile so the
// admin table can show it without re-probing. It carries no CLI output:
// output goes back to the caller that asked for the test and is then dropped.
type TestRecord struct {
	At        int64  `json:"at"`
	OK        bool   `json:"ok"`
	ProjectID string `json:"projectId,omitempty"`
	Model     string `json:"model,omitempty"`
	// Message is a short, already-masked reason for a failure. Empty on
	// success.
	Message string `json:"message,omitempty"`
}

// TestResult is one probe's full outcome. Output is raw CLI output with the
// resolved vault value masked out of it.
type TestResult struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
	// Error is why the probe failed, kept apart from Output because the two
	// answer different questions and a failing CLI often supplies both. A
	// rejected key, for instance, makes the claude CLI print an unrelated
	// warning about connectors and then hang: reporting only what it printed
	// tells the operator nothing about why Test failed.
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationMs"`
}

// View is the API projection of a profile: the stored profile plus whether
// its vault key resolves right now. The value itself never reaches it.
type View struct {
	Endpoint
	// KeyResolved reports that APIKeyRef names a vault entry holding a value.
	// False is exactly why a run would fail before it started, so the admin
	// table says so instead of leaving the operator to discover it in a probe.
	KeyResolved bool `json:"keyResolved"`
}

// Choice is what the composer needs in order to offer one endpoint: the
// section label, which CLI runs, and the models listed under it. No base URL
// and no key reference — a chat member is not an administrator.
type Choice struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	CLI    CLI     `json:"cli"`
	Models []Model `json:"models"`
}

// Choices projects the enabled profiles for the composer, in label order.
func Choices(endpoints []Endpoint) []Choice {
	choices := make([]Choice, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !endpoint.Enabled {
			continue
		}
		models := append([]Model(nil), endpoint.Models...)
		if models == nil {
			models = []Model{}
		}
		choices = append(choices, Choice{
			ID:     endpoint.ID,
			Label:  endpoint.Label,
			CLI:    endpoint.CLI,
			Models: models,
		})
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].Label != choices[j].Label {
			return choices[i].Label < choices[j].Label
		}
		return choices[i].ID < choices[j].ID
	})
	return choices
}

// Find returns the profile with this id.
func Find(endpoints []Endpoint, id string) (Endpoint, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Endpoint{}, false
	}
	for _, endpoint := range endpoints {
		if endpoint.ID == id {
			return endpoint.Clone(), true
		}
	}
	return Endpoint{}, false
}

// BadgeText is the line the chat header shows so nobody mistakes whose model
// produced a piece of client code.
func BadgeText(label, model string) string {
	label = strings.TrimSpace(label)
	model = strings.TrimSpace(model)
	if label == "" {
		return ""
	}
	if model == "" {
		return "running via " + label + " — not Anthropic"
	}
	return "running on " + model + " via " + label + " — not Anthropic"
}
