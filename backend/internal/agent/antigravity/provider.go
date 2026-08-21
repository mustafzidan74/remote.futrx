package antigravity

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
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

type ProjectResolver interface {
	Get(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
	Start(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
	ListSecrets(ctx context.Context, id serviceproject.ID) ([]serviceproject.Secret, error)
}

type Provider struct {
	projects      ProjectResolver
	containerDeps provisioning.ContainerDependencies
	profile       provisioning.Profile
}

func New(projects ProjectResolver, containerDeps provisioning.ContainerDependencies) *Provider {
	return &Provider{projects: projects, containerDeps: containerDeps, profile: Profile()}
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

	output, runErr := p.streamPrintRun(ctx, cmd, req, emit)
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
	// it up to the host so every other project inherits it: an operator who
	// signs in once, in whichever project they happened to be in, should not
	// have to repeat the URL-and-code flow for the next one.
	//
	// Best effort on purpose — the turn the operator asked for has already
	// succeeded, and failing it now over a credential copy would be the wrong
	// trade. A failure is logged and the next successful run tries again.
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

// streamPrintRun executes one agy print-mode process, forwarding stdout to the
// chat as raw text deltas. Chunked reads (not line scanning) keep blank lines
// intact so markdown paragraphs survive, and text streams as it arrives. The
// combined output tail is returned for error reporting.
func (p *Provider) streamPrintRun(
	ctx context.Context,
	cmd *exec.Cmd,
	req agent.RunRequest,
	emit func(agent.Event),
) (string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("spawn agy: %w", err)
	}

	var tail tailBuffer
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 8192), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("agy[%s] stderr: %s", req.ConversationID, line)
			tail.append(line + "\n")
		}
	}()

	reader := bufio.NewReader(stdout)
	chunk := make([]byte, 4096)
	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			text := string(chunk[:n])
			tail.append(text)
			emit(agent.Event{
				T:              time.Now().UnixMilli(),
				Type:           agent.EventAssistantTextDelta,
				Provider:       agent.ProviderAntigravity,
				ConversationID: req.ConversationID,
				ItemKind:       agent.ItemMessage,
				Text:           text,
			})
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && ctx.Err() == nil {
				log.Printf("agy[%s] stdout: %v", req.ConversationID, readErr)
			}
			break
		}
	}

	err = cmd.Wait()
	<-stderrDone
	return tail.String(), err
}

func isSignInError(output string) bool {
	lowered := strings.ToLower(output)
	return strings.Contains(lowered, "sign in") || strings.Contains(lowered, "signed out") ||
		strings.Contains(lowered, "not authenticated")
}

// tailBuffer keeps the last few KB of process output for error messages.
// Appended to from both the stdout loop and the stderr goroutine.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

const tailBufferLimit = 4096

func (b *tailBuffer) append(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, s...)
	if len(b.data) > tailBufferLimit {
		b.data = b.data[len(b.data)-tailBufferLimit:]
	}
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// syncCredentialToHost copies this container's sign-in back to the host.
//
// The credential never reaches a log line: only the container it came from and
// the error, if any, are recorded.
func (p *Provider) syncCredentialToHost(containerName, conversationID string) {
	if containerName == "" || p.containerDeps.Credentials == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), credentialSyncTimeout)
	defer cancel()
	if err := p.containerDeps.Credentials.SyncFromContainer(ctx, containerName, p.profile.Credentials); err != nil {
		log.Printf("antigravity[%s] sync auth from %s: %v", conversationID, containerName, err)
	}
}
