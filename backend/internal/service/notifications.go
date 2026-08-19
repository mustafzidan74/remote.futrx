package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	servicepostrun "github.com/futrx-com/remote.futrx.com/internal/service/postrun"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
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
	// summarizer is the optional auxiliary model. When it answers, a run
	// notification carries one useful sentence instead of the raw tail of the
	// agent'''s last message; when it does not — switched off, unreachable,
	// slow, or simply empty — the raw tail is exactly what still goes out.
	summarizer runSummarizer
}

// runSummarizer condenses an agent”'s closing words into one sentence. An
// empty answer means "use what you would have sent anyway", which is why it
// returns no error: this observer has nothing useful to do with one.
type runSummarizer interface {
	Summarize(ctx context.Context, output string) string
}

var (
	_ prompt.RunObserver          = (*notifyObserver)(nil)
	_ serviceschedule.RunObserver = (*notifyObserver)(nil)
	_ servicehealth.Alerter       = (*notifyObserver)(nil)
	_ servicemonitoring.Announcer = (*notifyObserver)(nil)
	_ servicepostrun.Notifier     = (*notifyObserver)(nil)
	_ servicesitewatch.Alerter    = (*notifyObserver)(nil)
)

// PlatformStarted reports that the backend process came up. It is the one
// notification nobody can infer from the outside: by the time a human looks,
// a crash-restarted box is answering again as if nothing happened, so the
// restart itself — and the version it came back on — is the whole message.
func (o *notifyObserver) PlatformStarted(_ context.Context, version string) {
	if o == nil {
		return
	}
	o.notifications.Publish(servicenotify.Event{
		Event:   servicenotify.KindSystem,
		Status:  servicenotify.StatusStarted,
		Summary: "Remote started (version " + version + ", uptime reset).",
	})
}

// RunSettled reports a finished interactive run.
//
// Two kinds are skipped because another service reports them with context this
// observer cannot see: a scheduled run (the schedule service names its task)
// and a GitHub-driven one (the github service attaches the issue or pull
// request link, which is the only place that report is actionable from).
func (o *notifyObserver) RunSettled(ctx context.Context, outcome prompt.RunOutcome) {
	if o == nil || outcome.ScheduledTaskID != "" {
		return
	}
	if outcome.Synthetic == servicechat.SyntheticGitHubReview {
		return
	}
	// Observers run on the run goroutine, so anything that can wait on a
	// network call has to leave it. Only the path that may ask the auxiliary
	// model goes asynchronous; without one, the report is published inline
	// exactly as it always was.
	if o.willSummarize(outcome) {
		go o.publishRunSettled(context.Background(), outcome)
		return
	}
	o.publishRunSettled(ctx, outcome)
}

// willSummarize reports whether this outcome would reach the auxiliary model.
// A cancelled or failed run never does: its reason is already one short, exact
// sentence, and paraphrasing an error is how an error stops being useful.
func (o *notifyObserver) willSummarize(outcome prompt.RunOutcome) bool {
	return o.summarizer != nil && !outcome.Cancelled && outcome.Err == nil &&
		strings.TrimSpace(outcome.Output) != ""
}

func (o *notifyObserver) publishRunSettled(ctx context.Context, outcome prompt.RunOutcome) {
	kind, status := servicenotify.RunKind(outcome.Err, outcome.Cancelled)
	event := servicenotify.Event{
		Event:     kind,
		Status:    status,
		Summary:   o.runSummary(ctx, outcome),
		DedupeKey: fmt.Sprintf("run:%s:%d", outcome.ChatID, outcome.RunID),
	}
	o.describeChat(ctx, outcome.ChatID, &event)
	o.notifications.Publish(event)
}

// PublishChatEvent delivers an event another service composed, filling in the
// chat and project identity it cannot see. The post-run driver uses it to
// report an autopilot loop that stopped.
func (o *notifyObserver) PublishChatEvent(
	ctx context.Context,
	chatID servicechat.ID,
	event servicenotify.Event,
) {
	if o == nil {
		return
	}
	o.describeChat(ctx, chatID, &event)
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
	switch {
	case result.SkippedByGate:
		// A gate skip is not a failure: nothing ran, and nothing is wrong.
		status = servicenotify.StatusSkipped
		detail = "Skipped by its condition — " + result.GateReason
	case result.Err != nil:
		status = servicenotify.StatusFailed
		detail = result.Err.Error()
	}
	event := servicenotify.Event{
		Event:     servicenotify.KindScheduledRun,
		Status:    status,
		Summary:   scheduleSummary(scheduleLabel(task.Name, result), detail),
		DedupeKey: fmt.Sprintf("schedule:%s:%d", task.ID, task.LastRunAt),
	}
	o.describeChat(ctx, task.ChatID, &event)
	o.notifications.Publish(event)
}

// scheduleLabel decorates the task name with its chain position, so a chained
// run reads "deploy (chain 2/3)" instead of looking like an isolated fire.
func scheduleLabel(taskName string, result serviceschedule.RunResult) string {
	label := result.Chain.Label()
	if label == "" {
		return taskName
	}
	if strings.TrimSpace(taskName) == "" {
		return label
	}
	return taskName + " (" + label + ")"
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

// SiteStateChanged reports one of the operator's client websites changing
// state. The watcher has already applied its two-consecutive-checks rule, so
// every call here is a settled change and deserves exactly one message.
//
// The deep link points at the linked project when there is one, because that
// is where the person who can fix the site works; an unlinked site links to
// the application root, and the body names the page to open.
func (o *notifyObserver) SiteStateChanged(
	_ context.Context,
	site servicesitewatch.Site,
	alert servicesitewatch.Alert,
) {
	if o == nil {
		return
	}
	event := servicenotify.Event{
		Event:     servicenotify.KindSiteWatch,
		ProjectID: site.ProjectID,
		Status:    siteAlertStatus(alert),
		Summary:   servicenotify.Summary(alert.Summary),
		At:        alert.At,
		DedupeKey: alert.DedupeKey,
		URL:       servicenotify.ProjectURL(o.baseURL, site.ProjectID),
	}
	if site.ProjectID != "" && o.projects != nil {
		if project, err := o.projects.Get(context.Background(), serviceproject.ID(site.ProjectID)); err == nil {
			event.ProjectName = project.Name
			event.ProjectSlug = project.Slug
		}
	}
	o.notifications.Publish(event)
}

// siteAlertStatus maps a site's traffic light onto the same three words the
// health events use, so a webhook consumer switches on one vocabulary.
func siteAlertStatus(alert servicesitewatch.Alert) string {
	switch alert.Kind {
	case servicesitewatch.AlertDown:
		return servicenotify.StatusHealthCrit
	case servicesitewatch.AlertRecovered:
		return servicenotify.StatusHealthOK
	default:
		return servicenotify.StatusHealthWarn
	}
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
//
// When the auxiliary model is configured it gets first refusal on a
// *successful* run: a phone is a bad place to read the last 500 characters of
// a coding agent's answer, and one sentence saying what changed is what the
// notification is for. A failure already has a short, exact reason of its
// own, so no model is asked to paraphrase it.
func (o *notifyObserver) runSummary(ctx context.Context, outcome prompt.RunOutcome) string {
	if outcome.Cancelled {
		return "The run was cancelled before it finished."
	}
	if outcome.Err != nil {
		return servicenotify.Summary(outcome.Err.Error())
	}
	if o.summarizer != nil && strings.TrimSpace(outcome.Output) != "" {
		if summary := strings.TrimSpace(o.summarizer.Summarize(ctx, outcome.Output)); summary != "" {
			return servicenotify.Summary(summary)
		}
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

// gitHubNotifier translates the GitHub integration's sink-neutral event into
// the notification service's vocabulary, so neither package imports the
// other's types. It exists for the same reason screenshotNotifier does: the
// feature knows what happened, the observer knows which chat and project it
// happened in, and only the composition root may see both.
type gitHubNotifier struct {
	observer *notifyObserver
}

var _ servicegithub.Notifier = gitHubNotifier{}

func (n gitHubNotifier) PublishChatEvent(
	ctx context.Context,
	chatID servicechat.ID,
	event servicegithub.NotifyEvent,
) {
	if n.observer == nil {
		return
	}
	kind := servicenotify.KindRunFinished
	if event.Failed {
		kind = servicenotify.KindRunFailed
	} else if event.Status == servicenotify.StatusWaiting {
		// A delivery that stopped at the autoRun gate is precisely the
		// "somebody has to look at this" case the needs-attention event
		// exists for.
		kind = servicenotify.KindNeedsAttention
	}
	n.observer.PublishChatEvent(ctx, chatID, servicenotify.Event{
		Event:     kind,
		Status:    event.Status,
		Summary:   servicenotify.Summary(event.Summary),
		DedupeKey: event.DedupeKey,
		// The issue link replaces the chat deep link the observer would
		// otherwise fill in: for a run that came from GitHub, the thread that
		// asked for it is the more useful place to land.
		URL: event.URL,
	})
}
