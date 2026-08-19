// Package routing owns automatic model selection: the operator-editable
// policy stored at DATA_DIR/model-routing.json, and the pure decision that
// turns one prompt into the provider and model that should answer it.
//
// The policy is the whole contract. Nothing here talks to a provider, a
// container, or the ledger: the prompt service asks for a decision and applies
// it, and the usage service records which rule produced it.
package routing

import (
	"errors"
	"regexp"
	"strings"
)

// PolicyVersion is the schema version written to model-routing.json.
const PolicyVersion = 1

var (
	ErrInvalidPolicy = errors.New("invalid model routing policy")
	ErrUnavailable   = errors.New("model routing is unavailable")
)

// ConditionKind names what a rule looks at. Anything else is dropped on the
// way in, so a hand-edited document cannot arm a rule nobody can evaluate.
type ConditionKind string

const (
	// KindSynthetic matches a platform-issued run by its label
	// (servicechat.SyntheticAutoTest, SyntheticTeamReview, ...). The special
	// value "any" matches every synthetic run.
	KindSynthetic ConditionKind = "synthetic"
	// KindPromptShorterThan and KindPromptLongerThan compare the prompt's
	// length in characters against Value parsed as an integer.
	KindPromptShorterThan ConditionKind = "promptShorterThan"
	KindPromptLongerThan  ConditionKind = "promptLongerThan"
	// KindModeIs matches the chat's mode (chat, plan, code, review, ...).
	KindModeIs ConditionKind = "modeIs"
	// KindSkillSelected matches when the named skill is selected for the run.
	KindSkillSelected ConditionKind = "skillSelected"
	// KindProjectIs matches a project by id or slug.
	KindProjectIs ConditionKind = "projectIs"
	// KindRegex matches the prompt against a Go regular expression. An
	// unparsable expression makes the whole policy invalid rather than
	// silently never matching.
	KindRegex ConditionKind = "regex"
)

// RoutedByDefault is the ledger's routedBy value for a run the policy default
// claimed, as opposed to a named rule or a heuristic. It is a reserved id: a
// rule may not be called "default".
const RoutedByDefault = "default"

// SyntheticAny is the KindSynthetic value that matches any platform-issued
// run rather than one particular label.
const SyntheticAny = "any"

// ModelRef is a provider plus the model id inside it. An empty Model means
// "that provider's own default", which is what the composer calls Auto.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	// ReasoningEffort optionally overrides the chat's effort when this
	// reference wins. Empty leaves the chat's own setting alone.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

func (m ModelRef) empty() bool {
	return strings.TrimSpace(m.Provider) == ""
}

func (m ModelRef) normalize() ModelRef {
	return ModelRef{
		Provider:        strings.ToLower(strings.TrimSpace(m.Provider)),
		Model:           strings.TrimSpace(m.Model),
		ReasoningEffort: strings.ToLower(strings.TrimSpace(m.ReasoningEffort)),
	}
}

// Key is the ledger's spelling of a destination: "provider/model", with an
// empty model meaning that provider's own default. The savings report
// classifies a run by comparing this against the policy's two poles, so both
// halves must be written the same way.
func (m ModelRef) Key() string {
	if m.Provider == "" {
		return ""
	}
	return m.Provider + "/" + m.Model
}

// Label renders a reference the way the composer pill and the transcript
// header show it.
func (m ModelRef) Label() string {
	provider := ProviderLabel(m.Provider)
	model := strings.TrimSpace(m.Model)
	if model == "" {
		return provider + " Auto"
	}
	return provider + " " + ModelLabel(m.Provider, model)
}

// Condition is a rule's single test.
type Condition struct {
	Kind  ConditionKind `json:"kind"`
	Value string        `json:"value,omitempty"`
}

// Rule is one row of the policy: a condition and the model that wins when it
// matches. Rules are evaluated in order and the first match stops the scan.
type Rule struct {
	ID   string    `json:"id"`
	When Condition `json:"when"`
	Use  ModelRef  `json:"use"`
	// Note is the human name the transcript header and the savings report
	// show. It is the only part of a rule an operator reads at a glance.
	Note string `json:"note,omitempty"`
	// Enabled is true unless the operator switched this rule off. It is
	// always serialized so an absent key never reads as "off".
	Enabled bool `json:"enabled"`
}

// Policy is the whole document stored at DATA_DIR/model-routing.json.
type Policy struct {
	Version   int   `json:"version"`
	UpdatedAt int64 `json:"updatedAt,omitempty"`
	// Enabled is the master switch. Off means every run uses the model the
	// chat already names, exactly as it did before routing existed.
	Enabled bool `json:"enabled"`
	// Default is the model a routed run falls back to when no rule and no
	// heuristic claims it.
	Default ModelRef `json:"default"`
	Rules   []Rule   `json:"rules"`
	// Cheap and Expensive are the two poles the built-in heuristics and the
	// savings report reason about.
	Cheap     ModelRef `json:"cheapModel"`
	Expensive ModelRef `json:"expensiveModel"`
	// AutoHeuristics turns on the built-in prompt-shape classifier that runs
	// after the rule list. Off leaves the rules as the only routing logic.
	AutoHeuristics bool `json:"autoHeuristics"`
	// UpdatedBy is the administrator who last saved the document.
	UpdatedBy string `json:"updatedBy,omitempty"`
}

// Input is everything a decision may look at. It is filled in by the prompt
// service from the chat's metadata and the prompt about to run.
type Input struct {
	// Pinned is true when the chat is pinned to the model the user picked
	// (chat meta modelPolicy "pinned"). A pinned chat is never routed.
	Pinned bool
	// Provider and Model are what the chat would have used without routing.
	Provider string
	Model    string
	// ReasoningEffort is the chat's own effort setting.
	ReasoningEffort string
	Prompt          string
	Mode            string
	// Synthetic is the platform label of a non-human run, empty for a prompt
	// a person typed.
	Synthetic string
	ProjectID string
	// ProjectSlug lets a projectIs rule name a project the readable way.
	ProjectSlug string
	// Skills are the trigger names selected for this run.
	Skills []string
}

// Decision is what the prompt service applies. Provider and Model are always
// filled in — a decision that routes nowhere simply repeats the chat's own
// choice with Routed false.
type Decision struct {
	Provider        string `json:"provider"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// Routed reports whether routing changed anything. False means the chat's
	// own provider and model are in force.
	Routed bool `json:"routed"`
	// RuleID names the rule that won, empty for a heuristic or the default.
	RuleID string `json:"ruleId,omitempty"`
	// RuleNote is that rule's human name, or the heuristic's.
	RuleNote string `json:"rule,omitempty"`
	// Reason explains the decision in one sentence, including why a fallback
	// happened. It is shown in the transcript header and the policy tester.
	Reason string `json:"reason"`
}

// View is the admin-facing read: the policy plus what the deployment can
// actually route to right now.
type View struct {
	Policy Policy `json:"policy"`
	// Providers lists every provider with a live host credential. A rule
	// pointing anywhere else falls back at run time.
	Providers []string `json:"providers"`
	// Catalog is the model list per provider, in the order the pickers show
	// them.
	Catalog []ProviderModels `json:"catalog"`
}

// ProviderModels is one provider's entry in the catalog.
type ProviderModels struct {
	Provider string        `json:"provider"`
	Label    string        `json:"label"`
	Models   []CatalogItem `json:"models"`
}

// CatalogItem is one selectable model.
type CatalogItem struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// TestInput is a pasted prompt the administrator wants explained.
type TestInput struct {
	Prompt      string   `json:"prompt"`
	Mode        string   `json:"mode,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Model       string   `json:"model,omitempty"`
	Synthetic   string   `json:"synthetic,omitempty"`
	ProjectID   string   `json:"projectId,omitempty"`
	ProjectSlug string   `json:"projectSlug,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	// Pinned asks what a chat pinned to its own model would do. The composer
	// preview sends it so the hint matches the pill.
	Pinned bool `json:"pinned,omitempty"`
}

// Normalize repairs a policy that arrived from an administrator or from a
// hand-edited file: it lower-cases providers, drops rules with no usable
// condition or destination, de-duplicates ids, and rejects a regex that does
// not compile.
func (p Policy) Normalize() (Policy, error) {
	out := Policy{
		Version:        PolicyVersion,
		UpdatedAt:      p.UpdatedAt,
		Enabled:        p.Enabled,
		Default:        p.Default.normalize(),
		Cheap:          p.Cheap.normalize(),
		Expensive:      p.Expensive.normalize(),
		AutoHeuristics: p.AutoHeuristics,
		UpdatedBy:      strings.TrimSpace(p.UpdatedBy),
	}
	if out.Default.empty() {
		return Policy{}, ErrInvalidPolicy
	}
	seen := map[string]bool{}
	for index, rule := range p.Rules {
		normalized, ok, err := normalizeRule(rule, index)
		if err != nil {
			return Policy{}, err
		}
		if !ok || seen[normalized.ID] {
			continue
		}
		seen[normalized.ID] = true
		out.Rules = append(out.Rules, normalized)
	}
	return out, nil
}

func normalizeRule(rule Rule, index int) (Rule, bool, error) {
	kind, ok := normalizeKind(rule.When.Kind)
	if !ok {
		return Rule{}, false, nil
	}
	use := rule.Use.normalize()
	if use.empty() {
		return Rule{}, false, nil
	}
	value := strings.TrimSpace(rule.When.Value)
	switch kind {
	case KindRegex:
		if value == "" {
			return Rule{}, false, nil
		}
		if _, err := regexp.Compile(value); err != nil {
			return Rule{}, false, ErrInvalidPolicy
		}
	case KindPromptShorterThan, KindPromptLongerThan:
		if parseLength(value) <= 0 {
			return Rule{}, false, ErrInvalidPolicy
		}
	case KindSynthetic:
		if value == "" {
			value = SyntheticAny
		}
		value = strings.ToLower(value)
	case KindModeIs:
		value = strings.ToLower(value)
	case KindSkillSelected, KindProjectIs:
		if value == "" {
			return Rule{}, false, nil
		}
	}
	id := strings.TrimSpace(rule.ID)
	if id == "" {
		id = "rule-" + strings.TrimSpace(itoa(index+1))
	}
	return Rule{
		ID:      id,
		When:    Condition{Kind: kind, Value: value},
		Use:     use,
		Note:    strings.TrimSpace(rule.Note),
		Enabled: rule.Enabled,
	}, true, nil
}

func normalizeKind(kind ConditionKind) (ConditionKind, bool) {
	switch ConditionKind(strings.TrimSpace(string(kind))) {
	case KindSynthetic:
		return KindSynthetic, true
	case KindPromptShorterThan:
		return KindPromptShorterThan, true
	case KindPromptLongerThan:
		return KindPromptLongerThan, true
	case KindModeIs:
		return KindModeIs, true
	case KindSkillSelected:
		return KindSkillSelected, true
	case KindProjectIs:
		return KindProjectIs, true
	case KindRegex:
		return KindRegex, true
	default:
		return "", false
	}
}

// Rule looks up one rule by id.
func (p Policy) Rule(id string) (Rule, bool) {
	for _, rule := range p.Rules {
		if rule.ID == id {
			return rule, true
		}
	}
	return Rule{}, false
}

func parseLength(value string) int {
	total := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		total = total*10 + int(r-'0')
		if total > 1_000_000 {
			return 1_000_000
		}
	}
	return total
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
