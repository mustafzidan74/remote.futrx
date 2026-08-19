package github

import (
	"context"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// Store persists the per-project automation settings and delivery ring.
// Implementations serialize concurrent callers per project, exactly like the
// portal and share stores.
type Store interface {
	Get(ctx context.Context, projectID serviceproject.ID) (Settings, error)
	Save(ctx context.Context, projectID serviceproject.ID, settings Settings) error
	Delete(ctx context.Context, projectID serviceproject.ID) error
}

// Command is one invocation inside a project container.
//
// Argv is never handed to a shell. It becomes argv of the container CLI
// verbatim, so an issue title containing `$(rm -rf /)` is inert text to every
// layer below this one. Nothing in this package builds a shell string.
type Command struct {
	ContainerName string
	Argv          []string
	// Timeout bounds this single invocation. Zero means QuickTimeout.
	Timeout time.Duration
	// Stdin, when non-empty, is piped to the command. It exists so a pull
	// request body — arbitrary multi-line text — never has to travel as an
	// argv element.
	Stdin string
}

// CLI runs `git` and `gh` inside a project's container.
//
// The implementation owns the container runtime; this package owns the policy
// about what may be run. The container's own environment supplies the GitHub
// credential, which is why no token ever crosses this interface.
type CLI interface {
	// Available reports whether the host can reach a container runtime.
	Available() bool
	// Run returns the command's combined output. A non-nil error means a
	// non-zero exit; the output is still returned so callers can classify it.
	Run(ctx context.Context, cmd Command) (string, error)
}

// Projects is the lookup this service needs: the container to shell into, the
// status to refuse a stopped project with, and the two mutators that own the
// repository link on project metadata.
type Projects interface {
	Get(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
	SetGitHubLink(
		ctx context.Context,
		id serviceproject.ID,
		link serviceproject.GitHubLink,
		actor string,
	) (serviceproject.Meta, error)
	ClearGitHubLink(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
	// Start converges the project's container to running. A webhook can
	// arrive at a project that has been stopped for a week.
	Start(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
}

// Chats is the chat half a webhook needs: find the chat an issue already owns,
// or make one.
type Chats interface {
	List(ctx context.Context) ([]servicechat.Meta, error)
	Get(ctx context.Context, id servicechat.ID) (servicechat.Meta, error)
	Create(ctx context.Context, in servicechat.CreateInput) (servicechat.Meta, error)
}

// Starter launches an agent run. It is the same narrow port the post-run
// driver uses, so a webhook-triggered run travels the one code path every
// other run does — including the one-run-per-chat lock and the audit line.
type Starter interface {
	Start(input prompt.StartInput, emitTransient func(servicechat.Event)) (prompt.RunHandle, error)
}

// Notifier publishes an outbound notification for a run this service started.
// It is a port rather than a direct dependency on the notification service so
// this package stays free of sink vocabulary.
type Notifier interface {
	// PublishChatEvent delivers an event composed here, letting the observer
	// fill in the chat and project identity it alone can see.
	PublishChatEvent(ctx context.Context, chatID servicechat.ID, event NotifyEvent)
}

// CommitSubjects writes a conventional-commit subject from the shape of a
// diff. It is a port rather than a direct dependency on the auxiliary model
// so this package stays free of any model vocabulary, and so a deployment
// without one simply leaves it nil.
//
// Every caller of this port must have a fallback. The deterministic dated
// message is that fallback, and it is what ships whenever Available is false,
// Subject errors, or Subject answers with nothing.
type CommitSubjects interface {
	// Available reports whether asking is worth a round trip right now.
	Available() bool
	// Subject returns one commit subject line for the given diff shape.
	Subject(ctx context.Context, diffShape string) (string, error)
}

// NotifyEvent is the small, sink-neutral shape this package publishes. The
// composition root maps it onto the notification service's own event type.
type NotifyEvent struct {
	// Failed selects the failure event kind over the success one.
	Failed bool
	// Status is the lifecycle word carried to the sink.
	Status string
	// Summary is the human sentence.
	Summary string
	// URL is the issue or pull request the run came from — the one link that
	// makes a phone notification actionable.
	URL string
	// DedupeKey collapses repeated deliveries of the same logical event.
	DedupeKey string
}
