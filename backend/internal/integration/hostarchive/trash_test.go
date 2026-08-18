package hostarchive

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

func TestTrashUntrashRoundTrip(t *testing.T) {
	root := t.TempDir()
	storage := NewTrashStorage(filepath.Join(root, "trash"))
	projectDir := filepath.Join(root, "projects", "demo")
	writeTree(t, projectDir, map[string]string{
		"workspace/index.php":    "<?php",
		"agent-home/claude/auth": "token",
	})

	trashed, err := storage.Trash(context.Background(), serviceproject.ID("abcd12"), projectDir)
	if err != nil {
		t.Fatalf("Trash() error = %v", err)
	}
	if _, err := os.Stat(projectDir); !os.IsNotExist(err) {
		t.Fatal("Trash() left the live project directory in place")
	}
	if got := readFile(t, filepath.Join(trashed, "workspace", "index.php")); got != "<?php" {
		t.Fatalf("trashed workspace = %q", got)
	}

	if err := storage.Untrash(context.Background(), serviceproject.ID("abcd12"), projectDir); err != nil {
		t.Fatalf("Untrash() error = %v", err)
	}
	if got := readFile(t, filepath.Join(projectDir, "agent-home", "claude", "auth")); got != "token" {
		t.Fatalf("restored agent home = %q", got)
	}
	if _, err := os.Stat(trashed); !os.IsNotExist(err) {
		t.Fatal("Untrash() left the trash entry behind")
	}
}

func TestTrashStorageEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, storage *TrashStorage, root string)
	}{
		{
			name: "a project whose files were never created still gets an entry",
			run: func(t *testing.T, storage *TrashStorage, root string) {
				path, err := storage.Trash(context.Background(), "abcd12", filepath.Join(root, "projects", "ghost"))
				if err != nil {
					t.Fatalf("Trash() error = %v", err)
				}
				if info, err := os.Stat(path); err != nil || !info.IsDir() {
					t.Fatalf("Trash() did not create the entry: %v", err)
				}
			},
		},
		{
			name: "a leftover entry from a failed purge is set aside, never overwritten",
			run: func(t *testing.T, storage *TrashStorage, root string) {
				writeTree(t, filepath.Join(storage.root, "abcd12"), map[string]string{"workspace/old": "previous"})
				projectDir := filepath.Join(root, "projects", "demo")
				writeTree(t, projectDir, map[string]string{"workspace/new": "current"})

				if _, err := storage.Trash(context.Background(), "abcd12", projectDir); err != nil {
					t.Fatalf("Trash() error = %v", err)
				}
				if got := readFile(t, filepath.Join(storage.root, "abcd12", "workspace", "new")); got != "current" {
					t.Fatalf("trashed workspace = %q", got)
				}
				entries, _ := os.ReadDir(storage.root)
				if len(entries) != 2 {
					t.Fatalf("trash root entries = %d, want the new entry plus the stale one", len(entries))
				}
			},
		},
		{
			name: "restoring onto an existing directory is refused",
			run: func(t *testing.T, storage *TrashStorage, root string) {
				writeTree(t, filepath.Join(storage.root, "abcd12"), map[string]string{"workspace/a": "a"})
				projectDir := filepath.Join(root, "projects", "demo")
				writeTree(t, projectDir, map[string]string{"workspace/b": "b"})

				if err := storage.Untrash(context.Background(), "abcd12", projectDir); err == nil {
					t.Fatal("Untrash() overwrote a live project directory")
				}
			},
		},
		{
			name: "restoring an entry that is already gone is a no-op",
			run: func(t *testing.T, storage *TrashStorage, root string) {
				if err := storage.Untrash(context.Background(), "abcd12", filepath.Join(root, "projects", "demo")); err != nil {
					t.Fatalf("Untrash() error = %v", err)
				}
			},
		},
		{
			name: "purge removes the entry and is idempotent",
			run: func(t *testing.T, storage *TrashStorage, root string) {
				writeTree(t, filepath.Join(storage.root, "abcd12"), map[string]string{"workspace/a": "a"})
				for range 2 {
					if err := storage.PurgeTrash(context.Background(), "abcd12"); err != nil {
						t.Fatalf("PurgeTrash() error = %v", err)
					}
				}
				if _, err := os.Stat(filepath.Join(storage.root, "abcd12")); !os.IsNotExist(err) {
					t.Fatal("PurgeTrash() left the entry behind")
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.run(t, NewTrashStorage(filepath.Join(root, "trash")), root)
		})
	}
}

func TestCopyTreePreservesSymlinks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src")
	writeTree(t, source, map[string]string{"skills/build/SKILL.md": "# build"})
	link := filepath.Join(source, "link")
	if err := os.Symlink(filepath.Join(source, "skills", "build"), link); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}

	target := filepath.Join(root, "dst")
	if err := copyTree(source, target); err != nil {
		t.Fatalf("copyTree() error = %v", err)
	}
	info, err := os.Lstat(filepath.Join(target, "link"))
	if err != nil {
		t.Fatalf("copied link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("copyTree() followed a symlink instead of recreating it")
	}
}
