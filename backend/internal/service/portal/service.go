package portal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	servicegithistory "github.com/futrx-com/remote.futrx.com/internal/service/githistory"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// Audit action names for the portal lifecycle. They are re-exported from the
// audit registry so callers of this package (and its tests) do not have to
// import both.
const (
	ActionPortalEnable  = serviceaudit.ActionPortalEnable
	ActionPortalRotate  = serviceaudit.ActionPortalRotate
	ActionPortalDisable = serviceaudit.ActionPortalDisable
)

// Service is the policy layer over client portals: it mints and checks
// tokens, and composes the read-only page from services that already enforce
// their own rules.
type Service struct {
	repo           Repository
	projects       Projects
	shares         Shares
	history        History
	usage          Usage
	audit          serviceaudit.Recorder
	baseURL        string
	publicHostname string
	limiter        *limiter
	now            func() time.Time
}

// Option customizes a Service at construction.
type Option func(*Service)

// WithShares enables the "Latest preview" section. Without it the portal never
// links a preview, which is the safe default.
func WithShares(shares Shares) Option {
	return func(s *Service) { s.shares = shares }
}

// WithHistory enables the "Recent changes" section.
func WithHistory(history History) Option {
	return func(s *Service) { s.history = history }
}

// WithUsage enables the optional activity line.
func WithUsage(usage Usage) Option {
	return func(s *Service) { s.usage = usage }
}

// WithAudit records the portal lifecycle actions.
func WithAudit(recorder serviceaudit.Recorder) Option {
	return func(s *Service) { s.audit = serviceaudit.RecorderOrNop(recorder) }
}

// WithClock replaces the wall clock, so page timestamps and the rate-limit
// window are testable.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// New builds the portal service. baseURL is the platform's public origin; the
// preview hostnames are derived from it exactly as the share links are.
func New(repo Repository, projects Projects, baseURL string, options ...Option) *Service {
	service := &Service{
		repo:     repo,
		projects: projects,
		audit:    serviceaudit.Nop{},
		baseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		now:      time.Now,
	}
	service.publicHostname = hostnameOf(service.baseURL)
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	service.limiter = newLimiter(limiterWindow, limiterBudget, func() time.Time { return service.now() })
	return service
}

// Get returns the member-facing settings. It never carries the token.
func (s *Service) Get(ctx context.Context, projectID serviceproject.ID) (Settings, error) {
	if s == nil || s.repo == nil {
		return Settings{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return Settings{}, serviceproject.ErrInvalidID
	}
	record, err := s.load(ctx, projectID)
	if err != nil {
		return Settings{}, err
	}
	return record.view(), nil
}

// Save applies a member's update. Enabling a switched-off portal and rotating
// a live one both mint a token, and the plaintext link is returned exactly
// once in that response; nothing else ever sees it again.
func (s *Service) Save(
	ctx context.Context,
	projectID serviceproject.ID,
	input UpdateInput,
) (Settings, error) {
	settings, err := s.save(ctx, projectID, input)
	s.recordSave(ctx, projectID, input, settings, err)
	return settings, err
}

func (s *Service) save(
	ctx context.Context,
	projectID serviceproject.ID,
	input UpdateInput,
) (Settings, error) {
	if s == nil || s.repo == nil {
		return Settings{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return Settings{}, serviceproject.ErrInvalidID
	}
	if _, err := s.projects.Get(ctx, projectID); err != nil {
		return Settings{}, err
	}
	current, err := s.load(ctx, projectID)
	if err != nil {
		return Settings{}, err
	}

	now := s.now()
	next := current
	next.Enabled = input.Enabled
	next.ShowPreview = input.ShowPreview
	next.ShowChangelog = input.ShowChangelog
	next.ShowUsage = input.ShowUsage
	next.BrandTitle = sanitizeBrandTitle(input.BrandTitle)
	next.Note = sanitizeNote(input.Note)
	// The note carries its own timestamp: the page dates the message, and a
	// toggle change must not make a month-old note look like it was written
	// today.
	switch {
	case next.Note == "":
		next.NoteUpdatedAt = 0
	case next.Note != current.Note:
		next.NoteUpdatedAt = now.UnixMilli()
	}
	next.UpdatedAt = now.UnixMilli()

	var token string
	switch {
	case !input.Enabled:
		// Disabling drops the digest as well as the flag: a portal that is
		// turned back on gets a fresh link, so a client who kept the old one
		// cannot walk back in.
		next.TokenHash = ""
	case input.Rotate || current.TokenHash == "":
		token, err = newToken()
		if err != nil {
			return Settings{}, err
		}
		next.TokenHash = hashToken(token)
		if next.CreatedAt == 0 {
			next.CreatedAt = now.UnixMilli()
		}
	}

	if err := s.repo.Save(ctx, projectID, next); err != nil {
		return Settings{}, err
	}
	view := next.view()
	if token != "" {
		view.URL = s.URL(projectID, token)
	}
	return view, nil
}

// URL is the client-facing link. It lives on the main application host, not on
// a preview host, so it is served by this backend and never by a container.
func (s *Service) URL(projectID serviceproject.ID, token string) string {
	if s.baseURL == "" || token == "" {
		return ""
	}
	return s.baseURL + "/portal/" + url.PathEscape(string(projectID)) +
		"?t=" + url.QueryEscape(token)
}

// View authorizes a public request and composes the page. Every failure that
// is not a rate limit collapses to ErrNotFound, so the route cannot be used to
// discover which project ids exist or which portals are enabled.
func (s *Service) View(
	ctx context.Context,
	projectID serviceproject.ID,
	token string,
	clientKey string,
) (Page, error) {
	if s == nil || s.repo == nil {
		return Page{}, ErrNotFound
	}
	if !s.limiter.allow(clientKey) {
		return Page{}, ErrRateLimited
	}
	if !serviceproject.ValidID(projectID) || strings.TrimSpace(token) == "" {
		return Page{}, ErrNotFound
	}
	record, err := s.repo.Get(ctx, projectID)
	if err != nil || !record.Live() {
		return Page{}, ErrNotFound
	}
	digest := hashToken(strings.TrimSpace(token))
	if subtle.ConstantTimeCompare([]byte(record.TokenHash), []byte(digest)) != 1 {
		return Page{}, ErrNotFound
	}
	project, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return Page{}, ErrNotFound
	}
	return s.compose(ctx, project, record), nil
}

// compose builds the view model. Each section degrades on its own: a stopped
// container, a workspace with no git repository, or an unavailable ledger
// leaves that section with a friendly note instead of failing the page.
func (s *Service) compose(
	ctx context.Context,
	project serviceproject.Meta,
	record Portal,
) Page {
	now := s.now()
	title := record.BrandTitle
	if title == "" {
		title = project.Name
	}
	page := Page{
		Title: title,
		// The brand title is consulted before the note, and the project name
		// only when the operator wrote neither.
		Direction:        direction(record.BrandTitle, record.Note, project.Name),
		StatusLabel:      statusLabel(project.Status),
		Running:          project.Status == serviceproject.StatusRunning,
		Note:             noteLines(record.Note),
		NoteUpdatedLabel: noteUpdatedLabel(record),
		UpdatedAtLabel:   now.UTC().Format("2 Jan 2006, 15:04 UTC"),
	}
	if record.ShowPreview {
		page.Previews, page.PreviewsNote = s.previews(ctx, project)
	}
	if record.ShowChangelog {
		page.Changelog, page.ChangelogNote = s.changelog(ctx, project)
	}
	if record.ShowUsage {
		page.ShowUsage = true
		page.UsageRuns = s.runs(ctx, project, now)
	}
	return page
}

// previews lists the ports that currently hold a live public share. A port
// without one would send the visitor to the platform login, so it is left out
// entirely rather than shown as a broken link.
func (s *Service) previews(ctx context.Context, project serviceproject.Meta) ([]PreviewLink, string) {
	if s.shares == nil || s.publicHostname == "" || project.Slug == "" {
		return nil, "No public preview is available right now."
	}
	shares, err := s.shares.List(ctx, project.ID)
	if err != nil {
		return nil, "No public preview is available right now."
	}
	seen := map[int]struct{}{}
	ports := make([]int, 0, len(shares))
	for _, share := range shares {
		if _, done := seen[share.Port]; done {
			continue
		}
		seen[share.Port] = struct{}{}
		ports = append(ports, share.Port)
	}
	sort.Ints(ports)

	links := make([]PreviewLink, 0, len(ports))
	for _, port := range ports {
		links = append(links, PreviewLink{
			Port: port,
			URL: "https://" + project.Slug + "--" + strconv.Itoa(port) +
				".dev." + s.publicHostname + "/",
		})
	}
	if len(links) == 0 {
		return nil, "No public preview is available right now."
	}
	return links, ""
}

// changelog reads the workspace's most recent commits and groups them by day.
// Author emails are dropped: the client is not an audience for the team's
// contact details.
func (s *Service) changelog(ctx context.Context, project serviceproject.Meta) ([]ChangeDay, string) {
	if s.history == nil || strings.TrimSpace(project.Cwd) == "" {
		return nil, "No change history is available for this project yet."
	}
	commits, err := s.history.Commits(ctx, project.Cwd, "", ChangelogCommits)
	if err != nil {
		return nil, "This project's workspace is not tracked in git yet, so there is no change history to show."
	}
	if len(commits.Commits) == 0 {
		return nil, "No changes have been committed yet."
	}
	return groupCommitsByDay(commits.Commits), ""
}

// groupCommitsByDay preserves the log's newest-first order while collapsing
// each day into one heading.
func groupCommitsByDay(commits []servicegithistory.Commit) []ChangeDay {
	days := make([]ChangeDay, 0, len(commits))
	for _, commit := range commits {
		at := time.UnixMilli(commit.AuthorDate).UTC()
		label := at.Format("Monday, 2 January 2006")
		entry := ChangeCommit{
			ShortSHA: commit.ShortSHA,
			Subject:  clampLine(commit.Subject, 200),
			Author:   clampLine(commit.AuthorName, 60),
			Time:     at.Format("15:04"),
		}
		if len(days) > 0 && days[len(days)-1].Label == label {
			days[len(days)-1].Commits = append(days[len(days)-1].Commits, entry)
			continue
		}
		days = append(days, ChangeDay{Label: label, Commits: []ChangeCommit{entry}})
	}
	return days
}

// runs counts the project's agent runs over the usage window. Only the count
// reaches the page — never a cost.
func (s *Service) runs(ctx context.Context, project serviceproject.Meta, now time.Time) int64 {
	if s.usage == nil {
		return 0
	}
	summary, err := s.usage.ProjectSummary(
		ctx,
		string(project.ID),
		now.Add(-UsageWindow).UnixMilli(),
		now.UnixMilli(),
		"",
		true,
	)
	if err != nil {
		return 0
	}
	return summary.Totals.Runs
}

func (s *Service) load(ctx context.Context, projectID serviceproject.ID) (Portal, error) {
	record, err := s.repo.Get(ctx, projectID)
	if err != nil {
		return Portal{}, err
	}
	if record.CreatedAt == 0 && record.UpdatedAt == 0 && record.TokenHash == "" && !record.Enabled {
		// Nothing stored yet: hand back the defaults so the settings form
		// opens with the intended toggles rather than everything off.
		return DefaultPortal(), nil
	}
	return record, nil
}

// recordSave writes one audit entry per lifecycle transition. A save that only
// edits the note or the toggles is not a lifecycle event and is not recorded.
func (s *Service) recordSave(
	ctx context.Context,
	projectID serviceproject.ID,
	input UpdateInput,
	settings Settings,
	err error,
) {
	if s == nil || s.audit == nil {
		return
	}
	action := ""
	switch {
	case !input.Enabled:
		action = ActionPortalDisable
	case input.Rotate:
		action = ActionPortalRotate
	case settings.URL != "" || err != nil:
		action = ActionPortalEnable
	}
	if action == "" {
		return
	}
	// A disable on an already-disabled portal is not a transition worth a log
	// line, but a failed attempt always is.
	s.audit.Record(ctx, serviceaudit.Result(
		action,
		serviceaudit.Target{Type: serviceaudit.TargetProject, ID: string(projectID)},
		serviceaudit.Meta{
			"showPreview":   input.ShowPreview,
			"showChangelog": input.ShowChangelog,
			"showUsage":     input.ShowUsage,
		},
		err,
	))
}

func direction(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if isRTL(value) {
			return "rtl"
		}
		return "ltr"
	}
	return "ltr"
}

func statusLabel(status serviceproject.Status) string {
	switch status {
	case serviceproject.StatusRunning:
		return "Running"
	case serviceproject.StatusStopped:
		return "Stopped"
	case serviceproject.StatusProvisioning:
		return "Starting up"
	case serviceproject.StatusError:
		return "Needs attention"
	default:
		return "Unknown"
	}
}

func noteLines(note string) []string {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	return strings.Split(note, "\n")
}

// noteUpdatedLabel dates the operator note for the page. A record written
// before the field existed has no date, and prints none rather than pretending
// the note is as old as the portal.
func noteUpdatedLabel(record Portal) string {
	if strings.TrimSpace(record.Note) == "" || record.NoteUpdatedAt <= 0 {
		return ""
	}
	return time.UnixMilli(record.NoteUpdatedAt).UTC().Format("2 Jan 2006, 15:04 UTC")
}

func hostnameOf(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	buf := make([]byte, TokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate portal token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
