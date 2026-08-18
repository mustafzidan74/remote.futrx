package filechat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// The team policy is the one piece of chat metadata a restart has to restore
// exactly: the orchestrator resumes a loop from stored state alone, so a field
// lost in the round trip is a loop that silently forgets where it was.
func TestStoreRoundTripsTheTeamPolicy(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, servicechat.Meta{
		Title:    "Ship the checkout flow",
		Provider: servicechat.ProviderClaude,
		Team: servicechat.TeamPolicy{
			Enabled:   true,
			MaxLoops:  3,
			AutoFix:   true,
			Phase:     servicechat.TeamPhaseReviewing,
			LoopsUsed: 1,
			Verdict:   servicechat.TeamVerdictFix,
			EnabledBy: "operator@example.com",
			UpdatedAt: 1_700_000_000_000,
			Roles: servicechat.TeamRoles{
				Implementer: servicechat.TeamRole{Provider: servicechat.ProviderClaude, Enabled: true},
				Reviewer: servicechat.TeamRole{
					Provider: servicechat.ProviderCodex,
					Model:    "gpt-5-codex",
					Enabled:  true,
					ChatID:   "aaaabbbb",
				},
				Tester: servicechat.TeamRole{Provider: servicechat.ProviderKimi, Enabled: true, ChatID: "ccccdddd"},
			},
			Hops: []servicechat.TeamHop{{
				Loop:     1,
				Role:     servicechat.TeamRoleReviewer,
				Kind:     servicechat.SyntheticTeamReview,
				ChatID:   "aaaabbbb",
				Verdict:  servicechat.TeamVerdictFix,
				Findings: "1. handle the empty cart",
				At:       1_700_000_000_001,
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A fresh store instance is what makes this a persistence test rather than
	// a cache test.
	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	team := loaded.Team
	if !team.Enabled || team.MaxLoops != 3 || !team.AutoFix || team.LoopsUsed != 1 {
		t.Fatalf("policy = %+v", team)
	}
	if team.Phase != servicechat.TeamPhaseReviewing || team.Verdict != servicechat.TeamVerdictFix {
		t.Errorf("phase=%q verdict=%q", team.Phase, team.Verdict)
	}
	if team.EnabledBy != "operator@example.com" || team.UpdatedAt != 1_700_000_000_000 {
		t.Errorf("ownership = %q at %d", team.EnabledBy, team.UpdatedAt)
	}
	if team.Roles.Reviewer.Provider != servicechat.ProviderCodex ||
		team.Roles.Reviewer.Model != "gpt-5-codex" ||
		team.Roles.Reviewer.ChatID != "aaaabbbb" {
		t.Errorf("reviewer = %+v", team.Roles.Reviewer)
	}
	if team.Roles.Tester.ChatID != "ccccdddd" {
		t.Errorf("tester = %+v", team.Roles.Tester)
	}
	if len(team.Hops) != 1 || team.Hops[0].Findings != "1. handle the empty cart" ||
		team.Hops[0].Kind != servicechat.SyntheticTeamReview {
		t.Errorf("timeline = %+v", team.Hops)
	}
}

func TestStoreRoundTripsCompanionIdentity(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, servicechat.Meta{
		Title:         "🧐 Reviewer — Ship the checkout flow",
		CompanionOf:   "aaaabbbb",
		CompanionRole: servicechat.TeamRoleReviewer,
	})
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CompanionOf != "aaaabbbb" || loaded.CompanionRole != servicechat.TeamRoleReviewer {
		t.Fatalf("companion identity = %q/%q", loaded.CompanionOf, loaded.CompanionRole)
	}
}

// A chat written before team mode existed must come back with a policy the
// orchestrator can reason about rather than a zeroed loop budget.
func TestStoreNormalizesALegacyChat(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "chats", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "meta.json"),
		[]byte(`{"id":"abcd","title":"Older than team mode","createdAt":1,"lastMessageAt":10}`),
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

	if loaded.Team.Enabled {
		t.Errorf("team mode must default off")
	}
	if loaded.Team.MaxLoops != servicechat.DefaultTeamLoops {
		t.Errorf("maxLoops = %d, want the documented default", loaded.Team.MaxLoops)
	}
	if !loaded.Team.Roles.Implementer.Enabled {
		t.Errorf("the implementer seat must always be enabled")
	}
}
