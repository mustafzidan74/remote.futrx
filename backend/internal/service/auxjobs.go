package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// AuxJobDriver is the auxiliary model's foothold in the chat lifecycle: it
// watches settled runs and, when the little model is available, replaces the
// two things the platform otherwise does crudely — the chat's truncated title
// and the missing one-line summary the sidebar shows underneath it.
//
// It lives in the composition package for the same reason notifyObserver
// does: it is the only place allowed to hold the chat repository and an
// optional service at once. Everything it does is best effort and happens off
// the run goroutine, so a slow or dead endpoint costs a chat nothing.
type AuxJobDriver struct {
	aux   *serviceauxmodel.Service
	chats servicechat.Repository
	// background is the work queue. It is a plain goroutine per settled run
	// rather than a pool because the work is at most one HTTP call with a
	// hard timeout, and a settled run is a rare event on a single-box
	// deployment.
	background func(func())
}

var _ prompt.RunObserver = (*AuxJobDriver)(nil)

func newAuxJobDriver(aux *serviceauxmodel.Service, chats servicechat.Repository) *AuxJobDriver {
	return &AuxJobDriver{
		aux:   aux,
		chats: chats,
		background: func(work func()) {
			go work()
		},
	}
}

// RunSettled is called on the run goroutine, so it must return immediately.
// The work it schedules is what may take a second.
func (d *AuxJobDriver) RunSettled(_ context.Context, outcome prompt.RunOutcome) {
	if d == nil || d.aux == nil || d.chats == nil {
		return
	}
	// A cancelled or failed run has no closing words worth summarizing, and
	// its first prompt is still there for the next run to title.
	if outcome.Cancelled || outcome.Err != nil {
		return
	}
	if !d.aux.Available(serviceauxmodel.JobChatTitle) &&
		!d.aux.Available(serviceauxmodel.JobChatSummary) {
		return
	}
	chatID, output := outcome.ChatID, outcome.Output
	d.background(func() { d.settle(chatID, output) })
}

// RunToolStarted is not interesting to this driver; it exists to satisfy the
// observer interface.
func (d *AuxJobDriver) RunToolStarted(context.Context, servicechat.ID, string) {}

// settleTimeout bounds the whole post-run pass, including both model calls.
// The model has its own per-request timeout; this one keeps a wedged
// goroutine from outliving the chat it was working on.
const settleTimeout = 3 * time.Minute

func (d *AuxJobDriver) settle(chatID servicechat.ID, output string) {
	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()

	meta, err := d.chats.Get(ctx, chatID)
	if err != nil {
		return
	}
	d.maybeRetitle(ctx, meta)
	d.maybeSummarize(ctx, chatID, output)
}

// maybeRetitle replaces a machine-made title with a better machine-made
// title. The guard is exact rather than heuristic: the stored title has to be
// *character for character* what TitleFromPrompt would have produced for this
// chat's first prompt. A human who renamed the chat, and a title this driver
// already wrote, both fail that test and are left alone.
func (d *AuxJobDriver) maybeRetitle(ctx context.Context, meta servicechat.Meta) {
	if !d.aux.Available(serviceauxmodel.JobChatTitle) {
		return
	}
	firstPrompt := d.firstUserPrompt(ctx, meta.ID)
	if firstPrompt == "" {
		return
	}
	if strings.TrimSpace(meta.Title) != servicechat.TitleFromPrompt(firstPrompt) {
		return
	}
	d.writeTitle(ctx, meta.ID, firstPrompt)
}

// RegenerateTitle rewrites a chat's title on demand. Unlike the automatic
// pass it does not care what the current title is: a person asked for a new
// one, so a title they typed themselves is fair game to replace.
func (d *AuxJobDriver) RegenerateTitle(
	ctx context.Context,
	id servicechat.ID,
) (servicechat.Meta, error) {
	if d == nil || d.aux == nil || d.chats == nil {
		return servicechat.Meta{}, serviceauxmodel.ErrDisabled
	}
	firstPrompt := d.firstUserPrompt(ctx, id)
	if strings.TrimSpace(firstPrompt) == "" {
		return servicechat.Meta{}, fmt.Errorf(
			"%w: this chat has no message to name it after yet", serviceauxmodel.ErrEmptyInput)
	}
	title, err := d.aux.Title(ctx, firstPrompt)
	if err != nil {
		return servicechat.Meta{}, err
	}
	if title == "" {
		return servicechat.Meta{}, fmt.Errorf(
			"%w: the auxiliary model returned an empty title", serviceauxmodel.ErrEmptyInput)
	}
	return d.chats.Update(ctx, id, func(m *servicechat.Meta) { m.Title = title })
}

func (d *AuxJobDriver) writeTitle(ctx context.Context, id servicechat.ID, firstPrompt string) {
	title, err := d.aux.Title(ctx, firstPrompt)
	if err != nil || title == "" {
		return
	}
	if _, err := d.chats.Update(ctx, id, func(m *servicechat.Meta) { m.Title = title }); err != nil {
		log.Printf("auxmodel: storing the generated title for chat %s failed: %v", id, err)
	}
}

// maybeSummarize stores the one-line subtitle the sidebar and the dashboard
// show. It is rewritten on every settled run on purpose: the summary answers
// "what is this chat about *now*", which is what makes it useful for finding
// a conversation again a week later.
func (d *AuxJobDriver) maybeSummarize(ctx context.Context, id servicechat.ID, output string) {
	if !d.aux.Available(serviceauxmodel.JobChatSummary) {
		return
	}
	if strings.TrimSpace(output) == "" {
		return
	}
	summary, err := d.aux.ChatSummary(ctx, output)
	if err != nil || summary == "" {
		return
	}
	if _, err := d.chats.Update(ctx, id, func(m *servicechat.Meta) { m.Summary = summary }); err != nil {
		log.Printf("auxmodel: storing the summary for chat %s failed: %v", id, err)
	}
}

// firstUserPrompt is the message the chat is named after: the first thing a
// human typed into it. Synthetic prompts — an autopilot nudge, a scheduled
// injection, an imported GitHub comment — are skipped, because naming a chat
// after the platform's own words would describe the machinery rather than the
// work.
func (d *AuxJobDriver) firstUserPrompt(ctx context.Context, id servicechat.ID) string {
	events, err := d.chats.ReadEvents(ctx, id)
	if err != nil {
		return ""
	}
	for _, event := range events {
		if event.Type != "user" || event.Synthetic != "" {
			continue
		}
		if text := strings.TrimSpace(event.Text); text != "" {
			return text
		}
	}
	return ""
}

// auxRunSummarizer adapts the auxiliary model to the notification observer's
// one-sentence summary. It is a separate tiny type so notifications.go
// depends on a behaviour rather than on the whole service, and so a
// deployment without the model simply has a nil summarizer.
type auxRunSummarizer struct {
	aux *serviceauxmodel.Service
}

// Summarize returns the one sentence a phone notification should carry, or an
// empty string when the model cannot help. The caller falls back to the raw
// tail it has always sent.
func (s auxRunSummarizer) Summarize(ctx context.Context, output string) string {
	if s.aux == nil || !s.aux.Available(serviceauxmodel.JobRunSummary) {
		return ""
	}
	summary, err := s.aux.RunSummary(ctx, output)
	if err != nil {
		return ""
	}
	return summary
}

// auxCommitMessages adapts the auxiliary model to the GitHub service's
// commit-subject port, for the same reason.
type auxCommitMessages struct {
	aux *serviceauxmodel.Service
}

func (c auxCommitMessages) Available() bool {
	return c.aux != nil && c.aux.Available(serviceauxmodel.JobCommitMessage)
}

func (c auxCommitMessages) Subject(ctx context.Context, diffStat string) (string, error) {
	if c.aux == nil {
		return "", serviceauxmodel.ErrDisabled
	}
	return c.aux.CommitMessage(ctx, diffStat)
}

// auxRunObserver keeps a nil driver out of the observer list. A typed nil
// pointer stored in the interface would still be dispatched to, and the
// prompt service's own nil check would not catch it.
func auxRunObserver(driver *AuxJobDriver) prompt.RunObserver {
	if driver == nil {
		return nil
	}
	return driver
}
