package transcribe

import (
	"errors"
	"strings"
	"testing"
)

func TestMaskSecretNeverLeaksAReusableKey(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{name: "empty stays empty", secret: "", want: ""},
		{name: "whitespace only stays empty", secret: "   ", want: ""},
		{name: "short key hides entirely", secret: "abcd", want: "••••"},
		{name: "long key keeps four characters", secret: "sk-proj-0123456789abcdef", want: "••••cdef"},
		{name: "trimmed before masking", secret: "  sk-live-wxyz  ", want: "••••wxyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskSecret(tt.secret)
			if got != tt.want {
				t.Fatalf("MaskSecret(%q) = %q, want %q", tt.secret, got, tt.want)
			}
			trimmed := strings.TrimSpace(tt.secret)
			if len(trimmed) > 4 && strings.Contains(got, trimmed) {
				t.Fatalf("MaskSecret(%q) echoed the whole secret", tt.secret)
			}
		})
	}
}

func TestPublicConfigNeverCarriesTheKey(t *testing.T) {
	cfg := Config{
		Enabled:         true,
		Provider:        ProviderOpenAI,
		APIKey:          "sk-proj-supersecret-tail",
		Model:           DefaultModel,
		DefaultLanguage: "ar-EG",
		UpdatedAt:       1700000000000,
	}

	public := cfg.Public()

	if public.APIKeyMasked != "••••tail" {
		t.Fatalf("APIKeyMasked = %q, want the masked tail", public.APIKeyMasked)
	}
	if !public.Configured || !public.Enabled {
		t.Fatalf("Public() = %+v, want configured and enabled", public)
	}
	if public.DefaultLanguage != "ar-EG" || public.Model != DefaultModel {
		t.Fatalf("Public() lost scalar fields: %+v", public)
	}
	if len(public.Models) == 0 {
		t.Fatal("Public() should advertise the selectable models")
	}
	if public.UpdatedAt != cfg.UpdatedAt {
		t.Fatalf("UpdatedAt = %d, want %d", public.UpdatedAt, cfg.UpdatedAt)
	}
}

func TestClientConfigHidesEverythingButTheLimits(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		wantEnabled bool
	}{
		{
			name:        "enabled with a key is offered",
			config:      Config{Enabled: true, APIKey: "sk-1234", DefaultLanguage: "ar-EG"},
			wantEnabled: true,
		},
		{
			name:        "enabled without a key is not offered",
			config:      Config{Enabled: true},
			wantEnabled: false,
		},
		{
			name:        "a stored key with the switch off is not offered",
			config:      Config{APIKey: "sk-1234"},
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.config.Client()
			if client.Enabled != tt.wantEnabled {
				t.Fatalf("Client().Enabled = %t, want %t", client.Enabled, tt.wantEnabled)
			}
			if client.MaxBytes != MaxAudioBytes || client.MaxSeconds != 300 {
				t.Fatalf("Client() limits = %d bytes / %d s", client.MaxBytes, client.MaxSeconds)
			}
		})
	}
}

// Write-only key semantics are what let the admin form show a mask: the form
// resubmits an empty string on every save that does not change the key.
func TestApplyKeepsTheStoredKeyUnlessTheCallerReplacesOrClearsIt(t *testing.T) {
	stored := Config{
		Enabled:  true,
		Provider: ProviderOpenAI,
		APIKey:   "sk-original",
		Model:    DefaultModel,
	}

	tests := []struct {
		name    string
		input   UpdateInput
		wantKey string
	}{
		{
			name:    "blank key keeps the stored one",
			input:   UpdateInput{Enabled: true, Model: DefaultModel},
			wantKey: "sk-original",
		},
		{
			name:    "whitespace key keeps the stored one",
			input:   UpdateInput{Enabled: true, APIKey: "   ", Model: DefaultModel},
			wantKey: "sk-original",
		},
		{
			name:    "a new key replaces it",
			input:   UpdateInput{Enabled: true, APIKey: "  sk-rotated  ", Model: DefaultModel},
			wantKey: "sk-rotated",
		},
		{
			name:    "the clear flag wins over a submitted key",
			input:   UpdateInput{Enabled: true, APIKey: "sk-ignored", ClearAPIKey: true},
			wantKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stored.Apply(tt.input)
			if got.APIKey != tt.wantKey {
				t.Fatalf("APIKey = %q, want %q", got.APIKey, tt.wantKey)
			}
			if stored.APIKey != "sk-original" {
				t.Fatal("Apply mutated the receiver")
			}
		})
	}
}

func TestApplyFillsDefaultsForFieldsTheFormLeftBlank(t *testing.T) {
	got := DefaultConfig().Apply(UpdateInput{Enabled: false})

	if got.Provider != ProviderOpenAI {
		t.Fatalf("Provider = %q, want %q", got.Provider, ProviderOpenAI)
	}
	if got.Model != DefaultModel {
		t.Fatalf("Model = %q, want %q", got.Model, DefaultModel)
	}
}

func TestValidateRejectsConfigurationsTheProviderWouldRefuse(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:   "off and empty is fine",
			config: DefaultConfig(),
		},
		{
			name:   "on with a key is fine",
			config: Config{Enabled: true, Provider: ProviderOpenAI, APIKey: "sk-1", Model: "whisper-1"},
		},
		{
			name:    "on without a key is refused",
			config:  Config{Enabled: true, Provider: ProviderOpenAI, Model: DefaultModel},
			wantErr: true,
		},
		{
			name:    "an unknown provider is refused",
			config:  Config{Provider: "deepgram", Model: DefaultModel},
			wantErr: true,
		},
		{
			name:    "an unknown model is refused",
			config:  Config{Provider: ProviderOpenAI, Model: "whisper-9"},
			wantErr: true,
		},
		{
			name:    "a nonsense default language is refused",
			config:  Config{Provider: ProviderOpenAI, Model: DefaultModel, DefaultLanguage: "klingon"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.config)
			if tt.wantErr && !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("validate(%+v) = %v, want ErrInvalidConfig", tt.config, err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validate(%+v) = %v, want nil", tt.config, err)
			}
		})
	}
}

func TestLanguageHintReducesBCP47ToTheProviderSubtag(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{tag: "ar-EG", want: "ar"},
		{tag: "ar-SA", want: "ar"},
		{tag: "en-GB", want: "en"},
		{tag: "AR", want: "ar"},
		{tag: "  en-US  ", want: "en"},
		{tag: "auto", want: ""},
		{tag: "", want: ""},
		{tag: "klingon", want: ""},
		{tag: "x-private", want: ""},
		{tag: "1a-ZZ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := LanguageHint(tt.tag); got != tt.want {
				t.Fatalf("LanguageHint(%q) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}
