package hostarchive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
)

func TestPackArgs(t *testing.T) {
	tests := []struct {
		name        string
		zstd        bool
		sourceDir   string
		entries     []string
		stagingDir  string
		stagedFiles []string
		want        []string
	}{
		{
			name:        "both groups with zstd",
			zstd:        true,
			sourceDir:   "/src",
			entries:     []string{"workspace", "agent-home"},
			stagingDir:  "/stage",
			stagedFiles: []string{"meta.json", "db.sql"},
			want: []string{
				"-c", "--zstd", "-f", "out.tar.gz",
				"-C", "/src", "workspace", "agent-home",
				"-C", "/stage", "meta.json", "db.sql",
			},
		},
		{
			name:        "gzip fallback",
			zstd:        false,
			sourceDir:   "/src",
			entries:     []string{"workspace"},
			stagingDir:  "/stage",
			stagedFiles: []string{"meta.json"},
			want: []string{
				"-c", "-z", "-f", "out.tar.gz",
				"-C", "/src", "workspace",
				"-C", "/stage", "meta.json",
			},
		},
		{
			name:        "no source entries still archives the manifest",
			zstd:        false,
			sourceDir:   "/src",
			entries:     nil,
			stagingDir:  "/stage",
			stagedFiles: []string{"meta.json"},
			want:        []string{"-c", "-z", "-f", "out.tar.gz", "-C", "/stage", "meta.json"},
		},
		{
			name:       "no staged files",
			zstd:       false,
			sourceDir:  "/src",
			entries:    []string{"workspace"},
			stagingDir: "/stage",
			want:       []string{"-c", "-z", "-f", "out.tar.gz", "-C", "/src", "workspace"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := PackArgs("out.tar.gz", test.zstd, test.sourceDir, test.entries, test.stagingDir, test.stagedFiles)
			if !slices.Equal(got, test.want) {
				t.Fatalf("PackArgs() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPackAndRestoreRoundTrip(t *testing.T) {
	archiver := newTestArchiver(t)
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "demo")
	writeTree(t, projectDir, map[string]string{
		"workspace/index.php":        "<?php echo 'v1';",
		"workspace/nested/notes.txt": "original notes",
		"agent-home/claude/auth":     "token",
	})

	manifest, err := json.Marshal(servicesnapshot.Manifest{
		SchemaVersion: servicesnapshot.ManifestSchemaVersion,
		SnapshotID:    "abc123",
		ProjectID:     "p1",
		Slug:          "demo",
		Template:      "wordpress",
		Directories:   []string{"workspace", "agent-home"},
		Database:      "mysql",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := archiver.Pack(context.Background(), servicesnapshot.PackRequest{
		ProjectID: "p1",
		Name:      "20260818T101500Z-abc123",
		SourceDir: projectDir,
		Entries:   []string{"workspace", "agent-home"},
		Manifest:  manifest,
		Database:  []byte("CREATE DATABASE wordpress;"),
	})
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	if result.SizeBytes == 0 {
		t.Fatal("Pack() reported an empty archive")
	}
	if !strings.HasPrefix(result.Archive, "20260818T101500Z-abc123.tar.") {
		t.Fatalf("Pack() archive = %q, want the requested base name", result.Archive)
	}
	if entries, _ := os.ReadDir(filepath.Join(archiver.root, "p1")); len(entries) != 1 {
		t.Fatalf("staging left behind: %v", entries)
	}

	// The dump is readable straight out of the archive, which is how an
	// un-trashed project gets its database back.
	dump, err := archiver.ReadDatabase(context.Background(), "p1", result.Archive)
	if err != nil {
		t.Fatalf("ReadDatabase() error = %v", err)
	}
	if string(dump) != "CREATE DATABASE wordpress;" {
		t.Fatalf("ReadDatabase() = %q", dump)
	}

	// Mutate the live tree so the restore is observable.
	writeTree(t, projectDir, map[string]string{
		"workspace/index.php": "<?php echo 'v2 - broken';",
		"workspace/extra.txt": "written after the snapshot",
	})

	restored, err := archiver.Restore(context.Background(), servicesnapshot.RestoreRequest{
		ProjectID:  "p1",
		Archive:    result.Archive,
		ProjectDir: projectDir,
		StashName:  ".pre-restore-20260818T110000Z",
		Entries:    []string{"workspace", "agent-home"},
	})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if string(restored.Database) != "CREATE DATABASE wordpress;" {
		t.Fatalf("Restore() database = %q", restored.Database)
	}

	if got := readFile(t, filepath.Join(projectDir, "workspace", "index.php")); got != "<?php echo 'v1';" {
		t.Fatalf("restored index.php = %q", got)
	}
	if got := readFile(t, filepath.Join(projectDir, "workspace", "nested", "notes.txt")); got != "original notes" {
		t.Fatalf("restored notes.txt = %q", got)
	}
	if got := readFile(t, filepath.Join(projectDir, "agent-home", "claude", "auth")); got != "token" {
		t.Fatalf("restored agent home = %q", got)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "workspace", "extra.txt")); err == nil {
		t.Fatal("restore left a file that was not in the archive")
	}

	// The replaced tree is kept, not destroyed: a restore of the wrong
	// snapshot must be recoverable.
	stashed := readFile(t, filepath.Join(restored.StashPath, "workspace", "extra.txt"))
	if stashed != "written after the snapshot" {
		t.Fatalf("stashed extra.txt = %q", stashed)
	}
}

func TestPackWithoutDatabaseReadsBackNothing(t *testing.T) {
	archiver := newTestArchiver(t)
	projectDir := filepath.Join(t.TempDir(), "demo")
	writeTree(t, projectDir, map[string]string{"workspace/app.js": "console.log(1)"})

	result, err := archiver.Pack(context.Background(), servicesnapshot.PackRequest{
		ProjectID: "p2",
		Name:      "snap",
		SourceDir: projectDir,
		// agent-home was never created; a missing entry must not fail the pack.
		Entries:  []string{"workspace", "agent-home"},
		Manifest: []byte(`{"schemaVersion":1}`),
	})
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	dump, err := archiver.ReadDatabase(context.Background(), "p2", result.Archive)
	if err != nil {
		t.Fatalf("ReadDatabase() error = %v", err)
	}
	if len(dump) != 0 {
		t.Fatalf("ReadDatabase() = %q, want nothing", dump)
	}
}

func TestRemoveAndRemoveProject(t *testing.T) {
	archiver := newTestArchiver(t)
	projectDir := filepath.Join(t.TempDir(), "demo")
	writeTree(t, projectDir, map[string]string{"workspace/a": "a"})

	result, err := archiver.Pack(context.Background(), servicesnapshot.PackRequest{
		ProjectID: "p3", Name: "snap", SourceDir: projectDir,
		Entries: []string{"workspace"}, Manifest: []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := archiver.Remove(context.Background(), "p3", result.Archive); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	// Removing an archive that is already gone is not an error: retention and
	// an explicit delete can race.
	if err := archiver.Remove(context.Background(), "p3", result.Archive); err != nil {
		t.Fatalf("Remove() second call error = %v", err)
	}
	if err := archiver.RemoveProject(context.Background(), "p3"); err != nil {
		t.Fatalf("RemoveProject() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(archiver.root, "p3")); !os.IsNotExist(err) {
		t.Fatal("RemoveProject() left the project directory behind")
	}
}

func newTestArchiver(t *testing.T) *Archiver {
	t.Helper()
	archiver := NewArchiver(t.TempDir())
	if !archiver.Available() {
		t.Skip("tar is not on PATH")
	}
	return archiver
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
