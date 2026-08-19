package routing

import (
	"strings"
	"testing"
)

// allConnected is the availability of a host that has every provider signed
// in, which is the baseline the rule table is exercised against.
func allConnected() Availability {
	return Availability{
		Known:     true,
		Providers: map[string]bool{"claude": true, "codex": true, "kimi": true, "antigravity": true},
	}
}

var (
	cheap     = ModelRef{Provider: "claude", Model: "haiku"}
	expensive = ModelRef{Provider: "claude", Model: "opus"}
	fallback  = ModelRef{Provider: "codex", Model: "gpt-5.4"}
)

// armedPolicy is one enabled rule of each kind, in a deliberate order, on top
// of a default that belongs to a different provider so a fallback is visible.
func armedPolicy() Policy {
	return Policy{
		Enabled:   true,
		Default:   fallback,
		Cheap:     cheap,
		Expensive: expensive,
		Rules: []Rule{
			{ID: "synthetic-any", When: Condition{Kind: KindSynthetic, Value: SyntheticAny}, Use: cheap, Note: "checks", Enabled: true},
			{ID: "mode-chat", When: Condition{Kind: KindModeIs, Value: "chat"}, Use: cheap, Note: "chat mode", Enabled: true},
			{ID: "skill-browser", When: Condition{Kind: KindSkillSelected, Value: "browser"}, Use: expensive, Note: "browser work", Enabled: true},
			{ID: "project-acme", When: Condition{Kind: KindProjectIs, Value: "acme"}, Use: expensive, Note: "acme", Enabled: true},
			{ID: "regex-migrate", When: Condition{Kind: KindRegex, Value: `(?i)migration`}, Use: expensive, Note: "migration", Enabled: true},
			{ID: "long", When: Condition{Kind: KindPromptLongerThan, Value: "2000"}, Use: expensive, Note: "long", Enabled: true},
			{ID: "short", When: Condition{Kind: KindPromptShorterThan, Value: "200"}, Use: cheap, Note: "short", Enabled: true},
		},
	}
}

func TestDecideRuleTable(t *testing.T) {
	base := Input{Provider: "claude", Model: "sonnet", Mode: "code", Prompt: strings.Repeat("x", 500)}

	cases := []struct {
		name       string
		mutate     func(*Input)
		policy     func(Policy) Policy
		wantRouted bool
		wantRule   string
		wantRef    ModelRef
	}{
		{
			name:       "synthetic run matches the first rule",
			mutate:     func(in *Input) { in.Synthetic = "autotest" },
			wantRouted: true,
			wantRule:   "synthetic-any",
			wantRef:    cheap,
		},
		{
			name:   "named synthetic label matches",
			mutate: func(in *Input) { in.Synthetic = "team-review" },
			policy: func(p Policy) Policy {
				p.Rules[0].When.Value = "team-review"
				return p
			},
			wantRouted: true,
			wantRule:   "synthetic-any",
			wantRef:    cheap,
		},
		{
			name:   "wrong synthetic label falls through to the default",
			mutate: func(in *Input) { in.Synthetic = "autopilot" },
			policy: func(p Policy) Policy {
				p.Rules[0].When.Value = "team-review"
				return p
			},
			wantRouted: true,
			wantRule:   "",
			wantRef:    fallback,
		},
		{
			name:       "chat mode",
			mutate:     func(in *Input) { in.Mode = "chat" },
			wantRouted: true,
			wantRule:   "mode-chat",
			wantRef:    cheap,
		},
		{
			name:       "selected skill",
			mutate:     func(in *Input) { in.Skills = []string{"scheduled-tasks", "browser"} },
			wantRouted: true,
			wantRule:   "skill-browser",
			wantRef:    expensive,
		},
		{
			name:       "project by slug",
			mutate:     func(in *Input) { in.ProjectSlug = "acme" },
			wantRouted: true,
			wantRule:   "project-acme",
			wantRef:    expensive,
		},
		{
			name:       "project by id",
			mutate:     func(in *Input) { in.ProjectID = "acme" },
			wantRouted: true,
			wantRule:   "project-acme",
			wantRef:    expensive,
		},
		{
			name:       "regex over the prompt",
			mutate:     func(in *Input) { in.Prompt = "plan the database Migration" },
			wantRouted: true,
			wantRule:   "regex-migrate",
			wantRef:    expensive,
		},
		{
			name:       "prompt longer than the bound",
			mutate:     func(in *Input) { in.Prompt = strings.Repeat("y", 2500) },
			wantRouted: true,
			wantRule:   "long",
			wantRef:    expensive,
		},
		{
			name:       "prompt shorter than the bound",
			mutate:     func(in *Input) { in.Prompt = "fix the typo" },
			wantRouted: true,
			wantRule:   "short",
			wantRef:    cheap,
		},
		{
			name:       "short prompt carrying a code fence is not short work",
			mutate:     func(in *Input) { in.Prompt = "why?\n```go\nx := 1\n```" },
			wantRouted: true,
			wantRule:   "",
			wantRef:    fallback,
		},
		{
			name:       "nothing matches, the default claims the run",
			wantRouted: true,
			wantRule:   "",
			wantRef:    fallback,
		},
		{
			name: "a disabled rule never fires",
			policy: func(p Policy) Policy {
				p.Rules[1].Enabled = false
				return p
			},
			mutate:     func(in *Input) { in.Mode = "chat" },
			wantRouted: true,
			wantRule:   "",
			wantRef:    fallback,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			policy := armedPolicy()
			if testCase.policy != nil {
				policy = testCase.policy(policy)
			}
			input := base
			if testCase.mutate != nil {
				testCase.mutate(&input)
			}
			got := Decide(policy, input, allConnected())
			if got.Routed != testCase.wantRouted {
				t.Fatalf("Routed = %v, want %v (reason %q)", got.Routed, testCase.wantRouted, got.Reason)
			}
			if got.RuleID != testCase.wantRule {
				t.Fatalf("RuleID = %q, want %q (reason %q)", got.RuleID, testCase.wantRule, got.Reason)
			}
			if got.Provider != testCase.wantRef.Provider || got.Model != testCase.wantRef.Model {
				t.Fatalf("destination = %s/%s, want %s/%s",
					got.Provider, got.Model, testCase.wantRef.Provider, testCase.wantRef.Model)
			}
			if got.Reason == "" {
				t.Fatal("every decision must carry a reason")
			}
		})
	}
}

func TestDecideOrderingIsFirstMatchWins(t *testing.T) {
	policy := armedPolicy()
	// A synthetic run in chat mode with a short prompt matches three rules;
	// only the first one in the list may decide.
	got := Decide(policy, Input{
		Provider: "claude", Model: "sonnet", Mode: "chat", Prompt: "ping", Synthetic: "autotest",
	}, allConnected())
	if got.RuleID != "synthetic-any" {
		t.Fatalf("RuleID = %q, want the first matching rule", got.RuleID)
	}

	// Reordering the list reorders the outcome, with nothing else changed.
	policy.Rules[0], policy.Rules[1] = policy.Rules[1], policy.Rules[0]
	got = Decide(policy, Input{
		Provider: "claude", Model: "sonnet", Mode: "chat", Prompt: "ping", Synthetic: "autotest",
	}, allConnected())
	if got.RuleID != "mode-chat" {
		t.Fatalf("RuleID = %q, want the rule now sitting first", got.RuleID)
	}
}

func TestDecidePinnedChatIsNeverRouted(t *testing.T) {
	got := Decide(armedPolicy(), Input{
		Pinned: true, Provider: "claude", Model: "sonnet", Mode: "chat", Prompt: "ping",
		Synthetic: "autotest", ReasoningEffort: "high",
	}, allConnected())
	if got.Routed {
		t.Fatal("a pinned chat must never be routed")
	}
	if got.Provider != "claude" || got.Model != "sonnet" || got.ReasoningEffort != "high" {
		t.Fatalf("pinned run changed: %+v", got)
	}
	if !strings.Contains(got.Reason, "pinned") {
		t.Fatalf("Reason = %q, want it to say the chat is pinned", got.Reason)
	}
}

func TestDecideDisabledPolicyKeepsTheChatsOwnModel(t *testing.T) {
	policy := armedPolicy()
	policy.Enabled = false
	got := Decide(policy, Input{Provider: "codex", Model: "gpt-5.5", Mode: "chat"}, allConnected())
	if got.Routed || got.Provider != "codex" || got.Model != "gpt-5.5" {
		t.Fatalf("disabled policy changed the run: %+v", got)
	}
}

func TestDecideFallsBackWhenTheDestinationIsUnavailable(t *testing.T) {
	onlyCodex := Availability{Known: true, Providers: map[string]bool{"codex": true}}

	t.Run("rule destination disconnected falls back to the default", func(t *testing.T) {
		got := Decide(armedPolicy(), Input{
			Provider: "codex", Model: "gpt-5.5", Mode: "chat", Prompt: "ping",
		}, onlyCodex)
		if !got.Routed || got.Provider != fallback.Provider || got.Model != fallback.Model {
			t.Fatalf("destination = %s/%s, want the default", got.Provider, got.Model)
		}
		if !strings.Contains(got.Reason, "not connected") {
			t.Fatalf("Reason = %q, want it to name the disconnected provider", got.Reason)
		}
	})

	t.Run("default also unavailable keeps the chat's own model", func(t *testing.T) {
		policy := armedPolicy()
		policy.Default = ModelRef{Provider: "kimi"}
		got := Decide(policy, Input{
			Provider: "codex", Model: "gpt-5.5", Mode: "chat", Prompt: "ping",
		}, onlyCodex)
		if got.Routed {
			t.Fatal("nothing was routable, so the run must not be marked routed")
		}
		if got.Provider != "codex" || got.Model != "gpt-5.5" {
			t.Fatalf("destination = %s/%s, want the chat's own model", got.Provider, got.Model)
		}
	})

	t.Run("model outside the catalog falls back", func(t *testing.T) {
		policy := armedPolicy()
		policy.Rules[1].Use = ModelRef{Provider: "codex", Model: "gpt-9-imaginary"}
		got := Decide(policy, Input{
			Provider: "claude", Model: "sonnet", Mode: "chat", Prompt: "ping",
		}, allConnected())
		if got.Model == "gpt-9-imaginary" {
			t.Fatal("a model outside the catalog must never reach a provider")
		}
		if !strings.Contains(got.Reason, "catalog") {
			t.Fatalf("Reason = %q, want it to name the catalog", got.Reason)
		}
	})
}

func TestDecideHeuristicsRunAfterTheRules(t *testing.T) {
	policy := Policy{
		Enabled: true, AutoHeuristics: true,
		Default: fallback, Cheap: cheap, Expensive: expensive,
		Rules: []Rule{
			{ID: "explicit", When: Condition{Kind: KindModeIs, Value: "review"}, Use: fallback, Note: "review", Enabled: true},
		},
	}
	base := Input{Provider: "claude", Model: "sonnet", Mode: "code"}

	cases := []struct {
		name    string
		input   Input
		wantID  string
		wantRef ModelRef
	}{
		{"a matching rule still wins", Input{Provider: "claude", Mode: "review", Prompt: "ok"}, "explicit", fallback},
		{"long prompt", withPrompt(base, strings.Repeat("z", 2100)), HeuristicExpensive, expensive},
		{"hard-work wording", withPrompt(base, "please refactor the scheduler"), HeuristicExpensive, expensive},
		{"synthetic run", withSynthetic(withPrompt(base, "check it"), "autotest"), HeuristicCheap, cheap},
		{"short prompt", withPrompt(base, "bump the version"), HeuristicCheap, cheap},
		{"ordinary prompt falls through to the default", withPrompt(base, strings.Repeat("w", 400)), "", fallback},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := Decide(policy, testCase.input, allConnected())
			if got.RuleID != testCase.wantID {
				t.Fatalf("RuleID = %q, want %q (reason %q)", got.RuleID, testCase.wantID, got.Reason)
			}
			if got.Provider != testCase.wantRef.Provider || got.Model != testCase.wantRef.Model {
				t.Fatalf("destination = %s/%s, want %s/%s",
					got.Provider, got.Model, testCase.wantRef.Provider, testCase.wantRef.Model)
			}
		})
	}

	t.Run("heuristics off leaves the default", func(t *testing.T) {
		off := policy
		off.AutoHeuristics = false
		got := Decide(off, withPrompt(base, "bump the version"), allConnected())
		if got.RuleID != "" || got.Provider != fallback.Provider {
			t.Fatalf("heuristics still ran: %+v", got)
		}
	})
}

func TestDecideCarriesTheRuleReasoningEffort(t *testing.T) {
	policy := armedPolicy()
	policy.Rules[1].Use = ModelRef{Provider: "claude", Model: "haiku", ReasoningEffort: "low"}
	got := Decide(policy, Input{
		Provider: "claude", Model: "sonnet", Mode: "chat", Prompt: "ping", ReasoningEffort: "high",
	}, allConnected())
	if got.ReasoningEffort != "low" {
		t.Fatalf("ReasoningEffort = %q, want the rule's own", got.ReasoningEffort)
	}

	policy.Rules[1].Use.ReasoningEffort = ""
	got = Decide(policy, Input{
		Provider: "claude", Model: "sonnet", Mode: "chat", Prompt: "ping", ReasoningEffort: "high",
	}, allConnected())
	if got.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %q, want the chat's own", got.ReasoningEffort)
	}
}

func TestDecideSkipsTheConnectivityCheckWhenNothingCanBeAsked(t *testing.T) {
	got := Decide(armedPolicy(), Input{
		Provider: "claude", Model: "sonnet", Mode: "chat", Prompt: "ping",
	}, Availability{})
	if !got.Routed || got.Model != cheap.Model {
		t.Fatalf("unknown availability blocked routing: %+v", got)
	}
}

func withPrompt(in Input, prompt string) Input {
	in.Prompt = prompt
	return in
}

func withSynthetic(in Input, kind string) Input {
	in.Synthetic = kind
	return in
}
