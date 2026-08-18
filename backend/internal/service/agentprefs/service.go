package agentprefs

import (
	"context"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// Service reads and writes the platform preference document and resolves the
// effective instructions for one run.
type Service struct {
	repo     Repository
	users    UserOverrides
	projects ProjectDirectory
	audit    audit.Recorder
	now      func() time.Time
}

type Option func(*Service)

// WithUserOverrides enables the per-user reply-language override. Without it
// every run sees the platform value.
func WithUserOverrides(users UserOverrides) Option {
	return func(s *Service) { s.users = users }
}

// WithProjects enables the ApplyToNewProjects rule. Without it that setting
// behaves like ApplyToAll, because nothing can date a project.
func WithProjects(projects ProjectDirectory) Option {
	return func(s *Service) { s.projects = projects }
}

func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

func New(repo Repository, options ...Option) *Service {
	service := &Service{repo: repo, audit: audit.Nop{}, now: time.Now}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Get returns the stored document, or the defaults when nothing was ever
// saved. A nil service (no store configured) still answers with defaults so
// callers never have to nil-check before rendering.
func (s *Service) Get(ctx context.Context) (Preferences, error) {
	if s == nil || s.repo == nil {
		return Defaults(), nil
	}
	stored, found, err := s.repo.Load(ctx)
	if err != nil {
		return Defaults(), err
	}
	if !found {
		return Defaults(), nil
	}
	return Normalize(stored), nil
}

// Update applies a partial edit and stamps who made it. The timestamp is also
// the cut-off the ApplyToNewProjects rule compares projects against, so every
// save re-dates that boundary.
func (s *Service) Update(ctx context.Context, input UpdateInput, actor string) (Preferences, error) {
	if s == nil || s.repo == nil {
		return Defaults(), ErrUnavailable
	}
	current, err := s.Get(ctx)
	if err != nil {
		return Preferences{}, err
	}

	next := current
	if input.ReplyLanguage != nil {
		next.ReplyLanguage = *input.ReplyLanguage
	}
	if input.Tone != nil {
		next.Tone = *input.Tone
	}
	if input.ExtraInstructions != nil {
		next.ExtraInstructions = *input.ExtraInstructions
	}
	if input.ApplyTo != nil {
		next.ApplyTo = *input.ApplyTo
	}
	next.UpdatedBy = actor
	next.UpdatedAt = s.now().UnixMilli()

	next = Normalize(next)
	if err := Validate(next); err != nil {
		return Preferences{}, err
	}
	if err := s.repo.Save(ctx, next); err != nil {
		return Preferences{}, err
	}
	entry := audit.Result(
		audit.ActionSettingsAgentPreferences,
		audit.Target{
			Type: audit.TargetServer,
			ID:   "agent-preferences",
			Name: "Reply preferences",
		},
		audit.Meta{
			"replyLanguage": next.ReplyLanguage,
			"tone":          string(next.Tone),
			"applyTo":       string(next.ApplyTo),
		},
		nil,
	)
	if actor != "" {
		entry.Actor = audit.Actor{Email: audit.NormalizeActorEmail(actor), IsAdmin: true}
	}
	s.audit.Record(ctx, entry)
	return next, nil
}

// RunPreamble renders the short preference line for one agent run, or "" when
// nothing should be injected. The per-user override wins over the platform
// language; everything else is platform policy.
func (s *Service) RunPreamble(ctx context.Context, identity Identity, projectID string) string {
	prefs, language, ok := s.resolve(ctx, identity, projectID)
	if !ok {
		return ""
	}
	return Preamble(prefs, language)
}

// WorkspaceBlock renders the managed AGENTS.md body for a project container.
// It deliberately ignores per-user overrides: the file is shared by everyone
// working in that project, so it can only carry the platform value.
func (s *Service) WorkspaceBlock(ctx context.Context, projectID string) (string, error) {
	prefs, _, ok := s.resolve(ctx, Identity{}, projectID)
	if !ok {
		return "", nil
	}
	return Instructions(prefs, prefs.ReplyLanguage), nil
}

// resolve loads the document, decides whether it governs this project, and
// picks the effective language.
func (s *Service) resolve(
	ctx context.Context,
	identity Identity,
	projectID string,
) (Preferences, string, bool) {
	if s == nil {
		return Preferences{}, "", false
	}
	prefs, err := s.Get(ctx)
	if err != nil {
		return Preferences{}, "", false
	}

	language := prefs.ReplyLanguage
	if s.users != nil {
		if override := normalizeLanguage(s.users.ReplyLanguage(ctx, identity)); override != LanguageAuto {
			language = override
		}
	}
	if !s.governs(ctx, prefs, projectID) {
		return Preferences{}, "", false
	}
	if strings.TrimSpace(Instructions(prefs, language)) == "" {
		return Preferences{}, "", false
	}
	return prefs, language, true
}

// governs applies the ApplyTo rule. A project the directory cannot date is
// treated as in scope: refusing to inject because a lookup failed would make
// the setting silently stop working.
func (s *Service) governs(ctx context.Context, prefs Preferences, projectID string) bool {
	if prefs.ApplyTo != ApplyToNewProjects {
		return true
	}
	if projectID == "" {
		return false
	}
	if s.projects == nil {
		return true
	}
	createdAt, ok := s.projects.CreatedAt(ctx, projectID)
	if !ok {
		return true
	}
	return createdAt >= prefs.UpdatedAt
}
