// Package lighthouse runs Google's Lighthouse against a project's own pages,
// inside the project's own container.
//
// The alternative is the PageSpeed Insights API, and it is a bad fit for the
// work this platform is for. It needs a key, it is rate limited, it can only
// reach pages that are already published, and it audits the one URL you hand
// it. A site being rebuilt is none of those things: it is unpublished, it is
// behind a login, it has twenty templates, and it gets measured twenty times a
// day rather than once a week.
//
// Running the real Lighthouse locally answers the same numbers with none of
// that. The container already has the headless Chromium that Playwright
// installed, so the only new thing is the CLI.
//
// Two things this package deliberately does not do:
//
//   - It does not store or serve Lighthouse's HTML report. That report is a
//     full HTML document containing the audited page's own titles, URLs and
//     screenshots, and serving it from the platform's origin would hand a
//     hostile page a script in the operator's session. The numbers are parsed
//     out and rendered natively instead.
//   - It does not claim to be a field measurement. These are lab numbers from
//     one machine under simulated throttling, which is what PageSpeed's own
//     lab half is; real-user data is a different thing and this never pretends
//     otherwise.
package lighthouse

import (
	"errors"
	"strings"
	"time"
)

// ID identifies one audit run inside a project.
type ID string

// Status is the lifecycle of one run. An audit is tens of seconds per page, so
// runs happen in the background and can fail halfway.
type Status string

const (
	StatusRunning Status = "running"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

func (s Status) Terminal() bool { return s == StatusReady || s == StatusFailed }

// FormFactor is the device Lighthouse emulates.
type FormFactor string

const (
	// FormFactorMobile is the default, because it is the number Google ranks
	// on. An operator who only ever looks at desktop is measuring something
	// their search traffic does not experience.
	FormFactorMobile  FormFactor = "mobile"
	FormFactorDesktop FormFactor = "desktop"
)

// NormalizeFormFactor maps an incoming value onto the two supported ones.
// Anything unrecognized becomes mobile rather than an error: the caller asked
// for an audit, and refusing one over a typo in an optional field would be
// pedantry.
func NormalizeFormFactor(value string) FormFactor {
	if strings.EqualFold(strings.TrimSpace(value), string(FormFactorDesktop)) {
		return FormFactorDesktop
	}
	return FormFactorMobile
}

const (
	// MaxPaths bounds one run. Lighthouse is roughly half a minute per page
	// with throttling simulated on top, so six is a few minutes — enough to
	// cover a site's real templates without holding the container hostage.
	MaxPaths = 6

	// RetentionCount is how many runs a project keeps. It is deliberately
	// larger than the number of pages in a run: the point of keeping them is
	// the trend, and a fortnight of weekly checks has to fit.
	RetentionCount = 20

	// PageTimeout bounds one page's audit and RunTimeout the whole set.
	// Lighthouse's own default is generous; a page that cannot finish inside
	// two minutes is not slow, it is broken, and saying so quickly is more
	// useful than waiting.
	PageTimeout = 2 * time.Minute
	RunTimeout  = 12 * time.Minute

	// InstallTimeout bounds the one-off npm install of the CLI into a
	// container that predates it.
	InstallTimeout = 5 * time.Minute

	// MaxReportBytes rejects a report too large to be worth parsing. A real
	// one is a few hundred kilobytes; ten megabytes means something other
	// than a report came back over the container CLI.
	MaxReportBytes = 10 << 20

	// MaxOpportunities is how many failing audits one page reports. The full
	// list runs to dozens and reads as noise; the worst handful is what
	// actually gets fixed.
	MaxOpportunities = 8
)

var (
	ErrUnavailable  = errors.New("lighthouse audits are not configured on this server")
	ErrNotRunning   = errors.New("the project container is not running")
	ErrNotFound     = errors.New("audit run not found")
	ErrBusy         = errors.New("an audit is already running for this project")
	ErrNoPaths      = errors.New("give at least one page path to audit")
	ErrTooManyPaths = errors.New("one audit run covers at most 6 pages")
	ErrInvalidPort  = errors.New("preview port must be between 1024 and 65535")
	ErrInvalidPath  = errors.New("audit path must start with / and must not contain '..'")
	ErrBadReport    = errors.New("lighthouse did not return a report this platform could read")
)

// ErrToolingMissing reports that the container has no Lighthouse CLI. It is a
// 409 with a fix rather than a 500, because the operator resolves it with one
// button and not by retrying.
var ErrToolingMissing = errors.New(
	"the Lighthouse CLI is not installed in this container: install it from this panel, " +
		"or rebuild the project base image (new images ship with it)",
)

const (
	minPort       = 1024
	maxPort       = 65535
	maxPathLength = 512
	// unsafePathChars are the characters that must never reach a
	// shell-adjacent container command.
	unsafePathChars = " \t\r\n\"'\\"
)

// RunInput is the caller's half of "audit these pages".
type RunInput struct {
	Port int `json:"port"`
	// Paths are in-app paths, each rooted. Duplicates are collapsed.
	Paths      []string `json:"paths"`
	FormFactor string   `json:"formFactor,omitempty"`
	Label      string   `json:"label,omitempty"`
}

// Normalize fills in the defaults and rejects everything the container should
// never be asked for. It is pure, so the transport can trust the service and a
// test can drive it without a container.
func (in RunInput) Normalize() (RunInput, error) {
	out := in
	if out.Port < minPort || out.Port > maxPort {
		return RunInput{}, ErrInvalidPort
	}

	paths := make([]string, 0, len(out.Paths))
	seen := make(map[string]struct{}, len(out.Paths))
	for _, raw := range out.Paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if !strings.HasPrefix(path, "/") ||
			strings.Contains(path, "..") ||
			len(path) > maxPathLength ||
			strings.ContainsAny(path, unsafePathChars) {
			return RunInput{}, ErrInvalidPath
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return RunInput{}, ErrNoPaths
	}
	if len(paths) > MaxPaths {
		return RunInput{}, ErrTooManyPaths
	}
	out.Paths = paths
	out.FormFactor = string(NormalizeFormFactor(out.FormFactor))

	label := strings.TrimSpace(out.Label)
	const labelLimit = 120
	if len(label) > labelLimit {
		label = label[:labelLimit]
	}
	out.Label = label
	return out, nil
}

// Run is one audit of one or more pages.
type Run struct {
	ID         ID         `json:"id"`
	Status     Status     `json:"status"`
	Error      string     `json:"error,omitempty"`
	Label      string     `json:"label,omitempty"`
	Port       int        `json:"port"`
	Paths      []string   `json:"paths"`
	FormFactor FormFactor `json:"formFactor"`
	// Reports is one entry per path, in the order they were requested.
	Reports    []Report `json:"reports"`
	CreatedBy  string   `json:"createdBy,omitempty"`
	CreatedAt  int64    `json:"createdAt"`
	FinishedAt int64    `json:"finishedAt,omitempty"`
}

// Report is one page's audit.
//
// Every score is a pointer. Lighthouse omits a category's score when it could
// not compute one, and a missing score rendered as zero would tell the operator
// their page failed completely when in fact it was never measured.
type Report struct {
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`

	Performance   *int `json:"performance,omitempty"`
	Accessibility *int `json:"accessibility,omitempty"`
	BestPractices *int `json:"bestPractices,omitempty"`
	SEO           *int `json:"seo,omitempty"`

	Metrics       []Metric  `json:"metrics,omitempty"`
	Opportunities []Finding `json:"opportunities,omitempty"`

	Version   string `json:"version,omitempty"`
	FetchedAt int64  `json:"fetchedAt,omitempty"`
}

// Measured reports whether this page produced numbers.
func (r Report) Measured() bool { return r.Error == "" && r.Performance != nil }

// Metric is one timing, in the units Lighthouse reports it.
type Metric struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Display is Lighthouse's own rendering ("1.2 s", "0.05"), kept verbatim
	// so the platform never disagrees with the tool about what a number reads
	// as.
	Display string  `json:"display,omitempty"`
	Value   float64 `json:"value"`
	Unit    string  `json:"unit,omitempty"`
	// Score is 0..1, or nil where the metric carries no pass/fail of its own.
	Score *float64 `json:"score,omitempty"`
}

// Finding is one failing audit worth acting on.
type Finding struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Category string   `json:"category,omitempty"`
	Display  string   `json:"display,omitempty"`
	Score    *float64 `json:"score,omitempty"`
	// SavingsMs is what Lighthouse estimates fixing it would return, where it
	// estimates anything at all.
	SavingsMs float64 `json:"savingsMs,omitempty"`
}

// TrackedMetrics are the timings this platform keeps, and the order it shows
// them in.
//
// It is a fixed list rather than everything Lighthouse produces: the full set
// is over a hundred audits, and a panel that shows all of them is one nobody
// reads. These are the ones a decision gets made on — the three Core Web
// Vitals Google ranks with, plus the three that explain them.
var TrackedMetrics = []struct {
	ID    string
	Label string
}{
	{"largest-contentful-paint", "Largest Contentful Paint"},
	{"cumulative-layout-shift", "Cumulative Layout Shift"},
	{"total-blocking-time", "Total Blocking Time"},
	{"first-contentful-paint", "First Contentful Paint"},
	{"speed-index", "Speed Index"},
	{"server-response-time", "Server Response Time"},
}

// State is one project's stored audit history.
type State struct {
	Runs []Run `json:"runs,omitempty"`
}

// Overview is everything the project's audit panel renders in one request.
type Overview struct {
	Runs []Run `json:"runs"`
	// Running reports an audit in flight, so the panel polls rather than
	// offering a button guaranteed to answer ErrBusy.
	Running bool `json:"running"`
	// Installed reports whether the container has the CLI, so the panel can
	// offer the one-off install instead of a run that is certain to fail.
	// Nil means the question was not asked (the container is not running).
	Installed *bool `json:"installed,omitempty"`
}
