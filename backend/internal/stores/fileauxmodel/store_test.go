package fileauxmodel

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
)

func TestLoadOnAFreshServerAnswersWithTheDefaults(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	cfg, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.Enabled {
		t.Fatal("a server with no document must start with the auxiliary model off")
	}
	if cfg.BaseURL != serviceauxmodel.DefaultOllamaBaseURL || cfg.Model != serviceauxmodel.DefaultModel {
		t.Fatalf("defaults = %+v, want the local Ollama the docs describe", cfg)
	}
	for _, job := range serviceauxmodel.Jobs() {
		if !cfg.Jobs.Enabled(job) {
			t.Fatalf("%s is off by default; every job should be on once the service is", job)
		}
	}
}

func TestSaveRoundTripsIncludingTheKeyAndTheToggles(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	saved := serviceauxmodel.Config{
		Enabled:        true,
		Provider:       serviceauxmodel.ProviderOpenAICompatible,
		BaseURL:        "https://api.example.com/v1",
		Model:          "gpt-4o-mini",
		APIKey:         "sk-round-trip-1234",
		TimeoutSeconds: 15,
		MaxTokens:      400,
		Jobs: serviceauxmodel.JobSettings{
			serviceauxmodel.JobCommitMessage: serviceauxmodel.SourceOff,
		},
		UpdatedAt: 1723987200000,
	}
	if err := store.Save(context.Background(), saved); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	// A second store over the same directory is the "restart the process"
	// case, which is the one that matters for a settings document.
	reopened, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	loaded, err := reopened.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if loaded.APIKey != "sk-round-trip-1234" {
		t.Fatalf("APIKey = %q, want the stored credential back", loaded.APIKey)
	}
	if loaded.BaseURL != "https://api.example.com/v1" || loaded.Model != "gpt-4o-mini" {
		t.Fatalf("endpoint = %+v", loaded)
	}
	if loaded.TimeoutSeconds != 15 || loaded.MaxTokens != 400 {
		t.Fatalf("limits = %+v", loaded)
	}
	if loaded.Jobs.Enabled(serviceauxmodel.JobCommitMessage) {
		t.Fatal("a disabled job came back enabled")
	}
	if !loaded.Jobs.Enabled(serviceauxmodel.JobChatTitle) {
		t.Fatal("a job that was never touched came back disabled")
	}
	// The masked view is what the API returns; the file is what holds the key.
	if masked := loaded.Public().APIKeyMasked; masked != "••••1234" {
		t.Fatalf("masked key = %q", masked)
	}
}

func TestTheDocumentIsPrivateBecauseItCanHoldAKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not meaningful on Windows")
	}
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if err := store.Save(context.Background(), serviceauxmodel.DefaultConfig()); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("aux-model.json mode = %o, want 600", mode)
	}
}

func TestAnUnreadableDocumentIsReportedRatherThanGuessedAt(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := store.Load(context.Background()); err == nil {
		t.Fatal("Load() = nil, want the parse failure reported so the caller can log it")
	}
}

func TestAnEmptyDocumentDegradesToTheDefaults(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.Model != serviceauxmodel.DefaultModel {
		t.Fatalf("config = %+v, want the defaults", cfg)
	}
}
