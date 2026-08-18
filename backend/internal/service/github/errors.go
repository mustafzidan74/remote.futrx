package github

import "errors"

var (
	// ErrUnavailable reports a deployment without a settings store or a
	// container runtime, so every route answers 503 rather than panicking.
	ErrUnavailable = errors.New("GitHub integration is unavailable on this server")
	// ErrNotLinked is returned by everything that needs a repository.
	ErrNotLinked = errors.New("this project is not linked to a GitHub repository")
	// ErrAlreadyLinked refuses a second link, because the stored branch,
	// webhook secret and delivery log all belong to the first one.
	ErrAlreadyLinked = errors.New("this project is already linked to a GitHub repository - unlink it first")
	// ErrInvalidRepo rejects a reference that is not owner/repo or a
	// github.com URL naming the same.
	ErrInvalidRepo = errors.New("expected owner/repo or a github.com repository URL")
	// ErrNotRunning refuses container work while the project is stopped.
	ErrNotRunning = errors.New("this project's container is not running")
	// ErrAuth reports that `gh` inside the container has no usable
	// credential. The remedy is a GITHUB_TOKEN secret, so the message says so.
	ErrAuth = errors.New(
		"GitHub authentication failed inside the container - " +
			"add a GITHUB_TOKEN secret to this project or to the platform vault",
	)
	// ErrRepoUnreachable reports a repository `gh repo view` could not read
	// with the container's credential.
	ErrRepoUnreachable = errors.New("that repository could not be read with this container's GitHub token")
	// ErrWorkspaceNotEmpty refuses a clone that would land on existing files.
	ErrWorkspaceNotEmpty = errors.New("/workspace is not empty - clone refused")
	// ErrNotRepository reports that /workspace has no git repository, so
	// there is nothing to push.
	ErrNotRepository = errors.New("/workspace is not a git repository")
	// ErrDirtyWorkspace reports uncommitted changes on a pull request request
	// that did not confirm a commit. The handler turns it into a 409 carrying
	// the default commit message, which is what makes the dialog possible.
	ErrDirtyWorkspace = errors.New("there are uncommitted changes in /workspace")
	// ErrNothingToPush reports a head branch identical to its base with no
	// commits of its own — GitHub would reject the pull request anyway.
	ErrNothingToPush = errors.New("no commits to open a pull request with")
	// ErrHeadIsBase refuses a pull request from a branch onto itself.
	ErrHeadIsBase = errors.New("the head branch and the base branch are the same")
	// ErrInvalidBranch rejects a branch name git would not accept.
	ErrInvalidBranch = errors.New("invalid branch name")
	// ErrTitleRequired is returned when neither a title nor a commit to
	// derive one from exists.
	ErrTitleRequired = errors.New("a pull request title is required")
	// ErrInvalidNumber rejects a pull request number that is not positive.
	ErrInvalidNumber = errors.New("invalid pull request number")
	// ErrChatRequired reports an import with no destination chat.
	ErrChatRequired = errors.New("choose a chat to import the comments into")
	// ErrChatMismatch refuses to import into a chat belonging to a different
	// project.
	ErrChatMismatch = errors.New("that chat does not belong to this project")
	// ErrNoComments reports a pull request with nothing to import.
	ErrNoComments = errors.New("that pull request has no review or issue comments yet")
	// ErrWebhookDisabled reports a delivery to a project with no secret.
	ErrWebhookDisabled = errors.New("no webhook secret is configured for this project")
	// ErrBadSignature reports a delivery whose HMAC did not verify.
	ErrBadSignature = errors.New("signature verification failed")
	// ErrPayloadTooLarge reports a body over MaxPayloadBytes.
	ErrPayloadTooLarge = errors.New("payload too large")
	// ErrRateLimited reports a caller over the per-IP delivery budget.
	ErrRateLimited = errors.New("too many deliveries")
)
