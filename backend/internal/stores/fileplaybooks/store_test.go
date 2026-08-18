package fileplaybooks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	serviceplaybooks "github.com/futrx-com/remote.futrx.com/internal/service/playbooks"
)

func TestLoadReportsAbsentBeforeAnythingIsSaved(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	list, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatal("Load reported a document before one was written")
	}
	if len(list) != 0 {
		t.Fatalf("Load returned %d entries, want none", len(list))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved := []serviceplaybooks.Playbook{
		{
			ID:     "security-review",
			Title:  "🔒 Security review",
			Icon:   "🔒",
			Hint:   "Read-only audit.",
			Prompt: "Review /workspace.",
			Mode:   "review",
			Skills: []serviceplaybooks.SkillRef{
				{Name: "wp-guard", Command: "wp-guard", Source: "global"},
			},
			Order: 0,
		},
	}

	if err := store.Save(context.Background(), saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load did not report the saved document")
	}
	if len(loaded) != 1 {
		t.Fatalf("Load returned %d entries, want 1", len(loaded))
	}
	got := loaded[0]
	if got.ID != "security-review" || got.Title != "🔒 Security review" || got.Mode != "review" {
		t.Fatalf("round trip lost fields: %#v", got)
	}
	if len(got.Skills) != 1 || got.Skills[0].Command != "wp-guard" {
		t.Fatalf("round trip lost skills: %#v", got.Skills)
	}
}

func TestSaveKeepsAnEmptyLibraryDistinguishableFromAbsent(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Save(context.Background(), nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	list, found, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("an emptied library must still read back as present, or it gets reseeded")
	}
	if len(list) != 0 {
		t.Fatalf("Load returned %d entries, want none", len(list))
	}
}

func TestSaveWritesOwnerOnlyDocument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows")
	}
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), []serviceplaybooks.Playbook{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}

func TestLoadRejectsCorruptDocument(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(context.Background()); err == nil {
		t.Fatal("Load accepted a corrupt document")
	}
}
