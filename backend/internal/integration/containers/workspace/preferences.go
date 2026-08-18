package workspace

// Reply-preference provisioning maintains one managed region inside the
// project's own /workspace/AGENTS.md. That file belongs to the user (and to
// whatever template seeded it), so the platform owns only the text between its
// markers and never touches a byte outside them.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
)

const (
	// WorkspaceInstructionsPath is the per-project instruction file the
	// managed block lives in. Templates seed it and users edit it; both keep
	// working because the block is spliced in rather than overwritten.
	WorkspaceInstructionsPath = "/workspace/AGENTS.md"

	// BlockOpenMarker and BlockCloseMarker bracket the managed region. They
	// are HTML comments so they stay invisible in any Markdown renderer.
	BlockOpenMarker  = "<!-- remote:preferences -->"
	BlockCloseMarker = "<!-- /remote:preferences -->"

	preferencesFileMode = "644"
)

// PreferenceSource renders the managed block body for one project, or "" when
// nothing should be injected. It is a plain function rather than an interface
// so the service layer can supply it without either package importing the
// other.
type PreferenceSource func(ctx context.Context, projectID string) (string, error)

// WithReplyPreferences installs the source at construction time.
func WithReplyPreferences(source PreferenceSource) Option {
	return func(p *Provisioner) { p.setPreferences(source) }
}

// SetReplyPreferences installs the source after construction. The container
// stack is built before the services that own the preference document, so this
// is how the two are joined; the mutex covers that late binding.
func (p *Provisioner) SetReplyPreferences(source PreferenceSource) {
	p.setPreferences(source)
}

func (p *Provisioner) setPreferences(source PreferenceSource) {
	p.preferencesMu.Lock()
	defer p.preferencesMu.Unlock()
	p.preferences = source
}

func (p *Provisioner) preferenceSource() PreferenceSource {
	p.preferencesMu.RLock()
	defer p.preferencesMu.RUnlock()
	return p.preferences
}

// EnsureReplyPreferences regenerates the managed block in the project's
// /workspace/AGENTS.md. It runs on every run start so an admin edit propagates
// without recreating anything, and it is a no-op when the merged file is
// byte-identical to what is already there.
//
// Failures are the caller's to tolerate: an unreachable instruction file must
// not stop an agent run, only leave it without the preference.
func (p *Provisioner) EnsureReplyPreferences(
	ctx context.Context,
	containerName string,
	projectID string,
) error {
	source := p.preferenceSource()
	if source == nil {
		return nil
	}
	if !p.runner.Available() {
		return command.ErrUnavailable
	}

	body, err := source(ctx, projectID)
	if err != nil {
		return fmt.Errorf("resolve reply preferences: %w", err)
	}

	existing := p.readWorkspaceInstructions(ctx, containerName)
	merged := ApplyManagedBlock(existing, body)
	if merged == existing {
		return nil
	}
	return p.writeWorkspaceInstructions(ctx, containerName, merged)
}

// readWorkspaceInstructions returns the file's current contents, or "" when it
// does not exist yet. A read failure is indistinguishable from an absent file
// here on purpose: both mean "there is no user content to preserve", and the
// write that follows is what surfaces a real problem.
func (p *Provisioner) readWorkspaceInstructions(ctx context.Context, containerName string) string {
	out, err := command.RunWithTimeout(
		ctx, p.runner, 10*time.Second,
		"exec", containerName, "--", "cat", WorkspaceInstructionsPath,
	)
	if err != nil {
		return ""
	}
	return out
}

func (p *Provisioner) writeWorkspaceInstructions(
	ctx context.Context,
	containerName string,
	content string,
) error {
	// /workspace is a bind mount that always exists, and its ownership and
	// mode are the host's business — so this deliberately creates no
	// directory and changes no permissions on the way in.
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tmp, err := os.CreateTemp("", "futrx-preferences-*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write preferences: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close preferences: %w", err)
	}

	out, err := p.runner.Run(
		wctx, "file", "push", "--mode="+preferencesFileMode,
		tmp.Name(), containerName+WorkspaceInstructionsPath,
	)
	if err != nil {
		return fmt.Errorf("push %s: %w; output: %s", WorkspaceInstructionsPath, err, out)
	}
	return nil
}

// ApplyManagedBlock splices body into existing between the markers.
//
// Three cases, and all three are idempotent:
//
//   - no markers yet: the block is appended, leaving whatever the user or a
//     template wrote untouched above it;
//   - markers present: only the text between them is replaced;
//   - empty body: the block and its markers are removed entirely, so turning
//     the feature off actually cleans up after itself.
func ApplyManagedBlock(existing, body string) string {
	body = strings.TrimSpace(body)
	before, after, found := splitAroundBlock(existing)

	if body == "" {
		if !found {
			return existing
		}
		joined := strings.TrimRight(before, "\n")
		if trailing := strings.TrimLeft(after, "\n"); trailing != "" {
			if joined == "" {
				return trailing
			}
			return joined + "\n\n" + trailing
		}
		if joined == "" {
			return ""
		}
		return joined + "\n"
	}

	block := BlockOpenMarker + "\n" + body + "\n" + BlockCloseMarker + "\n"
	head := strings.TrimRight(before, "\n")
	if head != "" {
		head += "\n\n"
	}
	tail := strings.TrimLeft(after, "\n")
	if tail != "" {
		tail = "\n" + tail
	}
	return head + block + tail
}

// splitAroundBlock returns the text before and after an existing managed
// block.
//
// An opening marker with no closing one means the file was truncated
// mid-write. The unterminated region is taken to run to the end of the file
// and is replaced wholesale: any other reading either appends a second block
// on every run or leaves a marker that swallows the next one.
func splitAroundBlock(existing string) (before, after string, found bool) {
	start := strings.Index(existing, BlockOpenMarker)
	if start < 0 {
		return existing, "", false
	}
	rest := existing[start+len(BlockOpenMarker):]
	end := strings.Index(rest, BlockCloseMarker)
	if end < 0 {
		return existing[:start], "", true
	}
	return existing[:start], rest[end+len(BlockCloseMarker):], true
}

// preferencesState is embedded in Provisioner; declared here so the whole
// reply-preference capability reads as one file.
type preferencesState struct {
	preferencesMu sync.RWMutex
	preferences   PreferenceSource
}
