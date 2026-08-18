package filegithub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

const projectID = serviceproject.ID("abcd1234")

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store, dir
}

func TestGetOnAProjectWithNoSettings(t *testing.T) {
	store, _ := newStore(t)

	// A project that has never been configured must read as the zero value,
	// not as an error: the service turns that into its defaults.
	settings, err := store.Get(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.WebhookConfigured() || settings.AutoRun || len(settings.Deliveries) != 0 {
		t.Fatalf("settings = %+v, want the zero value", settings)
	}
	if settings.LabelOrDefault() != servicegithub.DefaultLabel {
		t.Fatalf("label = %q, want the default", settings.LabelOrDefault())
	}
}

func TestSaveAndGetRoundTrip(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	want := servicegithub.Settings{
		Secret:      "s3cret",
		Label:       "agent",
		AutoRun:     true,
		CommentBack: true,
		EnabledAt:   1755500000000,
		EnabledBy:   "admin@example.test",
		UpdatedAt:   1755500000001,
		Deliveries: []servicegithub.Delivery{
			{ID: "d1", At: 1, Event: "issues", Number: 7, Outcome: servicegithub.OutcomeRan},
		},
	}
	if err := store.Save(ctx, projectID, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get(ctx, projectID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Secret != want.Secret || got.Label != want.Label || got.AutoRun != want.AutoRun ||
		got.CommentBack != want.CommentBack || got.EnabledBy != want.EnabledBy {
		t.Fatalf("got = %+v, want %+v", got, want)
	}
	if len(got.Deliveries) != 1 || got.Deliveries[0].ID != "d1" ||
		got.Deliveries[0].Outcome != servicegithub.OutcomeRan {
		t.Fatalf("deliveries = %+v, want the saved row", got.Deliveries)
	}
}

func TestSettingsPersistAcrossReopen(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()

	if err := store.Save(ctx, projectID, servicegithub.Settings{Secret: "keep-me"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := reopened.Get(ctx, projectID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Secret != "keep-me" {
		t.Fatalf("secret = %q, want it to survive a restart", got.Secret)
	}
}

func TestSettingsAreOnePerProject(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	const other = serviceproject.ID("beef0001")

	if err := store.Save(ctx, projectID, servicegithub.Settings{Secret: "first"}); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := store.Save(ctx, other, servicegithub.Settings{Secret: "second"}); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	first, _ := store.Get(ctx, projectID)
	second, _ := store.Get(ctx, other)
	if first.Secret != "first" || second.Secret != "second" {
		t.Fatalf("secrets crossed over: %q and %q", first.Secret, second.Secret)
	}
}

func TestDeleteRemovesTheSecretAndIsIdempotent(t *testing.T) {
	store, dir := newStore(t)
	ctx := context.Background()

	if err := store.Save(ctx, projectID, servicegithub.Settings{Secret: "s3cret", AutoRun: true}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(ctx, projectID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// The file itself has to be gone, not merely emptied: unlinking a
	// repository must not leave a live webhook secret on disk.
	if _, err := os.Stat(filepath.Join(dir, "github", string(projectID)+".json")); !os.IsNotExist(err) {
		t.Fatalf("settings file still exists after Delete (stat err = %v)", err)
	}
	// Deleting again is how unlinking a never-linked project behaves.
	if err := store.Delete(ctx, projectID); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	got, err := store.Get(ctx, projectID)
	if err != nil {
		t.Fatalf("Get after Delete: %v", err)
	}
	if got.WebhookConfigured() || got.AutoRun {
		t.Fatalf("settings = %+v, want the zero value after Delete", got)
	}
}

func TestCancelledContextIsRefusedBeforeTouchingDisk(t *testing.T) {
	store, _ := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Get(ctx, projectID); err == nil {
		t.Fatal("Get with a cancelled context should fail")
	}
	if err := store.Save(ctx, projectID, servicegithub.Settings{}); err == nil {
		t.Fatal("Save with a cancelled context should fail")
	}
	if err := store.Delete(ctx, projectID); err == nil {
		t.Fatal("Delete with a cancelled context should fail")
	}
}
