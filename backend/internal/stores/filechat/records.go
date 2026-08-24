package filechat

import (
	"encoding/json"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

type metaRecord struct {
	ID              string                  `json:"id"`
	Title           string                  `json:"title"`
	Provider        string                  `json:"provider,omitempty"`
	ClaudeSessionID string                  `json:"claudeSessionId,omitempty"`
	CodexSessionID  string                  `json:"codexSessionId,omitempty"`
	KimiSessionID   string                  `json:"kimiSessionId,omitempty"`
	TmuxSession     string                  `json:"tmuxSession,omitempty"`
	Cwd             string                  `json:"cwd,omitempty"`
	CreatedAt       int64                   `json:"createdAt"`
	LastMessageAt   int64                   `json:"lastMessageAt"`
	LastReadAt      int64                   `json:"lastReadAt,omitempty"`
	Model           string                  `json:"model,omitempty"`
	Mode            string                  `json:"mode,omitempty"`
	ReasoningEffort string                  `json:"reasoningEffort,omitempty"`
	ServiceTier     string                  `json:"serviceTier,omitempty"`
	ModelPolicy     string                  `json:"modelPolicy,omitempty"`
	EndpointID      string                  `json:"endpointId,omitempty"`
	DirectModel     servicechat.DirectModel `json:"directModel,omitempty"`
	ProjectID       string                  `json:"projectId,omitempty"`
	ForkPending     bool                    `json:"forkPending,omitempty"`
	SelectedSkills  []skillRefRecord        `json:"selectedSkills,omitempty"`
	Autopilot       autopilotRecord         `json:"autopilot,omitempty"`
	AutoTest        autoTestRecord          `json:"autoTest,omitempty"`
	Team            teamRecord              `json:"team,omitempty"`
	CompanionOf     string                  `json:"companionOf,omitempty"`
	CompanionRole   string                  `json:"companionRole,omitempty"`
}

// teamRecord is the persisted shape of a chat's multi-agent workflow. It
// mirrors servicechat.TeamPolicy field for field so the store stays the only
// place that knows the on-disk names.
type teamRecord struct {
	Enabled   bool            `json:"enabled,omitempty"`
	Roles     teamRolesRecord `json:"roles,omitempty"`
	MaxLoops  int             `json:"maxLoops,omitempty"`
	AutoFix   bool            `json:"autoFix,omitempty"`
	Phase     string          `json:"phase,omitempty"`
	LoopsUsed int             `json:"loopsUsed,omitempty"`
	Verdict   string          `json:"verdict,omitempty"`
	Hops      []teamHopRecord `json:"hops,omitempty"`
	EnabledBy string          `json:"enabledBy,omitempty"`
	UpdatedAt int64           `json:"updatedAt,omitempty"`
}

type teamRolesRecord struct {
	Implementer teamRoleRecord `json:"implementer,omitempty"`
	Reviewer    teamRoleRecord `json:"reviewer,omitempty"`
	Tester      teamRoleRecord `json:"tester,omitempty"`
}

type teamRoleRecord struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Enabled  bool   `json:"enabled,omitempty"`
	ChatID   string `json:"chatId,omitempty"`
}

type teamHopRecord struct {
	Loop     int    `json:"loop,omitempty"`
	Role     string `json:"role,omitempty"`
	Kind     string `json:"kind,omitempty"`
	ChatID   string `json:"chatId,omitempty"`
	Verdict  string `json:"verdict,omitempty"`
	Findings string `json:"findings,omitempty"`
	At       int64  `json:"at,omitempty"`
}

type autopilotRecord struct {
	Enabled        bool   `json:"enabled,omitempty"`
	MaxRounds      int    `json:"maxRounds,omitempty"`
	RoundsUsed     int    `json:"roundsUsed,omitempty"`
	MaxDurationMin int    `json:"maxDurationMin,omitempty"`
	StartedAt      int64  `json:"startedAt,omitempty"`
	EnabledBy      string `json:"enabledBy,omitempty"`
}

type autoTestRecord struct {
	Enabled   bool   `json:"enabled,omitempty"`
	EnabledBy string `json:"enabledBy,omitempty"`
}

type skillRefRecord struct {
	Name     string `json:"name"`
	Command  string `json:"command,omitempty"`
	Provider string `json:"provider,omitempty"`
	Source   string `json:"source,omitempty"`
}

func metaRecordFromDomain(m servicechat.Meta) metaRecord {
	return metaRecord{
		ID:              string(m.ID),
		Title:           m.Title,
		Provider:        string(m.Provider),
		ClaudeSessionID: m.ClaudeSessionID,
		CodexSessionID:  m.CodexSessionID,
		KimiSessionID:   m.KimiSessionID,
		TmuxSession:     m.TmuxSession,
		Cwd:             m.Cwd,
		CreatedAt:       m.CreatedAt,
		LastMessageAt:   m.LastMessageAt,
		LastReadAt:      m.LastReadAt,
		Model:           m.Model,
		Mode:            m.Mode,
		ReasoningEffort: m.ReasoningEffort,
		ServiceTier:     m.ServiceTier,
		ModelPolicy:     m.ModelPolicy,
		EndpointID:      m.EndpointID,
		DirectModel:     m.DirectModel,
		ProjectID:       string(m.ProjectID),
		ForkPending:     m.ForkPending,
		SelectedSkills:  skillRefRecordsFromDomain(m.SelectedSkills),
		Autopilot: autopilotRecord{
			Enabled:        m.Autopilot.Enabled,
			MaxRounds:      m.Autopilot.MaxRounds,
			RoundsUsed:     m.Autopilot.RoundsUsed,
			MaxDurationMin: m.Autopilot.MaxDurationMin,
			StartedAt:      m.Autopilot.StartedAt,
			EnabledBy:      m.Autopilot.EnabledBy,
		},
		AutoTest: autoTestRecord{
			Enabled:   m.AutoTest.Enabled,
			EnabledBy: m.AutoTest.EnabledBy,
		},
		Team:          teamRecordFromDomain(m.Team),
		CompanionOf:   string(m.CompanionOf),
		CompanionRole: m.CompanionRole,
	}
}

func teamRecordFromDomain(policy servicechat.TeamPolicy) teamRecord {
	return teamRecord{
		Enabled: policy.Enabled,
		Roles: teamRolesRecord{
			Implementer: teamRoleRecordFromDomain(policy.Roles.Implementer),
			Reviewer:    teamRoleRecordFromDomain(policy.Roles.Reviewer),
			Tester:      teamRoleRecordFromDomain(policy.Roles.Tester),
		},
		MaxLoops:  policy.MaxLoops,
		AutoFix:   policy.AutoFix,
		Phase:     policy.Phase,
		LoopsUsed: policy.LoopsUsed,
		Verdict:   policy.Verdict,
		Hops:      teamHopRecordsFromDomain(policy.Hops),
		EnabledBy: policy.EnabledBy,
		UpdatedAt: policy.UpdatedAt,
	}
}

func teamRoleRecordFromDomain(role servicechat.TeamRole) teamRoleRecord {
	return teamRoleRecord{
		Provider: string(role.Provider),
		Model:    role.Model,
		Enabled:  role.Enabled,
		ChatID:   string(role.ChatID),
	}
}

func teamHopRecordsFromDomain(hops []servicechat.TeamHop) []teamHopRecord {
	if len(hops) == 0 {
		return nil
	}
	records := make([]teamHopRecord, 0, len(hops))
	for _, hop := range hops {
		records = append(records, teamHopRecord{
			Loop:     hop.Loop,
			Role:     hop.Role,
			Kind:     hop.Kind,
			ChatID:   string(hop.ChatID),
			Verdict:  hop.Verdict,
			Findings: hop.Findings,
			At:       hop.At,
		})
	}
	return records
}

func (r teamRecord) toDomain() servicechat.TeamPolicy {
	return servicechat.NormalizeTeam(servicechat.TeamPolicy{
		Enabled: r.Enabled,
		Roles: servicechat.TeamRoles{
			Implementer: r.Roles.Implementer.toDomain(),
			Reviewer:    r.Roles.Reviewer.toDomain(),
			Tester:      r.Roles.Tester.toDomain(),
		},
		MaxLoops:  r.MaxLoops,
		AutoFix:   r.AutoFix,
		Phase:     r.Phase,
		LoopsUsed: r.LoopsUsed,
		Verdict:   r.Verdict,
		Hops:      teamHopRecordsToDomain(r.Hops),
		EnabledBy: r.EnabledBy,
		UpdatedAt: r.UpdatedAt,
	})
}

func (r teamRoleRecord) toDomain() servicechat.TeamRole {
	return servicechat.TeamRole{
		Provider: servicechat.Provider(r.Provider),
		Model:    r.Model,
		Enabled:  r.Enabled,
		ChatID:   servicechat.ID(r.ChatID),
	}
}

func teamHopRecordsToDomain(records []teamHopRecord) []servicechat.TeamHop {
	if len(records) == 0 {
		return nil
	}
	hops := make([]servicechat.TeamHop, 0, len(records))
	for _, record := range records {
		hops = append(hops, servicechat.TeamHop{
			Loop:     record.Loop,
			Role:     record.Role,
			Kind:     record.Kind,
			ChatID:   servicechat.ID(record.ChatID),
			Verdict:  record.Verdict,
			Findings: record.Findings,
			At:       record.At,
		})
	}
	return hops
}

func (r metaRecord) toDomain() servicechat.Meta {
	lastReadAt := r.LastReadAt
	if lastReadAt == 0 {
		lastReadAt = r.LastMessageAt
	}
	provider := servicechat.NormalizeProvider(servicechat.Provider(r.Provider))
	return servicechat.Meta{
		ID:              servicechat.ID(r.ID),
		Title:           r.Title,
		Provider:        provider,
		ClaudeSessionID: r.ClaudeSessionID,
		CodexSessionID:  r.CodexSessionID,
		KimiSessionID:   r.KimiSessionID,
		TmuxSession:     r.TmuxSession,
		Cwd:             r.Cwd,
		CreatedAt:       r.CreatedAt,
		LastMessageAt:   r.LastMessageAt,
		LastReadAt:      lastReadAt,
		Model:           r.Model,
		Mode:            r.Mode,
		ReasoningEffort: servicechat.NormalizeReasoningEffort(r.ReasoningEffort),
		ServiceTier:     servicechat.NormalizeServiceTier(r.ServiceTier),
		ModelPolicy:     servicechat.NormalizeModelPolicy(r.ModelPolicy),
		EndpointID:      servicechat.NormalizeEndpointID(r.EndpointID),
		DirectModel:     servicechat.NormalizeDirectModel(r.DirectModel),
		ProjectID:       servicechat.ProjectID(r.ProjectID),
		ForkPending:     r.ForkPending,
		SelectedSkills:  servicechat.NormalizeSelectedSkills(skillRefRecordsToDomain(r.SelectedSkills), provider),
		// Normalizing on read is what lets a chat written before post-run
		// policies existed answer the driver's questions: it comes back with
		// the documented defaults rather than a zeroed round budget.
		Autopilot: servicechat.NormalizeAutopilot(servicechat.AutopilotPolicy{
			Enabled:        r.Autopilot.Enabled,
			MaxRounds:      r.Autopilot.MaxRounds,
			RoundsUsed:     r.Autopilot.RoundsUsed,
			MaxDurationMin: r.Autopilot.MaxDurationMin,
			StartedAt:      r.Autopilot.StartedAt,
			EnabledBy:      r.Autopilot.EnabledBy,
		}),
		AutoTest: servicechat.NormalizeAutoTest(servicechat.AutoTestPolicy{
			Enabled:   r.AutoTest.Enabled,
			EnabledBy: r.AutoTest.EnabledBy,
		}),
		Team:          r.Team.toDomain(),
		CompanionOf:   servicechat.ID(r.CompanionOf),
		CompanionRole: r.CompanionRole,
	}
}

func skillRefRecordsFromDomain(skills []servicechat.SkillRef) []skillRefRecord {
	if len(skills) == 0 {
		return nil
	}
	records := make([]skillRefRecord, 0, len(skills))
	for _, skill := range skills {
		records = append(records, skillRefRecord{
			Name:     skill.Name,
			Command:  skill.Command,
			Provider: string(skill.Provider),
			Source:   skill.Source,
		})
	}
	return records
}

func skillRefRecordsToDomain(records []skillRefRecord) []servicechat.SkillRef {
	if len(records) == 0 {
		return nil
	}
	skills := make([]servicechat.SkillRef, 0, len(records))
	for _, record := range records {
		skills = append(skills, servicechat.SkillRef{
			Name:     record.Name,
			Command:  record.Command,
			Provider: servicechat.Provider(record.Provider),
			Source:   record.Source,
		})
	}
	return skills
}

type eventRecord struct {
	Seq             int64           `json:"seq,omitempty"`
	T               int64           `json:"t"`
	Type            string          `json:"type"`
	Text            string          `json:"text,omitempty"`
	MessageID       string          `json:"messageId,omitempty"`
	ID              string          `json:"id,omitempty"`
	Name            string          `json:"name,omitempty"`
	Input           json.RawMessage `json:"input,omitempty"`
	Output          string          `json:"output,omitempty"`
	IsError         bool            `json:"isError,omitempty"`
	ToolName        string          `json:"toolName,omitempty"`
	Subtype         string          `json:"subtype,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
	ClaudeSessionID string          `json:"claudeSessionId,omitempty"`
	CodexSessionID  string          `json:"codexSessionId,omitempty"`
	KimiSessionID   string          `json:"kimiSessionId,omitempty"`
	Provider        string          `json:"provider,omitempty"`
	Usage           json.RawMessage `json:"usage,omitempty"`
	Message         string          `json:"message,omitempty"`
	Running         bool            `json:"running,omitempty"`
	Synthetic       string          `json:"synthetic,omitempty"`
	Routing         *routingRecord  `json:"routing,omitempty"`
}

// routingRecord is the persisted shape of one automatic model-routing
// decision. It mirrors servicechat.EventRouting field for field so the store
// stays the only place that knows the on-disk names.
type routingRecord struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	RuleID   string `json:"ruleId,omitempty"`
	Rule     string `json:"rule,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func routingRecordFromDomain(routing *servicechat.EventRouting) *routingRecord {
	if routing == nil {
		return nil
	}
	return &routingRecord{
		Provider: routing.Provider,
		Model:    routing.Model,
		RuleID:   routing.RuleID,
		Rule:     routing.Rule,
		Reason:   routing.Reason,
	}
}

func (r *routingRecord) toDomain() *servicechat.EventRouting {
	if r == nil {
		return nil
	}
	return &servicechat.EventRouting{
		Provider: r.Provider,
		Model:    r.Model,
		RuleID:   r.RuleID,
		Rule:     r.Rule,
		Reason:   r.Reason,
	}
}

func eventRecordFromDomain(ev servicechat.Event) eventRecord {
	return eventRecord{
		Seq:             ev.Seq,
		T:               ev.T,
		Type:            ev.Type,
		Text:            ev.Text,
		MessageID:       ev.MessageID,
		ID:              ev.ID,
		Name:            ev.Name,
		Input:           ev.Input,
		Output:          ev.Output,
		IsError:         ev.IsError,
		ToolName:        ev.ToolName,
		Subtype:         ev.Subtype,
		Data:            ev.Data,
		ClaudeSessionID: ev.ClaudeSessionID,
		CodexSessionID:  ev.CodexSessionID,
		KimiSessionID:   ev.KimiSessionID,
		Provider:        string(ev.Provider),
		Usage:           ev.Usage,
		Message:         ev.Message,
		Running:         ev.Running,
		Synthetic:       servicechat.NormalizeSynthetic(ev.Synthetic),
		Routing:         routingRecordFromDomain(ev.Routing),
	}
}

func (r eventRecord) toDomain() servicechat.Event {
	return servicechat.Event{
		Seq:             r.Seq,
		T:               r.T,
		Type:            r.Type,
		Text:            r.Text,
		MessageID:       r.MessageID,
		ID:              r.ID,
		Name:            r.Name,
		Input:           r.Input,
		Output:          r.Output,
		IsError:         r.IsError,
		ToolName:        r.ToolName,
		Subtype:         r.Subtype,
		Data:            r.Data,
		ClaudeSessionID: r.ClaudeSessionID,
		CodexSessionID:  r.CodexSessionID,
		KimiSessionID:   r.KimiSessionID,
		Provider:        servicechat.Provider(r.Provider),
		Usage:           r.Usage,
		Message:         r.Message,
		Running:         r.Running,
		Synthetic:       servicechat.NormalizeSynthetic(r.Synthetic),
		Routing:         r.Routing.toDomain(),
	}
}
