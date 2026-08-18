// Package transcribe turns a recorded audio clip into text through a hosted
// speech-to-text provider, so a browser without the Web Speech API (Firefox)
// — or a user who wants better Arabic than the browser gives — can still
// dictate into the chat composer.
//
// The browser path needs no server at all; this package exists only for the
// optional fallback an admin configures with an API key. Audio is streamed
// through to the provider and is never written to disk or to a log.
package transcribe

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidConfig is returned for configurations the operator can fix.
var ErrInvalidConfig = errors.New("invalid transcription settings")

// ProviderOpenAI is the only provider implemented today. The field is stored
// so a second provider can be added without a migration.
const ProviderOpenAI = "openai"

// DefaultModel favours the cheaper, faster transcription model. whisper-1 is
// the documented alternative for operators who want the original endpoint.
const DefaultModel = "gpt-4o-mini-transcribe"

// supportedModels is the allowlist. Anything else is rejected on save so a
// typo cannot turn into a provider 400 on somebody's first dictation.
var supportedModels = []string{DefaultModel, "gpt-4o-transcribe", "whisper-1"}

// SupportedModels lists the selectable transcription models.
func SupportedModels() []string {
	return append([]string(nil), supportedModels...)
}

// Config is the global transcription configuration persisted at
// DATA_DIR/transcription.json.
type Config struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey,omitempty"`
	Model    string `json:"model"`
	// DefaultLanguage is the BCP-47 tag the composer preselects for users who
	// have not chosen one. Empty means "let the browser and the provider
	// detect it".
	DefaultLanguage string `json:"defaultLanguage"`
	UpdatedAt       int64  `json:"updatedAt,omitempty"`
}

// DefaultConfig is what a server starts with: off, no key, cheap model.
func DefaultConfig() Config {
	return Config{
		Enabled:  false,
		Provider: ProviderOpenAI,
		Model:    DefaultModel,
	}
}

// Normalize trims operator-entered values and fills fields a document written
// by an older build may be missing.
func (c Config) Normalize() Config {
	c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
	if c.Provider == "" {
		c.Provider = ProviderOpenAI
	}
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.Model = strings.TrimSpace(c.Model)
	if c.Model == "" {
		c.Model = DefaultModel
	}
	c.DefaultLanguage = strings.TrimSpace(c.DefaultLanguage)
	return c
}

// Configured reports whether the provider has everything it needs to run.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

// Active reports whether the composer should offer server transcription: the
// switch is on and a key is stored.
func (c Config) Active() bool {
	return c.Enabled && c.Configured()
}

// PublicConfig is the admin-facing view. The API key is never echoed back:
// the caller sees only whether one is stored and its masked tail.
type PublicConfig struct {
	Enabled         bool     `json:"enabled"`
	Configured      bool     `json:"configured"`
	Provider        string   `json:"provider"`
	APIKeyMasked    string   `json:"apiKeyMasked,omitempty"`
	Model           string   `json:"model"`
	DefaultLanguage string   `json:"defaultLanguage"`
	Models          []string `json:"models"`
	UpdatedAt       int64    `json:"updatedAt,omitempty"`
}

// Public renders the admin-facing view of a configuration.
func (c Config) Public() PublicConfig {
	c = c.Normalize()
	return PublicConfig{
		Enabled:         c.Enabled,
		Configured:      c.Configured(),
		Provider:        c.Provider,
		APIKeyMasked:    MaskSecret(c.APIKey),
		Model:           c.Model,
		DefaultLanguage: c.DefaultLanguage,
		Models:          SupportedModels(),
		UpdatedAt:       c.UpdatedAt,
	}
}

// ClientConfig is what every signed-in user may read: enough for the composer
// to decide whether to offer the server option and how long a clip may be. It
// carries no provider identity and no key material.
type ClientConfig struct {
	Enabled         bool   `json:"enabled"`
	DefaultLanguage string `json:"defaultLanguage"`
	MaxBytes        int64  `json:"maxBytes"`
	MaxSeconds      int    `json:"maxSeconds"`
}

// Client renders the composer-facing view.
func (c Config) Client() ClientConfig {
	c = c.Normalize()
	return ClientConfig{
		Enabled:         c.Active(),
		DefaultLanguage: c.DefaultLanguage,
		MaxBytes:        MaxAudioBytes,
		MaxSeconds:      int(MaxAudioDuration.Seconds()),
	}
}

const maskPrefix = "••••"

// MaskSecret renders a stored secret as the mask prefix plus its last four
// characters: enough for an operator to recognize which credential is
// installed, never enough to reuse it.
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

// UpdateInput is the admin PUT body. The key follows write-only semantics: an
// empty string keeps whatever is stored (the client only ever saw a mask), and
// ClearAPIKey removes it.
type UpdateInput struct {
	Enabled         bool   `json:"enabled"`
	Provider        string `json:"provider"`
	APIKey          string `json:"apiKey"`
	ClearAPIKey     bool   `json:"clearApiKey"`
	Model           string `json:"model"`
	DefaultLanguage string `json:"defaultLanguage"`
}

// Apply folds an update onto the stored configuration, preserving the key the
// caller did not resubmit.
func (c Config) Apply(input UpdateInput) Config {
	next := c.Normalize()
	next.Enabled = input.Enabled
	if provider := strings.TrimSpace(input.Provider); provider != "" {
		next.Provider = strings.ToLower(provider)
	}
	if model := strings.TrimSpace(input.Model); model != "" {
		next.Model = model
	}
	next.DefaultLanguage = strings.TrimSpace(input.DefaultLanguage)
	switch {
	case input.ClearAPIKey:
		next.APIKey = ""
	case strings.TrimSpace(input.APIKey) != "":
		next.APIKey = strings.TrimSpace(input.APIKey)
	}
	return next.Normalize()
}

// validate rejects configurations that would fail at the provider, so the
// operator learns about the problem on save rather than on someone's first
// dictation.
func validate(cfg Config) error {
	cfg = cfg.Normalize()
	if cfg.Provider != ProviderOpenAI {
		return invalidConfig("provider %q is not supported (only %q for now)", cfg.Provider, ProviderOpenAI)
	}
	if !supportedModel(cfg.Model) {
		return invalidConfig("model %q is not one of %s", cfg.Model, strings.Join(supportedModels, ", "))
	}
	if cfg.DefaultLanguage != "" && LanguageHint(cfg.DefaultLanguage) == "" {
		return invalidConfig("default language %q is not a language tag", cfg.DefaultLanguage)
	}
	if cfg.Enabled && !cfg.Configured() {
		return invalidConfig("add an API key before enabling server transcription")
	}
	return nil
}

func invalidConfig(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, fmt.Sprintf(format, args...))
}

func supportedModel(model string) bool {
	for _, candidate := range supportedModels {
		if candidate == model {
			return true
		}
	}
	return false
}

// LanguageHint reduces a BCP-47 tag ("ar-EG") to the ISO-639-1 primary subtag
// ("ar") the transcription API expects. It returns "" for anything that is not
// a two-letter primary subtag, including the composer's "auto" sentinel, so an
// unusable hint is omitted rather than rejected by the provider.
func LanguageHint(tag string) string {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if tag == "" || tag == "auto" {
		return ""
	}
	primary, _, _ := strings.Cut(tag, "-")
	if len(primary) != 2 {
		return ""
	}
	for _, r := range primary {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return primary
}
