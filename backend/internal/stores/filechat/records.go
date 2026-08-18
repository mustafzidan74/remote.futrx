package filechat

import (
	"encoding/json"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

type metaRecord struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Provider        string           `json:"provider,omitempty"`
	ClaudeSessionID string           `json:"claudeSessionId,omitempty"`
	CodexSessionID  string           `json:"codexSessionId,omitempty"`
	KimiSessionID   string           `json:"kimiSessionId,omitempty"`
	TmuxSession     string           `json:"tmuxSession,omitempty"`
	Cwd             string           `json:"cwd,omitempty"`
	CreatedAt       int64            `json:"createdAt"`
	LastMessageAt   int64            `json:"lastMessageAt"`
	LastReadAt      int64            `json:"lastReadAt,omitempty"`
	Model           string           `json:"model,omitempty"`
	Mode            string           `json:"mode,omitempty"`
	ReasoningEffort string           `json:"reasoningEffort,omitempty"`
	ServiceTier     string           `json:"serviceTier,omitempty"`
	ProjectID       string           `json:"projectId,omitempty"`
	ForkPending     bool             `json:"forkPending,omitempty"`
	SelectedSkills  []skillRefRecord `json:"selectedSkills,omitempty"`
	Autopilot       autopilotRecord  `json:"autopilot,omitempty"`
	AutoTest        autoTestRecord   `json:"autoTest,omitempty"`
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
	}
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
	}
}
