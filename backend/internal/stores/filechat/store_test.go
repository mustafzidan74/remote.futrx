package filechat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

func TestStoreListUsesCachedMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "chats", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "meta.json"),
		[]byte(`{"id":"abcd","title":"Existing","createdAt":1,"lastMessageAt":10}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	list, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "abcd" || list[0].Title != "Existing" {
		t.Fatalf("loaded list = %#v", list)
	}
	if list[0].LastReadAt != 10 {
		t.Fatalf("legacy chat should start read, got lastReadAt=%d", list[0].LastReadAt)
	}

	if _, err := store.Create(context.Background(), servicechat.Meta{ID: "beef", Title: "New", CreatedAt: 2, LastMessageAt: 20}); err != nil {
		t.Fatal(err)
	}
	list, err = store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "beef" {
		t.Fatalf("created list = %#v", list)
	}

	if _, err := store.Update(context.Background(), "abcd", func(m *servicechat.Meta) {
		m.Title = "Renamed"
		m.LastMessageAt = 30
	}); err != nil {
		t.Fatal(err)
	}
	list, err = store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != "abcd" || list[0].Title != "Renamed" {
		t.Fatalf("updated list = %#v", list)
	}

	if _, err := store.AppendEvent(context.Background(), "beef", servicechat.Event{T: 40, Type: "user", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	list, err = store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != "beef" || list[0].LastMessageAt != 40 {
		t.Fatalf("append list = %#v", list)
	}

	if err := store.Delete(context.Background(), "beef"); err != nil {
		t.Fatal(err)
	}
	list, err = store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "abcd" {
		t.Fatalf("deleted list = %#v", list)
	}
}

func TestStoreReadsEventPages(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), servicechat.Meta{ID: "abcd"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := store.AppendEvent(context.Background(), "abcd", servicechat.Event{
			T:    int64(i),
			Type: "user",
			Text: string(rune('a' + i - 1)),
		}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := store.ReadEventsPage(context.Background(), "abcd", servicechat.EventPageQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].Seq != 4 || page.Events[1].Seq != 5 {
		t.Fatalf("latest page = %#v", page)
	}
	if !page.HasMore || page.NextBefore != 4 || page.LastSeq != 5 {
		t.Fatalf("latest page cursors = %#v", page)
	}

	older, err := store.ReadEventsPage(context.Background(), "abcd", servicechat.EventPageQuery{Limit: 2, BeforeSeq: page.NextBefore})
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Events) != 2 || older.Events[0].Seq != 2 || older.Events[1].Seq != 3 {
		t.Fatalf("older page = %#v", older)
	}

	after, err := store.ReadEventsAfter(context.Background(), "abcd", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].Seq != 4 || after[1].Seq != 5 {
		t.Fatalf("after = %#v", after)
	}
}

func TestStoreRewindClearsProviderSessionIDs(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), servicechat.Meta{
		ID:              "abcd",
		CreatedAt:       10,
		LastMessageAt:   10,
		ClaudeSessionID: "claude-session",
		CodexSessionID:  "codex-session",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.AppendEvent(context.Background(), "abcd", servicechat.Event{
		T:    20,
		Type: "user",
		Text: "keep",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(context.Background(), "abcd", servicechat.Event{
		T:    30,
		Type: "user",
		Text: "rewind from here",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(context.Background(), "abcd", servicechat.Event{
		T:    40,
		Type: "assistant_text",
		Text: "remove",
	}); err != nil {
		t.Fatal(err)
	}

	kept, err := store.TruncateEventsBefore(context.Background(), "abcd", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Text != "keep" {
		t.Fatalf("kept events = %#v", kept)
	}

	events, err := store.ReadEvents(context.Background(), "abcd")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Text != "keep" {
		t.Fatalf("persisted events = %#v", events)
	}

	meta, err := store.Get(context.Background(), "abcd")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ClaudeSessionID != "" || meta.CodexSessionID != "" {
		t.Fatalf("session ids were not cleared: %#v", meta)
	}
	if meta.LastMessageAt != 20 {
		t.Fatalf("LastMessageAt = %d, want 20", meta.LastMessageAt)
	}
}

func TestStorePersistsSelectedSkills(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	created, err := store.Create(context.Background(), servicechat.Meta{
		ID:       "abcd",
		Provider: servicechat.ProviderCodex,
		SelectedSkills: []servicechat.SkillRef{
			{Name: "Custom Skill", Command: "custom", Source: "user"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.SelectedSkills) != 1 || created.SelectedSkills[0].Provider != servicechat.ProviderCodex {
		t.Fatalf("created skills = %#v", created.SelectedSkills)
	}

	loaded, err := store.Get(context.Background(), "abcd")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.SelectedSkills) != 1 || loaded.SelectedSkills[0].Command != "custom" {
		t.Fatalf("loaded skills = %#v", loaded.SelectedSkills)
	}

	updated, err := store.Update(context.Background(), "abcd", func(m *servicechat.Meta) {
		m.SelectedSkills = append(m.SelectedSkills, servicechat.SkillRef{
			Name:     "Review",
			Command:  "review",
			Provider: servicechat.ProviderCodex,
			Source:   "project",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.SelectedSkills) != 2 || updated.SelectedSkills[1].Source != "project" {
		t.Fatalf("updated skills = %#v", updated.SelectedSkills)
	}

	reopened, err := New(store.root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = reopened.Get(context.Background(), "abcd")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.SelectedSkills) != 2 || loaded.SelectedSkills[0].Provider != servicechat.ProviderCodex {
		t.Fatalf("reloaded skills = %#v", loaded.SelectedSkills)
	}
}

func TestStorePersistsPostRunPolicies(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := store.Create(ctx, servicechat.Meta{
		ID: "abcd",
		Autopilot: servicechat.AutopilotPolicy{
			Enabled:        true,
			MaxRounds:      12,
			MaxDurationMin: 240,
			RoundsUsed:     3,
			StartedAt:      1755518400000,
			EnabledBy:      "operator@example.com",
		},
		AutoTest: servicechat.AutoTestPolicy{Enabled: true, EnabledBy: "qa@example.com"},
	}); err != nil {
		t.Fatal(err)
	}

	// Reopening is what proves the policy reached disk rather than only the
	// in-process metadata cache.
	reopened, err := New(store.root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(ctx, "abcd")
	if err != nil {
		t.Fatal(err)
	}
	want := servicechat.AutopilotPolicy{
		Enabled:        true,
		MaxRounds:      12,
		MaxDurationMin: 240,
		RoundsUsed:     3,
		StartedAt:      1755518400000,
		EnabledBy:      "operator@example.com",
	}
	if loaded.Autopilot != want {
		t.Errorf("reloaded autopilot = %+v, want %+v", loaded.Autopilot, want)
	}
	if !loaded.AutoTest.Enabled || loaded.AutoTest.EnabledBy != "qa@example.com" {
		t.Errorf("reloaded auto-test = %+v", loaded.AutoTest)
	}
}

// A chat written before post-run policies existed must still come back with
// usable limits, so the driver never reads a zero round budget as "stop".
func TestStoreDefaultsPoliciesForALegacyChat(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "chats", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "meta.json"),
		[]byte(`{"id":"abcd","title":"Old chat","createdAt":1,"lastMessageAt":10}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(context.Background(), "abcd")
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Autopilot.Enabled || loaded.AutoTest.Enabled {
		t.Errorf("policies default to on: %+v / %+v", loaded.Autopilot, loaded.AutoTest)
	}
	if loaded.Autopilot.MaxRounds != servicechat.DefaultAutopilotRounds {
		t.Errorf("maxRounds = %d, want %d", loaded.Autopilot.MaxRounds, servicechat.DefaultAutopilotRounds)
	}
	if loaded.Autopilot.MaxDurationMin != servicechat.DefaultAutopilotDurationMin {
		t.Errorf(
			"maxDurationMin = %d, want %d",
			loaded.Autopilot.MaxDurationMin, servicechat.DefaultAutopilotDurationMin,
		)
	}
}

func TestStorePersistsSyntheticEventLabel(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.Create(ctx, servicechat.Meta{ID: "abcd"}); err != nil {
		t.Fatal(err)
	}

	for _, ev := range []servicechat.Event{
		{T: 1, Type: "user", Text: "ship it"},
		{T: 2, Type: "user", Text: "keep going", Synthetic: servicechat.SyntheticAutopilot},
		{T: 3, Type: "user", Text: "verify", Synthetic: "not-a-kind"},
	} {
		if _, err := store.AppendEvent(ctx, "abcd", ev); err != nil {
			t.Fatal(err)
		}
	}

	events, err := store.ReadEvents(ctx, "abcd")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("read %d events, want 3", len(events))
	}
	if events[0].Synthetic != "" {
		t.Errorf("human prompt carries synthetic = %q", events[0].Synthetic)
	}
	if events[1].Synthetic != servicechat.SyntheticAutopilot {
		t.Errorf("synthetic = %q, want %q", events[1].Synthetic, servicechat.SyntheticAutopilot)
	}
	// An unrecognized label must not survive: badging a prompt as
	// platform-issued is a claim about who asked for the work.
	if events[2].Synthetic != "" {
		t.Errorf("unknown label survived as %q", events[2].Synthetic)
	}
}
