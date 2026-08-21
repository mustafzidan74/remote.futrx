// Package providerpool is the platform's pool of third-party model API
// providers — the free tiers of Gemini, Groq, Cerebras, OpenRouter, Zhipu
// GLM, Mistral, Moonshot/Kimi, GitHub Models and anything else that speaks
// one of the three wire shapes below.
//
// It exists for one reason: those free tiers are generous but small, and each
// one runs out on its own schedule. An operator should be able to connect
// several, let the platform move to the next one when a quota is exhausted,
// and — the part that is usually missing — actually see how much of each free
// tier has been consumed.
//
// Three things are deliberately true of this package:
//
//   - It never serves an agent run. Agent runs go through the provider CLIs
//     and their own credentials; this pool is for the platform's own small
//     jobs and the bulk lane.
//   - The documented free-tier limits it ships are *seed data*, not truth. A
//     vendor changes them without notice, so every limit is nullable, every
//     limit is editable, and no code path treats a seeded number as a fact
//     about the world. What the vendor's own rate-limit headers say always
//     wins over what we counted.
//   - Nothing here may become load bearing. A caller that cannot get a
//     provider gets an error and falls back to whatever it did before.
package providerpool

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// ErrInvalidProvider is returned for entries the operator can fix. It is the
// only validation error the handler maps to 400.
var ErrInvalidProvider = errors.New("invalid provider")

var (
	// ErrNoProvider reports that nothing in the pool could take the request:
	// every candidate is disabled, keyless, cooling down, or over a limit.
	ErrNoProvider = errors.New("no provider in the pool can take this request")
	// ErrUnknownProvider names a provider id that is not in the registry.
	ErrUnknownProvider = errors.New("unknown provider")
	// ErrEmptyPrompt reports a completion asked for with nothing to send.
	ErrEmptyPrompt = errors.New("nothing to send to the provider pool")
)

// Kind is the wire shape a provider speaks. Everything above this layer is
// shared; only the request path, the JSON, and the auth header differ.
type Kind string

const (
	// KindOpenAI posts to {baseUrl}/chat/completions with a bearer token. It
	// covers every seed this package ships, Gemini included — Google runs an
	// OpenAI-compatible surface at /v1beta/openai/ and it is the easier one.
	KindOpenAI Kind = "openai"
	// KindGemini posts to Google's native
	// {baseUrl}/models/{model}:generateContent with an x-goog-api-key header.
	KindGemini Kind = "gemini"
	// KindAnthropic posts to {baseUrl}/messages with x-api-key plus the
	// anthropic-version header.
	KindAnthropic Kind = "anthropic"
)

// Kinds lists every supported wire shape, in the order the settings panel
// offers them.
func Kinds() []Kind { return []Kind{KindOpenAI, KindGemini, KindAnthropic} }

// NormalizeKind maps operator input onto a supported shape, defaulting to the
// OpenAI-compatible one because that is what almost everything speaks.
func NormalizeKind(kind string) Kind {
	switch Kind(strings.ToLower(strings.TrimSpace(kind))) {
	case KindGemini:
		return KindGemini
	case KindAnthropic:
		return KindAnthropic
	default:
		return KindOpenAI
	}
}

// Capability is what a model is worth using for. It is advisory routing
// metadata, not a promise: "bulk" means cheap and fast rather than good.
type Capability string

const (
	CapabilityText Capability = "text"
	CapabilityCode Capability = "code"
	CapabilityBulk Capability = "bulk"
)

// Capabilities lists every capability tag in panel order.
func Capabilities() []Capability {
	return []Capability{CapabilityText, CapabilityCode, CapabilityBulk}
}

func normalizeCapabilities(values []Capability) []Capability {
	seen := make(map[Capability]bool, len(values))
	out := make([]Capability, 0, len(values))
	for _, value := range values {
		capability := Capability(strings.ToLower(strings.TrimSpace(string(value))))
		switch capability {
		case CapabilityText, CapabilityCode, CapabilityBulk:
		default:
			continue
		}
		if seen[capability] {
			continue
		}
		seen[capability] = true
		out = append(out, capability)
	}
	return out
}

// Model is one model a provider offers. GoodFor keeps the wire name the
// registry document uses.
type Model struct {
	ID            string       `json:"id"`
	Label         string       `json:"label,omitempty"`
	ContextTokens int          `json:"contextTokens,omitempty"`
	GoodFor       []Capability `json:"good_for,omitempty"`
}

// Suits reports whether this model claims a capability. A model with no tags
// at all suits everything: an operator who pasted a model id and stopped
// should not find it silently unreachable.
func (m Model) Suits(capability Capability) bool {
	if capability == "" || len(m.GoodFor) == 0 {
		return true
	}
	for _, tag := range m.GoodFor {
		if tag == capability {
			return true
		}
	}
	return false
}

// Limits are the documented free-tier caps. Every field is nullable because
// "this vendor publishes no daily request cap" and "this vendor's daily cap
// is zero" are different facts, and only one of them is real.
//
// Nothing in this package treats a limit as authoritative. They drive the
// meters in the UI and the "skip a provider that is over its counted limit"
// rule, and both of those are advisory by construction.
type Limits struct {
	RPM           *int `json:"rpm"`
	RPD           *int `json:"rpd"`
	TPM           *int `json:"tpm"`
	TPD           *int `json:"tpd"`
	MonthlyTokens *int `json:"monthlyTokens"`
}

// SeedLimitsNote is stamped on every shipped template. It is a string on the
// document rather than a comment in code precisely so the operator reads it.
const SeedLimitsNote = "As documented at seeding time — verify against the vendor's current published limits."

// Provider is one connected API provider.
//
// A key may arrive two ways: APIKeyRef names a key in the platform Secrets
// vault, which is the recommended shape because the value then lives in one
// place; APIKey is an inline value for an operator who does not want a vault
// entry. APIKey is write-only over the API — reads return a mask — and a ref
// always wins over an inline value when both are set.
type Provider struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	Kind      Kind    `json:"kind"`
	BaseURL   string  `json:"baseUrl"`
	APIKeyRef string  `json:"apiKeyRef,omitempty"`
	APIKey    string  `json:"apiKey,omitempty"`
	Models    []Model `json:"models"`
	Limits    Limits  `json:"limits"`
	Priority  int     `json:"priority"`
	Enabled   bool    `json:"enabled"`
	Notes     string  `json:"notes,omitempty"`
	// LimitsNote explains where the numbers in Limits came from. Seeds carry
	// SeedLimitsNote; an operator who edits the limits clears it, which is
	// how the panel knows to stop showing the "verify" warning.
	LimitsNote string `json:"limitsNote,omitempty"`
	// Seed marks an entry this package shipped rather than one the operator
	// typed. It only affects presentation.
	Seed      bool  `json:"seed,omitempty"`
	UpdatedAt int64 `json:"updatedAt,omitempty"`
}

// idPattern is what an id may look like. It travels in a URL path and keys
// every usage counter, so it stays boring on purpose.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}[a-z0-9]$`)

// reservedIDs are ids the admin routes spend on their own verbs. Keeping the
// list here rather than in the handler is what makes it impossible to create
// a provider that shadows a route.
var reservedIDs = map[string]bool{"reorder": true, "settings": true}

// vaultKeyPattern matches the Secrets vault's own key shape, so a typo is
// caught on save rather than at 3am when a quota runs out and the failover
// cannot find a key.
var vaultKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Normalize trims operator input and fills in what an older document or a
// half-filled form left out.
func (p Provider) Normalize() Provider {
	p.ID = strings.ToLower(strings.TrimSpace(p.ID))
	p.Label = strings.TrimSpace(p.Label)
	if p.Label == "" {
		p.Label = p.ID
	}
	p.Kind = NormalizeKind(string(p.Kind))
	p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	p.APIKeyRef = strings.TrimSpace(p.APIKeyRef)
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.Notes = strings.TrimSpace(p.Notes)
	p.LimitsNote = strings.TrimSpace(p.LimitsNote)
	models := make([]Model, 0, len(p.Models))
	seen := make(map[string]bool, len(p.Models))
	for _, model := range p.Models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		model.Label = strings.TrimSpace(model.Label)
		if model.Label == "" {
			model.Label = model.ID
		}
		if model.ContextTokens < 0 {
			model.ContextTokens = 0
		}
		model.GoodFor = normalizeCapabilities(model.GoodFor)
		models = append(models, model)
	}
	p.Models = models
	p.Limits = p.Limits.Normalize()
	if p.Priority < 0 {
		p.Priority = 0
	}
	return p
}

// Normalize drops limits that are not positive numbers. A vendor cap of zero
// is meaningless — it would mean "you may make no requests" — so it is read
// as "not documented" rather than stored as a cap nothing can satisfy.
func (l Limits) Normalize() Limits {
	clean := func(value *int) *int {
		if value == nil || *value <= 0 {
			return nil
		}
		return value
	}
	return Limits{
		RPM:           clean(l.RPM),
		RPD:           clean(l.RPD),
		TPM:           clean(l.TPM),
		TPD:           clean(l.TPD),
		MonthlyTokens: clean(l.MonthlyTokens),
	}
}

// HasKey reports whether a credential is configured at all. It says nothing
// about whether a referenced vault key actually resolves — that is answered
// at call time, because the vault can change under us.
func (p Provider) HasKey() bool {
	return p.APIKeyRef != "" || p.APIKey != ""
}

// PickModel chooses the model to use for one need. A preferred model id wins
// if this provider has it; otherwise the first model that claims the wanted
// capability and clears the context floor.
func (p Provider) PickModel(need Need) (Model, bool) {
	preferred := strings.TrimSpace(need.PreferModel)
	if preferred != "" {
		for _, model := range p.Models {
			if model.ID == preferred {
				return model, true
			}
		}
	}
	for _, model := range p.Models {
		if !model.Suits(need.Capability()) {
			continue
		}
		if need.MinContext > 0 && model.ContextTokens > 0 && model.ContextTokens < need.MinContext {
			continue
		}
		return model, true
	}
	return Model{}, false
}

// Settings is the pool's global policy: whether the platform may move on when
// a quota runs out, and which provider it uses when it may not.
type Settings struct {
	// AutoSwitch on means "walk the priority order and skip whatever cannot
	// take this right now". Off means "use exactly the preferred provider,
	// and fail if it cannot".
	AutoSwitch bool `json:"autoSwitch"`
	// PreferredProviderID is the manual choice. Empty with AutoSwitch off
	// means the pool declines every request, which is the honest reading of
	// "do not switch, and I have not said what to use".
	PreferredProviderID string `json:"preferredProviderId,omitempty"`
	UpdatedAt           int64  `json:"updatedAt,omitempty"`
}

// Registry is the whole document persisted at DATA_DIR/providers.json.
type Registry struct {
	Providers []Provider `json:"providers"`
	Settings  Settings   `json:"settings"`
	// Seeded records that the shipped templates have been installed once, so
	// an operator who deletes every seed does not get them back on restart.
	Seeded bool `json:"seeded"`
}

// Normalize cleans every entry, drops entries with no usable id, and puts the
// list in the order Pick walks: priority ascending, then label.
func (r Registry) Normalize() Registry {
	providers := make([]Provider, 0, len(r.Providers))
	seen := make(map[string]bool, len(r.Providers))
	for _, provider := range r.Providers {
		provider = provider.Normalize()
		if provider.ID == "" || seen[provider.ID] {
			continue
		}
		seen[provider.ID] = true
		providers = append(providers, provider)
	}
	sort.SliceStable(providers, func(i, j int) bool {
		if providers[i].Priority != providers[j].Priority {
			return providers[i].Priority < providers[j].Priority
		}
		return providers[i].Label < providers[j].Label
	})
	r.Providers = providers
	r.Settings.PreferredProviderID = strings.ToLower(strings.TrimSpace(r.Settings.PreferredProviderID))
	return r
}

// Clone returns a registry that shares nothing with this one.
//
// It matters because Registry is passed around by value while its provider
// slice is not: an editor that assigned into r.Providers[i] would reach
// through the copy and mutate the live document, and a write that then failed
// would leave the running pool describing something that was never stored.
func (r Registry) Clone() Registry {
	providers := make([]Provider, len(r.Providers))
	copy(providers, r.Providers)
	for index := range providers {
		models := make([]Model, len(providers[index].Models))
		copy(models, providers[index].Models)
		for position := range models {
			tags := make([]Capability, len(models[position].GoodFor))
			copy(tags, models[position].GoodFor)
			models[position].GoodFor = tags
		}
		providers[index].Models = models
	}
	r.Providers = providers
	return r
}

// Find returns one provider by id.
func (r Registry) Find(id string) (Provider, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, provider := range r.Providers {
		if provider.ID == id {
			return provider, true
		}
	}
	return Provider{}, false
}

// validate rejects entries that would fail at the endpoint, so an operator
// learns about it on save rather than from a silent failover nobody notices.
func validate(provider Provider) error {
	provider = provider.Normalize()
	if !idPattern.MatchString(provider.ID) {
		return invalid("the id must be 2-40 lower-case letters, digits or hyphens, starting and ending with a letter or digit")
	}
	if reservedIDs[provider.ID] {
		return invalid("%q is reserved for an API route and cannot be a provider id", provider.ID)
	}
	if provider.BaseURL == "" {
		return invalid("a base URL is required")
	}
	parsed, err := url.Parse(provider.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return invalid("the base URL must be an absolute http(s) URL")
	}
	if len(provider.Models) == 0 {
		return invalid("list at least one model id")
	}
	if provider.APIKeyRef != "" && !vaultKeyPattern.MatchString(provider.APIKeyRef) {
		return invalid("a Secrets-vault key name looks like MY_API_KEY")
	}
	if provider.Enabled && !provider.HasKey() {
		return invalid("add an API key, or a Secrets-vault key name, before enabling %s", provider.Label)
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidProvider, fmt.Sprintf(format, args...))
}

const maskPrefix = "••••"

// MaskSecret renders a stored key as the mask prefix plus its last four
// characters: enough to recognize which credential is installed, never enough
// to reuse it.
func MaskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return maskPrefix
	}
	return maskPrefix + string(runes[len(runes)-4:])
}
