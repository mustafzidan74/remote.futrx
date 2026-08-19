package routing

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type memoryRepo struct {
	policy Policy
	found  bool
	saves  int
	saveIn Policy
	err    error
}

func (r *memoryRepo) Load(context.Context) (Policy, bool, error) {
	if r.err != nil {
		return Policy{}, false, r.err
	}
	return r.policy, r.found, nil
}

func (r *memoryRepo) Save(_ context.Context, policy Policy) error {
	r.saves++
	r.saveIn = policy
	r.policy = policy
	r.found = true
	return nil
}

type stubProviders []string

func (p stubProviders) Connected() []string { return []string(p) }

func TestPolicySeedsTheShippedDefaultOnce(t *testing.T) {
	repo := &memoryRepo{}
	service := New(repo)

	policy, err := service.Policy(context.Background())
	if err != nil {
		t.Fatalf("Policy() error = %v", err)
	}
	if policy.Enabled {
		t.Fatal("a fresh install must ship with routing off")
	}
	if len(policy.Rules) == 0 {
		t.Fatal("the shipped default must carry the seeded rules")
	}
	for _, rule := range policy.Rules {
		if rule.Enabled {
			t.Fatalf("seeded rule %q must ship disabled", rule.ID)
		}
	}
	if repo.saves != 1 {
		t.Fatalf("saves = %d, want the seed written exactly once", repo.saves)
	}

	if _, err := service.Policy(context.Background()); err != nil {
		t.Fatalf("second Policy() error = %v", err)
	}
	if repo.saves != 1 {
		t.Fatalf("saves = %d, want the cached policy reused", repo.saves)
	}
}

func TestPolicyFallsBackWhenTheStoredDocumentIsUnusable(t *testing.T) {
	// A hand-edited file with no default is not a policy anything can run.
	repo := &memoryRepo{found: true, policy: Policy{Enabled: true}}
	policy, err := New(repo).Policy(context.Background())
	if err != nil {
		t.Fatalf("Policy() error = %v", err)
	}
	if policy.Enabled {
		t.Fatal("an unusable document must fall back to the shipped default, which is off")
	}
}

func TestUpdateRejectsAnInvalidPolicy(t *testing.T) {
	repo := &memoryRepo{}
	service := New(repo)
	if _, err := service.Policy(context.Background()); err != nil {
		t.Fatalf("seed error = %v", err)
	}
	saves := repo.saves

	_, err := service.Update(context.Background(), Policy{
		Enabled: true,
		Default: ModelRef{Provider: "claude", Model: "sonnet"},
		Rules: []Rule{
			{ID: "bad", When: Condition{Kind: KindRegex, Value: "("}, Use: cheap, Enabled: true},
		},
	}, "admin@example.com")
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Update() error = %v, want ErrInvalidPolicy", err)
	}
	if repo.saves != saves {
		t.Fatal("an invalid policy must not be persisted")
	}
}

func TestUpdateNormalizesAndStamps(t *testing.T) {
	repo := &memoryRepo{}
	service := New(repo, WithProviders(stubProviders{"claude"}))

	view, err := service.Update(context.Background(), Policy{
		Enabled: true,
		Default: ModelRef{Provider: "  CLAUDE ", Model: " sonnet "},
		Rules: []Rule{
			{When: Condition{Kind: "nonsense"}, Use: cheap, Enabled: true},
			{ID: "keep", When: Condition{Kind: KindModeIs, Value: "CHAT"}, Use: cheap, Enabled: true},
			{ID: "keep", When: Condition{Kind: KindModeIs, Value: "plan"}, Use: cheap, Enabled: true},
		},
	}, "Admin@Example.com")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if view.Policy.Default.Provider != "claude" || view.Policy.Default.Model != "sonnet" {
		t.Fatalf("default = %+v, want it trimmed and lower-cased", view.Policy.Default)
	}
	if len(view.Policy.Rules) != 1 || view.Policy.Rules[0].When.Value != "chat" {
		t.Fatalf("rules = %+v, want the unknown kind dropped and the duplicate id collapsed", view.Policy.Rules)
	}
	if view.Policy.UpdatedAt == 0 || view.Policy.UpdatedBy != "Admin@Example.com" {
		t.Fatalf("policy was not stamped: %+v", view.Policy)
	}
	if len(view.Providers) != 1 || view.Providers[0] != "claude" {
		t.Fatalf("Providers = %v, want the connected list", view.Providers)
	}
	if len(view.Catalog) == 0 {
		t.Fatal("the view must carry the model catalog the pickers render")
	}
}

func TestRouteUsesTheStoredPolicy(t *testing.T) {
	stored := armedPolicy()
	stored.Version = PolicyVersion
	repo := &memoryRepo{found: true, policy: stored}
	service := New(repo, WithProviders(stubProviders{"claude", "codex"}))

	decision := service.Route(context.Background(), Input{
		Provider: "codex", Model: "gpt-5.5", Mode: "chat", Prompt: "ping",
	})
	if !decision.Routed || decision.Model != cheap.Model {
		t.Fatalf("Route() = %+v, want the chat-mode rule", decision)
	}
}

func TestRouteKeepsTheRunWhenThePolicyCannotBeRead(t *testing.T) {
	service := New(&memoryRepo{err: errors.New("disk on fire")})
	decision := service.Route(context.Background(), Input{Provider: "codex", Model: "gpt-5.5"})
	if decision.Routed || decision.Provider != "codex" || decision.Model != "gpt-5.5" {
		t.Fatalf("Route() = %+v, want the chat's own model", decision)
	}
}

func TestNilServiceRoutesNothing(t *testing.T) {
	var service *Service
	decision := service.Route(context.Background(), Input{Provider: "claude", Model: "opus"})
	if decision.Routed || decision.Model != "opus" {
		t.Fatalf("Route() = %+v, want the input unchanged", decision)
	}
}

func TestTestExplainsAPastedPrompt(t *testing.T) {
	stored := armedPolicy()
	repo := &memoryRepo{found: true, policy: stored}
	service := New(repo, WithProviders(stubProviders{"claude", "codex"}))

	decision, err := service.Test(context.Background(), TestInput{
		Prompt: "plan the database migration", Mode: "code", Provider: "codex",
	})
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if decision.RuleID != "regex-migrate" {
		t.Fatalf("RuleID = %q, want the regex rule", decision.RuleID)
	}
	if !strings.Contains(decision.Reason, "migration") {
		t.Fatalf("Reason = %q, want it to name the rule", decision.Reason)
	}
}

func TestNormalizeRejectsABadLengthBound(t *testing.T) {
	_, err := Policy{
		Default: ModelRef{Provider: "claude"},
		Rules: []Rule{
			{ID: "x", When: Condition{Kind: KindPromptShorterThan, Value: "lots"}, Use: cheap, Enabled: true},
		},
	}.Normalize()
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Normalize() error = %v, want ErrInvalidPolicy", err)
	}
}

func TestDefaultPolicyNormalizesCleanly(t *testing.T) {
	normalized, err := DefaultPolicy().Normalize()
	if err != nil {
		t.Fatalf("the shipped default must normalize: %v", err)
	}
	if len(normalized.Rules) != len(DefaultPolicy().Rules) {
		t.Fatalf("rules = %d, want every seeded rule to survive normalization", len(normalized.Rules))
	}
	for _, rule := range normalized.Rules {
		if !KnownModel(rule.Use.Provider, rule.Use.Model) {
			t.Fatalf("seeded rule %q points outside the catalog", rule.ID)
		}
	}
	for _, ref := range []ModelRef{normalized.Default, normalized.Cheap, normalized.Expensive} {
		if !KnownModel(ref.Provider, ref.Model) {
			t.Fatalf("seeded reference %+v is outside the catalog", ref)
		}
	}
}
