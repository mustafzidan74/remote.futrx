package fileglobalsecrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
)

func TestLoadReportsAnEmptyVaultBeforeAnythingIsSaved(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("a fresh install must load cleanly, got %v", err)
	}
	if len(secrets) != 0 {
		t.Fatalf("secrets = %+v", secrets)
	}
}

func TestSaveAndLoadRoundTripEveryKind(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	want := []serviceglobalsecrets.Secret{
		{
			Key:         "GITHUB_TOKEN",
			Kind:        serviceglobalsecrets.KindEnv,
			Value:       "ghp_secret",
			Scope:       serviceglobalsecrets.Scope{All: true},
			Description: "gh CLI auth",
			UpdatedAt:   1700000000000,
			UpdatedBy:   "admin@example.com",
		},
		{
			Key:   "NPMRC",
			Kind:  serviceglobalsecrets.KindFile,
			Path:  "/root/.npmrc",
			Value: "//registry.npmjs.org/:_authToken=tok",
			Scope: serviceglobalsecrets.Scope{ProjectIDs: []string{"p1", "p2"}},
		},
		{
			Key:   "HESTIA",
			Kind:  serviceglobalsecrets.KindSSH,
			Scope: serviceglobalsecrets.Scope{All: true},
			SSH: &serviceglobalsecrets.SSHTarget{
				Name:           "hestia",
				Host:           "203.0.113.10",
				Port:           2222,
				User:           "admin",
				PrivateKey:     "-----BEGIN OPENSSH PRIVATE KEY-----\nkey\n",
				KnownHostsLine: "203.0.113.10 ssh-ed25519 AAAA",
			},
		},
	}
	if err := store.Save(ctx, want); err != nil {
		t.Fatal(err)
	}

	// A second store over the same directory proves the document, not the
	// in-memory copy, is what survives a restart.
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("loaded %d entries, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index].Key != want[index].Key || got[index].Kind != want[index].Kind {
			t.Fatalf("entry %d = %+v, want %+v", index, got[index], want[index])
		}
		if got[index].Value != want[index].Value || got[index].Path != want[index].Path {
			t.Fatalf("entry %d lost its value or path: %+v", index, got[index])
		}
	}
	if got[1].Scope.All || len(got[1].Scope.ProjectIDs) != 2 {
		t.Fatalf("scope did not round-trip: %+v", got[1].Scope)
	}
	if got[2].SSH == nil || got[2].SSH.PrivateKey != want[2].SSH.PrivateKey {
		t.Fatalf("ssh target did not round-trip: %+v", got[2].SSH)
	}
	if got[2].SSH.Port != 2222 || got[2].SSH.KnownHostsLine == "" {
		t.Fatalf("ssh detail did not round-trip: %+v", got[2].SSH)
	}
}

func TestSaveReplacesTheWholeDocumentAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := store.Save(ctx, []serviceglobalsecrets.Secret{
		{Key: "A", Kind: serviceglobalsecrets.KindEnv, Value: "1"},
		{Key: "B", Kind: serviceglobalsecrets.KindEnv, Value: "2"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, []serviceglobalsecrets.Secret{
		{Key: "B", Kind: serviceglobalsecrets.KindEnv, Value: "2"},
	}); err != nil {
		t.Fatal(err)
	}

	secrets, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(secrets) != 1 || secrets[0].Key != "B" {
		t.Fatalf("secrets = %+v", secrets)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", entry.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("document missing: %v", err)
	}
}

func TestSaveOfAnEmptyVaultWritesAnEmptyArray(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "[]" {
		t.Fatalf("document = %q", string(data))
	}
}
