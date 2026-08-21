package providerpool

import (
	"context"
	"errors"
	"testing"
)

func TestSaveCreatesThenUpdatesAndKeepsAKeyTheClientNeverSaw(t *testing.T) {
	clock := newClock()
	service, store, _ := newTestService(t, nil, autoSwitch(), &scriptedCompleter{}, clock)
	ctx := context.Background()

	view, err := service.Save(ctx, ProviderInput{
		ID:      "groq",
		Label:   "Groq",
		Kind:    "openai",
		BaseURL: "https://api.groq.com/openai/v1",
		APIKey:  "gsk-original-1111",
		Models:  []Model{{ID: "llama-3.3-70b-versatile"}},
		Enabled: true,
	}, "admin@example.com")
	if err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if len(view.Providers) != 1 || view.Providers[0].APIKeyMasked != "••••1111" {
		t.Fatalf("view = %+v, want one provider with a masked key", view.Providers)
	}

	// The panel re-submits the form with an empty key, because a mask is all
	// it ever had. That must not wipe the credential.
	if _, err := service.Save(ctx, ProviderInput{
		ID:      "groq",
		Label:   "Groq (fast)",
		BaseURL: "https://api.groq.com/openai/v1",
		Models:  []Model{{ID: "llama-3.3-70b-versatile"}},
		Enabled: true,
	}, "admin@example.com"); err != nil {
		t.Fatalf("Save() update = %v", err)
	}
	stored, _ := store.registry.Find("groq")
	if stored.APIKey != "gsk-original-1111" {
		t.Fatalf("apiKey = %q, want the stored credential kept through an update that could not restate it", stored.APIKey)
	}
	if stored.Label != "Groq (fast)" {
		t.Fatalf("label = %q, want the update applied", stored.Label)
	}

	// Removing a key is explicit.
	if _, err := service.Save(ctx, ProviderInput{
		ID:          "groq",
		BaseURL:     "https://api.groq.com/openai/v1",
		Models:      []Model{{ID: "llama-3.3-70b-versatile"}},
		ClearAPIKey: true,
		Enabled:     false,
	}, "admin@example.com"); err != nil {
		t.Fatalf("Save() clear = %v", err)
	}
	stored, _ = store.registry.Find("groq")
	if stored.APIKey != "" {
		t.Fatal("clearApiKey did not remove the credential")
	}
}

func TestEditingTheLimitsRetiresTheSeedWarning(t *testing.T) {
	clock := newClock()
	seeded := provider("groq", 10, func(p *Provider) {
		p.LimitsNote = SeedLimitsNote
		p.Seed = true
		p.Limits = Limits{RPD: intp(1000)}
	})
	service, store, _ := newTestService(t, []Provider{seeded}, autoSwitch(), &scriptedCompleter{}, clock)
	ctx := context.Background()

	// Saving without touching the numbers keeps the warning: nobody checked.
	if _, err := service.Save(ctx, ProviderInput{
		ID:      "groq",
		BaseURL: seeded.BaseURL,
		APIKey:  "gsk-1234",
		Models:  seeded.Models,
		Limits:  Limits{RPD: intp(1000)},
		Enabled: true,
	}, "admin@example.com"); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	stored, _ := store.registry.Find("groq")
	if stored.LimitsNote != SeedLimitsNote {
		t.Fatal("the verify warning went away without anybody verifying anything")
	}

	// Changing a number means the operator went and looked.
	if _, err := service.Save(ctx, ProviderInput{
		ID:      "groq",
		BaseURL: seeded.BaseURL,
		APIKey:  "gsk-1234",
		Models:  seeded.Models,
		Limits:  Limits{RPD: intp(500)},
		Enabled: true,
	}, "admin@example.com"); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	stored, _ = store.registry.Find("groq")
	if stored.LimitsNote != "" {
		t.Fatalf("limitsNote = %q, want it cleared once the operator edited the numbers", stored.LimitsNote)
	}
}

func TestDeleteForgetsTheCountersAndDropsAPinToTheDeletedProvider(t *testing.T) {
	clock := newClock()
	first := provider("first", 10)
	second := provider("second", 20)
	service, store, _ := newTestService(t, []Provider{first, second},
		Settings{AutoSwitch: false, PreferredProviderID: "first"}, &scriptedCompleter{}, clock)
	ctx := context.Background()

	if _, err := service.Complete(ctx, Request{Prompt: "spend something"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if service.tracker.snapshot("first").dayRequests != 1 {
		t.Fatal("the request was not counted")
	}

	if _, err := service.Delete(ctx, "first", "admin@example.com"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if _, found := store.registry.Find("first"); found {
		t.Fatal("the provider was not removed")
	}
	if store.registry.Settings.PreferredProviderID != "" {
		t.Fatalf("preferred = %q, want the pin dropped with the provider it named",
			store.registry.Settings.PreferredProviderID)
	}
	if service.tracker.snapshot("first").dayRequests != 0 {
		t.Fatal("a re-created id would inherit a stranger's consumption")
	}

	if _, err := service.Delete(ctx, "nobody", "admin@example.com"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("Delete() on an unknown id = %v, want ErrUnknownProvider", err)
	}
}

func TestReorderRewritesPriorityAndKeepsUnnamedProvidersAtTheBack(t *testing.T) {
	clock := newClock()
	service, store, _ := newTestService(t, []Provider{
		provider("alpha", 10), provider("bravo", 20), provider("charlie", 30),
	}, autoSwitch(), &scriptedCompleter{}, clock)

	view, err := service.Reorder(context.Background(),
		[]string{"charlie", "alpha", "charlie" /* a duplicate is ignored */}, "admin@example.com")
	if err != nil {
		t.Fatalf("Reorder() = %v", err)
	}
	got := make([]string, 0, len(view.Providers))
	for _, provider := range view.Providers {
		got = append(got, provider.ID)
	}
	// charlie and alpha were named; bravo was not and lands after them rather
	// than being dropped somewhere arbitrary.
	want := []string{"charlie", "alpha", "bravo"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if store.registry.Providers[0].ID != "charlie" {
		t.Fatal("the new order was not persisted")
	}
}

func TestSaveSettingsRefusesAPolicyThatWouldDeclineEverything(t *testing.T) {
	clock := newClock()
	service, _, _ := newTestService(t, []Provider{provider("groq", 10)}, autoSwitch(), &scriptedCompleter{}, clock)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   SettingsInput
		wantErr bool
	}{
		{name: "auto-switch on needs no preference", input: SettingsInput{AutoSwitch: true}},
		{
			name:  "auto-switch off with a real preference is fine",
			input: SettingsInput{AutoSwitch: false, PreferredProviderID: "groq"},
		},
		{
			name:    "auto-switch off with nothing chosen is refused",
			input:   SettingsInput{AutoSwitch: false},
			wantErr: true,
		},
		{
			name:    "a preference naming a provider that does not exist is refused",
			input:   SettingsInput{AutoSwitch: true, PreferredProviderID: "nobody"},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.SaveSettings(ctx, test.input, "admin@example.com")
			if test.wantErr {
				if !errors.Is(err, ErrInvalidProvider) {
					t.Fatalf("SaveSettings() = %v, want ErrInvalidProvider", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SaveSettings() = %v", err)
			}
		})
	}
}

func TestAFailedWriteLeavesThePoolRunningOnWhatIsOnDisk(t *testing.T) {
	clock := newClock()
	original := provider("groq", 10)
	store := &memoryStore{registry: Registry{Providers: []Provider{original}, Settings: autoSwitch(), Seeded: true}}
	service := New(context.Background(), store,
		WithCompleter(&scriptedCompleter{}), WithUsageLog(&memoryLog{}), WithClock(clock.Now))

	store.saveErr = errors.New("disk full")
	if _, err := service.Save(context.Background(), ProviderInput{
		ID:      "groq",
		Label:   "Renamed",
		BaseURL: original.BaseURL,
		APIKey:  "gsk-1234",
		Models:  original.Models,
		Enabled: true,
	}, "admin@example.com"); err == nil {
		t.Fatal("Save() = nil, want the write failure reported")
	}
	live, _ := service.Registry().Find("groq")
	if live.Label != "groq" {
		t.Fatalf("label = %q, want the in-memory registry left on what is actually stored", live.Label)
	}
}

func TestStatusReportsWhyAProviderCannotBeUsed(t *testing.T) {
	clock := newClock()
	tests := []struct {
		name string
		make func() Provider
		want Status
	}{
		{
			name: "ready",
			make: func() Provider { return provider("ready", 10) },
			want: StatusReady,
		},
		{
			name: "disabled",
			make: func() Provider {
				return provider("disabled", 10, func(p *Provider) { p.Enabled = false })
			},
			want: StatusDisabled,
		},
		{
			name: "no key",
			make: func() Provider {
				// Enabled with no key cannot be saved through the API, but a
				// hand-edited document can look like this and the panel has
				// to say something useful about it.
				return provider("nokey", 10, func(p *Provider) { p.APIKey = "" })
			},
			want: StatusNoKey,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _, _ := newTestService(t, []Provider{test.make()}, autoSwitch(), &scriptedCompleter{}, clock)
			if got := service.View().Providers[0].Status; got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestANewServerInstallsTheSeedsAndSavesThemOnce(t *testing.T) {
	clock := newClock()
	store := &memoryStore{}
	service := New(context.Background(), store,
		WithCompleter(&scriptedCompleter{}), WithUsageLog(&memoryLog{}), WithClock(clock.Now))

	view := service.View()
	if len(view.Providers) != len(Seeds()) {
		t.Fatalf("a fresh server has %d providers, want every shipped template", len(view.Providers))
	}
	if view.Available {
		t.Fatal("a fresh server reported a usable pool; every seed ships off and keyless")
	}
	if store.saves != 1 {
		t.Fatalf("saved %d times on boot, want exactly one write of the installed seeds", store.saves)
	}

	// A second boot over the same document must not write again.
	New(context.Background(), store,
		WithCompleter(&scriptedCompleter{}), WithUsageLog(&memoryLog{}), WithClock(clock.Now))
	if store.saves != 1 {
		t.Fatalf("saved %d times across two boots, want the seeding to happen once", store.saves)
	}
}

func TestAnUnreadableRegistryLeavesAnEmptyPoolRatherThanKillingTheBoot(t *testing.T) {
	clock := newClock()
	store := &memoryStore{loadErr: errors.New("permission denied")}
	service := New(context.Background(), store,
		WithCompleter(&scriptedCompleter{}), WithUsageLog(&memoryLog{}), WithClock(clock.Now))

	if service.Available() {
		t.Fatal("a pool that could not read its own document reported itself usable")
	}
	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Complete() = %v, want the caller to fall back", err)
	}
}
