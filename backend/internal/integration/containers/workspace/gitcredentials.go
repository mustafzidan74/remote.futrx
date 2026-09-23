package workspace

import (
	"context"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
)

// gitSystemConfig routes git's github.com credentials through gh — the same
// lines `gh auth setup-git` writes. gh answers from the GITHUB_TOKEN (or
// GH_TOKEN) the container already inherits from a project secret or a vault
// entry, so `git push` and `git fetch` over https work with nothing typed.
//
// Without it the token reached `gh` but never `git`: an agent asked to push
// got "could not read Username for 'https://github.com'" and reported the
// project as not connected to GitHub, though the secret was right there. With
// no token set, gh returns nothing and git fails exactly as it did before.
//
// The empty `helper =` first clears any helper configured at a lower level,
// so a stale store helper cannot answer ahead of gh.
const gitSystemConfig = `# Written by Remote: git authenticates to GitHub with the GITHUB_TOKEN
# secret through gh. Remove this file to opt out.
[credential "https://github.com"]
	helper =
	helper = !/usr/bin/gh auth git-credential
[credential "https://gist.github.com"]
	helper =
	helper = !/usr/bin/gh auth git-credential
`

const (
	gitSystemConfigPath = "/etc/gitconfig"
	gitSystemConfigHash = "/etc/.remote-gitconfig.sha256"
)

// EnsureGitCredentialHelper publishes gitSystemConfig. It is hash-marked, so
// it is written once per container and again only when this content changes;
// an operator who edits or deletes the file afterwards keeps their version.
// The container images ship no /etc/gitconfig of their own.
func (p *Provisioner) EnsureGitCredentialHelper(ctx context.Context, containerName string) error {
	if !p.runner.Available() {
		return command.ErrUnavailable
	}
	return p.publisher.Push(ctx, containerName, []byte(gitSystemConfig),
		gitSystemConfigHash, "644", gitSystemConfigPath)
}
