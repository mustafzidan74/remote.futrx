package filetranscribe

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
)

func TestLoadReturnsDefaultsBeforeAnythingIsSaved(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := servicetranscribe.DefaultConfig()
	if cfg.Enabled != want.Enabled || cfg.Model != want.Model || cfg.Provider != want.Provider {
		t.Fatalf("Load = %+v, want defaults %+v", cfg, want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	saved := servicetranscribe.Config{
		Enabled:         true,
		Provider:        "  OpenAI  ",
		APIKey:          "  sk-proj-secret  ",
		Model:           "  whisper-1  ",
		DefaultLanguage: "  ar-EG  ",
		UpdatedAt:       1700000000000,
	}
	if err := store.Save(context.Background(), saved); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := servicetranscribe.Config{
		Enabled:         true,
		Provider:        servicetranscribe.ProviderOpenAI,
		APIKey:          "sk-proj-secret",
		Model:           "whisper-1",
		DefaultLanguage: "ar-EG",
		UpdatedAt:       1700000000000,
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

// A document written before a field existed must still load: the operator
// should not have to re-enter settings after an upgrade.
func TestLoadFillsMissingFieldsFromDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte(`{"enabled":true,"apiKey":"sk-old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Provider != servicetranscribe.ProviderOpenAI {
		t.Fatalf("Provider = %q, want the default provider", cfg.Provider)
	}
	if cfg.Model != servicetranscribe.DefaultModel {
		t.Fatalf("Model = %q, want the default model", cfg.Model)
	}
	if cfg.APIKey != "sk-old" || !cfg.Enabled {
		t.Fatalf("Load lost the stored fields: %+v", cfg)
	}
}

func TestSaveWritesAPrivateFileAndLeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), servicetranscribe.Config{APIKey: "sk-secret"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	// Windows does not implement POSIX permission bits; the deployment target
	// is Linux, where the provider key must not be world readable.
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("%s mode = %o, want 600", fileName, mode)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("Save left a temp file behind: %s", entry.Name())
		}
	}
}

func TestLoadRejectsACorruptDocument(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load(context.Background()); err == nil {
		t.Fatal("Load must report a corrupt settings document rather than silently reset it")
	}
}
