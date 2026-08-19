package routing

// Seeded rule ids. They are stable strings so a savings report keeps
// attributing hits to the same rule across upgrades, and so the docs can name
// one.
const (
	RuleSyntheticChecks = "synthetic-checks"
	RuleChatMode        = "chat-mode"
	RuleShortPrompt     = "short-prompt"
	RuleHardWork        = "hard-work"
	RuleLongPrompt      = "long-prompt"
)

// Heuristic ids, reported in Decision.RuleID when AutoHeuristics decided a
// run. They are namespaced so a report can never confuse one with a rule the
// operator wrote.
const (
	HeuristicCheap     = "heuristic:cheap"
	HeuristicExpensive = "heuristic:expensive"
)

// Heuristic thresholds. They match the seeded rules so an operator who turns
// the rules off and the heuristics on gets roughly the same behaviour.
const (
	shortPromptChars = 200
	longPromptChars  = 2000
)

// hardWorkPattern is the case-insensitive alternation the "hard work" rule
// and the expensive heuristic both look for.
const hardWorkPattern = `(?i)\b(refactor|architect(ure|ural)?|debug|migrat(e|ion)|root cause|race condition|redesign)\b`

// DefaultPolicy is what model-routing.json is seeded with on first read.
//
// Routing is off and every rule is disabled: a fresh install behaves exactly
// as it did before this feature existed, and the seeded rules are a starting
// point the administrator reviews and arms rather than a policy that silently
// changes which model answers.
//
// The two poles are deliberately conservative — Claude Haiku for the cheap
// end and Claude Opus for the expensive one — because Claude is the provider
// most deployments connect first. An administrator with Codex or Kimi
// connected is expected to repoint them.
func DefaultPolicy() Policy {
	cheap := ModelRef{Provider: "claude", Model: "haiku"}
	expensive := ModelRef{Provider: "claude", Model: "opus"}
	return Policy{
		Version:        PolicyVersion,
		Enabled:        false,
		Default:        ModelRef{Provider: "claude", Model: "sonnet"},
		Cheap:          cheap,
		Expensive:      expensive,
		AutoHeuristics: true,
		Rules: []Rule{
			{
				ID:      RuleSyntheticChecks,
				When:    Condition{Kind: KindSynthetic, Value: SyntheticAny},
				Use:     cheap,
				Note:    "Platform checks (auto-test, team review, team test) run cheap",
				Enabled: false,
			},
			{
				ID:      RuleChatMode,
				When:    Condition{Kind: KindModeIs, Value: "chat"},
				Use:     cheap,
				Note:    "Chat mode answers questions, it does not change files",
				Enabled: false,
			},
			{
				ID:      RuleShortPrompt,
				When:    Condition{Kind: KindPromptShorterThan, Value: "200"},
				Use:     cheap,
				Note:    "Short prompt with no code block",
				Enabled: false,
			},
			{
				ID:      RuleHardWork,
				When:    Condition{Kind: KindRegex, Value: hardWorkPattern},
				Use:     expensive,
				Note:    "Refactor, architecture, debugging, or migration work",
				Enabled: false,
			},
			{
				ID:      RuleLongPrompt,
				When:    Condition{Kind: KindPromptLongerThan, Value: "2000"},
				Use:     expensive,
				Note:    "Long prompt carries a lot of context",
				Enabled: false,
			},
		},
	}
}
