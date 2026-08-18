package chat

import (
	"strings"
	"time"
)

// Team mode turns one chat into a coordinated multi-agent workflow: the chat
// the human types in is the *implementer*, and the platform runs a *reviewer*
// and a *tester* over the same workspace in companion chats of their own.
//
// The policy lives on the chat because that is the thing an operator switches
// on, and because the orchestrator (internal/service/team) must be able to
// resume a loop after a restart from stored state alone. This file owns the
// vocabulary, the bounds, and the pure patch/normalize rules; the decisions
// that spend a loop live in the team service.

// Team role names. They are stored on the companion chat as CompanionRole, so
// they are part of the persisted shape and must stay stable.
const (
	// TeamRoleImplementer is the chat the human prompts. It has no companion
	// chat of its own — it *is* the parent.
	TeamRoleImplementer = "implementer"
	// TeamRoleReviewer reads the diff with fresh eyes and returns a verdict.
	TeamRoleReviewer = "reviewer"
	// TeamRoleTester runs the Playwright pass and returns a verdict.
	TeamRoleTester = "tester"
)

// Team loop phases. The empty phase is "idle": armed but between loops.
const (
	TeamPhaseIdle      = ""
	TeamPhaseReviewing = "reviewing"
	TeamPhaseTesting   = "testing"
	TeamPhaseFixing    = "fixing"
	TeamPhaseDone      = "done"
	TeamPhaseError     = "error"
)

// Team verdicts. Ship/Fix come from the reviewer, Pass/Fail from the tester,
// and Unknown is what an agent that ignored the marker instruction produces.
const (
	TeamVerdictShip    = "ship"
	TeamVerdictFix     = "fix"
	TeamVerdictPass    = "pass"
	TeamVerdictFail    = "fail"
	TeamVerdictUnknown = "unknown"
)

// Loop bounds. Two loops is the default because the second one is where a
// review usually stops finding new things; the cap exists so a mis-typed API
// call cannot bill an unattended review-fix cycle all night.
const (
	DefaultTeamLoops = 2
	MinTeamLoops     = 1
	MaxTeamLoops     = 5

	// MaxTeamHops bounds the stored timeline. A loop writes at most three
	// hops, so this keeps several sessions of history without letting
	// meta.json grow without limit.
	MaxTeamHops = 24
)

// TeamPolicy is a chat's team-mode configuration and the live state of its
// current loop. Configuration is written by the operator through PATCH; the
// state half (Phase, LoopsUsed, Verdict, Hops, and the role chat ids) is
// written only by the team service.
type TeamPolicy struct {
	Enabled bool      `json:"enabled"`
	Roles   TeamRoles `json:"roles"`
	// MaxLoops caps how many review→fix rounds one team session may spend.
	MaxLoops int `json:"maxLoops,omitempty"`
	// AutoFix decides whether a FIX verdict is sent back to the implementer
	// automatically. With it off the loop stops and reports instead.
	AutoFix bool `json:"autoFix"`

	// Phase is where the loop currently is; LoopsUsed counts the fix rounds
	// already spent. Both are read by the header pill.
	Phase     string `json:"phase,omitempty"`
	LoopsUsed int    `json:"loopsUsed,omitempty"`
	// Verdict is the last verdict the loop settled on.
	Verdict string `json:"verdict,omitempty"`
	// Hops is the timeline the Team panel renders, oldest first.
	Hops []TeamHop `json:"hops,omitempty"`
	// EnabledBy is the human who switched team mode on. Every synthetic run
	// the loop starts is attributed to them.
	EnabledBy string `json:"enabledBy,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// TeamRoles is the cast. The implementer is always the parent chat; the other
// two get companion chats.
type TeamRoles struct {
	Implementer TeamRole `json:"implementer"`
	Reviewer    TeamRole `json:"reviewer"`
	Tester      TeamRole `json:"tester"`
}

// TeamRole is one seat: which provider fills it, optionally which model, and
// — for the reviewer and tester — whether it takes part at all.
//
// An empty Provider means "the platform picks", which is how a chat armed
// before a second provider was connected still gets a fresh-eyes reviewer once
// one is.
type TeamRole struct {
	Provider Provider `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Enabled  bool     `json:"enabled"`
	// ChatID is the companion chat this role runs in. It is empty for the
	// implementer, and assigned by the team service on the first hop.
	ChatID ID `json:"chatId,omitempty"`
}

// TeamHop is one recorded step of the loop: who ran, what it decided, and
// which chat the operator can open to read it in full.
type TeamHop struct {
	// Loop is the fix round this hop belongs to, counting from zero.
	Loop int    `json:"loop"`
	Role string `json:"role"`
	// Kind is the synthetic label of the run this hop started, so the panel
	// and the audit log use the same word.
	Kind     string `json:"kind,omitempty"`
	ChatID   ID     `json:"chatId,omitempty"`
	Verdict  string `json:"verdict,omitempty"`
	Findings string `json:"findings,omitempty"`
	At       int64  `json:"at,omitempty"`
}

// TeamInput patches the configuration half of the policy. Every field is
// optional so the composer can flip the switch without restating the cast.
type TeamInput struct {
	Enabled  *bool           `json:"enabled,omitempty"`
	Roles    *TeamRolesInput `json:"roles,omitempty"`
	MaxLoops *int            `json:"maxLoops,omitempty"`
	AutoFix  *bool           `json:"autoFix,omitempty"`
}

// TeamRolesInput patches individual seats. An absent seat is left alone.
type TeamRolesInput struct {
	Implementer *TeamRoleInput `json:"implementer,omitempty"`
	Reviewer    *TeamRoleInput `json:"reviewer,omitempty"`
	Tester      *TeamRoleInput `json:"tester,omitempty"`
}

// TeamRoleInput patches one seat.
type TeamRoleInput struct {
	Provider *Provider `json:"provider,omitempty"`
	Model    *string   `json:"model,omitempty"`
	Enabled  *bool     `json:"enabled,omitempty"`
}

// KnownProvider reports whether a provider string is one the platform runs.
// NormalizeProvider deliberately collapses anything unknown to codex, which is
// the right default for a chat but the wrong answer for validating a team seat
// — "gpt5" must be refused, not silently turned into Codex.
func KnownProvider(provider Provider) bool {
	switch provider {
	case ProviderClaude, ProviderCodex, ProviderKimi, ProviderAntigravity:
		return true
	default:
		return false
	}
}

// NormalizeTeamProvider keeps "unset" distinguishable from "codex". An empty
// seat provider means the team service resolves it against whatever providers
// are actually connected at run time.
func NormalizeTeamProvider(provider Provider) Provider {
	trimmed := Provider(strings.ToLower(strings.TrimSpace(string(provider))))
	if trimmed == "" || !KnownProvider(trimmed) {
		return ""
	}
	return trimmed
}

// NormalizeTeamRole trims one seat.
func NormalizeTeamRole(role TeamRole) TeamRole {
	role.Provider = NormalizeTeamProvider(role.Provider)
	role.Model = strings.TrimSpace(role.Model)
	return role
}

// NormalizeTeam fills in the defaults and clamps the limits of a stored
// policy. Like NormalizeAutopilot it runs on the way out of the store, so a
// chat written before team mode existed answers the orchestrator's questions
// with the documented defaults rather than a zeroed loop budget.
func NormalizeTeam(policy TeamPolicy) TeamPolicy {
	policy.MaxLoops = clamp(policy.MaxLoops, DefaultTeamLoops, MinTeamLoops, MaxTeamLoops)
	if policy.LoopsUsed < 0 {
		policy.LoopsUsed = 0
	}
	policy.Roles.Implementer = NormalizeTeamRole(policy.Roles.Implementer)
	policy.Roles.Reviewer = NormalizeTeamRole(policy.Roles.Reviewer)
	policy.Roles.Tester = NormalizeTeamRole(policy.Roles.Tester)
	// The implementer is the chat itself; a stored "disabled implementer"
	// would mean a team with nobody writing code.
	policy.Roles.Implementer.Enabled = true
	policy.Roles.Implementer.ChatID = ""
	policy.Phase = NormalizeTeamPhase(policy.Phase)
	policy.Verdict = NormalizeTeamVerdict(policy.Verdict)
	policy.EnabledBy = strings.TrimSpace(policy.EnabledBy)
	policy.Hops = trimTeamHops(policy.Hops)
	return policy
}

// NormalizeTeamPhase maps an unrecognized phase to idle. A phase nobody can
// interpret must not leave the header pill claiming work is in flight.
func NormalizeTeamPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case TeamPhaseReviewing:
		return TeamPhaseReviewing
	case TeamPhaseTesting:
		return TeamPhaseTesting
	case TeamPhaseFixing:
		return TeamPhaseFixing
	case TeamPhaseDone:
		return TeamPhaseDone
	case TeamPhaseError:
		return TeamPhaseError
	default:
		return TeamPhaseIdle
	}
}

// NormalizeTeamVerdict maps an unrecognized verdict to "". Only "unknown" is
// kept as a value of its own, because "the agent gave no verdict" is a
// different fact from "no verdict has been reached yet".
func NormalizeTeamVerdict(verdict string) string {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case TeamVerdictShip:
		return TeamVerdictShip
	case TeamVerdictFix:
		return TeamVerdictFix
	case TeamVerdictPass:
		return TeamVerdictPass
	case TeamVerdictFail:
		return TeamVerdictFail
	case TeamVerdictUnknown:
		return TeamVerdictUnknown
	default:
		return ""
	}
}

// TeamActive reports whether a loop currently has a hop in flight. The
// post-run driver reads it to stand down: autopilot and the team loop would
// otherwise both prompt the implementer chat.
func TeamActive(policy TeamPolicy) bool {
	if !policy.Enabled {
		return false
	}
	switch NormalizeTeamPhase(policy.Phase) {
	case TeamPhaseReviewing, TeamPhaseTesting, TeamPhaseFixing:
		return true
	default:
		return false
	}
}

// ValidTeamInput reports whether a patch is inside the accepted bounds.
// Absent fields are always valid: they mean "leave it".
func ValidTeamInput(in TeamInput) bool {
	if in.MaxLoops != nil && (*in.MaxLoops < MinTeamLoops || *in.MaxLoops > MaxTeamLoops) {
		return false
	}
	if in.Roles == nil {
		return true
	}
	for _, role := range []*TeamRoleInput{in.Roles.Implementer, in.Roles.Reviewer, in.Roles.Tester} {
		if role == nil || role.Provider == nil {
			continue
		}
		// An explicit empty provider is the documented way to hand the choice
		// back to the platform, so only a non-empty unknown value is refused.
		if *role.Provider != "" && !KnownProvider(NormalizeTeamProvider(*role.Provider)) {
			return false
		}
	}
	return true
}

// ApplyTeam folds a patch into the stored policy.
//
// Switching team mode on is the transition that resets the loop: the counter
// goes back to zero, the phase to idle, and the previous timeline is dropped,
// so re-arming a chat that already spent its budget actually gives it a fresh
// one. The companion chat ids deliberately survive — reusing the reviewer's
// chat is what keeps its history in one place instead of littering the
// project with one chat per session.
func ApplyTeam(policy TeamPolicy, in TeamInput, actorEmail string, now time.Time) TeamPolicy {
	policy = NormalizeTeam(policy)
	if in.MaxLoops != nil {
		policy.MaxLoops = clamp(*in.MaxLoops, DefaultTeamLoops, MinTeamLoops, MaxTeamLoops)
	}
	if in.AutoFix != nil {
		policy.AutoFix = *in.AutoFix
	}
	if in.Roles != nil {
		policy.Roles.Implementer = applyTeamRole(policy.Roles.Implementer, in.Roles.Implementer)
		policy.Roles.Reviewer = applyTeamRole(policy.Roles.Reviewer, in.Roles.Reviewer)
		policy.Roles.Tester = applyTeamRole(policy.Roles.Tester, in.Roles.Tester)
		policy.Roles.Implementer.Enabled = true
		policy.Roles.Implementer.ChatID = ""
	}
	if in.Enabled == nil || *in.Enabled == policy.Enabled {
		policy.UpdatedAt = now.UnixMilli()
		return policy
	}

	policy.Enabled = *in.Enabled
	policy.Phase = TeamPhaseIdle
	policy.UpdatedAt = now.UnixMilli()
	if !policy.Enabled {
		return policy
	}
	policy.LoopsUsed = 0
	policy.Verdict = ""
	policy.Hops = nil
	if actor := strings.TrimSpace(actorEmail); actor != "" {
		policy.EnabledBy = actor
	}
	return policy
}

// AppendTeamHop adds one step to the stored timeline, keeping only the most
// recent MaxTeamHops entries.
func AppendTeamHop(hops []TeamHop, hop TeamHop) []TeamHop {
	hop.Role = strings.TrimSpace(hop.Role)
	hop.Verdict = NormalizeTeamVerdict(hop.Verdict)
	hop.Findings = strings.TrimSpace(hop.Findings)
	return trimTeamHops(append(hops, hop))
}

func applyTeamRole(role TeamRole, in *TeamRoleInput) TeamRole {
	if in == nil {
		return role
	}
	if in.Provider != nil {
		next := NormalizeTeamProvider(*in.Provider)
		// A seat that changes provider cannot keep its companion chat: the
		// chat's own provider, session, and skills all belong to the old one.
		if next != role.Provider {
			role.ChatID = ""
		}
		role.Provider = next
	}
	if in.Model != nil {
		role.Model = strings.TrimSpace(*in.Model)
	}
	if in.Enabled != nil {
		role.Enabled = *in.Enabled
	}
	return role
}

func trimTeamHops(hops []TeamHop) []TeamHop {
	if len(hops) == 0 {
		return nil
	}
	if len(hops) <= MaxTeamHops {
		return hops
	}
	return append([]TeamHop(nil), hops[len(hops)-MaxTeamHops:]...)
}
