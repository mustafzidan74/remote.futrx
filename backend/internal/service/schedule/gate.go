package schedule

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// ConditionKind names one gate. Gates are evaluated before an occurrence is
// claimed, so a closed gate costs no agent tokens and no container boot.
type ConditionKind string

const (
	ConditionOutputContains ConditionKind = "outputContains"
	ConditionHTTPStatus     ConditionKind = "httpStatus"
	ConditionCommandExit    ConditionKind = "commandExitCode"
	ConditionWeekdays       ConditionKind = "weekdays"
	ConditionNotIfRanWithin ConditionKind = "notIfRanWithin"
)

const (
	// GateCommandTimeout bounds one commandExitCode probe inside the
	// container. The scheduler loop waits on it, so it is deliberately short.
	GateCommandTimeout = 30 * time.Second
	// GateHTTPTimeout bounds one httpStatus probe.
	GateHTTPTimeout = 10 * time.Second
	maxGatePattern  = 512
	maxGateCommand  = 2 << 10
)

// SelfTask is the reserved inLastRunOf value meaning "this task".
const SelfTask = "self"

// Condition is a single pre-run gate. Only the fields its Kind names are
// meaningful; the rest are ignored and stripped on save.
type Condition struct {
	Kind ConditionKind `json:"kind"`

	// outputContains
	Pattern     string `json:"pattern,omitempty"`
	InLastRunOf string `json:"inLastRunOf,omitempty"`

	// httpStatus
	URL string `json:"url,omitempty"`

	// commandExitCode
	Command string `json:"command,omitempty"`

	// Expect is the awaited HTTP status (default 200) or exit code
	// (default 0). A pointer so an explicit 0 is distinguishable from unset.
	Expect *int `json:"expect,omitempty"`

	// weekdays: 0 = Sunday … 6 = Saturday, evaluated in the task's timezone.
	Weekdays []int `json:"weekdays,omitempty"`

	// notIfRanWithin
	Minutes int `json:"minutes,omitempty"`
}

// GateOutcome is the settled verdict of one gate evaluation.
type GateOutcome struct {
	Passed bool
	// Reason is always populated: it is what the skipped occurrence records
	// and what the notification shows.
	Reason string
}

// HTTPProbe answers the httpStatus gate. It is a port so tests decide the
// answer without a network.
type HTTPProbe interface {
	Status(ctx context.Context, url string) (int, error)
}

// CommandResult is one shell probe run inside a project workspace.
type CommandResult struct {
	Output   string
	ExitCode int
}

// GitSnapshot is a best-effort picture of the workspace repository, taken
// before and after a run so history can report what changed.
type GitSnapshot struct {
	// Repository is false when /workspace is not a git checkout, in which
	// case every other field is empty and the caller records nothing.
	Repository bool
	Head       string
	// Status is `git status --porcelain` output.
	Status string
	// DiffStat is `git diff --stat` output for the unstaged working tree.
	DiffStat string
}

// Workspace runs bounded probes inside a task's project container. Every
// method is best effort: a deployment without a container runtime leaves this
// port nil, which closes the commandExitCode gate and leaves run history
// without file information.
type Workspace interface {
	RunCommand(
		ctx context.Context,
		projectID serviceproject.ID,
		command string,
		timeout time.Duration,
	) (CommandResult, error)
	GitSnapshot(ctx context.Context, projectID serviceproject.ID) (GitSnapshot, error)
	// GitShowStat returns `git show --stat <ref>` for a commit the run made.
	GitShowStat(ctx context.Context, projectID serviceproject.ID, ref string) (string, error)
}

func normalizeCondition(condition *Condition) *Condition {
	if condition == nil {
		return nil
	}
	trimmed := Condition{
		Kind:        ConditionKind(strings.TrimSpace(string(condition.Kind))),
		Pattern:     strings.TrimSpace(condition.Pattern),
		InLastRunOf: strings.TrimSpace(condition.InLastRunOf),
		URL:         strings.TrimSpace(condition.URL),
		Command:     strings.TrimSpace(condition.Command),
		Expect:      condition.Expect,
		Minutes:     condition.Minutes,
	}
	if trimmed.Kind == "" {
		// An empty kind is how a caller clears the gate.
		return nil
	}
	// Keep only the fields this kind reads, so a stored task never carries a
	// stale URL or command from an earlier edit.
	switch trimmed.Kind {
	case ConditionOutputContains:
		if trimmed.InLastRunOf == "" {
			trimmed.InLastRunOf = SelfTask
		}
		trimmed.URL, trimmed.Command, trimmed.Expect, trimmed.Minutes = "", "", nil, 0
	case ConditionHTTPStatus:
		trimmed.Pattern, trimmed.InLastRunOf, trimmed.Command, trimmed.Minutes = "", "", "", 0
	case ConditionCommandExit:
		trimmed.Pattern, trimmed.InLastRunOf, trimmed.URL, trimmed.Minutes = "", "", "", 0
	case ConditionWeekdays:
		trimmed.Weekdays = normalizeWeekdays(condition.Weekdays)
		trimmed.Pattern, trimmed.InLastRunOf, trimmed.URL, trimmed.Command = "", "", "", ""
		trimmed.Expect, trimmed.Minutes = nil, 0
	case ConditionNotIfRanWithin:
		trimmed.Pattern, trimmed.InLastRunOf, trimmed.URL, trimmed.Command = "", "", "", ""
		trimmed.Expect = nil
	}
	return &trimmed
}

func normalizeWeekdays(days []int) []int {
	seen := make(map[int]struct{}, len(days))
	normalized := make([]int, 0, len(days))
	for _, day := range days {
		if day < 0 || day > 6 {
			// Out-of-range values are kept so validation can reject them
			// rather than silently changing which days a task fires on.
			normalized = append(normalized, day)
			continue
		}
		if _, duplicate := seen[day]; duplicate {
			continue
		}
		seen[day] = struct{}{}
		normalized = append(normalized, day)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func validateCondition(task Task) error {
	condition := task.Condition
	if condition == nil {
		return nil
	}
	switch condition.Kind {
	case ConditionOutputContains:
		if condition.Pattern == "" {
			return ErrGatePatternRequired
		}
		if len(condition.Pattern) > maxGatePattern {
			return fmt.Errorf("%w (at most %d bytes)", ErrGatePatternRequired, maxGatePattern)
		}
		if _, err := regexp.Compile(condition.Pattern); err != nil {
			return fmt.Errorf("%w: %s", ErrGateInvalidPattern, err)
		}
		if condition.InLastRunOf != SelfTask && !ValidID(ID(condition.InLastRunOf)) {
			return ErrGateInvalidReference
		}
	case ConditionHTTPStatus:
		parsed, err := url.Parse(condition.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return ErrGateInvalidURL
		}
		if expect := conditionExpect(condition, http.StatusOK); expect < 100 || expect > 599 {
			return ErrGateInvalidExpect
		}
	case ConditionCommandExit:
		if condition.Command == "" {
			return ErrGateCommandRequired
		}
		if len(condition.Command) > maxGateCommand {
			return fmt.Errorf("%w (at most %d bytes)", ErrGateCommandRequired, maxGateCommand)
		}
		if expect := conditionExpect(condition, 0); expect < 0 || expect > 255 {
			return ErrGateInvalidExpect
		}
	case ConditionWeekdays:
		if len(condition.Weekdays) == 0 {
			return ErrGateWeekdaysRequired
		}
		for _, day := range condition.Weekdays {
			if day < 0 || day > 6 {
				return ErrGateWeekdaysRequired
			}
		}
	case ConditionNotIfRanWithin:
		if condition.Minutes <= 0 || condition.Minutes > 365*24*60 {
			return ErrGateInvalidMinutes
		}
	default:
		return fmt.Errorf("%w %q", ErrGateInvalidKind, condition.Kind)
	}
	return nil
}

func conditionExpect(condition *Condition, fallback int) int {
	if condition == nil || condition.Expect == nil {
		return fallback
	}
	return *condition.Expect
}

// evaluateGate answers whether an occurrence of task may start. A gate that
// cannot be evaluated (missing port, unreachable probe) fails closed: skipping
// a run is recoverable, running one the operator gated off is not.
func (s *Service) evaluateGate(ctx context.Context, task Task, now time.Time) GateOutcome {
	condition := task.Condition
	if condition == nil {
		return GateOutcome{Passed: true}
	}
	switch condition.Kind {
	case ConditionWeekdays:
		return s.evaluateWeekdays(task, now)
	case ConditionNotIfRanWithin:
		return evaluateNotIfRanWithin(task, now)
	case ConditionOutputContains:
		return s.evaluateOutputContains(ctx, task)
	case ConditionHTTPStatus:
		return s.evaluateHTTPStatus(ctx, condition)
	case ConditionCommandExit:
		return s.evaluateCommandExit(ctx, task, condition)
	default:
		return GateOutcome{Reason: fmt.Sprintf("unknown gate kind %q", condition.Kind)}
	}
}

func (s *Service) evaluateWeekdays(task Task, now time.Time) GateOutcome {
	location, err := time.LoadLocation(task.Timezone)
	if err != nil {
		location = time.UTC
	}
	today := int(now.In(location).Weekday())
	for _, day := range task.Condition.Weekdays {
		if day == today {
			return GateOutcome{Passed: true, Reason: "weekday " + weekdayName(today) + " is allowed"}
		}
	}
	return GateOutcome{Reason: "weekday " + weekdayName(today) + " is not in the allowed set"}
}

func evaluateNotIfRanWithin(task Task, now time.Time) GateOutcome {
	window := int64(task.Condition.Minutes) * 60_000
	// LastRunEnd is zero until a run settles; fall back to the start so a
	// still-running or crashed occurrence still counts as "ran recently".
	last := task.LastRunEnd
	if last == 0 {
		last = task.LastRunAt
	}
	if last == 0 {
		return GateOutcome{Passed: true, Reason: "no previous run"}
	}
	elapsed := now.UnixMilli() - last
	if elapsed < window {
		return GateOutcome{Reason: fmt.Sprintf(
			"last run was %s ago, inside the %d-minute window",
			time.Duration(elapsed)*time.Millisecond, task.Condition.Minutes,
		)}
	}
	return GateOutcome{Passed: true}
}

func (s *Service) evaluateOutputContains(ctx context.Context, task Task) GateOutcome {
	condition := task.Condition
	pattern, err := regexp.Compile(condition.Pattern)
	if err != nil {
		return GateOutcome{Reason: "gate pattern no longer compiles: " + err.Error()}
	}
	source := task
	if condition.InLastRunOf != SelfTask {
		referenced, err := s.repo.Get(ctx, ID(condition.InLastRunOf))
		if err != nil {
			return GateOutcome{Reason: "referenced task is unavailable: " + err.Error()}
		}
		if referenced.ProjectID != task.ProjectID {
			return GateOutcome{Reason: "referenced task belongs to another project"}
		}
		source = referenced
	}
	text := s.lastRunText(ctx, source)
	if strings.TrimSpace(text) == "" {
		return GateOutcome{Reason: "referenced task has no recorded run output yet"}
	}
	if pattern.MatchString(text) {
		return GateOutcome{Passed: true}
	}
	return GateOutcome{Reason: fmt.Sprintf(
		"last run of %q does not match /%s/", source.Name, condition.Pattern,
	)}
}

// lastRunText is what an outputContains gate matches against: the verdict
// marker first, then the stored summary of the most recent recorded run.
func (s *Service) lastRunText(ctx context.Context, task Task) string {
	parts := make([]string, 0, 3)
	if task.LastResult != "" {
		parts = append(parts, task.LastResult)
	}
	if s.history != nil {
		if records, err := s.history.List(ctx, task.ID); err == nil && len(records) > 0 {
			latest := records[len(records)-1]
			if latest.Result != "" && latest.Result != task.LastResult {
				parts = append(parts, latest.Result)
			}
			if latest.Summary != "" {
				parts = append(parts, latest.Summary)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (s *Service) evaluateHTTPStatus(ctx context.Context, condition *Condition) GateOutcome {
	probe := s.httpProbe
	if probe == nil {
		return GateOutcome{Reason: "no HTTP probe is configured"}
	}
	expect := conditionExpect(condition, http.StatusOK)
	ctx, cancel := context.WithTimeout(ctx, GateHTTPTimeout)
	defer cancel()
	status, err := probe.Status(ctx, condition.URL)
	if err != nil {
		return GateOutcome{Reason: "probe " + condition.URL + " failed: " + err.Error()}
	}
	if status != expect {
		return GateOutcome{Reason: fmt.Sprintf(
			"%s answered %d, expected %d", condition.URL, status, expect,
		)}
	}
	return GateOutcome{Passed: true}
}

func (s *Service) evaluateCommandExit(
	ctx context.Context,
	task Task,
	condition *Condition,
) GateOutcome {
	if s.workspace == nil {
		return GateOutcome{Reason: "no workspace command runner is configured"}
	}
	expect := conditionExpect(condition, 0)
	result, err := s.workspace.RunCommand(
		ctx, task.ProjectID, condition.Command, GateCommandTimeout,
	)
	if err != nil {
		return GateOutcome{Reason: "gate command could not run: " + err.Error()}
	}
	if result.ExitCode != expect {
		return GateOutcome{Reason: fmt.Sprintf(
			"gate command exited %d, expected %d%s",
			result.ExitCode, expect, commandTail(result.Output),
		)}
	}
	return GateOutcome{Passed: true}
}

func commandTail(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	const limit = 200
	if len(output) > limit {
		output = output[len(output)-limit:]
	}
	return ": " + output
}

func weekdayName(day int) string {
	if day < 0 || day > 6 {
		return "unknown"
	}
	return time.Weekday(day).String()
}

// httpStatusProbe is the default httpStatus transport: one GET, redirects not
// followed, so the gate observes the status the operator named rather than
// wherever it points.
type httpStatusProbe struct {
	client *http.Client
}

func newHTTPStatusProbe() *httpStatusProbe {
	return &httpStatusProbe{client: &http.Client{
		Timeout: GateHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (p *httpStatusProbe) Status(ctx context.Context, target string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}
