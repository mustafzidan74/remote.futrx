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
	agentruntime "github.com/futrx-com/remote.futrx.com/internal/integration/agents/runtime"
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

	tried := map[string]bool{}
	var catalog []string
	// Fallbacks are ranked against the model the user asked for, not the last
	// one tried, so the chain walks one family in a stable order.
	origin := ""
	for attempt := 0; ; attempt++ {
		outcome, err := p.runOnce(ctx, req, emit, attempt < maxCapacityFallbacks)
		if err != nil || !outcome.capacity {
			return err
		}

		// Google had no capacity for the model. Try the nearest model of the same
		// family, continuing the conversation when one was started.
		current := firstNonEmpty(req.Model, outcome.capacityModel)
		tried[current] = true
		if origin == "" {
			origin = current
		}
		if catalog == nil {
			catalog = p.modelCatalog(ctx, outcome.containerName)
		}
		next := nextCapacityFallback(origin, catalog, tried)
		if next == "" {
			emit(outcome.failure)
			return agent.ErrRunFailed
		}
		// A status line, not reply text: written into the reply it was copied,
		// searched and summarised in the project journal as if the model had
		// said it.
		emit(modelFallbackEvent(req.ConversationID, current, next))
		req.Model = next
		if outcome.sessionID != "" {
			req.ResumeID = outcome.sessionID
			req.Prompt = capacityContinuePrompt
		}
	}
}

// runOutcome is what one agy process left behind for the fallback loop.
type runOutcome struct {
	containerName string
	sessionID     string
	// capacity is set when the run failed only because its model had no
	// capacity and a fallback may still be tried; failure is the event that
	// was held back, emitted if no fallback remains.
	capacity      bool
	capacityModel string
	failure       agent.Event
}

func (p *Provider) runOnce(ctx context.Context, req agent.RunRequest, emit func(agent.Event), mayFallback bool) (runOutcome, error) {
	cmd, containerName, err := p.buildCmd(ctx, req, p.args(req), emit)
	if err != nil {
		return runOutcome{}, err
	}
	outcome := runOutcome{containerName: containerName}

	parser := NewParser(req)
	var stderr stderrLines
	reportedFailure := false
	forward := func(ev agent.Event) {
		if ev.Type == agent.EventRunFailed {
			if isCapacity, model := capacityFailure(ev.Message); isCapacity && mayFallback {
				outcome.capacity, outcome.capacityModel, outcome.failure = true, model, ev
				return
			}
			reportedFailure = true
		}
		emit(ev)
	}
	runErr := agentruntime.RunProcess(ctx, cmd, parser, forward, agentruntime.ProcessOptions{
		Name:           "agy",
		LogID:          req.ConversationID,
		Provider:       agent.ProviderAntigravity,
		ConversationID: req.ConversationID,
		OnStderr:       stderr.add,
	})
	outcome.sessionID = parser.SessionID()
	if errors.Is(ctx.Err(), context.Canceled) {
		return runOutcome{}, nil
	}
	if reportedFailure {
		return runOutcome{}, agent.ErrRunFailed
	}
	if outcome.capacity {
		return outcome, nil
	}
	if runErr != nil {
		stderr := strings.TrimSpace(agentruntime.ErrorStderr(runErr))
		message := fmt.Sprintf("agy run failed: %v", runErr)
		if stderr != "" {
			message = fmt.Sprintf("%s; output: %s", message, tail(stderr, 4096))
		}
		if isSignInError(stderr) {
			message = signInHint
		}
		failure := agent.Event{
			T:              time.Now().UnixMilli(),
			Type:           agent.EventRunFailed,
			Provider:       agent.ProviderAntigravity,
			ConversationID: req.ConversationID,
			Message:        message,
		}
		if isCapacity, model := capacityFailure(stderr); isCapacity && mayFallback {
			outcome.capacity, outcome.capacityModel, outcome.failure = true, model, failure
			return outcome, nil
		}
		emit(failure)
		return runOutcome{}, agent.ErrRunFailed
	}

	// A run that worked proves this container holds a usable credential. Pull
	// it up to the host so every other project inherits it. Best effort on
	// purpose — the turn the operator asked for has already succeeded, and
	// failing it now over a credential copy would be the wrong trade.
	p.syncCredentialToHost(containerName, req.ConversationID)

	// agy exits cleanly, without a result line, when a headless run needs a
	// permission it cannot ask for. Closing that as a success left the chat
	// with a tool card that looked like it was still running and no reply.
	if !parser.Completed() {
		if permission, denied := deniedPermission(stderr.text()); denied {
			reason := permissionStopReason(req.Mode, permission)
			for _, ev := range parser.FailOpenTools(reason) {
				emit(ev)
			}
			emit(agent.Event{
				T:              time.Now().UnixMilli(),
				Type:           agent.EventRunFailed,
				Provider:       agent.ProviderAntigravity,
				ConversationID: req.ConversationID,
				Message:        reason,
			})
			return runOutcome{}, agent.ErrRunFailed
		}
	}

	// The result line closes the run with its token usage. A stream that ended
	// cleanly without one still has to be closed for the chat to settle.
	if !parser.Completed() {
		emit(agent.Event{
			T:              time.Now().UnixMilli(),
			Type:           agent.EventRunCompleted,
			Provider:       agent.ProviderAntigravity,
			ConversationID: req.ConversationID,
			Usage:          agent.Usage{Model: req.Model}.Raw(),
		})
	}
	return runOutcome{}, nil
}

// modelCatalog lists the model ids agy offers in the run's container. An empty
// list simply means no fallback.
func (p *Provider) modelCatalog(ctx context.Context, containerName string) []string {
	lookupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := agentruntime.NewCapabilityCommand(lookupCtx, agent.CapabilityRequest{ContainerName: containerName},
		[]string{"HOME=" + containerAgentHome}, "agy", "models")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("antigravity: list models for capacity fallback: %v", err)
		return []string{}
	}
	return modelIDs(string(output))
}

func tail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
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
