package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

// notifyObserver translates run and schedule lifecycle signals into outbound
// notification events. It lives in the composition package because it is the
// only place allowed to reach across the chat, project, and notify services.
type notifyObserver struct {
	notifications *servicenotify.Service
	chats         servicechat.Repository
	projects      *serviceproject.Service
	// baseURL is the public origin the project deep link is built from. Run
	// notifications get theirs from the notify service, which already knows
	// it; the project link is assembled here because only this observer knows
	// which project the event is about.
	baseURL string
}

var (
	_ prompt.RunObserver          = (*notifyObserver)(nil)
	_ serviceschedule.RunObserver = (*notifyObserver)(nil)
	_ servicehealth.Alerter       = (*notifyObserver)(nil)
)

// RunSettled reports a finished interactive run. Scheduled runs are skipped:
// the schedule service reports those with richer task context.
func (o *notifyObserver) RunSettled(ctx context.Context, outcome prompt.RunOutcome) {
	if o == nil || outcome.ScheduledTaskID != "" {
		return
	}
	kind, status := servicenotify.RunKind(outcome.Err, outcome.Cancelled)
	event := servicenotify.Event{
		Event:     kind,
		Status:    status,
		Summary:   runSummary(outcome),
		DedupeKey: fmt.Sprintf("run:%s:%d", outcome.ChatID, outcome.RunID),
	}
	o.describeChat(ctx, outcome.ChatID, &event)
	o.notifications.Publish(event)
}

// RunToolStarted reports the tool calls that hand control back to the human.
// Every such call is its own event: an agent that asks twice pings twice.
func (o *notifyObserver) RunToolStarted(ctx context.Context, chatID servicechat.ID, toolName string) {
	if o == nil || !servicenotify.NeedsAttentionTool(toolName) {
		return
	}
	event := servicenotify.Event{
		Event:   servicenotify.KindNeedsAttention,
		Status:  servicenotify.StatusWaiting,
		Summary: "The agent called " + toolName + " and is waiting for a human decision.",
	}
	o.describeChat(ctx, chatID, &event)
	o.notifications.Publish(event)
}

// ScheduledRunFinished reports the outcome of a scheduled task run.
func (o *notifyObserver) ScheduledRunFinished(
	ctx context.Context,
	task serviceschedule.Task,
	result serviceschedule.RunResult,
) {
	if o == nil {
		return
	}
	status := servicenotify.StatusSucceeded
	detail := result.Output
	if result.Err != nil {
		status = servicenotify.StatusFailed
		detail = result.Err.Error()
	}
	event := servicenotify.Event{
		Event:     servicenotify.KindScheduledRun,
		Status:    status,
		Summary:   scheduleSummary(task.Name, detail),
		DedupeKey: fmt.Sprintf("schedule:%s:%d", task.ID, task.LastRunAt),
	}
	o.describeChat(ctx, task.ChatID, &event)
	o.notifications.Publish(event)
}

// ProjectHealthChanged reports a project container crossing a health
// threshold, or recovering from one. The health service has already applied
// its hysteresis, so every call here is a settled transition and deserves
// exactly one message.
func (o *notifyObserver) ProjectHealthChanged(
	_ context.Context,
	project serviceproject.Meta,
	health servicehealth.ProjectHealth,
) {
	if o == nil {
		return
	}
	name := project.Slug
	if name == "" {
		name = project.Name
	}
	o.notifications.Publish(servicenotify.Event{
		Event:       servicenotify.KindProjectHealth,
		ProjectID:   string(project.ID),
		ProjectSlug: project.Slug,
		ProjectName: project.Name,
		Status:      string(health.Status),
		Summary:     servicenotify.Summary(servicehealth.AlertSummary(name, health)),
		URL:         servicenotify.ProjectURL(o.baseURL, string(project.ID)),
		// One message per settled transition: a project that stays critical
		// for an hour is reported once, not sixty times.
		DedupeKey: fmt.Sprintf("health:%s:%s:%d", project.ID, health.Status, health.LastCheckedAt),
	})
}

// describeChat fills the chat and project identity fields of an event. Lookup
// failures leave the fields empty rather than dropping the notification.
func (o *notifyObserver) describeChat(
	ctx context.Context,
	chatID servicechat.ID,
	event *servicenotify.Event,
) {
	event.ChatID = string(chatID)
	if o.chats == nil || chatID == "" {
		return
	}
	// A cancelled run hands us a cancelled context; the lookups behind this
	// enrichment are cheap and local, so run them anyway.
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	meta, err := o.chats.Get(ctx, chatID)
	if err != nil {
		return
	}
	event.ChatTitle = meta.Title
	event.Provider = string(meta.Provider)
	if meta.ProjectID == "" || o.projects == nil {
		return
	}
	event.ProjectID = string(meta.ProjectID)
	project, err := o.projects.Get(ctx, serviceproject.ID(meta.ProjectID))
	if err != nil {
		return
	}
	event.ProjectName = project.Name
	event.ProjectSlug = project.Slug
}

// runSummary prefers the agent's own last words and falls back to the failure
// reason so a failure notification is never empty.
func runSummary(outcome prompt.RunOutcome) string {
	if outcome.Cancelled {
		return "The run was cancelled before it finished."
	}
	if outcome.Err != nil {
		return servicenotify.Summary(outcome.Err.Error())
	}
	return servicenotify.Summary(outcome.Output)
}

// scheduleSummary keeps the task name in the body, since the event payload has
// no dedicated task field.
func scheduleSummary(taskName, detail string) string {
	detail = servicenotify.Summary(detail)
	taskName = strings.TrimSpace(taskName)
	switch {
	case taskName == "":
		return detail
	case detail == "":
		return "Task: " + taskName
	default:
		return "Task: " + taskName + "\n" + detail
	}
}

// usageDigestSource adapts the usage ledger to the notify service's digest
// port. It lives here for the same reason notifyObserver does: it is the only
// place allowed to hold both services at once.
type usageDigestSource struct {
	usage *serviceusage.Service
}

var _ servicenotify.DigestSource = usageDigestSource{}

// digestProjectLimit bounds how many project rows the aggregation carries into
// the message builder, which trims further.
const digestProjectLimit = 10

// WeeklyDigest aggregates every project's runs in the window. The digest is an
// operator-wide report, so it reads with admin scope rather than a caller's.
func (s usageDigestSource) WeeklyDigest(
	ctx context.Context,
	from, to int64,
) (servicenotify.Digest, error) {
	if s.usage == nil {
		return servicenotify.Digest{}, errors.New("the usage ledger is unavailable")
	}
	byProject, err := s.usage.Summary(ctx, serviceusage.Query{
		From:    from,
		To:      to,
		GroupBy: serviceusage.GroupByProject,
	}, "", true)
	if err != nil {
		return servicenotify.Digest{}, err
	}

	digest := servicenotify.Digest{
		From:         from,
		To:           to,
		TotalCostUSD: byProject.Totals.CostUSD,
		Runs:         byProject.Totals.Runs,
	}
	// Groups arrive sorted by cost, so the head is already the interesting end.
	for index, group := range byProject.Groups {
		if index >= digestProjectLimit {
			break
		}
		digest.Projects = append(digest.Projects, servicenotify.DigestProject{
			Name:    group.Label,
			CostUSD: group.CostUSD,
			Runs:    group.Runs,
		})
	}

	byModel, err := s.usage.Summary(ctx, serviceusage.Query{
		From:    from,
		To:      to,
		GroupBy: serviceusage.GroupByModel,
	}, "", true)
	if err != nil {
		return servicenotify.Digest{}, err
	}
	if len(byModel.Groups) > 0 {
		digest.TopModel = byModel.Groups[0].Label
	}
	return digest, nil
}
