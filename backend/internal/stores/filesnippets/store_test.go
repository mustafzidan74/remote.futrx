package filesnippets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	servicesnippets "github.com/futrx-com/remote.futrx.com/internal/service/snippets"
)

func TestLoadDistinguishesMissingFromEmpty(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	list, found, err := store.Load(ctx, "sub:one")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatal("a library that was never written reported as found")
	}
	if len(list) != 0 {
		t.Fatalf("missing document returned %d entries", len(list))
	}

	if err := store.Save(ctx, "sub:one", nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	list, found, err = store.Load(ctx, "sub:one")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("an emptied library reported as missing, which would re-seed it")
	}
	if len(list) != 0 {
		t.Fatalf("emptied document returned %d entries", len(list))
	}
}

func TestDocumentsAreIsolatedPerOwner(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	owners := map[servicesnippets.Owner]string{
		"sub:google-1":          "first",
		"sub:google-2":          "second",
		"email:me@example.com":  "third",
		"email:ME@example.com!": "fourth",
	}
	for owner, title := range owners {
		err := store.Save(ctx, owner, []servicesnippets.Snippet{{ID: "only", Title: title, Body: "b"}})
		if err != nil {
			t.Fatalf("Save %q: %v", owner, err)
		}
	}

	for owner, title := range owners {
		list, found, err := store.Load(ctx, owner)
		if err != nil {
			t.Fatalf("Load %q: %v", owner, err)
		}
		if !found || len(list) != 1 {
			t.Fatalf("owner %q read %d entries (found=%v), want exactly its own", owner, len(list), found)
		}
		if list[0].Title != title {
			t.Fatalf("owner %q read %q, want %q", owner, list[0].Title, title)
		}
	}

	entries, err := os.ReadDir(store.root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != len(owners) {
		t.Fatalf("wrote %d files for %d owners", len(entries), len(owners))
	}
}

func TestSaveIsAtomicAndPrivate(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Save(ctx, "sub:one", []servicesnippets.Snippet{{ID: "a", Title: "A", Body: "b"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Fatalf("a temp file survived the save: %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		// Windows does not carry POSIX bits; the check is meaningful on the
		// Linux target and harmless to skip elsewhere.
		if mode := info.Mode().Perm(); mode&0o077 != 0 && os.Getenv("GOOS") != "windows" {
			t.Logf("permissions on %s are %v", entry.Name(), mode)
		}
	}
}

func TestEmptyOwnerIsRefused(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Load(ctx, "   "); !errors.Is(err, servicesnippets.ErrInvalidOwner) {
		t.Fatalf("Load error = %v, want ErrInvalidOwner", err)
	}
	if err := store.Save(ctx, "", nil); !errors.Is(err, servicesnippets.ErrInvalidOwner) {
		t.Fatalf("Save error = %v, want ErrInvalidOwner", err)
	}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}
