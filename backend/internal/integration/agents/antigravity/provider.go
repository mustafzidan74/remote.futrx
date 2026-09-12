package antigravity

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
)

// credentialSyncTimeout bounds the post-run copy of the sign-in back to the
// host. It is short because the turn is already finished and the operator is
// waiting on nothing: a slow copy should be abandoned, not waited out.
const credentialSyncTimeout = 30 * time.Second

// signInHint is deliberately explicit that this is done once. The sign-in is
// captured from whichever container it happens in and seeded into the rest, so
// an operator who reads "per workspace" and braces for repeating it in every
// project has been told the wrong thing.
const signInHint = "antigravity is not signed in — open this chat's Terminal, run `agy`, and complete the sign-in URL + code flow, then retry. You only need to do this once: the sign-in is copied to the platform and every other project inherits it"

type Provider struct {
	projectPreparer     agent.ProjectPreparer
	credentialCollector provisioning.CredentialCollector
	profile             provisioning.Profile
	binary              string
}

func newProvider(
	projectPreparer agent.ProjectPreparer,
	credentialCollector provisioning.CredentialCollector,
	profile provisioning.Profile,
) *Provider {
	return &Provider{
		projectPreparer:     projectPreparer,
		credentialCollector: credentialCollector,
		profile:             profile,
		binary:              profile.CLI.Binary,
	}
}

func (p *Provider) ID() agent.ProviderID {
	return agent.ProviderAntigravity
}

func (p *Provider) Parser(req agent.RunRequest) agent.LineParser {
	return NewParser(req)
}

func (p *Provider) Run(ctx context.Context, req agent.RunRequest, emit func(agent.Event)) error {
	if emit == nil {
		emit = func(agent.Event) {}
	}
	if req.Provider == "" {
		req.Provider = agent.ProviderAntigravity
	}
	// agy has no fork primitive; a forked chat simply starts fresh.
	if req.Fork {
		req.ResumeID = ""
	}

	cmd, containerName, err := p.buildCmd(ctx, req, p.args(req), emit)
	if err != nil {
		return err
	}

	store := conversationStore{containerName: containerName}
	var before map[string]struct{}
	if req.ResumeID == "" {
		before = store.list(ctx)
	}

	output, runErr := streamPrintRun(ctx, cmd, req, emit)
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	if runErr != nil {
		message := fmt.Sprintf("agy run failed: %v", runErr)
		if tail := strings.TrimSpace(output); tail != "" {
			message = fmt.Sprintf("%s; output: %s", message, tail)
		}
		if isSignInError(output) {
			message = signInHint
		}
		emit(agent.Event{
			T:              time.Now().UnixMilli(),
			Type:           agent.EventRunFailed,
			Provider:       agent.ProviderAntigravity,
			ConversationID: req.ConversationID,
			Message:        message,
		})
		return agent.ErrRunFailed
	}

	if req.ResumeID == "" {
		if id := store.newConversation(ctx, before); id != "" {
			emit(agent.Event{
				T:              time.Now().UnixMilli(),
				Type:           agent.EventSessionUpdated,
				Provider:       agent.ProviderAntigravity,
				ConversationID: req.ConversationID,
				SessionID:      id,
			})
		}
	}
	// A run that worked proves this container holds a usable credential. Pull
	// it up to the host so every other project inherits it. Best effort on
	// purpose — the turn the operator asked for has already succeeded, and
	// failing it now over a credential copy would be the wrong trade.
	p.syncCredentialToHost(containerName, req.ConversationID)

	// agy print mode reports no tokens and no price, so the completion event
	// carries the model alone; cost is recorded as unknown downstream.
	emit(agent.Event{
		T:              time.Now().UnixMilli(),
		Type:           agent.EventRunCompleted,
		Provider:       agent.ProviderAntigravity,
		ConversationID: req.ConversationID,
		Usage:          agent.Usage{Model: req.Model}.Raw(),
	})
	return nil
}

func isSignInError(output string) bool {
	lowered := strings.ToLower(output)
	return strings.Contains(lowered, "sign in") || strings.Contains(lowered, "signed out") ||
		strings.Contains(lowered, "not authenticated")
}

// syncCredentialToHost copies this container's sign-in back to the host. The
// credential never reaches a log line: only the container and the error are.
func (p *Provider) syncCredentialToHost(containerName, conversationID string) {
	if containerName == "" || p.credentialCollector == nil || p.profile.Credentials.Empty() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), credentialSyncTimeout)
	defer cancel()
	if err := p.credentialCollector.SyncFromContainer(ctx, containerName, p.profile.Credentials); err != nil {
		log.Printf("antigravity[%s] sync auth from %s: %v", conversationID, containerName, err)
	}
}
