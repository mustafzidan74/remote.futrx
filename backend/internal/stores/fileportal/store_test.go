package fileportal

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	serviceportal "github.com/futrx-com/remote.futrx.com/internal/service/portal"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

const projectID = serviceproject.ID("9f2a1c04")

func TestGetReturnsAZeroRecordBeforeAnythingIsSaved(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	record, err := store.Get(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if record.Enabled || record.TokenHash != "" {
		t.Fatalf("record = %+v, want the zero value", record)
	}
}

func TestSaveRoundTrips(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	want := serviceportal.Portal{
		Enabled:       true,
		TokenHash:     "1f2e3d",
		CreatedAt:     1700000000000,
		UpdatedAt:     1700000001000,
		ShowPreview:   true,
		ShowChangelog: true,
		ShowUsage:     false,
		BrandTitle:    "Acme Shop",
		Note:          "line one\nline two",
	}

	if err := store.Save(context.Background(), projectID, want); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	got, err := store.Get(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	// A second store over the same directory reads what the first wrote, which
	// is what makes the portal survive a restart.
	reopened, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	got, err = reopened.Get(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Get() after reopen = %v", err)
	}
	if got != want {
		t.Fatalf("after reopen = %+v, want %+v", got, want)
	}
}

func TestSavedFileIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes are not modelled on windows")
	}
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if err := store.Save(context.Background(), projectID, serviceportal.Portal{
		Enabled: true, TokenHash: "abc",
	}); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "portals", string(projectID)+".json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o, want 600: the file carries a token digest", mode)
	}
}

func TestSaveOverwritesThePreviousRecord(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, projectID, serviceportal.Portal{Enabled: true, TokenHash: "first"}); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if err := store.Save(ctx, projectID, serviceportal.Portal{Enabled: false}); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	got, err := store.Get(ctx, projectID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got.Enabled || got.TokenHash != "" {
		t.Fatalf("record = %+v, want the disabled record", got)
	}
}

func TestRecordsAreIsolatedPerProject(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	ctx := context.Background()
	other := serviceproject.ID("aabbccdd")

	if err := store.Save(ctx, projectID, serviceportal.Portal{Enabled: true, TokenHash: "one"}); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if err := store.Save(ctx, other, serviceportal.Portal{Enabled: true, TokenHash: "two"}); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	first, _ := store.Get(ctx, projectID)
	second, _ := store.Get(ctx, other)
	if first.TokenHash != "one" || second.TokenHash != "two" {
		t.Fatalf("records crossed: %+v / %+v", first, second)
	}
}
