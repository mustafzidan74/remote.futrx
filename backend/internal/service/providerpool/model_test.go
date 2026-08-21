package providerpool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidateRejectsWhatWouldFailAtTheProvider(t *testing.T) {
	valid := func(mutate func(*Provider)) Provider {
		p := Provider{
			ID:      "groq",
			Label:   "Groq",
			Kind:    KindOpenAI,
			BaseURL: "https://api.groq.com/openai/v1",
			APIKey:  "gsk-abcd1234",
			Enabled: true,
			Models:  []Model{{ID: "llama-3.3-70b-versatile"}},
		}
		if mutate != nil {
			mutate(&p)
		}
		return p.Normalize()
	}

	tests := []struct {
		name    string
		mutate  func(*Provider)
		wantErr bool
	}{
		{name: "a complete entry is accepted"},
		{name: "an empty id is refused", mutate: func(p *Provider) { p.ID = "" }, wantErr: true},
		{name: "an id with spaces is refused", mutate: func(p *Provider) { p.ID = "my provider" }, wantErr: true},
		{name: "a single-character id is refused", mutate: func(p *Provider) { p.ID = "g" }, wantErr: true},
		{
			name:    "an id that would shadow the reorder route is refused",
			mutate:  func(p *Provider) { p.ID = "reorder" },
			wantErr: true,
		},
		{name: "no base URL is refused", mutate: func(p *Provider) { p.BaseURL = "" }, wantErr: true},
		{
			name:    "a relative base URL is refused",
			mutate:  func(p *Provider) { p.BaseURL = "api.groq.com/v1" },
			wantErr: true,
		},
		{
			name:    "a non-http scheme is refused",
			mutate:  func(p *Provider) { p.BaseURL = "ftp://api.groq.com" },
			wantErr: true,
		},
		{name: "no models is refused", mutate: func(p *Provider) { p.Models = nil }, wantErr: true},
		{
			name:    "enabling a provider with no credential is refused",
			mutate:  func(p *Provider) { p.APIKey = "" },
			wantErr: true,
		},
		{
			name:   "a disabled provider with no credential is fine — that is what a seed is",
			mutate: func(p *Provider) { p.APIKey = ""; p.Enabled = false },
		},
		{
			name:   "a vault reference counts as a credential",
			mutate: func(p *Provider) { p.APIKey = ""; p.APIKeyRef = "GROQ_API_KEY" },
		},
		{
			name:    "a vault reference that is not a shell-style name is refused",
			mutate:  func(p *Provider) { p.APIKey = ""; p.APIKeyRef = "groq api key" },
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validate(valid(test.mutate))
			if test.wantErr {
				if !errors.Is(err, ErrInvalidProvider) {
					t.Fatalf("validate() = %v, want ErrInvalidProvider", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate() = %v, want nil", err)
			}
		})
	}
}

func TestNormalizeCleansUpWhatAFormLeftBehind(t *testing.T) {
	raw := Provider{
		ID:      "  GROQ  ",
		BaseURL: "https://api.groq.com/openai/v1/",
		Kind:    "OpenAI",
		APIKey:  "  gsk-1234  ",
		Models: []Model{
			{ID: " llama-3.3-70b-versatile ", GoodFor: []Capability{"TEXT", "code", "text", "nonsense"}},
			{ID: "llama-3.3-70b-versatile"}, // a duplicate id
			{ID: "   "},                     // an empty row the form left behind
		},
		Priority: -5,
	}
	got := raw.Normalize()

	if got.ID != "groq" {
		t.Fatalf("id = %q, want it lower-cased and trimmed", got.ID)
	}
	if got.Label != "groq" {
		t.Fatalf("label = %q, want the id when no label was typed", got.Label)
	}
	if got.BaseURL != "https://api.groq.com/openai/v1" {
		t.Fatalf("baseUrl = %q, want the trailing slash gone", got.BaseURL)
	}
	if got.Kind != KindOpenAI {
		t.Fatalf("kind = %q", got.Kind)
	}
	if got.APIKey != "gsk-1234" {
		t.Fatalf("apiKey = %q, want it trimmed", got.APIKey)
	}
	if len(got.Models) != 1 {
		t.Fatalf("models = %+v, want the duplicate and the blank row dropped", got.Models)
	}
	if len(got.Models[0].GoodFor) != 2 {
		t.Fatalf("good_for = %+v, want the duplicate and the unknown tag dropped", got.Models[0].GoodFor)
	}
	if got.Models[0].Label != got.Models[0].ID {
		t.Fatalf("model label = %q, want the id when none was typed", got.Models[0].Label)
	}
	if got.Priority != 0 {
		t.Fatalf("priority = %d, want a negative priority clamped", got.Priority)
	}
}

func TestALimitOfZeroMeansNotDocumentedRatherThanNoAllowance(t *testing.T) {
	zero := 0
	negative := -10
	real := 250
	got := Limits{RPM: &zero, RPD: &real, TPM: &negative}.Normalize()

	if got.RPM != nil {
		t.Fatal("a documented cap of zero would mean \"you may make no requests\"; it must read as unknown")
	}
	if got.TPM != nil {
		t.Fatal("a negative cap must read as unknown")
	}
	if got.RPD == nil || *got.RPD != 250 {
		t.Fatalf("rpd = %v, want the real number kept", got.RPD)
	}
}

func TestMaskSecretShowsEnoughToRecognizeAndNotEnoughToReuse(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{name: "no secret masks to nothing", secret: "", want: ""},
		{name: "whitespace is not a secret", secret: "   ", want: ""},
		{name: "a short secret shows no tail at all", secret: "abcd", want: "••••"},
		{name: "a normal key shows its last four", secret: "gsk-abcdefgh9999", want: "••••9999"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := MaskSecret(test.secret); got != test.want {
				t.Fatalf("MaskSecret(%q) = %q, want %q", test.secret, got, test.want)
			}
		})
	}
}

func TestTheAdminViewNeverCarriesACredential(t *testing.T) {
	clock := newClock()
	inline := provider("inline", 10, func(p *Provider) { p.APIKey = "gsk-super-secret-9999" })
	service, _, _ := newTestService(t, []Provider{inline}, autoSwitch(), &scriptedCompleter{}, clock)

	encoded, err := json.Marshal(service.View())
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	if strings.Contains(string(encoded), "gsk-super-secret-9999") {
		t.Fatalf("the admin view returned the stored key: %s", encoded)
	}
	if !strings.Contains(string(encoded), "9999") {
		t.Fatal("the admin view dropped the mask an operator recognizes the key by")
	}
}

func TestTheQuotaCardCarriesNoEndpointAndNoKeyState(t *testing.T) {
	clock := newClock()
	only := provider("only", 10, func(p *Provider) {
		p.APIKey = "gsk-secret-4321"
		p.BaseURL = "https://internal.example.com/v1"
		p.Limits = Limits{RPD: intp(100)}
	})
	service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), &scriptedCompleter{}, clock)
	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	encoded, err := json.Marshal(service.Quota())
	if err != nil {
		t.Fatalf("marshal quota: %v", err)
	}
	for _, forbidden := range []string{"gsk-secret-4321", "4321", "internal.example.com"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("the member view leaked %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(string(encoded), `"label":"only"`) {
		t.Fatalf("the member view dropped the label it exists to show: %s", encoded)
	}
}

/* ------------------------------------------------------------------ *
 * The shipped seeds
 * ------------------------------------------------------------------ */

func TestEverySeedIsValidDisabledAndKeyless(t *testing.T) {
	seeds := Seeds()
	if len(seeds) < 8 {
		t.Fatalf("shipped %d seeds, want one per documented free tier", len(seeds))
	}
	ids := map[string]bool{}
	for _, seed := range seeds {
		t.Run(seed.ID, func(t *testing.T) {
			if ids[seed.ID] {
				t.Fatalf("duplicate seed id %q", seed.ID)
			}
			ids[seed.ID] = true
			if seed.Enabled {
				t.Fatal("a seed must ship switched off — installing templates cannot start spending anything")
			}
			if seed.HasKey() {
				t.Fatal("a seed must ship with no credential")
			}
			if seed.LimitsNote != SeedLimitsNote {
				t.Fatalf("limitsNote = %q, want the \"verify\" warning on every shipped template", seed.LimitsNote)
			}
			if err := validate(seed.Normalize()); err != nil {
				t.Fatalf("the shipped template does not pass its own validation: %v", err)
			}
			if len(seed.Models) == 0 {
				t.Fatal("a seed with no model is a form the operator has to fill in anyway")
			}
		})
	}

	// The eight the feature promises, by the base URLs they are documented at.
	wantBaseURLs := map[string]string{
		"gemini":        "https://generativelanguage.googleapis.com/v1beta/openai",
		"groq":          "https://api.groq.com/openai/v1",
		"cerebras":      "https://api.cerebras.ai/v1",
		"openrouter":    "https://openrouter.ai/api/v1",
		"zhipu-glm":     "https://open.bigmodel.cn/api/paas/v4",
		"mistral":       "https://api.mistral.ai/v1",
		"moonshot":      "https://api.moonshot.cn/v1",
		"github-models": "https://models.inference.ai.azure.com",
	}
	for id, wantURL := range wantBaseURLs {
		found := false
		for _, seed := range seeds {
			if seed.ID != id {
				continue
			}
			found = true
			if got := seed.Normalize().BaseURL; got != wantURL {
				t.Fatalf("%s base URL = %q, want %q", id, got, wantURL)
			}
		}
		if !found {
			t.Fatalf("no seed for %s", id)
		}
	}
}

func TestSeedingHappensOnceAndNeverResurrectsADeletedTemplate(t *testing.T) {
	fresh, changed := SeedInto(Registry{})
	if !changed || len(fresh.Providers) != len(Seeds()) {
		t.Fatalf("a fresh registry got %d providers, want every seed", len(fresh.Providers))
	}
	if !fresh.Seeded {
		t.Fatal("the registry was not marked as seeded")
	}

	// The operator deletes everything they do not want.
	pruned := Registry{Providers: fresh.Providers[:1], Seeded: fresh.Seeded}
	again, _ := SeedInto(pruned)
	if len(again.Providers) != 1 {
		t.Fatalf("re-seeding restored %d providers; a deleted template must stay deleted",
			len(again.Providers))
	}
}

func TestRegistryNormalizeOrdersByPriorityAndDropsDuplicates(t *testing.T) {
	registry := Registry{Providers: []Provider{
		provider("charlie", 30),
		provider("alpha", 10),
		provider("bravo", 20),
		provider("alpha", 99), // a duplicate id: the first one wins
		{ID: "   "},           // an entry with no usable id
	}}.Normalize()

	got := make([]string, 0, len(registry.Providers))
	for _, p := range registry.Providers {
		got = append(got, p.ID)
	}
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("providers = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("providers = %v, want %v", got, want)
		}
	}
}

func TestModelSuitsTreatsAnUntaggedModelAsUsableForAnything(t *testing.T) {
	untagged := Model{ID: "mystery"}
	for _, capability := range Capabilities() {
		if !untagged.Suits(capability) {
			t.Fatalf("an untagged model was refused for %q; an operator who pasted a model id and stopped should not find it unreachable", capability)
		}
	}
	tagged := Model{ID: "cheap", GoodFor: []Capability{CapabilityBulk}}
	if tagged.Suits(CapabilityCode) {
		t.Fatal("a bulk-only model was offered for code")
	}
	if !tagged.Suits(CapabilityBulk) {
		t.Fatal("a bulk model was refused for bulk")
	}
}

func TestNeedDerivesACapabilityFromItsJobName(t *testing.T) {
	tests := []struct {
		name string
		need Need
		want Capability
	}{
		{name: "the bulk lane wants a cheap model", need: Need{Job: "bulk"}, want: CapabilityBulk},
		{name: "a commit subject is code-shaped", need: Need{Job: "commitMessage"}, want: CapabilityCode},
		{name: "a chat title is ordinary prose", need: Need{Job: "chatTitle"}, want: CapabilityText},
		{name: "nothing named is prose", need: Need{}, want: CapabilityText},
		{
			name: "an explicit want beats whatever the job name implies",
			need: Need{Job: "bulk", Want: CapabilityCode},
			want: CapabilityCode,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.need.Capability(); got != test.want {
				t.Fatalf("Capability() = %q, want %q", got, test.want)
			}
		})
	}
}
