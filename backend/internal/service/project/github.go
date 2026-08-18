package project

// The GitHub repository link is part of project identity, so it lives on the
// project's own metadata document rather than in a side store: a project that
// has been linked should say so everywhere a project is described, including
// the listing the sidebar renders.
//
// Everything that is *not* identity — the webhook secret, the automation
// toggles, the delivery log — is deliberately kept out of here and owned by
// the github service, because this document is handed verbatim to every
// project member.

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// GitHubLink binds a project to exactly one GitHub repository.
type GitHubLink struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	// DefaultBranch is what `gh repo view` reported at link time. It is the
	// base a pull request targets when the caller names none.
	DefaultBranch string `json:"defaultBranch,omitempty"`
	LinkedAt      int64  `json:"linkedAt"`
	LinkedBy      string `json:"linkedBy,omitempty"`
}

// FullName is the owner/repo spelling every `gh` invocation takes.
func (l GitHubLink) FullName() string {
	if l.Owner == "" || l.Repo == "" {
		return ""
	}
	return l.Owner + "/" + l.Repo
}

// ownerRepoPattern is GitHub's own grammar for the two path segments: letters,
// digits, dot, dash and underscore. Validating here means no other layer has
// to wonder whether a stored name is safe to hand to a command line.
var ownerRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// ValidGitHubName reports whether one path segment of a repository reference
// is well formed.
func ValidGitHubName(value string) bool {
	return ownerRepoPattern.MatchString(value)
}

// SetGitHubLink stores the repository binding. The caller is responsible for
// having verified the repository is reachable; this method only persists.
func (s *Service) SetGitHubLink(ctx context.Context, id ID, link GitHubLink, actor string) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	if !ValidGitHubName(link.Owner) || !ValidGitHubName(link.Repo) {
		return Meta{}, ErrInvalidGitHubRepo
	}
	link.LinkedBy = strings.ToLower(strings.TrimSpace(actor))
	if link.LinkedAt == 0 {
		link.LinkedAt = time.Now().UnixMilli()
	}
	m, err := s.repo.Update(ctx, id, func(m *Meta) {
		stored := link
		m.GitHub = &stored
	})
	s.record(ctx, audit.ActionProjectGitHubLink, auditProjectTarget(id, m), audit.Meta{
		"repo":          link.FullName(),
		"defaultBranch": link.DefaultBranch,
	}, err)
	return m, err
}

// ClearGitHubLink unlinks the repository. It is idempotent: unlinking a
// project that was never linked succeeds and changes nothing.
func (s *Service) ClearGitHubLink(ctx context.Context, id ID) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	previous, _ := s.repo.Get(ctx, id)
	m, err := s.repo.Update(ctx, id, func(m *Meta) {
		m.GitHub = nil
	})
	repo := ""
	if previous.GitHub != nil {
		repo = previous.GitHub.FullName()
	}
	s.record(ctx, audit.ActionProjectGitHubUnlink, auditProjectTarget(id, m), audit.Meta{
		"repo": repo,
	}, err)
	return m, err
}
