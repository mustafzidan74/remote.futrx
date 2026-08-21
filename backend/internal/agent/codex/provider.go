package codex

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
	agentruntime "github.com/futrx-com/remote.futrx.com/internal/agent/runtime"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

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
	return agent.ProviderCodex
}

func (p *Provider) Parser(req agent.RunRequest) agent.LineParser {
	return NewParser(req)
}

func (p *Provider) Run(ctx context.Context, req agent.RunRequest, emit func(agent.Event)) error {
	if emit == nil {
		emit = func(agent.Event) {}
	}
	if req.Provider == "" {
		req.Provider = agent.ProviderCodex
	}

	if req.Fork && req.ResumeID != "" {
		if newID, ferr := p.forkCodexSession(ctx, req); ferr != nil {
			log.Printf("codex[%s] fork session: %v — starting fresh", req.ConversationID, ferr)
			req.ResumeID = ""
		} else {
			req.ResumeID = newID
			emit(agent.Event{
				T:              time.Now().UnixMilli(),
				Type:           agent.EventSessionUpdated,
				Provider:       agent.ProviderCodex,
				ConversationID: req.ConversationID,
				SessionID:      newID,
			})
		}
	}
	req.Fork = false

	cmd, containerName, err := p.buildCmd(ctx, req, p.args(req), emit)
	if err != nil {
		return err
	}
	err = agentruntime.RunProcess(ctx, cmd, p.Parser(req), emit, agentruntime.ProcessOptions{
		Name:           "codex",
		LogID:          req.ConversationID,
		Provider:       agent.ProviderCodex,
		ConversationID: req.ConversationID,
	})
	if err != nil && req.ResumeID != "" && strings.Contains(strings.ToLower(agentruntime.ErrorStderr(err)), "no rollout found") {
		return fmt.Errorf("%w: %s", agent.ErrSessionNotFound, strings.TrimSpace(agentruntime.ErrorStderr(err)))
	}
	// A run that talked to a third party has no ChatGPT token to carry back,
	// and pulling that container's auth.json onto the host would overwrite
	// the operator's own.
	if err == nil && containerName != "" && req.Endpoint == nil && p.containerDeps.Credentials != nil {
		syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if syncErr := p.syncCredentialsFromContainer(syncCtx, containerName); syncErr != nil {
			log.Printf("codex[%s] sync auth from %s: %v", req.ConversationID, containerName, syncErr)
		}
	}
	return err
}

// forkCodexSession duplicates the parent rollout under a fresh session id so a
// forked chat continues from the same history without mutating the parent's
// transcript. Codex's headless `exec` has no fork primitive, so we copy the
// rollout file: the parent's uuid (unique) is rewritten everywhere in the file
// and in its name, yielding a session codex can resume by id.
func (p *Provider) forkCodexSession(ctx context.Context, req agent.RunRequest) (string, error) {
	newID, err := newUUID()
	if err != nil {
		return "", err
	}
	parent := req.ResumeID
	script := fmt.Sprintf(`set -e
src=$(find /root/.codex/sessions -name '*-%[1]s.jsonl' 2>/dev/null | head -1)
[ -n "$src" ] || { echo NOTFOUND >&2; exit 3; }
dest="$(dirname "$src")/$(basename "$src" | sed 's/%[1]s/%[2]s/')"
sed 's/%[1]s/%[2]s/g' "$src" > "$dest"`, parent, newID)

	var cmd *exec.Cmd
	if req.ProjectID != "" && p.projects != nil {
		project, perr := p.projects.Get(ctx, serviceproject.ID(req.ProjectID))
		if perr != nil {
			return "", perr
		}
		if project.ContainerName == "" {
			return "", fmt.Errorf("project %s has no container", project.ID)
		}
		cmd = exec.CommandContext(ctx, "lxc", "exec", project.ContainerName, "--", "sh", "-c", script)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", script)
	}
	if out, cerr := cmd.CombinedOutput(); cerr != nil {
		return "", fmt.Errorf("copy rollout: %w: %s", cerr, strings.TrimSpace(string(out)))
	}
	return newID, nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
