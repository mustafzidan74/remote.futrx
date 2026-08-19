package filemcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
)

func TestRegistryRoundTripsAndStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()

	loaded, err := store.Load(ctx)
	if err != nil || len(loaded) != 0 {
		t.Fatalf("a fresh install should read an empty registry, got %#v / %v", loaded, err)
	}

	want := []servicemcp.Server{{
		Name: "fetch", Transport: servicemcp.TransportStdio, Command: "uvx",
		Scope: servicemcp.Scope{All: true},
	}}
	if err := store.Save(ctx, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got, err := reopened.Load(ctx)
	if err != nil || len(got) != 1 || got[0].Name != "fetch" {
		t.Fatalf("Load() = %#v / %v", got, err)
	}
}

func TestRegistryIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes are not meaningful on Windows")
	}
	dir := t.TempDir()
	store, _ := New(dir)
	if err := store.Save(context.Background(), nil); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o, want 600", mode)
	}
}

func TestProjectStoreRoundTripsAndStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewProjectStore(dir)
	if err != nil {
		t.Fatalf("NewProjectStore() error = %v", err)
	}
	ctx := context.Background()

	settings, err := store.Load(ctx, "p1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(settings.Disabled) != 0 || len(settings.Servers) != 0 {
		t.Fatalf("a project that never saved anything should read empty, got %#v", settings)
	}

	if err := store.Save(ctx, "p1", servicemcp.ProjectSettings{
		Disabled:       []string{"fetch"},
		MaterializedAt: 42,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load(ctx, "p1")
	if err != nil || len(got.Disabled) != 1 || got.MaterializedAt != 42 {
		t.Fatalf("Load() = %#v / %v", got, err)
	}
	// One project's document must not become another's.
	other, err := store.Load(ctx, "p2")
	if err != nil || len(other.Disabled) != 0 {
		t.Fatalf("p2 = %#v / %v", other, err)
	}
}

func TestProjectStoreRefusesAnIDThatCouldEscapeTheDirectory(t *testing.T) {
	store, err := NewProjectStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewProjectStore() error = %v", err)
	}
	for _, id := range []string{"", "../../etc/passwd", "a/b", "a\\b", "with space"} {
		t.Run(id, func(t *testing.T) {
			if _, err := store.Load(context.Background(), id); !errors.Is(err, ErrInvalidProjectID) {
				t.Fatalf("Load(%q) error = %v, want ErrInvalidProjectID", id, err)
			}
			if err := store.Save(context.Background(), id, servicemcp.ProjectSettings{}); !errors.Is(err, ErrInvalidProjectID) {
				t.Fatalf("Save(%q) error = %v, want ErrInvalidProjectID", id, err)
			}
		})
	}
}
