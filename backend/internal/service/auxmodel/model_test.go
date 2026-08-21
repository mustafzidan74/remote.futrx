package auxmodel

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeFillsAndClamps(t *testing.T) {
	tests := []struct {
		name string
		in   Config
		want Config
	}{
		{
			name: "an empty document becomes the local Ollama default",
			in:   Config{},
			want: Config{
				Provider:       ProviderOllama,
				BaseURL:        DefaultOllamaBaseURL,
				Model:          DefaultModel,
				TimeoutSeconds: DefaultTimeoutSeconds,
				MaxTokens:      DefaultMaxTokens,
			},
		},
		{
			name: "an unknown provider falls back to ollama",
			in:   Config{Provider: "anthropic", BaseURL: "http://x:1", Model: "m"},
			want: Config{
				Provider:       ProviderOllama,
				BaseURL:        "http://x:1",
				Model:          "m",
				TimeoutSeconds: DefaultTimeoutSeconds,
				MaxTokens:      DefaultMaxTokens,
			},
		},
		{
			name: "a trailing slash never reaches the URL builders",
			in: Config{
				Provider: ProviderOpenAICompatible,
				BaseURL:  "  https://api.example.com/v1/  ",
				Model:    " gpt-4o-mini ",
			},
			want: Config{
				Provider:       ProviderOpenAICompatible,
				BaseURL:        "https://api.example.com/v1",
				Model:          "gpt-4o-mini",
				TimeoutSeconds: DefaultTimeoutSeconds,
				MaxTokens:      DefaultMaxTokens,
			},
		},
		{
			name: "an absurd timeout and token cap are clamped",
			in: Config{
				Provider:       ProviderOllama,
				BaseURL:        "http://x:1",
				Model:          "m",
				TimeoutSeconds: 9999,
				MaxTokens:      999999,
			},
			want: Config{
				Provider:       ProviderOllama,
				BaseURL:        "http://x:1",
				Model:          "m",
				TimeoutSeconds: MaxTimeoutSeconds,
				MaxTokens:      MaxMaxTokens,
			},
		},
		{
			name: "a sub-minimum timeout is raised rather than accepted",
			in: Config{
				Provider:       ProviderOllama,
				BaseURL:        "http://x:1",
				Model:          "m",
				TimeoutSeconds: 1,
				MaxTokens:      1,
			},
			want: Config{
				Provider:       ProviderOllama,
				BaseURL:        "http://x:1",
				Model:          "m",
				TimeoutSeconds: MinTimeoutSeconds,
				MaxTokens:      MinMaxTokens,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in.Normalize()
			test.want.Jobs = DefaultJobSettings()
			if got.Provider != test.want.Provider || got.BaseURL != test.want.BaseURL ||
				got.Model != test.want.Model || got.TimeoutSeconds != test.want.TimeoutSeconds ||
				got.MaxTokens != test.want.MaxTokens {
				t.Fatalf("Normalize() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestJobSettingsDefaultToOn(t *testing.T) {
	// A document written before a job existed must not switch that job off for
	// everybody: an absent key means "on", not "false".
	partial := JobSettings{JobChatTitle: SourceOff}
	normalized := partial.Normalize()

	if normalized.Enabled(JobChatTitle) {
		t.Fatal("an explicitly disabled job came back enabled")
	}
	for _, job := range Jobs() {
		if job == JobChatTitle {
			continue
		}
		if !normalized.Enabled(job) {
			t.Fatalf("%s should default to on when the document never mentioned it", job)
		}
	}
	if normalized.Enabled(Job("invented")) {
		t.Fatal("an unknown job name must never be reported enabled")
	}
	if len(normalized) != len(Jobs()) {
		t.Fatalf("normalized map has %d entries, want one per known job", len(normalized))
	}
}

func TestMaskSecretRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{name: "nothing stored masks to nothing", secret: "", want: ""},
		{name: "whitespace is not a secret", secret: "   ", want: ""},
		{name: "a short secret reveals nothing", secret: "abc", want: "••••"},
		{name: "a normal key keeps its last four", secret: "sk-proj-abcdef1234", want: "••••1234"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MaskSecret(test.secret)
			if got != test.want {
				t.Fatalf("MaskSecret(%q) = %q, want %q", test.secret, got, test.want)
			}
			if trimmed := strings.TrimSpace(test.secret); trimmed != "" && strings.Contains(got, trimmed) {
				t.Fatalf("MaskSecret(%q) leaked the whole secret", test.secret)
			}
		})
	}
}

func TestPublicNeverCarriesTheKeyAndApplyKeepsIt(t *testing.T) {
	stored := Config{
		Enabled:        true,
		Provider:       ProviderOpenAICompatible,
		BaseURL:        "https://api.example.com",
		Model:          "gpt-4o-mini",
		APIKey:         "sk-secret-value-9876",
		TimeoutSeconds: 20,
		MaxTokens:      200,
	}.Normalize()

	public := stored.Public()
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public config: %v", err)
	}
	if strings.Contains(string(encoded), "sk-secret-value-9876") {
		t.Fatalf("the admin view carried the stored key: %s", encoded)
	}
	if public.APIKeyMasked != "••••9876" || !public.KeyConfigured {
		t.Fatalf("masked key = %q, configured = %v", public.APIKeyMasked, public.KeyConfigured)
	}

	// The browser only ever saw the mask, so an empty key in the update body
	// has to mean "keep what is stored".
	kept := stored.Apply(UpdateInput{
		Enabled:  true,
		Provider: ProviderOpenAICompatible,
		BaseURL:  "https://api.example.com",
		Model:    "gpt-4o-mini",
		APIKey:   "",
	})
	if kept.APIKey != "sk-secret-value-9876" {
		t.Fatalf("an empty key in the body dropped the stored one: %q", kept.APIKey)
	}

	cleared := stored.Apply(UpdateInput{
		Enabled:     true,
		Provider:    ProviderOpenAICompatible,
		BaseURL:     "https://api.example.com",
		Model:       "gpt-4o-mini",
		ClearAPIKey: true,
	})
	if cleared.APIKey != "" {
		t.Fatalf("clearApiKey left %q behind", cleared.APIKey)
	}
}

func TestApplyPatchesOneJobToggleWithoutTouchingTheRest(t *testing.T) {
	stored := DefaultConfig()
	next := stored.Apply(UpdateInput{
		Enabled:  true,
		Provider: ProviderOllama,
		BaseURL:  DefaultOllamaBaseURL,
		Model:    DefaultModel,
		Jobs:     map[string]string{string(JobCommitMessage): string(SourceOff), "not-a-job": string(SourceOff)},
	})

	if next.Jobs.Enabled(JobCommitMessage) {
		t.Fatal("the commit-message toggle was not applied")
	}
	for _, job := range Jobs() {
		if job == JobCommitMessage {
			continue
		}
		if !next.Jobs.Enabled(job) {
			t.Fatalf("%s was switched off by a patch that never named it", job)
		}
	}
}

func TestValidateRejectsWhatWouldFailAtTheEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:   "the default local setup is valid",
			config: DefaultConfig(),
		},
		{
			name: "a relative base URL is refused",
			config: Config{
				Provider: ProviderOllama, BaseURL: "127.0.0.1:11434", Model: "m",
			},
			wantErr: true,
		},
		{
			name: "a non-http scheme is refused",
			config: Config{
				Provider: ProviderOllama, BaseURL: "ftp://example.com", Model: "m",
			},
			wantErr: true,
		},
		{
			name: "an openai-compatible endpoint with no model is refused",
			config: Config{
				Provider: ProviderOpenAICompatible, BaseURL: "https://api.example.com",
			},
			wantErr: true,
		},
		{
			name: "a hosted endpoint with no key is allowed, because many need none",
			config: Config{
				Enabled:  true,
				Provider: ProviderOpenAICompatible,
				BaseURL:  "http://127.0.0.1:8080",
				Model:    "local",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validate(test.config)
			if test.wantErr && !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("validate() = %v, want an ErrInvalidConfig", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validate() = %v, want nil", err)
			}
		})
	}
}

func TestEndpointURLsForgiveWhatOperatorsPaste(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		openAI  string
		ollamaU string
	}{
		{
			name:    "a bare host gets the full path",
			base:    "http://127.0.0.1:11434",
			openAI:  "http://127.0.0.1:11434/v1/chat/completions",
			ollamaU: "http://127.0.0.1:11434/api/chat",
		},
		{
			name:    "a base that already ends in /v1 is not given a second one",
			base:    "https://api.example.com/v1",
			openAI:  "https://api.example.com/v1/chat/completions",
			ollamaU: "https://api.example.com/v1/api/chat",
		},
		{
			name:    "a base that already ends in /api is not given a second one",
			base:    "http://127.0.0.1:11434/api/",
			openAI:  "http://127.0.0.1:11434/api/v1/chat/completions",
			ollamaU: "http://127.0.0.1:11434/api/chat",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ChatCompletionsURL(test.base); got != test.openAI {
				t.Fatalf("ChatCompletionsURL(%q) = %q, want %q", test.base, got, test.openAI)
			}
			if got := OllamaChatURL(test.base); got != test.ollamaU {
				t.Fatalf("OllamaChatURL(%q) = %q, want %q", test.base, got, test.ollamaU)
			}
		})
	}
}

func TestClientConfigHidesTheEndpointAndReportsPerJobAvailability(t *testing.T) {
	config := Config{
		Enabled:  true,
		Provider: ProviderOpenAICompatible,
		BaseURL:  "https://api.example.com",
		Model:    "gpt-4o-mini",
		APIKey:   "sk-abcdefgh",
		Jobs:     JobSettings{JobTranslate: SourceOff},
	}.Normalize()

	client := config.Client()
	encoded, err := json.Marshal(client)
	if err != nil {
		t.Fatalf("marshal client config: %v", err)
	}
	for _, forbidden := range []string{"api.example.com", "gpt-4o-mini", "sk-abcdefgh"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("the member view leaked %q: %s", forbidden, encoded)
		}
	}
	if !client.Enabled || client.Jobs[string(JobTranslate)] {
		t.Fatalf("client jobs = %+v, want translate off and the service on", client.Jobs)
	}
	if !client.Jobs[string(JobChatTitle)] {
		t.Fatal("a job left on should be reported available")
	}

	// Switching the whole service off must take every job with it, so no
	// button is ever offered for something that would immediately fall back.
	config.Enabled = false
	for job, available := range config.Client().Jobs {
		if available {
			t.Fatalf("%s reported available while the service is off", job)
		}
	}
}
