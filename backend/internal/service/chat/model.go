package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ID string
type ProjectID string
type Provider string

const (
	ProviderClaude      Provider = "claude"
	ProviderCodex       Provider = "codex"
	ProviderKimi        Provider = "kimi"
	ProviderAntigravity Provider = "antigravity"
)

type Meta struct {
	ID                   ID       `json:"id"`
	Title                string   `json:"title"`
	Provider             Provider `json:"provider,omitempty"`
	ClaudeSessionID      string   `json:"claudeSessionId,omitempty"`
	CodexSessionID       string   `json:"codexSessionId,omitempty"`
	KimiSessionID        string   `json:"kimiSessionId,omitempty"`
	AntigravitySessionID string   `json:"antigravitySessionId,omitempty"`
	TmuxSession          string   `json:"tmuxSession,omitempty"`
	Cwd                  string   `json:"cwd,omitempty"`
	CreatedAt            int64    `json:"createdAt"`
	LastMessageAt        int64    `json:"lastMessageAt"`
	LastReadAt           int64    `json:"lastReadAt,omitempty"`
	Running              bool     `json:"running,omitempty"`
	Model                string   `json:"model,omitempty"`
	Mode                 string   `json:"mode,omitempty"`
	ReasoningEffort      string   `json:"reasoningEffort,omitempty"`
	ServiceTier          string   `json:"serviceTier,omitempty"`
	// ModelPolicy is who picks the model for the next turn: "pinned" (the
	// default) uses the Provider/Model above exactly as the user chose them,
	// "auto" hands the choice to the platform's model-routing policy. Always
	// serialized so the browser never has to guess what an absent key means.
	ModelPolicy string `json:"modelPolicy"`
	// EndpointID points this chat's agent at one of the platform's
	// third-party agent endpoints (see internal/service/agentendpoints).
	// Empty — the default, and what every chat had before the register
	// existed — means the vendor's own endpoint: today's behaviour exactly.
	//
	// An endpoint pins the chat. It decides which CLI runs, and its model
	// list is what Model is chosen from, so a chat carrying one is not
	// offered to the automatic model router: there is nothing left to route.
	EndpointID string `json:"endpointId,omitempty"`
	// DirectModel answers this chat with a plain completion API — one of the
	// free-tier pool providers, or the local auxiliary model — instead of an
	// agent CLI in the project's container.
	//
	// Those models have no tools: no files, no shell, no repository. A chat
	// carrying one can answer questions and draft text and cannot change
	// anything, which is why it is a separate field rather than another value
	// of Provider. The zero value means the chat runs an agent, which is what
	// every chat did before this existed.
	DirectModel    DirectModel `json:"directModel,omitempty"`
	ProjectID      ProjectID   `json:"projectId,omitempty"`
	ForkPending    bool        `json:"forkPending,omitempty"`
	SelectedSkills []SkillRef  `json:"selectedSkills,omitempty"`
	// Summary is a one-line description of what this chat is about, written
	// by the optional auxiliary model after a run settles (see
	// internal/service/auxmodel). It is a search and scanning aid shown as a
	// subtitle in the sidebar and on the dashboard; a deployment without the
	// auxiliary model simply never has one, and nothing depends on it.
	Summary string `json:"summary,omitempty"`
	// Autopilot and AutoTest are the chat's post-run policies: what the
	// platform does on its own once an agent turn settles. Both default off,
	// and both are always serialized so the browser never has to guess
	// whether an absent key means "off" or "unknown".
	Autopilot AutopilotPolicy `json:"autopilot"`
	AutoTest  AutoTestPolicy  `json:"autoTest"`
	// Team is the chat's multi-agent workflow: implementer → reviewer →
	// tester, driven by internal/service/team. Always serialized for the same
	// reason the two policies above are.
	Team TeamPolicy `json:"team"`
	// CompanionOf names the parent chat when this chat is a team companion —
	// the reviewer's or tester's own thread. Companions are hidden from the
	// sidebar and opened from the parent's Team panel instead, so a team
	// session adds one row to the chat list rather than three.
	CompanionOf ID `json:"companionOf,omitempty"`
	// CompanionRole is which seat this companion fills (see TeamRoleReviewer
	// and TeamRoleTester). Empty on an ordinary chat.
	CompanionRole string `json:"companionRole,omitempty"`
}

// AutopilotPolicy keeps a chat working while the operator is away: every time
// the agent ends its turn without declaring the task done, the post-run driver
// sends one more "keep going" prompt. The counters are the safety rails — a
// loop that neither finishes nor errors still has to stop.
type AutopilotPolicy struct {
	Enabled bool `json:"enabled"`
	// MaxRounds caps how many synthetic continue prompts one autopilot
	// session may send. RoundsUsed counts the ones already spent.
	MaxRounds  int `json:"maxRounds,omitempty"`
	RoundsUsed int `json:"roundsUsed,omitempty"`
	// MaxDurationMin is the wall-clock budget measured from StartedAt, so a
	// slow agent cannot outlive the round cap by running long turns.
	MaxDurationMin int   `json:"maxDurationMin,omitempty"`
	StartedAt      int64 `json:"startedAt,omitempty"`
	// EnabledBy is the human who armed the loop. Synthetic runs are attributed
	// to them, because nobody else consented to the work.
	EnabledBy string `json:"enabledBy,omitempty"`
}

// AutoTestPolicy asks for a Playwright verification pass after every agent
// turn that changed something.
type AutoTestPolicy struct {
	Enabled   bool   `json:"enabled"`
	EnabledBy string `json:"enabledBy,omitempty"`
}

type SkillRef struct {
	Name     string   `json:"name"`
	Command  string   `json:"command,omitempty"`
	Provider Provider `json:"provider,omitempty"`
	Source   string   `json:"source,omitempty"`
}

type Event struct {
	Seq                  int64           `json:"seq,omitempty"`
	T                    int64           `json:"t"`
	Type                 string          `json:"type"`
	Text                 string          `json:"text,omitempty"`
	MessageID            string          `json:"messageId,omitempty"`
	ID                   string          `json:"id,omitempty"`
	Name                 string          `json:"name,omitempty"`
	Input                json.RawMessage `json:"input,omitempty"`
	Output               string          `json:"output,omitempty"`
	IsError              bool            `json:"isError,omitempty"`
	ToolName             string          `json:"toolName,omitempty"`
	Subtype              string          `json:"subtype,omitempty"`
	Data                 json.RawMessage `json:"data,omitempty"`
	ClaudeSessionID      string          `json:"claudeSessionId,omitempty"`
	CodexSessionID       string          `json:"codexSessionId,omitempty"`
	KimiSessionID        string          `json:"kimiSessionId,omitempty"`
	AntigravitySessionID string          `json:"antigravitySessionId,omitempty"`
	Provider             Provider        `json:"provider,omitempty"`
	Usage                json.RawMessage `json:"usage,omitempty"`
	Message              string          `json:"message,omitempty"`
	Running              bool            `json:"running,omitempty"`
	// Routing records which model actually answered this turn and why. It is
	// written onto the turn's user event, so reading a transcript back tells
	// the operator what a routed run cost them without consulting the ledger.
	// Nil on every turn that ran before routing existed, and on every turn a
	// chat answered with its own pinned model.
	Routing *EventRouting `json:"routing,omitempty"`
	// Synthetic labels a prompt the platform sent on the operator's behalf
	// (see SyntheticAutopilot / SyntheticAutoTest). Empty means a human typed
	// it. The browser badges the bubble so an unattended round is never
	// mistaken for something the operator asked for.
	Synthetic string `json:"synthetic,omitempty"`
}

// EventRouting is the audit trail of one automatic model-routing decision.
type EventRouting struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	// RuleID is the policy rule that won, empty when the default model or a
	// built-in heuristic decided.
	RuleID string `json:"ruleId,omitempty"`
	// Rule is that rule's human name, which is what the transcript shows.
	Rule string `json:"rule,omitempty"`
	// Reason is the one-sentence explanation, including any fallback.
	Reason string `json:"reason,omitempty"`
}

type EventPageQuery struct {
	Limit     int
	BeforeSeq int64
}

type EventPage struct {
	Events     []Event `json:"events"`
	NextBefore int64   `json:"nextBefore,omitempty"`
	LastSeq    int64   `json:"lastSeq"`
	HasMore    bool    `json:"hasMore"`
}

type CreateInput struct {
	Title           string      `json:"title,omitempty"`
	TmuxSession     string      `json:"tmuxSession,omitempty"`
	Cwd             string      `json:"cwd,omitempty"`
	Provider        Provider    `json:"provider,omitempty"`
	Model           string      `json:"model,omitempty"`
	Mode            string      `json:"mode,omitempty"`
	ReasoningEffort string      `json:"reasoningEffort,omitempty"`
	ServiceTier     string      `json:"serviceTier,omitempty"`
	ModelPolicy     string      `json:"modelPolicy,omitempty"`
	EndpointID      string      `json:"endpointId,omitempty"`
	DirectModel     DirectModel `json:"directModel,omitempty"`
	ProjectID       ProjectID   `json:"projectId,omitempty"`
	SelectedSkills  []SkillRef  `json:"selectedSkills,omitempty"`
	// CompanionOf and CompanionRole are set only by the team service when it
	// creates a reviewer or tester thread. They are decoded from a request
	// body like every other field, but the chat handler never reaches this
	// path with them set — a client that sends them just gets a hidden chat
	// it can still open, which is harmless.
	CompanionOf   ID     `json:"companionOf,omitempty"`
	CompanionRole string `json:"companionRole,omitempty"`
}

type UpdateInput struct {
	Title           *string   `json:"title,omitempty"`
	Cwd             *string   `json:"cwd,omitempty"`
	Provider        *Provider `json:"provider,omitempty"`
	Model           *string   `json:"model,omitempty"`
	Mode            *string   `json:"mode,omitempty"`
	ReasoningEffort *string   `json:"reasoningEffort,omitempty"`
	ServiceTier     *string   `json:"serviceTier,omitempty"`
	// ModelPolicy switches this chat between its pinned model and automatic
	// routing. Absent leaves the stored choice alone.
	ModelPolicy *string `json:"modelPolicy,omitempty"`
	// EndpointID repoints the chat at a third-party agent endpoint, or at the
	// vendor's own default when set to "". Absent leaves the stored choice
	// alone.
	EndpointID *string `json:"endpointId,omitempty"`
	// DirectModel repoints the chat at a completion-API model, or back at an
	// agent when set to the zero value. Absent leaves the stored choice alone.
	DirectModel    *DirectModel `json:"directModel,omitempty"`
	SelectedSkills *[]SkillRef  `json:"selectedSkills,omitempty"`
	// Autopilot and AutoTest patch the post-run policies. Absent leaves the
	// stored policy alone; present replaces only the fields it names.
	Autopilot *AutopilotInput `json:"autopilot,omitempty"`
	AutoTest  *AutoTestInput  `json:"autoTest,omitempty"`
	// Team patches the multi-agent workflow. Only its configuration half is
	// reachable from here: the loop counters, phase, and companion chat ids
	// belong to the team service.
	Team *TeamInput `json:"team,omitempty"`
	// ActorEmail is the caller the handler resolved from the session. It is
	// never decoded from a request body, because it decides who a synthetic
	// run is attributed to.
	ActorEmail string `json:"-"`
}

// AutopilotInput patches the autopilot policy. Every field is optional so the
// composer can flip the switch without restating the limits.
type AutopilotInput struct {
	Enabled        *bool `json:"enabled,omitempty"`
	MaxRounds      *int  `json:"maxRounds,omitempty"`
	MaxDurationMin *int  `json:"maxDurationMin,omitempty"`
}

// AutoTestInput patches the auto-test policy.
type AutoTestInput struct {
	Enabled *bool `json:"enabled,omitempty"`
}

func NormalizeProvider(provider Provider) Provider {
	switch provider {
	case ProviderClaude:
		return ProviderClaude
	case ProviderKimi:
		return ProviderKimi
	case ProviderAntigravity:
		return ProviderAntigravity
	default:
		return ProviderCodex
	}
}

func NormalizeReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none":
		return "none"
	case "minimal":
		return "minimal"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "xhigh":
		return "xhigh"
	case "max":
		return "max"
	case "ultra":
		return "ultra"
	default:
		return ""
	}
}

// Model policies: who picks the model for the next turn.
const (
	// ModelPolicyPinned uses the chat's own provider and model, which is what
	// every chat did before automatic routing existed and is still the
	// default for a new one.
	ModelPolicyPinned = "pinned"
	// ModelPolicyAuto hands the choice to the platform routing policy.
	ModelPolicyAuto = "auto"
)

// NormalizeEndpointID trims a third-party endpoint reference. The register
// decides whether the id exists and whether it is enabled; a chat only stores
// the handle, so a profile deleted after a chat was pointed at it simply
// stops resolving and the run says so.
func NormalizeEndpointID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

// NormalizeModelPolicy collapses anything unrecognized onto "pinned", so a
// document written before this field existed, or a client that sends noise,
// keeps today's behaviour.
func NormalizeModelPolicy(policy string) string {
	if strings.EqualFold(strings.TrimSpace(policy), ModelPolicyAuto) {
		return ModelPolicyAuto
	}
	return ModelPolicyPinned
}

// NormalizeServiceTier maps codex service_tier values we expose (default,
// priority, fast). "" = Auto (omit the flag). Unknown values collapse to "".
func NormalizeServiceTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "default":
		return "default"
	case "priority":
		return "priority"
	case "fast":
		return "fast"
	default:
		return ""
	}
}

func NormalizeSelectedSkills(skills []SkillRef, fallbackProvider Provider) []SkillRef {
	fallbackProvider = NormalizeProvider(fallbackProvider)
	seen := map[string]bool{}
	normalized := make([]SkillRef, 0, len(skills))
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		command := strings.TrimSpace(skill.Command)
		source := strings.TrimSpace(skill.Source)
		if command == "" {
			command = name
		}
		if name == "" {
			name = command
		}
		if name == "" || command == "" {
			continue
		}

		provider := skill.Provider
		if provider == "" {
			provider = fallbackProvider
		} else {
			provider = NormalizeProvider(provider)
		}
		key := strings.ToLower(string(provider)) + "\x00" + strings.ToLower(source) + "\x00" + strings.ToLower(command)
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, SkillRef{
			Name:     name,
			Command:  command,
			Provider: provider,
			Source:   source,
		})
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func ValidID(id ID) bool {
	if len(id) < 4 || len(id) > 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// TitleFromPrompt produces a short summary used when a chat is created with
// no explicit title. First 60 chars of the first prompt, single line.
func TitleFromPrompt(prompt string) string {
	t := strings.TrimSpace(prompt)
	t = strings.ReplaceAll(t, "\n", " ")
	t = strings.ReplaceAll(t, "\r", " ")
	for strings.Contains(t, "  ") {
		t = strings.ReplaceAll(t, "  ", " ")
	}
	if len(t) > 60 {
		t = t[:60] + "..."
	}
	if t == "" {
		t = fmt.Sprintf("Chat %s", time.Now().Format("Jan 2 15:04"))
	}
	return t
}

// DirectSource names where a chat's direct model comes from. The two differ in
// what an operator fixes when one stops answering — a pool provider's key and
// quota, or the local model's daemon — so the chat stores which it is rather
// than inferring it from a model id.
type DirectSource string

const (
	// DirectSourcePool is one provider in the free-tier pool.
	DirectSourcePool DirectSource = "pool"
	// DirectSourceLocal is the local auxiliary model.
	DirectSourceLocal DirectSource = "local"
)

// DirectModel is a chat's completion-API model. The zero value means the chat
// runs an agent.
type DirectModel struct {
	Source DirectSource `json:"source,omitempty"`
	// ProviderID is the pool provider's registry id, empty for the local
	// model since there is only one of those.
	ProviderID string `json:"providerId,omitempty"`
	Model      string `json:"model,omitempty"`
}

// Set reports whether this chat answers from a completion API.
func (m DirectModel) Set() bool { return m.Source != "" }

// Valid reports whether a stored choice is coherent enough to act on. An
// incoherent one is treated as unset, so a hand-edited document degrades to
// "this chat runs an agent" rather than to a run that cannot start.
func (m DirectModel) Valid() bool {
	switch m.Source {
	case "":
		return true
	case DirectSourceLocal:
		return true
	case DirectSourcePool:
		return strings.TrimSpace(m.ProviderID) != ""
	default:
		return false
	}
}

// NormalizeDirectModel trims a stored or submitted choice and drops one that
// makes no sense. An incoherent value becomes the zero value — "this chat runs
// an agent" — because that is the behaviour every chat already has and the one
// that cannot fail to start.
func NormalizeDirectModel(m DirectModel) DirectModel {
	m.Source = DirectSource(strings.ToLower(strings.TrimSpace(string(m.Source))))
	m.ProviderID = strings.ToLower(strings.TrimSpace(m.ProviderID))
	m.Model = strings.TrimSpace(m.Model)
	if !m.Valid() {
		return DirectModel{}
	}
	if m.Source == DirectSourceLocal {
		// The local model has one provider by definition; carrying an id
		// would just be something else to keep in sync.
		m.ProviderID = ""
	}
	return m
}
