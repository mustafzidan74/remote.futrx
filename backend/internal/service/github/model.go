// Package github links a project to one GitHub repository and does the four
// things that link makes possible: report the working tree's state against the
// remote, open a pull request from the chat, pull a pull request's review
// comments back into a chat as a prompt, and turn an inbound repository
// webhook into an agent run.
//
// Every git and gh invocation happens **inside the project's container**, never
// on the host. That is deliberate and load bearing: the credential this feature
// needs (`GITHUB_TOKEN`) is a project secret or a vault entry that LXD has
// already put in the container's environment, so the host process never holds
// it, never logs it, and cannot leak it into an error message. The host's only
// job is to decide what argv to run.
//
// The webhook half treats everything it receives as hostile. A delivery is a
// message from the public internet that asks this server to run an agent with
// root inside a container, so it has to clear four independent gates before it
// becomes a run: an HMAC signature over the raw body, a rate limit, a
// label/command/permission rule, and an `autoRun` opt-in that is off until an
// administrator turns it on.
package github

import (
	"strings"
	"time"
)

const (
	// DefaultLabel is the issue label that opts an issue into automation when
	// the operator names none.
	DefaultLabel = "remote-agent"
	// CommandPrefix is the comment verb that asks for a run. Everything after
	// it on the same line is the instruction.
	CommandPrefix = "/remote"
	// MaxPayloadBytes caps an inbound webhook body. GitHub's own deliveries
	// top out around 25 MiB for very large pushes; nothing this integration
	// reacts to comes close, so a small cap is free protection.
	MaxPayloadBytes = 1 << 20
	// SecretBytes is the entropy behind a generated webhook secret.
	SecretBytes = 32
	// MaxDeliveries is how many recent deliveries the panel lists.
	MaxDeliveries = 20
	// MaxPullRequests bounds the open-PR listing.
	MaxPullRequests = 20
	// MaxCommentsImported bounds how many comments one import folds into a
	// prompt, newest last. A 400-comment review thread would otherwise
	// produce a prompt no model can read.
	MaxCommentsImported = 60
	// MaxCommentBodyChars truncates a single comment inside the composed
	// prompt.
	MaxCommentBodyChars = 1500
	// SyntheticKind is the label an imported-review prompt carries onto the
	// chat event, so the browser can badge it as platform-issued.
	SyntheticKind = "github-review"
)

// Timeouts. Each bounds one class of container work; a caller that runs
// several commands budgets its own parent deadline on top.
const (
	// QuickTimeout bounds a single read-only git or gh call.
	QuickTimeout = 20 * time.Second
	// NetworkTimeout bounds a call that talks to github.com.
	NetworkTimeout = 90 * time.Second
	// CloneTimeout bounds `gh repo clone`, which can pull a large history.
	CloneTimeout = 10 * time.Minute
)

// Settings is the per-project automation configuration. It is stored beside
// the project rather than on it because it holds the webhook's shared secret
// in plaintext — HMAC verification needs the secret itself, not a digest — and
// project metadata is handed to every member.
type Settings struct {
	// Secret is the shared secret GitHub signs deliveries with. It is shown
	// to a human exactly once, when it is generated.
	Secret string `json:"secret,omitempty"`
	// Label is the issue label that opts an issue into automation. Empty
	// means DefaultLabel.
	Label string `json:"label,omitempty"`
	// AutoRun is the master switch for starting agent runs from inbound
	// events. It defaults to off, and only an administrator may turn it on:
	// an issue body is untrusted text, and a run executes with root inside
	// the project container.
	AutoRun bool `json:"autoRun,omitempty"`
	// CommentBack posts a link to the chat back onto the issue when a
	// triggered run settles.
	CommentBack bool   `json:"commentBack,omitempty"`
	EnabledAt   int64  `json:"enabledAt,omitempty"`
	EnabledBy   string `json:"enabledBy,omitempty"`
	UpdatedAt   int64  `json:"updatedAt,omitempty"`
	// Deliveries is the ring of recent inbound deliveries, newest first.
	Deliveries []Delivery `json:"deliveries,omitempty"`
}

// LabelOrDefault is the effective trigger label.
func (s Settings) LabelOrDefault() string {
	if trimmed := strings.TrimSpace(s.Label); trimmed != "" {
		return strings.ToLower(trimmed)
	}
	return DefaultLabel
}

// WebhookConfigured reports whether a secret exists to verify against.
func (s Settings) WebhookConfigured() bool {
	return strings.TrimSpace(s.Secret) != ""
}

// Delivery is one recorded inbound webhook. The payload itself is never
// stored: only what arrived, what was decided, and what happened.
type Delivery struct {
	ID       string `json:"id"`
	At       int64  `json:"at"`
	Event    string `json:"event"`
	Action   string `json:"action,omitempty"`
	Number   int    `json:"number,omitempty"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Sender   string `json:"sender,omitempty"`
	Outcome  string `json:"outcome"`
	Reason   string `json:"reason,omitempty"`
	ChatID   string `json:"chatId,omitempty"`
	RunStart bool   `json:"runStarted,omitempty"`
}

// Delivery outcomes, as shown in the panel.
const (
	OutcomeRan      = "ran"
	OutcomeChatOnly = "chat-only"
	OutcomeIgnored  = "ignored"
	OutcomeRejected = "rejected"
	OutcomeFailed   = "failed"
)

// PublicSettings is the settings view the panel receives. The secret is
// absent: it is shown once at generation time and never again.
type PublicSettings struct {
	WebhookConfigured bool   `json:"webhookConfigured"`
	WebhookURL        string `json:"webhookUrl,omitempty"`
	Label             string `json:"label"`
	AutoRun           bool   `json:"autoRun"`
	CommentBack       bool   `json:"commentBack"`
	EnabledAt         int64  `json:"enabledAt,omitempty"`
	EnabledBy         string `json:"enabledBy,omitempty"`
	UpdatedAt         int64  `json:"updatedAt,omitempty"`
	// Secret is populated only by the response that generated it.
	Secret     string     `json:"secret,omitempty"`
	Deliveries []Delivery `json:"deliveries"`
}

// SettingsInput is the panel's edit. Absent fields mean "leave it".
type SettingsInput struct {
	Label       *string `json:"label,omitempty"`
	AutoRun     *bool   `json:"autoRun,omitempty"`
	CommentBack *bool   `json:"commentBack,omitempty"`
	// Rotate mints a fresh webhook secret and returns it once.
	Rotate bool `json:"rotate,omitempty"`
	// Disable clears the secret, which stops every delivery at the door.
	Disable bool `json:"disable,omitempty"`
}

// LinkInput is what the link form submits: either a full URL or owner/repo.
type LinkInput struct {
	Repo string `json:"repo"`
}

// Status is the GitHub panel's live view of one project.
type Status struct {
	Linked bool   `json:"linked"`
	Owner  string `json:"owner,omitempty"`
	Repo   string `json:"repo,omitempty"`
	// DefaultBranch is what the repository reported when it was linked.
	DefaultBranch string `json:"defaultBranch,omitempty"`
	LinkedAt      int64  `json:"linkedAt,omitempty"`
	LinkedBy      string `json:"linkedBy,omitempty"`
	// ContainerRunning is false when the project's container is stopped, in
	// which case every other live field below is unknown.
	ContainerRunning bool `json:"containerRunning"`
	// AuthOK reports whether `gh auth status` succeeded inside the container.
	AuthOK bool `json:"authOk"`
	// AuthError is the reason authentication is unavailable, already
	// scrubbed of anything token-shaped.
	AuthError string `json:"authError,omitempty"`
	// WorkspaceRepo is false when /workspace is not a git repository yet.
	WorkspaceRepo bool `json:"workspaceRepo"`
	// WorkspaceEmpty reports whether /workspace has no entries at all, which
	// is the only state a clone is offered in.
	WorkspaceEmpty bool   `json:"workspaceEmpty"`
	Branch         string `json:"branch,omitempty"`
	Upstream       string `json:"upstream,omitempty"`
	Ahead          int    `json:"ahead"`
	Behind         int    `json:"behind"`
	Dirty          bool   `json:"dirty"`
	// DirtyCount is how many paths `git status --porcelain` reported.
	DirtyCount int `json:"dirtyCount"`
	// DefaultCommitMessage is the deterministic message the commit dialog
	// pre-fills. It is generated here so the browser and the server cannot
	// disagree about it.
	DefaultCommitMessage string `json:"defaultCommitMessage,omitempty"`
}

// PullRequest is one open pull request as the panel lists it.
type PullRequest struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	HeadBranch string `json:"headBranch,omitempty"`
	BaseBranch string `json:"baseBranch,omitempty"`
	Author     string `json:"author,omitempty"`
	Draft      bool   `json:"draft,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
	// Checks summarizes statusCheckRollup: "passing", "failing", "pending",
	// or "" when the pull request has no checks at all.
	Checks string `json:"checks,omitempty"`
	// ChecksPassed and ChecksTotal back the "3/5" the panel renders.
	ChecksPassed int `json:"checksPassed"`
	ChecksTotal  int `json:"checksTotal"`
}

// Check roll-up states.
const (
	ChecksPassing = "passing"
	ChecksFailing = "failing"
	ChecksPending = "pending"
)

// CreatePRInput is the body of POST /api/projects/{id}/github/pr.
type CreatePRInput struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	Base  string `json:"base,omitempty"`
	Head  string `json:"head,omitempty"`
	// Commit is the operator's explicit confirmation that the uncommitted
	// changes in /workspace may be committed. Without it a dirty workspace is
	// refused rather than silently swept into the pull request.
	Commit bool `json:"commit,omitempty"`
	// CommitMessage overrides the deterministic default. The platform never
	// asks an agent to write it: a commit message that varies per run makes
	// the audit trail unreadable.
	CommitMessage string `json:"commitMessage,omitempty"`
}

// CreatePRResult is what the caller gets back.
type CreatePRResult struct {
	URL string `json:"url"`
	// Branch is the head branch that was pushed.
	Branch string `json:"branch"`
	// Committed reports whether this call made a commit.
	Committed bool `json:"committed"`
}

// ImportInput selects the chat an import lands in.
type ImportInput struct {
	ChatID string `json:"chatId"`
}

// ImportResult reports what was imported.
type ImportResult struct {
	ChatID string `json:"chatId"`
	// Comments is how many comments made it into the prompt.
	Comments int `json:"comments"`
	// Started reports whether an agent run was launched. It is false when the
	// chat already has a run in flight; the prompt is still queued as text.
	Started bool   `json:"started"`
	Prompt  string `json:"prompt"`
}

// Comment is one review or issue comment, normalized across the two GitHub
// APIs they come from.
type Comment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	// Path and Line are set for review comments anchored to a diff.
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	// Diff is the review comment's hunk header, when GitHub supplied one.
	Diff      string `json:"diff,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	URL       string `json:"url,omitempty"`
}

// DefaultCommitMessage is the message the commit dialog pre-fills. It is
// deliberately deterministic — derived from the date and nothing else — so
// that reviewing a repository's history never requires guessing whether a
// model wrote the subject line.
func DefaultCommitMessage(now time.Time) string {
	return "Changes from Remote — " + now.UTC().Format("2006-01-02")
}
