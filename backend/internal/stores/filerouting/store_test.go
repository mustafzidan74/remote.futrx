package filerouting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	servicerouting "github.com/futrx-com/remote.futrx.com/internal/service/routing"
)

func TestLoadReportsAbsentRatherThanFailing(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	policy, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if found {
		t.Fatalf("found = true on an empty data dir, got %+v", policy)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	want := servicerouting.Policy{
		Version:        servicerouting.PolicyVersion,
		UpdatedAt:      1_700_000_000_000,
		UpdatedBy:      "admin@example.com",
		Enabled:        true,
		AutoHeuristics: true,
		Default:        servicerouting.ModelRef{Provider: "claude", Model: "sonnet"},
		Cheap:          servicerouting.ModelRef{Provider: "claude", Model: "haiku"},
		Expensive:      servicerouting.ModelRef{Provider: "claude", Model: "opus"},
		Rules: []servicerouting.Rule{
			{
				ID:      "chat-mode",
				When:    servicerouting.Condition{Kind: servicerouting.KindModeIs, Value: "chat"},
				Use:     servicerouting.ModelRef{Provider: "claude", Model: "haiku", ReasoningEffort: "low"},
				Note:    "Chat mode is cheap",
				Enabled: true,
			},
			{
				ID:   "long",
				When: servicerouting.Condition{Kind: servicerouting.KindPromptLongerThan, Value: "2000"},
				Use:  servicerouting.ModelRef{Provider: "codex", Model: "gpt-5.5"},
			},
		},
	}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, found, err := store.Load(context.Background())
	if err != nil || !found {
		t.Fatalf("Load() = %v, %v, %v", got, found, err)
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("round trip changed the document:\n got %s\nwant %s", gotJSON, wantJSON)
	}

	// The document names which model answers every unattended run, so it is
	// owner-only like every other policy file. Windows reports 0666 for every
	// file, so the assertion only means anything where modes exist.
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 && mode != 0o666 {
		t.Fatalf("mode = %o, want 600", mode)
	}
}

func TestSaveReplacesTheDocumentInPlace(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first := servicerouting.Policy{Default: servicerouting.ModelRef{Provider: "claude"}, Enabled: true}
	second := servicerouting.Policy{Default: servicerouting.ModelRef{Provider: "codex"}}
	if err := store.Save(context.Background(), first); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := store.Save(context.Background(), second); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	got, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Enabled || got.Default.Provider != "codex" {
		t.Fatalf("policy = %+v, want the second document", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("data dir holds %d files, want only %s", len(entries), FileName)
	}
}

func TestLoadReportsAParseFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil {
		t.Fatal("Load() error = nil, want a parse failure rather than a silent empty policy")
	}
}
