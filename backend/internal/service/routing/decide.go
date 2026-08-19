package routing

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Availability is what the deployment can actually route to. Providers is the
// set with a live host credential; Known is false when the deployment could
// not be asked, in which case the connectivity half of the check is skipped
// and only the catalog is enforced.
type Availability struct {
	Providers map[string]bool
	Known     bool
}

func (a Availability) allows(ref ModelRef) bool {
	if !KnownModel(ref.Provider, ref.Model) {
		return false
	}
	if !a.Known {
		return true
	}
	return a.Providers[ref.Provider]
}

// unavailableReason explains, in operator words, why a reference cannot be
// used right now.
func (a Availability) unavailableReason(ref ModelRef) string {
	if !KnownModel(ref.Provider, ref.Model) {
		return ProviderLabel(ref.Provider) + " " + ModelLabel(ref.Provider, ref.Model) +
			" is not in the model catalog"
	}
	return ProviderLabel(ref.Provider) + " is not connected"
}

// Decide is the whole routing policy as one pure function: the first enabled
// rule that matches wins, then the built-in heuristics when the policy asks
// for them, then the policy default. A destination the deployment cannot use
// falls back to the default, and a default it cannot use falls back to the
// chat's own model — always with the reason recorded, so an operator reading
// a transcript never has to guess why a run went where it went.
func Decide(policy Policy, input Input, availability Availability) Decision {
	own := Decision{
		Provider:        strings.ToLower(strings.TrimSpace(input.Provider)),
		Model:           strings.TrimSpace(input.Model),
		ReasoningEffort: strings.TrimSpace(input.ReasoningEffort),
	}
	if !policy.Enabled {
		own.Reason = "automatic routing is off"
		return own
	}
	if input.Pinned {
		own.Reason = "this chat is pinned to " + ModelRef{Provider: own.Provider, Model: own.Model}.Label()
		return own
	}

	candidate, ruleID, note, reason := selectCandidate(policy, input)
	if availability.allows(candidate) {
		return applied(own, candidate, ruleID, note, reason)
	}

	fallbackReason := reason + " — but " + availability.unavailableReason(candidate)
	if ruleID != "" && candidate != policy.Default && availability.allows(policy.Default) {
		return applied(
			own,
			policy.Default,
			"",
			"",
			fallbackReason+"; used the default model instead",
		)
	}
	own.Reason = fallbackReason + "; kept this chat's own model"
	return own
}

func applied(own Decision, ref ModelRef, ruleID, note, reason string) Decision {
	effort := ref.ReasoningEffort
	if effort == "" {
		effort = own.ReasoningEffort
	}
	return Decision{
		Provider:        ref.Provider,
		Model:           ref.Model,
		ReasoningEffort: effort,
		Routed:          true,
		RuleID:          ruleID,
		RuleNote:        note,
		Reason:          reason,
	}
}

// selectCandidate runs the ordered policy: rules, then heuristics, then the
// default. It reports the destination plus the rule that chose it.
func selectCandidate(policy Policy, input Input) (ModelRef, string, string, string) {
	for _, rule := range policy.Rules {
		if !rule.Enabled || !matches(rule.When, input) {
			continue
		}
		note := rule.Note
		if note == "" {
			note = rule.ID
		}
		return rule.Use, rule.ID, note, "rule \"" + note + "\" matched"
	}
	if policy.AutoHeuristics {
		if ref, id, note, ok := heuristic(policy, input); ok {
			return ref, id, note, note
		}
	}
	return policy.Default, "", "", "no rule matched, using the default model"
}

// heuristic is the built-in prompt-shape classifier that runs after the rule
// list. It only ever picks one of the policy's two declared poles, so an
// operator who changed them changed what it does.
func heuristic(policy Policy, input Input) (ModelRef, string, string, bool) {
	prompt := strings.TrimSpace(input.Prompt)
	length := utf8.RuneCountInString(prompt)

	if !policy.Expensive.empty() {
		if length > longPromptChars {
			return policy.Expensive, HeuristicExpensive, "long prompt, routed to the expensive model", true
		}
		if hardWork.MatchString(prompt) {
			return policy.Expensive,
				HeuristicExpensive,
				"prompt mentions refactor, architecture, debugging, or migration work",
				true
		}
	}
	if !policy.Cheap.empty() {
		if input.Synthetic != "" {
			return policy.Cheap, HeuristicCheap, "platform-issued run, routed to the cheap model", true
		}
		if strings.EqualFold(strings.TrimSpace(input.Mode), "chat") {
			return policy.Cheap, HeuristicCheap, "chat mode, routed to the cheap model", true
		}
		if length > 0 && length < shortPromptChars && !hasCodeFence(prompt) {
			return policy.Cheap, HeuristicCheap, "short prompt with no code block", true
		}
	}
	return ModelRef{}, "", "", false
}

var hardWork = regexp.MustCompile(hardWorkPattern)

// matches evaluates one condition. An unparsable regex never matches: the
// policy normalizer already rejected it on the way in, so reaching here means
// a hand-edited file, and a rule nobody can evaluate must not fire.
func matches(when Condition, input Input) bool {
	switch when.Kind {
	case KindSynthetic:
		if input.Synthetic == "" {
			return false
		}
		if when.Value == SyntheticAny {
			return true
		}
		return strings.EqualFold(input.Synthetic, when.Value)

	case KindPromptShorterThan:
		// A pasted snippet is never a short task however few characters
		// surround it, so a fenced code block disqualifies the prompt
		// regardless of its length.
		length := utf8.RuneCountInString(strings.TrimSpace(input.Prompt))
		return length > 0 && length < parseLength(when.Value) && !hasCodeFence(input.Prompt)

	case KindPromptLongerThan:
		return utf8.RuneCountInString(strings.TrimSpace(input.Prompt)) > parseLength(when.Value)

	case KindModeIs:
		return strings.EqualFold(strings.TrimSpace(input.Mode), when.Value)

	case KindSkillSelected:
		for _, skill := range input.Skills {
			if strings.EqualFold(strings.TrimSpace(skill), when.Value) {
				return true
			}
		}
		return false

	case KindProjectIs:
		return strings.EqualFold(input.ProjectID, when.Value) ||
			(input.ProjectSlug != "" && strings.EqualFold(input.ProjectSlug, when.Value))

	case KindRegex:
		expression, err := regexp.Compile(when.Value)
		if err != nil {
			return false
		}
		return expression.MatchString(input.Prompt)

	default:
		return false
	}
}

func hasCodeFence(prompt string) bool {
	return strings.Contains(prompt, "```")
}
