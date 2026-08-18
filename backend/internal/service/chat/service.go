package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

type Service struct {
	repo          Repository
	projects      ProjectResolver
	tmux          TmuxResolver
	runs          RunController
	defaultSkills DefaultSkillResolver
	audit         audit.Recorder
	now           func() time.Time
}

// Option configures optional Service collaborators.
type Option func(*Service)

// WithAudit records chat creation and deletion. Deleting a chat destroys its
// event log, so it belongs in the trail even though chats hold no secrets.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// DefaultSkillResolver supplies the skills a new project chat starts with.
// The global skills library implements it so an admin can pin a skill into
// every new chat without the chat service knowing what a global skill is.
type DefaultSkillResolver interface {
	DefaultSkills(ctx context.Context, projectID ProjectID, provider Provider) ([]SkillRef, error)
}

// WithDefaultSkills registers the resolver consulted when a project chat is
// created. A nil resolver leaves chats with only the caller's selection.
func WithDefaultSkills(resolver DefaultSkillResolver) Option {
	return func(s *Service) {
		s.defaultSkills = resolver
	}
}

// WithClock replaces the clock that stamps an armed autopilot's start time.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func New(
	repo Repository,
	projects ProjectResolver,
	tmux TmuxResolver,
	runs RunController,
	options ...Option,
) *Service {
	service := &Service{
		repo:     repo,
		projects: projects,
		tmux:     tmux,
		runs:     runs,
		audit:    audit.Nop{},
		now:      time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func (s *Service) recordChat(ctx context.Context, action string, meta Meta, id ID, err error) {
	if s == nil || s.audit == nil {
		return
	}
	target := audit.Target{Type: audit.TargetChat, ID: string(id), Name: meta.Title}
	entryMeta := audit.Meta{}
	if meta.ProjectID != "" {
		entryMeta["projectId"] = string(meta.ProjectID)
	}
	s.audit.Record(ctx, audit.Result(action, target, entryMeta, err))
}

func (s *Service) List(ctx context.Context) ([]Meta, error) {
	metas, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range metas {
		metas[i] = s.withRunning(metas[i])
	}
	return metas, nil
}

func (s *Service) Get(ctx context.Context, id ID) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	meta, err := s.repo.Get(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	return s.withRunning(meta), nil
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Meta, error) {
	meta, err := s.create(ctx, in)
	s.recordChat(ctx, audit.ActionChatCreate, meta, meta.ID, err)
	return meta, err
}

func (s *Service) create(ctx context.Context, in CreateInput) (Meta, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "New chat"
	}

	mode := in.Mode
	if mode == "" {
		mode = "code"
	}
	provider := NormalizeProvider(in.Provider)

	cwd := strings.TrimSpace(in.Cwd)
	if cwd == "" && in.ProjectID != "" && s.projects != nil {
		projectCwd, err := s.projects.WorkspaceForProject(ctx, in.ProjectID)
		if err != nil {
			return Meta{}, err
		}
		cwd = projectCwd
	}

	if cwd == "" && in.TmuxSession != "" && s.tmux != nil {
		if !s.tmux.ValidName(in.TmuxSession) {
			return Meta{}, ErrInvalidTmuxSession
		}
		if tmuxCwd, err := s.tmux.Cwd(ctx, in.TmuxSession); err == nil {
			cwd = tmuxCwd
		}
	}

	meta, err := s.repo.Create(ctx, Meta{
		Title:           title,
		Provider:        provider,
		TmuxSession:     in.TmuxSession,
		Cwd:             cwd,
		Model:           in.Model,
		Mode:            mode,
		ReasoningEffort: NormalizeReasoningEffort(in.ReasoningEffort),
		ServiceTier:     NormalizeServiceTier(in.ServiceTier),
		ProjectID:       in.ProjectID,
		SelectedSkills:  NormalizeSelectedSkills(s.withDefaultSkills(ctx, in, provider), provider),
		CompanionOf:     in.CompanionOf,
		CompanionRole:   strings.TrimSpace(in.CompanionRole),
	})
	if err != nil {
		return Meta{}, err
	}
	return s.withRunning(meta), nil
}

// withDefaultSkills appends the always-on skills of a project to the caller's
// selection. Normalization downstream drops anything the caller already
// picked, so an explicit selection is never duplicated.
func (s *Service) withDefaultSkills(ctx context.Context, in CreateInput, provider Provider) []SkillRef {
	if s.defaultSkills == nil || in.ProjectID == "" {
		return in.SelectedSkills
	}
	defaults, err := s.defaultSkills.DefaultSkills(ctx, in.ProjectID, provider)
	if err != nil || len(defaults) == 0 {
		return in.SelectedSkills
	}
	return append(append([]SkillRef(nil), in.SelectedSkills...), defaults...)
}

// Fork creates an independent copy of a chat from its latest state: same
// metadata and full visible history, plus a pending fork of the underlying
// agent session. The fork materializes on the next prompt — Claude via
// --fork-session, Codex via a copied rollout — so the parent is never mutated.
func (s *Service) Fork(ctx context.Context, id ID) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	src, err := s.repo.Get(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	events, err := s.repo.ReadEvents(ctx, id)
	if err != nil {
		return Meta{}, err
	}

	title := strings.TrimSpace(src.Title)
	if title == "" {
		title = "Untitled"
	}

	// Only pend a fork if there is a session to fork from; otherwise the copy
	// just starts fresh on first prompt. TmuxSession is intentionally not
	// copied — a fork must not hijack the parent's terminal.
	forkPending := src.ClaudeSessionID != "" || src.CodexSessionID != "" || src.KimiSessionID != ""

	forked, err := s.repo.Create(ctx, Meta{
		Title:           title + " (fork)",
		Provider:        src.Provider,
		ClaudeSessionID: src.ClaudeSessionID,
		CodexSessionID:  src.CodexSessionID,
		KimiSessionID:   src.KimiSessionID,
		Cwd:             src.Cwd,
		Model:           src.Model,
		Mode:            src.Mode,
		ReasoningEffort: src.ReasoningEffort,
		ServiceTier:     src.ServiceTier,
		ProjectID:       src.ProjectID,
		SelectedSkills:  src.SelectedSkills,
		ForkPending:     forkPending,
	})
	if err != nil {
		return Meta{}, err
	}

	// Copy the visible history so the fork opens on the same conversation.
	// Zero seq so the store assigns fresh, monotonic sequence numbers.
	for _, ev := range events {
		ev.Seq = 0
		if _, err := s.repo.AppendEvent(ctx, forked.ID, ev); err != nil {
			return Meta{}, err
		}
	}

	return s.withRunning(forked), nil
}

func (s *Service) Update(ctx context.Context, id ID, in UpdateInput) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	if in.Autopilot != nil && !ValidAutopilotInput(*in.Autopilot) {
		return Meta{}, ErrInvalidAutopilot
	}
	if in.Team != nil && !ValidTeamInput(*in.Team) {
		return Meta{}, ErrInvalidTeam
	}

	now := s.clock()
	meta, err := s.repo.Update(ctx, id, func(m *Meta) {
		if in.Title != nil {
			m.Title = strings.TrimSpace(*in.Title)
		}
		if in.Cwd != nil {
			m.Cwd = *in.Cwd
		}
		if in.Provider != nil {
			nextProvider := NormalizeProvider(*in.Provider)
			if nextProvider != m.Provider {
				m.SelectedSkills = nil
			}
			m.Provider = nextProvider
		}
		if in.Model != nil {
			m.Model = *in.Model
		}
		if in.Mode != nil {
			m.Mode = *in.Mode
		}
		if in.ReasoningEffort != nil {
			m.ReasoningEffort = NormalizeReasoningEffort(*in.ReasoningEffort)
		}
		if in.ServiceTier != nil {
			m.ServiceTier = NormalizeServiceTier(*in.ServiceTier)
		}
		if in.SelectedSkills != nil {
			m.SelectedSkills = NormalizeSelectedSkills(*in.SelectedSkills, m.Provider)
		}
		if in.Autopilot != nil {
			m.Autopilot = ApplyAutopilot(m.Autopilot, *in.Autopilot, in.ActorEmail, now)
		}
		if in.AutoTest != nil {
			m.AutoTest = ApplyAutoTest(m.AutoTest, *in.AutoTest, in.ActorEmail)
		}
		if in.Team != nil {
			m.Team = ApplyTeam(m.Team, *in.Team, in.ActorEmail, now)
		}
	})
	if err != nil {
		return Meta{}, err
	}
	return s.withRunning(meta), nil
}

func (s *Service) MarkRead(ctx context.Context, id ID) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	meta, err := s.repo.Update(ctx, id, func(m *Meta) {
		m.LastReadAt = m.LastMessageAt
	})
	if err != nil {
		return Meta{}, err
	}
	return s.withRunning(meta), nil
}

func (s *Service) MarkUnread(ctx context.Context, id ID) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	meta, err := s.repo.Update(ctx, id, func(m *Meta) {
		if m.LastMessageAt > 0 {
			m.LastReadAt = m.LastMessageAt - 1
		} else {
			m.LastReadAt = 0
		}
	})
	if err != nil {
		return Meta{}, err
	}
	return s.withRunning(meta), nil
}

func (s *Service) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

func (s *Service) withRunning(meta Meta) Meta {
	if s.runs != nil {
		meta.Running = s.runs.IsRunning(meta.ID)
	}
	return meta
}

func (s *Service) Delete(ctx context.Context, id ID) error {
	existing, _ := s.repo.Get(ctx, id)
	err := s.delete(ctx, id)
	s.recordChat(ctx, audit.ActionChatDelete, existing, id, err)
	return err
}

func (s *Service) delete(ctx context.Context, id ID) error {
	if !ValidID(id) {
		return ErrInvalidID
	}

	if s.runs != nil && s.runs.IsRunning(id) {
		if err := s.runs.Cancel(ctx, id); err != nil {
			return err
		}
	}

	return s.repo.Delete(ctx, id)
}

func (s *Service) Events(ctx context.Context, id ID) ([]Event, error) {
	if !ValidID(id) {
		return nil, ErrInvalidID
	}
	return s.repo.ReadEvents(ctx, id)
}

func (s *Service) EventPage(ctx context.Context, id ID, query EventPageQuery) (EventPage, error) {
	if !ValidID(id) {
		return EventPage{}, ErrInvalidID
	}
	return s.repo.ReadEventsPage(ctx, id, query)
}

func (s *Service) Rewind(ctx context.Context, id ID, beforeT int64) ([]Event, error) {
	if !ValidID(id) {
		return nil, ErrInvalidID
	}
	if beforeT <= 0 {
		return nil, ErrInvalidRewindTimestamp
	}
	if s.runs != nil && s.runs.IsRunning(id) {
		return nil, ErrChatRunning
	}
	return s.repo.TruncateEventsBefore(ctx, id, beforeT)
}

func (s *Service) UploadTarget(ctx context.Context, id ID) (string, error) {
	if !ValidID(id) {
		return "", ErrInvalidID
	}
	meta, err := s.repo.Get(ctx, id)
	if err != nil {
		return "", err
	}

	// Anchor uploads at the project's stable workspace root, not the chat's
	// (possibly tmux-driven) live cwd: a fixed root keeps the stored path
	// constant so the frontend can predict it exactly, and the .uploads/
	// subdir isolates attachments from the source tree.
	root := meta.Cwd
	if meta.ProjectID != "" && s.projects != nil {
		if ws, err := s.projects.WorkspaceForProject(ctx, meta.ProjectID); err == nil && ws != "" {
			root = ws
		}
	}

	if root == "" {
		root = os.Getenv("HOME")
		if root == "" {
			root = "/root"
		}
	}
	return filepath.Join(root, ".uploads"), nil
}
