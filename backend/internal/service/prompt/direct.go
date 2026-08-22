package prompt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// Direct answering: a turn served by a plain completion API instead of by an
// agent CLI inside the project's container.
//
// The models this reaches — the free-tier provider pool, and the local model —
// are text APIs. They have no filesystem, no shell, and no tools, so a chat
// pointed at one can answer questions, draft copy, translate and explain, and
// cannot change a single file. That limitation is the whole reason this path
// is separate rather than a provider registered alongside claude and codex:
// the two are not interchangeable, and a picker that implied they were would
// be lying.
//
// What it buys: the models here are free. A question that does not need the
// repository does not need to spend an agent subscription.

// ErrNoDirectResponder reports that nothing is wired to answer directly. It is
// a configuration fault rather than a user error: a chat can only carry a
// direct model if the picker offered one.
var ErrNoDirectResponder = errors.New("no direct model responder is configured")

// DirectSource and DirectModel are the chat package's types, aliased so this
// package's ports read naturally without a second definition to keep in sync.
type (
	DirectSource = servicechat.DirectSource
	DirectModel  = servicechat.DirectModel
)

const (
	DirectSourcePool  = servicechat.DirectSourcePool
	DirectSourceLocal = servicechat.DirectSourceLocal
)

// directLabel is what a badge or an error names the model by. It prefers the
// model id, which is what an operator recognizes.
func directLabel(m DirectModel) string {
	if model := strings.TrimSpace(m.Model); model != "" {
		return model
	}
	if m.Source == DirectSourceLocal {
		return "the local model"
	}
	if m.ProviderID != "" {
		return m.ProviderID
	}
	return "the selected model"
}

// DirectChoice is one entry the composer offers.
type DirectChoice struct {
	Source     DirectSource `json:"source"`
	ProviderID string       `json:"providerId,omitempty"`
	// ProviderLabel is the vendor's name as the operator typed it.
	ProviderLabel string `json:"providerLabel,omitempty"`
	Model         string `json:"model"`
	// ModelLabel is the readable name; the model id when there is no better.
	ModelLabel string `json:"modelLabel,omitempty"`
}

// DirectRequest is one turn for a direct model.
type DirectRequest struct {
	Model DirectModel
	// SystemPrompt carries the operator's reply preferences and the
	// no-tools framing.
	SystemPrompt string
	// UserText is the prompt with whatever visible history was included.
	UserText string
}

// DirectResult is what came back.
type DirectResult struct {
	Text string
	// ProviderID and Model report who actually answered. The pool may fail
	// over, so these are not always what the request asked for.
	ProviderID       string
	Model            string
	PromptTokens     int
	CompletionTokens int
}

// DirectResponder answers a turn without a container. Implemented in the
// composition root over the provider pool and the local auxiliary model.
type DirectResponder interface {
	Answer(ctx context.Context, req DirectRequest) (DirectResult, error)
	// Choices lists what the composer may offer right now. It is read per
	// request: an operator can enable a provider at any moment, and a stale
	// list would offer a model that no longer works or hide one that does.
	Choices(ctx context.Context) []DirectChoice
}

// WithDirectResponder installs the direct-answer path. Without it the picker
// offers no direct models and every chat runs an agent, exactly as before.
func WithDirectResponder(responder DirectResponder) Option {
	return func(s *Service) { s.direct = responder }
}

// directSystemPrompt frames what this model can and cannot do.
//
// Without it these models answer as though they had a repository in front of
// them — offering to "go ahead and edit the file" they cannot reach. Saying so
// up front turns a confusing non-action into a clear one.
const directSystemPrompt = "You are answering inside a developer workspace, but you have NO tools: " +
	"you cannot read or write files, run commands, browse, or see the repository. " +
	"Answer from the conversation alone. If a request needs the files or the shell, " +
	"say plainly that this chat has no access to them and that the operator should " +
	"switch the chat to a coding agent, then answer whatever part you can."

// answerDirectly serves one turn from a completion API and writes the same
// events an agent run would, so the transcript, the timeline and the usage
// ledger need to know nothing about which path answered.
func (rnr *Service) answerDirectly(
	ctx context.Context,
	id servicechat.ID,
	meta ChatMeta,
	prompt string,
	actor Actor,
	priorEvents []servicechat.Event,
	emit func(ChatEvent),
) error {
	if rnr.direct == nil {
		emit(ChatEvent{T: time.Now().UnixMilli(), Type: "error", Message: ErrNoDirectResponder.Error()})
		return ErrNoDirectResponder
	}

	system := directSystemPrompt
	if preference := rnr.replyPreference(ctx, actor, string(meta.ProjectID)); preference != "" {
		system += "\n\n" + preference
	}

	result, err := rnr.direct.Answer(ctx, DirectRequest{
		Model:        meta.DirectModel,
		SystemPrompt: system,
		UserText:     promptWithVisibleHistory(priorEvents, prompt),
	})
	if err != nil {
		emit(ChatEvent{
			T:    time.Now().UnixMilli(),
			Type: "error",
			Message: fmt.Sprintf(
				"%s could not answer: %v",
				directLabel(meta.DirectModel), err,
			),
		})
		return err
	}

	emit(ChatEvent{T: time.Now().UnixMilli(), Type: "assistant_text", Text: result.Text})
	emit(ChatEvent{T: time.Now().UnixMilli(), Type: "complete"})
	return nil
}
