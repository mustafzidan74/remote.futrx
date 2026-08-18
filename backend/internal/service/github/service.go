package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// Service is the GitHub integration.
type Service struct {
	store    Store
	cli      CLI
	projects Projects
	chats    Chats
	starter  Starter
	notifier Notifier
	audit    audit.Recorder
	baseURL  string
	now      func() time.Time

	// mu serializes the read-modify-write of one project's settings document.
	// The store locks per project too, but a delivery appends to the ring
	// after reading it, and that pair has to be atomic.
	mu sync.Mutex
}

// Option configures optional collaborators.
type Option func(*Service)

// WithAudit attaches the audit recorder.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithChats and WithStarter attach the two halves an inbound webhook needs to
// become a run. Without them a delivery is still verified, mapped, audited and
// recorded — it simply cannot start anything.
func WithChats(chats Chats) Option {
	return func(s *Service) { s.chats = chats }
}

// WithStarter attaches the prompt service.
func WithStarter(starter Starter) Option {
	return func(s *Service) { s.starter = starter }
}

// WithNotifier attaches the outbound notification port.
func WithNotifier(notifier Notifier) Option {
	return func(s *Service) { s.notifier = notifier }
}

// WithBaseURL supplies the public origin the webhook URL and the chat deep
// links are built from.
func WithBaseURL(baseURL string) Option {
	return func(s *Service) { s.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/") }
}

// WithClock replaces the clock, for tests.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// New builds the service. A nil store or nil CLI leaves it unavailable, which
// every route reports as 503 rather than as a failure of the caller's request.
func New(store Store, cli CLI, projects Projects, options ...Option) *Service {
	s := &Service{store: store, cli: cli, projects: projects, audit: audit.Nop{}, now: time.Now}
	for _, option := range options {
		option(s)
	}
	return s
}

// Available reports whether this deployment can serve the integration at all.
func (s *Service) Available() bool {
	return s != nil && s.store != nil && s.cli != nil && s.cli.Available() && s.projects != nil
}

/* ------------------------------------------------------------------ *
 * Repository link
 * ------------------------------------------------------------------ */

// Link validates a repository reference against the container's own GitHub
// credential and, if it reads, stores it on the project.
//
// Validation is `gh repo view` inside the container rather than an HTTP call
// from the host, and that is the whole point: the question is not "does this
// repository exist?" but "can *this project* reach it?", and only the
// container knows the answer because only the container holds the token.
func (s *Service) Link(
	ctx context.Context,
	projectID serviceproject.ID,
	in LinkInput,
	actor string,
) (serviceproject.Meta, error) {
	if !s.Available() {
		return serviceproject.Meta{}, ErrUnavailable
	}
	owner, repo, err := ParseRepoReference(in.Repo)
	if err != nil {
		return serviceproject.Meta{}, err
	}
	meta, err := s.runningProject(ctx, projectID)
	if err != nil {
		return serviceproject.Meta{}, err
	}
	if meta.GitHub != nil && meta.GitHub.FullName() != "" &&
		!strings.EqualFold(meta.GitHub.FullName(), owner+"/"+repo) {
		return serviceproject.Meta{}, ErrAlreadyLinked
	}

	view, err := s.viewRepository(ctx, meta.ContainerName, owner+"/"+repo)
	if err != nil {
		return serviceproject.Meta{}, err
	}
	return s.projects.SetGitHubLink(ctx, projectID, serviceproject.GitHubLink{
		// Prefer the casing GitHub itself reports over what the human typed.
		Owner:         view.Owner.Login,
		Repo:          view.Name,
		DefaultBranch: view.DefaultBranchRef.Name,
		LinkedAt:      s.now().UnixMilli(),
	}, actor)
}

// repoView is the subset of `gh repo view --json` this service reads.
type repoView struct {
	Name  string `json:"name"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	DefaultBranchRef struct {
		Name string `json:"name"`
	} `json:"defaultBranchRef"`
	IsPrivate bool   `json:"isPrivate"`
	URL       string `json:"url"`
}

func (s *Service) viewRepository(ctx context.Context, container, fullName string) (repoView, error) {
	out, err := s.run(ctx, Command{
		ContainerName: container,
		Argv:          repoViewArgv(fullName),
		Timeout:       NetworkTimeout,
	})
	if err != nil {
		if isAuthFailure(out) {
			return repoView{}, ErrAuth
		}
		return repoView{}, ErrRepoUnreachable
	}
	var view repoView
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &view); err != nil {
		return repoView{}, ErrRepoUnreachable
	}
	if view.Name == "" || view.Owner.Login == "" {
		return repoView{}, ErrRepoUnreachable
	}
	return view, nil
}

// Unlink drops the repository binding and the automation settings with it. A
// stale webhook secret for a repository nobody is linked to is a liability,
// not a convenience, so unlinking deletes it.
func (s *Service) Unlink(ctx context.Context, projectID serviceproject.ID) error {
	if s == nil || s.projects == nil {
		return ErrUnavailable
	}
	// Held across both writes: a delivery that is already past its signature
	// check will read-modify-write the settings document under this same lock,
	// and without it that append would recreate the file this call just
	// deleted — leaving a delivery log for a project nobody is linked to.
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.projects.ClearGitHubLink(ctx, projectID); err != nil {
		return err
	}
	if s.store != nil {
		return s.store.Delete(ctx, projectID)
	}
	return nil
}

/* ------------------------------------------------------------------ *
 * Status
 * ------------------------------------------------------------------ */

// Status reports what the panel shows. Every live field is best effort: a
// stopped container answers with the stored link and nothing else, because
// asking it anything would mean starting it, and reading a panel must never
// have that side effect.
func (s *Service) Status(ctx context.Context, projectID serviceproject.ID) (Status, error) {
	if !s.Available() {
		return Status{}, ErrUnavailable
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return Status{}, err
	}
	status := Status{DefaultCommitMessage: DefaultCommitMessage(s.now())}
	if meta.GitHub != nil {
		status.Linked = true
		status.Owner = meta.GitHub.Owner
		status.Repo = meta.GitHub.Repo
		status.DefaultBranch = meta.GitHub.DefaultBranch
		status.LinkedAt = meta.GitHub.LinkedAt
		status.LinkedBy = meta.GitHub.LinkedBy
	}
	// An unlinked project has nothing to report about a remote, and asking
	// costs three round trips into the container — one of them a 90-second
	// network call. Stop here.
	if !status.Linked {
		return status, nil
	}
	if meta.Status != serviceproject.StatusRunning || meta.ContainerName == "" {
		return status, nil
	}
	status.ContainerRunning = true

	probe, err := s.run(ctx, Command{
		ContainerName: meta.ContainerName,
		Argv:          workspaceProbeArgv(),
		Timeout:       QuickTimeout,
	})
	if err == nil {
		applyWorkspaceProbe(&status, probe)
	}
	if _, authErr := s.run(ctx, Command{
		ContainerName: meta.ContainerName,
		Argv:          authStatusArgv(),
		Timeout:       NetworkTimeout,
	}); authErr == nil {
		status.AuthOK = true
	} else {
		status.AuthError = ErrAuth.Error()
	}
	if status.WorkspaceRepo {
		if raw, statusErr := s.run(ctx, Command{
			ContainerName: meta.ContainerName,
			Argv:          statusArgv(),
			Timeout:       QuickTimeout,
		}); statusErr == nil {
			applyGitStatus(&status, raw)
		}
	}
	return status, nil
}

// applyWorkspaceProbe reads the three-line answer workspaceProbeArgv prints.
func applyWorkspaceProbe(status *Status, raw string) {
	for _, line := range strings.Split(raw, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		switch key {
		case "IS_REPO":
			status.WorkspaceRepo = strings.TrimSpace(value) == "true"
		case "ENTRIES":
			count, err := strconv.Atoi(strings.TrimSpace(value))
			status.WorkspaceEmpty = err == nil && count == 0
		}
	}
}

// applyGitStatus parses `git status -sb --porcelain=v1`.
//
// The first line is the branch header — `## feat/x...origin/feat/x [ahead 2,
// behind 1]` — and every line after it is one changed path. Both halves are
// read here so the panel and the pull-request flow agree about what "dirty"
// means.
func applyGitStatus(status *Status, raw string) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(raw, "\n"), "\r\n", "\n"), "\n")
	for index, line := range lines {
		if index == 0 && strings.HasPrefix(line, "## ") {
			parseBranchHeader(status, strings.TrimPrefix(line, "## "))
			continue
		}
		if strings.TrimSpace(line) != "" {
			status.DirtyCount++
		}
	}
	status.Dirty = status.DirtyCount > 0
}

func parseBranchHeader(status *Status, header string) {
	if tracking, rest, found := strings.Cut(header, " ["); found {
		header = tracking
		divergence := strings.TrimSuffix(rest, "]")
		for _, part := range strings.Split(divergence, ", ") {
			field, value, ok := strings.Cut(strings.TrimSpace(part), " ")
			if !ok {
				continue
			}
			count, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			switch field {
			case "ahead":
				status.Ahead = count
			case "behind":
				status.Behind = count
			}
		}
	}
	branch, upstream, found := strings.Cut(header, "...")
	status.Branch = strings.TrimSpace(branch)
	if found {
		status.Upstream = strings.TrimSpace(upstream)
	}
	// A repository with no commits prints `## No commits yet on main`.
	if strings.HasPrefix(status.Branch, "No commits yet on ") {
		status.Branch = strings.TrimPrefix(status.Branch, "No commits yet on ")
	}
}

/* ------------------------------------------------------------------ *
 * Clone
 * ------------------------------------------------------------------ */

// Clone fills an empty /workspace from the linked repository.
//
// The emptiness check is not a convenience: /workspace is a bind mount whose
// contents are the project's only durable state, and a clone that landed on
// top of a provisioned template would destroy work no snapshot was taken of.
// So a non-empty workspace is refused outright rather than merged into.
// The caller's identity is not a parameter: the audit recorder reads it from
// the request context the transport put it in, exactly as every other service
// on this platform does.
func (s *Service) Clone(ctx context.Context, projectID serviceproject.ID) error {
	err := s.clone(ctx, projectID)
	s.record(ctx, audit.ActionProjectGitHubClone, projectID, audit.Meta{}, err)
	return err
}

func (s *Service) clone(ctx context.Context, projectID serviceproject.ID) error {
	if !s.Available() {
		return ErrUnavailable
	}
	meta, err := s.runningProject(ctx, projectID)
	if err != nil {
		return err
	}
	if meta.GitHub == nil {
		return ErrNotLinked
	}
	status, err := s.Status(ctx, projectID)
	if err != nil {
		return err
	}
	if !status.WorkspaceEmpty {
		return ErrWorkspaceNotEmpty
	}
	out, err := s.run(ctx, Command{
		ContainerName: meta.ContainerName,
		Argv:          cloneArgv(meta.GitHub.FullName()),
		Timeout:       CloneTimeout,
	})
	if err != nil {
		if isAuthFailure(out) {
			return ErrAuth
		}
		return fmt.Errorf("clone %s: %s", meta.GitHub.FullName(), tail(out))
	}
	return nil
}

/* ------------------------------------------------------------------ *
 * Pull requests
 * ------------------------------------------------------------------ */

// CreatePR opens a pull request from the project's workspace.
//
// The one thing it will not do quietly is commit. Uncommitted changes are
// refused with ErrDirtyWorkspace unless the caller set Commit, because
// sweeping whatever happens to be in /workspace into a pull request is how an
// agent's scratch file ends up in someone's repository.
func (s *Service) CreatePR(
	ctx context.Context,
	projectID serviceproject.ID,
	in CreatePRInput,
) (CreatePRResult, error) {
	result, err := s.createPR(ctx, projectID, in)
	s.record(ctx, audit.ActionProjectGitHubPR, projectID, audit.Meta{
		"branch":    result.Branch,
		"base":      in.Base,
		"committed": result.Committed,
		"url":       result.URL,
	}, err)
	return result, err
}

func (s *Service) createPR(
	ctx context.Context,
	projectID serviceproject.ID,
	in CreatePRInput,
) (CreatePRResult, error) {
	if !s.Available() {
		return CreatePRResult{}, ErrUnavailable
	}
	meta, err := s.runningProject(ctx, projectID)
	if err != nil {
		return CreatePRResult{}, err
	}
	if meta.GitHub == nil {
		return CreatePRResult{}, ErrNotLinked
	}
	head := strings.TrimSpace(in.Head)
	base := strings.TrimSpace(in.Base)
	if base == "" {
		base = meta.GitHub.DefaultBranch
	}
	if head != "" && !ValidBranch(head) {
		return CreatePRResult{}, ErrInvalidBranch
	}
	if base != "" && !ValidBranch(base) {
		return CreatePRResult{}, ErrInvalidBranch
	}

	status, err := s.Status(ctx, projectID)
	if err != nil {
		return CreatePRResult{}, err
	}
	if !status.WorkspaceRepo {
		return CreatePRResult{}, ErrNotRepository
	}
	if status.Dirty && !in.Commit {
		return CreatePRResult{}, ErrDirtyWorkspace
	}

	container := meta.ContainerName
	// Branch: create the requested one, falling back to switching to it when
	// it already exists — the `git checkout -b X || git checkout X` shape,
	// written as two commands so neither needs a shell.
	if head != "" && head != status.Branch {
		if _, createErr := s.run(ctx, Command{
			ContainerName: container, Argv: checkoutNewArgv(head), Timeout: QuickTimeout,
		}); createErr != nil {
			if out, switchErr := s.run(ctx, Command{
				ContainerName: container, Argv: checkoutArgv(head), Timeout: QuickTimeout,
			}); switchErr != nil {
				return CreatePRResult{}, fmt.Errorf("check out %s: %s", head, tail(out))
			}
		}
	}
	if head == "" {
		head = status.Branch
	}
	// The fallback is as untrusted as the parameter: a detached HEAD parses as
	// "HEAD (no branch)", which git would take as far as a confusing push
	// failure instead of a clean refusal here.
	if !ValidBranch(head) {
		return CreatePRResult{}, ErrInvalidBranch
	}
	if base != "" && head == base {
		return CreatePRResult{}, ErrHeadIsBase
	}

	result := CreatePRResult{Branch: head}
	if in.Commit && status.Dirty {
		message := strings.TrimSpace(in.CommitMessage)
		if message == "" {
			message = DefaultCommitMessage(s.now())
		}
		if out, stageErr := s.run(ctx, Command{
			ContainerName: container, Argv: stageAllArgv(), Timeout: QuickTimeout,
		}); stageErr != nil {
			return CreatePRResult{}, fmt.Errorf("stage changes: %s", tail(out))
		}
		out, commitErr := s.run(ctx, Command{
			ContainerName: container, Argv: commitArgv(message), Timeout: QuickTimeout,
		})
		switch {
		case commitErr == nil:
			result.Committed = true
		case strings.Contains(strings.ToLower(out), "nothing to commit"):
			// Everything was already staged and committed by something else
			// between the status read and here. Not an error, but the caller
			// must not be told a commit was made.
		default:
			return CreatePRResult{}, fmt.Errorf("commit changes: %s", tail(out))
		}
	}

	if out, pushErr := s.run(ctx, Command{
		ContainerName: container, Argv: pushArgv(head), Timeout: NetworkTimeout,
	}); pushErr != nil {
		if isAuthFailure(out) {
			return CreatePRResult{}, ErrAuth
		}
		return CreatePRResult{}, fmt.Errorf("push %s: %s", head, tail(out))
	}

	argv, stdin := prCreateArgv(in, head, base)
	out, err := s.run(ctx, Command{
		ContainerName: container, Argv: argv, Stdin: stdin, Timeout: NetworkTimeout,
	})
	if err != nil {
		if isAuthFailure(out) {
			return CreatePRResult{}, ErrAuth
		}
		if strings.Contains(strings.ToLower(out), "no commits between") {
			return CreatePRResult{}, ErrNothingToPush
		}
		return CreatePRResult{}, fmt.Errorf("open pull request: %s", tail(out))
	}
	result.URL = firstPullRequestURL(out)
	if result.URL == "" {
		return CreatePRResult{}, fmt.Errorf("open pull request: %s", tail(out))
	}
	return result, nil
}

// firstPullRequestURL picks the pull request link out of gh's chatty output.
// gh prints progress lines around the URL, and which lines appear depends on
// the version, so the URL is found by shape rather than by position.
func firstPullRequestURL(output string) string {
	for _, field := range strings.Fields(output) {
		if !strings.HasPrefix(field, "https://") {
			continue
		}
		if parsed, err := url.Parse(field); err == nil && strings.Contains(parsed.Path, "/pull/") {
			return field
		}
	}
	return ""
}

// rawPullRequest is `gh pr list --json`'s shape.
type rawPullRequest struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	IsDraft     bool   `json:"isDraft"`
	UpdatedAt   string `json:"updatedAt"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	StatusCheckRollup []struct {
		// Checks runs report `conclusion`; legacy commit statuses report
		// `state`. Both are read so a repository using either is summarized.
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
	} `json:"statusCheckRollup"`
}

// ListPullRequests returns the repository's open pull requests with a
// summarized check state for each.
func (s *Service) ListPullRequests(
	ctx context.Context,
	projectID serviceproject.ID,
) ([]PullRequest, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}
	meta, err := s.runningProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if meta.GitHub == nil {
		return nil, ErrNotLinked
	}
	out, err := s.run(ctx, Command{
		ContainerName: meta.ContainerName,
		Argv:          prListArgv(meta.GitHub.FullName(), MaxPullRequests),
		Timeout:       NetworkTimeout,
	})
	if err != nil {
		if isAuthFailure(out) {
			return nil, ErrAuth
		}
		return nil, fmt.Errorf("list pull requests: %s", tail(out))
	}
	var raw []rawPullRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		return nil, fmt.Errorf("list pull requests: unexpected output: %s", tail(out))
	}
	list := make([]PullRequest, 0, len(raw))
	for _, entry := range raw {
		pull := PullRequest{
			Number:     entry.Number,
			Title:      entry.Title,
			URL:        entry.URL,
			HeadBranch: entry.HeadRefName,
			BaseBranch: entry.BaseRefName,
			Author:     entry.Author.Login,
			Draft:      entry.IsDraft,
			UpdatedAt:  entry.UpdatedAt,
		}
		for _, check := range entry.StatusCheckRollup {
			pull.ChecksTotal++
			switch summarizeCheck(check.Status, check.Conclusion, check.State) {
			case ChecksPassing:
				pull.ChecksPassed++
			case ChecksFailing:
				pull.Checks = ChecksFailing
			case ChecksPending:
				if pull.Checks == "" {
					pull.Checks = ChecksPending
				}
			}
		}
		if pull.ChecksTotal > 0 && pull.Checks == "" {
			pull.Checks = ChecksPassing
		}
		list = append(list, pull)
	}
	return list, nil
}

// summarizeCheck folds one check run or commit status into three words. A run
// that has not concluded is pending; anything conclusive that is not a success
// or a deliberate skip is a failure, so an unknown conclusion never reads as
// green.
func summarizeCheck(status, conclusion, state string) string {
	value := strings.ToUpper(strings.TrimSpace(conclusion))
	if value == "" {
		value = strings.ToUpper(strings.TrimSpace(state))
	}
	if value == "" {
		if strings.EqualFold(status, "COMPLETED") {
			return ChecksFailing
		}
		return ChecksPending
	}
	switch value {
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return ChecksPassing
	case "PENDING", "QUEUED", "IN_PROGRESS", "WAITING", "REQUESTED", "EXPECTED":
		return ChecksPending
	default:
		return ChecksFailing
	}
}

/* ------------------------------------------------------------------ *
 * Comment import
 * ------------------------------------------------------------------ */

// ImportComments folds a pull request's review conversation into a chat as a
// synthetic user prompt and starts a run on it.
//
// The prompt is inserted as a *user* message rather than injected as hidden
// context on purpose: the operator has to be able to read exactly what the
// agent was told, and to fork or rewind from that point like any other turn.
func (s *Service) ImportComments(
	ctx context.Context,
	projectID serviceproject.ID,
	number int,
	in ImportInput,
	actor string,
) (ImportResult, error) {
	result, err := s.importComments(ctx, projectID, number, in, actor)
	s.record(ctx, audit.ActionProjectGitHubImport, projectID, audit.Meta{
		"number":   number,
		"chatId":   result.ChatID,
		"comments": result.Comments,
		"started":  result.Started,
	}, err)
	return result, err
}

func (s *Service) importComments(
	ctx context.Context,
	projectID serviceproject.ID,
	number int,
	in ImportInput,
	actor string,
) (ImportResult, error) {
	if !s.Available() {
		return ImportResult{}, ErrUnavailable
	}
	if number <= 0 {
		return ImportResult{}, ErrInvalidNumber
	}
	chatID := servicechat.ID(strings.TrimSpace(in.ChatID))
	if chatID == "" {
		return ImportResult{}, ErrChatRequired
	}
	if s.chats == nil || s.starter == nil {
		return ImportResult{}, ErrUnavailable
	}
	meta, err := s.runningProject(ctx, projectID)
	if err != nil {
		return ImportResult{}, err
	}
	if meta.GitHub == nil {
		return ImportResult{}, ErrNotLinked
	}
	chat, err := s.chats.Get(ctx, chatID)
	if err != nil {
		return ImportResult{}, err
	}
	if string(chat.ProjectID) != string(projectID) {
		return ImportResult{}, ErrChatMismatch
	}

	fullName := meta.GitHub.FullName()
	review, err := s.fetchComments(ctx, meta.ContainerName, reviewCommentsArgv(fullName, number))
	if err != nil {
		return ImportResult{}, err
	}
	issue, err := s.fetchComments(ctx, meta.ContainerName, issueCommentsArgv(fullName, number))
	if err != nil {
		return ImportResult{}, err
	}
	comments := MergeComments(review, issue)
	if len(comments) == 0 {
		return ImportResult{}, ErrNoComments
	}

	text := ComposeReviewPrompt(fullName, number, comments)
	result := ImportResult{ChatID: string(chatID), Comments: len(comments), Prompt: text}
	handle, startErr := s.starter.Start(prompt.StartInput{
		ChatID:        chatID,
		Prompt:        text,
		Actor:         prompt.Actor{Email: actor},
		Synthetic:     SyntheticKind,
		ParentContext: context.WithoutCancel(ctx),
	}, nil)
	if startErr != nil {
		// A chat that is already running is not an error the operator needs to
		// fix: the comments were fetched and the prompt is returned, so the UI
		// can offer it. Anything else is reported.
		if errors.Is(startErr, prompt.ErrPromptAlreadyRunning) {
			return result, nil
		}
		return result, startErr
	}
	result.Started = true
	// Reported here rather than by the generic run observer, for the same
	// reason a webhook-triggered run is: the pull request that asked for the
	// work is a more useful place to land than the chat.
	go s.watchRun(projectID, chatID, pullRequestURL(fullName, number), 0, false, handle)
	return result, nil
}

// pullRequestURL is the canonical web address of one pull request. gh does not
// return it from the comment endpoints, and composing it here avoids a third
// round trip just to render a link.
func pullRequestURL(fullName string, number int) string {
	if fullName == "" || number <= 0 {
		return ""
	}
	return "https://github.com/" + fullName + "/pull/" + strconv.Itoa(number)
}

func (s *Service) fetchComments(ctx context.Context, container string, argv []string) ([]Comment, error) {
	out, err := s.run(ctx, Command{ContainerName: container, Argv: argv, Timeout: NetworkTimeout})
	if err != nil {
		if isAuthFailure(out) {
			return nil, ErrAuth
		}
		// A pull request with no comments at all answers 404 on neither
		// endpoint, so any error here is genuine.
		return nil, fmt.Errorf("read comments: %s", tail(out))
	}
	comments, err := ParseComments(out)
	if err != nil {
		return nil, fmt.Errorf("read comments: unexpected output: %s", tail(out))
	}
	return comments, nil
}

/* ------------------------------------------------------------------ *
 * Settings
 * ------------------------------------------------------------------ */

// Settings returns the panel's view. The stored secret never crosses this
// boundary; only whether one exists.
func (s *Service) Settings(ctx context.Context, projectID serviceproject.ID) (PublicSettings, error) {
	if s == nil || s.store == nil {
		return PublicSettings{}, ErrUnavailable
	}
	stored, err := s.store.Get(ctx, projectID)
	if err != nil {
		return PublicSettings{}, err
	}
	return s.publicSettings(projectID, stored, ""), nil
}

// SaveSettings applies an edit. Turning AutoRun on is an administrator's
// decision and the handler enforces that; this method only persists.
func (s *Service) SaveSettings(
	ctx context.Context,
	projectID serviceproject.ID,
	in SettingsInput,
	actor string,
) (PublicSettings, error) {
	if s == nil || s.store == nil {
		return PublicSettings{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.store.Get(ctx, projectID)
	if err != nil {
		return PublicSettings{}, err
	}
	issued := ""
	if in.Label != nil {
		label := strings.ToLower(strings.TrimSpace(*in.Label))
		if len(label) > 64 {
			label = label[:64]
		}
		stored.Label = label
	}
	if in.AutoRun != nil {
		stored.AutoRun = *in.AutoRun
	}
	if in.CommentBack != nil {
		stored.CommentBack = *in.CommentBack
	}
	switch {
	case in.Disable:
		// Clearing the secret is the off switch: with nothing to verify
		// against, every delivery is rejected at the door, and autoRun cannot
		// be left armed behind a disabled endpoint.
		stored.Secret = ""
		stored.AutoRun = false
		stored.EnabledAt = 0
		stored.EnabledBy = ""
	case in.Rotate || (stored.Secret == "" && in.AutoRun != nil && *in.AutoRun):
		secret, genErr := newSecret()
		if genErr != nil {
			return PublicSettings{}, genErr
		}
		issued = secret
		stored.Secret = secret
		stored.EnabledAt = s.now().UnixMilli()
		stored.EnabledBy = strings.ToLower(strings.TrimSpace(actor))
	}
	if stored.Secret == "" {
		stored.AutoRun = false
	}
	stored.UpdatedAt = s.now().UnixMilli()
	if err := s.store.Save(ctx, projectID, stored); err != nil {
		return PublicSettings{}, err
	}
	s.record(ctx, audit.ActionProjectGitHubSettings, projectID, audit.Meta{
		"autoRun":     stored.AutoRun,
		"commentBack": stored.CommentBack,
		"label":       stored.LabelOrDefault(),
		"rotated":     issued != "",
		"disabled":    in.Disable,
	}, nil)
	return s.publicSettings(projectID, stored, issued), nil
}

func (s *Service) publicSettings(
	projectID serviceproject.ID,
	stored Settings,
	issued string,
) PublicSettings {
	deliveries := stored.Deliveries
	if deliveries == nil {
		deliveries = []Delivery{}
	}
	return PublicSettings{
		WebhookConfigured: stored.WebhookConfigured(),
		WebhookURL:        s.WebhookURL(projectID),
		Label:             stored.LabelOrDefault(),
		AutoRun:           stored.AutoRun,
		CommentBack:       stored.CommentBack,
		EnabledAt:         stored.EnabledAt,
		EnabledBy:         stored.EnabledBy,
		UpdatedAt:         stored.UpdatedAt,
		Secret:            issued,
		Deliveries:        deliveries,
	}
}

// WebhookURL is the address pasted into the repository's webhook settings.
func (s *Service) WebhookURL(projectID serviceproject.ID) string {
	if s == nil || s.baseURL == "" {
		return ""
	}
	return s.baseURL + WebhookPath + string(projectID)
}

// WebhookPath is the public route prefix inbound deliveries arrive on.
const WebhookPath = "/hooks/github/"

func newSecret() (string, error) {
	buf := make([]byte, SecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

/* ------------------------------------------------------------------ *
 * Shared helpers
 * ------------------------------------------------------------------ */

// runningProject resolves a project and refuses one whose container is not up.
func (s *Service) runningProject(
	ctx context.Context,
	projectID serviceproject.ID,
) (serviceproject.Meta, error) {
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return serviceproject.Meta{}, err
	}
	if meta.Status != serviceproject.StatusRunning || meta.ContainerName == "" {
		return serviceproject.Meta{}, ErrNotRunning
	}
	return meta, nil
}

func (s *Service) run(ctx context.Context, cmd Command) (string, error) {
	if cmd.Timeout <= 0 {
		cmd.Timeout = QuickTimeout
	}
	return s.cli.Run(ctx, cmd)
}

func (s *Service) record(
	ctx context.Context,
	action string,
	projectID serviceproject.ID,
	meta audit.Meta,
	err error,
) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(action, audit.Target{
		Type: audit.TargetProject,
		ID:   string(projectID),
	}, meta, err))
}

// authFailureMarkers are what gh and git print when the credential is missing,
// expired, or lacks a scope. They are matched case-insensitively so the
// operator gets the actionable "add a GITHUB_TOKEN" message instead of a raw
// exit code.
var authFailureMarkers = []string{
	"gh auth login",
	"authentication failed",
	"could not resolve to a repository",
	"bad credentials",
	"http 401",
	"requires authentication",
	"no git remotes found",
	"gh: not found",
	"command not found",
	"permission denied",
	"github_token",
}

func isAuthFailure(output string) bool {
	lowered := strings.ToLower(output)
	for _, marker := range authFailureMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// tail keeps a command's failure readable when git prints a wall of hints. It
// is also the only place command output reaches an error message, which is why
// it is short: the less of a container's stdout that travels to a browser, the
// smaller the chance of carrying something that should have stayed inside.
func tail(output string) string {
	trimmed := strings.TrimSpace(output)
	const limit = 300
	if len(trimmed) <= limit {
		return trimmed
	}
	return "…" + trimmed[len(trimmed)-limit:]
}
