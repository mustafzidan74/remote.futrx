package github

// Every command this package runs is built here, as a []string, by a pure
// function. Nothing is interpolated into a shell string anywhere in this
// package, and nothing below this layer re-parses what these functions return.
// That is what makes an issue title of `"; rm -rf /` a title and not a shell.
//
// Keeping the builders pure also means the argv is testable without a
// container, which is the only way to be sure a flag never grows a leading `-`
// that git would read as an option.

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// commitIdentity is the author stamped on a commit this platform makes. It is
// a fixed, obviously-non-human identity so a reader of `git log` can tell at a
// glance which commits came from the platform rather than from a person.
const (
	commitAuthorName  = "Remote by FutrX"
	commitAuthorEmail = "remote@futrx.local"
)

// branchPattern is the subset of git's refname grammar this platform will
// create. It is narrower than git's own rules on purpose: a branch name can
// never begin with `-`, so it can never be read as a flag.
var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)

// ValidBranch reports whether a branch name is one this platform will create
// or check out.
func ValidBranch(name string) bool {
	if !branchPattern.MatchString(name) {
		return false
	}
	// git rejects these itself; rejecting them here keeps the error message
	// about the name rather than about git's exit code.
	return !strings.Contains(name, "..") && !strings.HasSuffix(name, ".lock") &&
		!strings.HasSuffix(name, "/") && !strings.Contains(name, "//")
}

// ParseRepoReference accepts what a human pastes — `owner/repo`,
// `https://github.com/owner/repo`, `git@github.com:owner/repo.git` — and
// returns the two path segments. Anything else is rejected rather than
// guessed at.
func ParseRepoReference(raw string) (owner string, repo string, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", ErrInvalidRepo
	}
	// SCP-style remote: git@github.com:owner/repo.git
	if idx := strings.Index(value, ":"); idx >= 0 && strings.Contains(value, "@") &&
		!strings.Contains(value, "//") {
		value = value[idx+1:]
	} else if strings.Contains(value, "://") {
		parsed, parseErr := url.Parse(value)
		if parseErr != nil {
			return "", "", ErrInvalidRepo
		}
		if host := strings.ToLower(parsed.Hostname()); host != "github.com" && host != "www.github.com" {
			return "", "", ErrInvalidRepo
		}
		value = parsed.Path
	}
	value = strings.Trim(value, "/")
	value = strings.TrimSuffix(value, ".git")
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return "", "", ErrInvalidRepo
	}
	owner, repo = parts[0], parts[1]
	if !serviceproject.ValidGitHubName(owner) || !serviceproject.ValidGitHubName(repo) {
		return "", "", ErrInvalidRepo
	}
	return owner, repo, nil
}

// repoViewArgv reads a repository's identity with the container's credential.
// It is the whole of link validation: a repository this token cannot read is
// a repository this project cannot use.
func repoViewArgv(fullName string) []string {
	return []string{
		"gh", "repo", "view", fullName,
		"--json", "name,owner,defaultBranchRef,isPrivate,url",
	}
}

// authStatusArgv asks gh whether it has a usable credential at all.
func authStatusArgv() []string {
	return []string{"gh", "auth", "status"}
}

// cloneArgv clones into /workspace. The trailing `.` is the destination:
// cloning *into* the existing bind mount rather than into a subdirectory is
// what keeps the container's four mounts intact.
func cloneArgv(fullName string) []string {
	return []string{"gh", "repo", "clone", fullName, "."}
}

// statusArgv is the one call behind ahead/behind/dirty. `-sb` prints the
// branch header (`## main...origin/main [ahead 2]`) followed by one line per
// changed path, which is everything the panel shows.
func statusArgv() []string {
	return []string{"git", "status", "-sb", "--porcelain=v1"}
}

// workspaceProbeArgv answers the three yes/no questions the panel asks before
// it offers anything: is this a repository, is the directory empty, and what
// remote is configured. It is one round trip because a stopped-then-started
// container makes each one cost real time.
//
// This is the single place in the package that runs a shell, and it runs a
// fixed program with no interpolation of any kind.
func workspaceProbeArgv() []string {
	const script = `printf 'IS_REPO='; git rev-parse --is-inside-work-tree 2>/dev/null || printf 'false\n'
printf 'ENTRIES='; ls -A /workspace 2>/dev/null | wc -l
printf 'REMOTE='; git remote get-url origin 2>/dev/null || printf '\n'`
	return []string{"sh", "-c", script}
}

// checkoutNewArgv creates a branch. It fails when the branch exists, which is
// exactly how the caller learns to switch to it instead.
func checkoutNewArgv(branch string) []string {
	return []string{"git", "checkout", "-b", branch}
}

// checkoutArgv switches to an existing branch.
func checkoutArgv(branch string) []string {
	return []string{"git", "checkout", branch}
}

// currentBranchArgv prints the checked-out branch, or nothing when HEAD is
// detached.
func currentBranchArgv() []string {
	return []string{"git", "rev-parse", "--abbrev-ref", "HEAD"}
}

// stageAllArgv stages every change, including deletions and new files.
func stageAllArgv() []string {
	return []string{"git", "add", "-A"}
}

// commitArgv commits the staged tree.
//
// The identity is supplied with `-c` rather than written into the container's
// git config, so committing never mutates state the project's own tooling
// reads. `--` is absent on purpose: `-m` already consumes the message, and the
// message is an argv element, so a message beginning with `-` is still a
// message.
func commitArgv(message string) []string {
	return []string{
		"git",
		"-c", "user.name=" + commitAuthorName,
		"-c", "user.email=" + commitAuthorEmail,
		"commit", "-m", message,
	}
}

// pushArgv publishes the branch and sets its upstream, so a second pull
// request from the same branch is a plain `git push`.
func pushArgv(branch string) []string {
	return []string{"git", "push", "--set-upstream", "origin", branch}
}

// prCreateArgv builds the `gh pr create` invocation.
//
// With no title, `--fill` lets gh derive the title and body from the commits,
// which is the right default for a branch whose commits already say what they
// did. With a title, the body is passed on stdin via `--body-file -` rather
// than as an argument: a pull request body is arbitrary multi-line text, and
// an empty body must not become an empty argv element gh would treat as a
// literal empty description.
func prCreateArgv(in CreatePRInput, head, base string) (argv []string, stdin string) {
	argv = []string{"gh", "pr", "create", "--head", head}
	if base != "" {
		argv = append(argv, "--base", base)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return append(argv, "--fill"), ""
	}
	argv = append(argv, "--title", title)
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return append(argv, "--body", ""), ""
	}
	return append(argv, "--body-file", "-"), body
}

// prListArgv lists open pull requests with the fields the panel renders.
// statusCheckRollup is what the "checks" column is derived from.
//
// `--repo` is not optional. Without it gh infers the repository from
// /workspace's own `origin`, which answers the wrong question twice: a
// workspace that is not a repository yet fails with "no git remotes found"
// (reported as an authentication problem, which it is not), and a workspace
// whose origin points somewhere else would list a different repository's pull
// requests than the one this project is linked to.
func prListArgv(fullName string, limit int) []string {
	if limit <= 0 || limit > MaxPullRequests {
		limit = MaxPullRequests
	}
	return []string{
		"gh", "pr", "list",
		"--repo", fullName,
		"--state", "open",
		"--limit", strconv.Itoa(limit),
		"--json", "number,title,url,headRefName,baseRefName,isDraft,author,updatedAt,statusCheckRollup",
	}
}

// reviewCommentsArgv reads the diff-anchored review comments of one pull
// request. `gh api` is used rather than `gh pr view` because only the REST
// endpoint carries the file path and line each comment is attached to.
func reviewCommentsArgv(fullName string, number int) []string {
	return []string{
		"gh", "api", "--paginate",
		"repos/" + fullName + "/pulls/" + strconv.Itoa(number) + "/comments",
	}
}

// issueCommentsArgv reads the conversation-tab comments. A pull request is an
// issue as far as this endpoint is concerned, which is why the path says
// `issues`.
func issueCommentsArgv(fullName string, number int) []string {
	return []string{
		"gh", "api", "--paginate",
		"repos/" + fullName + "/issues/" + strconv.Itoa(number) + "/comments",
	}
}

// issueCommentArgv posts the "a run finished, here is the chat" comment back
// onto the issue. The body travels on stdin for the same reason a pull request
// body does.
func issueCommentArgv(fullName string, number int) []string {
	return []string{
		"gh", "issue", "comment", strconv.Itoa(number),
		"--repo", fullName,
		"--body-file", "-",
	}
}
