// Package visualdiff answers "what did that change actually look like?".
//
// An agent edits a stylesheet and reports success. The edit did what it said,
// the page it was aimed at looks right, and three pages away a footer has
// collapsed. Nothing in the platform notices, because nothing in the platform
// was looking: the tests pass, the container is healthy, and the preview URL
// returns 200. The damage is found by a client.
//
// So this package photographs a set of pages before the work, photographs them
// again after, and reports which ones moved. The value is not in confirming
// that the page you edited changed — you knew that. It is the page you never
// touched showing up at 4% different.
//
// Two decisions shape the rest:
//
//   - A project has exactly one baseline at a time. Comparisons are cheap and
//     a baseline is a commitment: "this is what correct looked like". Keeping
//     a shelf of them invites comparing against the wrong one, which produces
//     confident nonsense.
//   - A comparison never mutates its baseline. Re-baselining is a separate,
//     deliberate act, because accepting a change is a judgement the operator
//     makes and not something a diff run should make on their behalf.
package visualdiff

import (
	"errors"
	"strings"
	"time"

	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
)

// ID identifies one baseline or one comparison inside a project.
type ID string

// Status is the lifecycle of a baseline or a comparison. Both photograph
// several pages, which is tens of seconds of work, so both run in the
// background and both can fail halfway through.
type Status string

const (
	StatusRunning Status = "running"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

// Terminal reports whether a record has stopped moving.
func (s Status) Terminal() bool { return s == StatusReady || s == StatusFailed }

const (
	// MaxPaths bounds one run. Each page is a browser launch and a page load,
	// so twelve is a few minutes of container work — enough to cover a site's
	// real templates (home, listing, detail, cart, checkout, account) and few
	// enough that a pasted list cannot occupy the container all afternoon.
	MaxPaths = 12

	// DefaultWidth and DefaultHeight are the comparison viewport. It is fixed
	// per baseline rather than per comparison: a diff between two different
	// viewports measures the viewport, not the change.
	DefaultWidth  = servicescreenshot.DefaultWidth
	DefaultHeight = servicescreenshot.DefaultHeight

	// RetentionCount is how many comparisons a project keeps. Older ones and
	// their images are evicted after each finished run.
	RetentionCount = 10

	// PageTimeout bounds one page's capture and RunTimeout the whole set.
	// RunTimeout is deliberately less than MaxPaths x PageTimeout: a run where
	// every page needs its full budget is a wedged container, and waiting nine
	// minutes to discover that helps nobody.
	PageTimeout = 45 * time.Second
	RunTimeout  = 6 * time.Minute

	// MaxPixels bounds one decoded image. A full-page capture of a long
	// marketing site is a few million pixels; forty million is a page that
	// would cost more memory to diff than this is worth.
	MaxPixels = 40 << 20

	// DefaultThreshold is how different two pixels must be before the pixel
	// counts as changed, as a fraction of the maximum perceptual distance.
	//
	// It is not zero. The same browser rendering the same page twice is
	// deterministic, but subpixel text rendering shifts by a hair when a
	// preceding element's height moves by a fraction, and a tool that reports
	// 0.4% for a page nobody touched teaches the operator to ignore it.
	DefaultThreshold = 0.10

	// ChangedPercentFloor is the point below which a page is reported as
	// unchanged. A tenth of a percent is a few hundred pixels somewhere in a
	// full-page shot: real, and never what the operator is looking for.
	ChangedPercentFloor = 0.1
)

var (
	ErrUnavailable  = errors.New("visual comparison is not configured on this server")
	ErrNotRunning   = errors.New("the project container is not running")
	ErrNoBaseline   = errors.New("this project has no baseline yet: take one before comparing")
	ErrNotFound     = errors.New("visual comparison not found")
	ErrBusy         = errors.New("a visual run is already in progress for this project")
	ErrNoPaths      = errors.New("give at least one page path to compare")
	ErrTooManyPaths = errors.New("a visual run covers at most 12 pages")
	ErrImageTooBig  = errors.New("the captured page is too large to compare")
	ErrInvalidPort  = servicescreenshot.ErrInvalidPort
	ErrInvalidPath  = servicescreenshot.ErrInvalidPath
	ErrInvalidSize  = servicescreenshot.ErrInvalidSize
)

// BaselineInput is the caller's half of "remember what this looks like now".
type BaselineInput struct {
	Port int `json:"port"`
	// Paths are in-app paths, each rooted. Duplicates are collapsed so a
	// pasted list cannot photograph the same page four times.
	Paths    []string `json:"paths"`
	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	FullPage bool     `json:"fullPage,omitempty"`
	// Threshold overrides DefaultThreshold for every comparison against this
	// baseline. It lives on the baseline rather than the comparison so two
	// runs of the same baseline are always measured the same way.
	Threshold float64 `json:"threshold,omitempty"`
}

// Normalize fills in the defaults and rejects everything the container should
// never be asked for. It is pure, so the transport can trust that the service
// validated and a test can drive it without a container.
func (in BaselineInput) Normalize() (BaselineInput, error) {
	out := in
	if out.Port < servicescreenshot.MinPort || out.Port > servicescreenshot.MaxPort {
		return BaselineInput{}, ErrInvalidPort
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
			len(path) > servicescreenshot.MaxPathLength ||
			strings.ContainsAny(path, unsafePathChars) {
			return BaselineInput{}, ErrInvalidPath
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return BaselineInput{}, ErrNoPaths
	}
	if len(paths) > MaxPaths {
		return BaselineInput{}, ErrTooManyPaths
	}
	out.Paths = paths

	if out.Width == 0 {
		out.Width = DefaultWidth
	}
	if out.Height == 0 {
		out.Height = DefaultHeight
	}
	if out.Width < servicescreenshot.MinWidth || out.Width > servicescreenshot.MaxWidth ||
		out.Height < servicescreenshot.MinHeight || out.Height > servicescreenshot.MaxHeight {
		return BaselineInput{}, ErrInvalidSize
	}

	// A threshold outside (0,1] is a typo rather than a preference: zero
	// reports every antialiased edge, and anything above one reports nothing.
	if out.Threshold <= 0 || out.Threshold > 1 {
		out.Threshold = DefaultThreshold
	}
	return out, nil
}

// unsafePathChars are the characters that must never reach a shell-adjacent
// container command. It mirrors the screenshot service's rule, which is the
// same browser being pointed at the same kind of URL.
const unsafePathChars = " \t\r\n\"'\\"

// Baseline is what this project looked like when the operator agreed it was
// correct.
type Baseline struct {
	ID        ID       `json:"id"`
	Status    Status   `json:"status"`
	Error     string   `json:"error,omitempty"`
	Port      int      `json:"port"`
	Paths     []string `json:"paths"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	FullPage  bool     `json:"fullPage,omitempty"`
	Threshold float64  `json:"threshold"`
	// Pages is one entry per path, in the order they were requested.
	Pages      []Shot `json:"pages"`
	CreatedBy  string `json:"createdBy,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
}

// Shot is one stored capture belonging to a baseline.
type Shot struct {
	Path string `json:"path"`
	File string `json:"file,omitempty"`
	// URL is the session-gated read route, filled in when a record is served.
	URL    string `json:"url,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	// Error explains one page that could not be photographed. The rest of the
	// run continues: eleven good pages and one failure is a useful answer, and
	// refusing the whole run over a single 404 would not be.
	Error string `json:"error,omitempty"`
}

// Captured reports whether this page has an image behind it.
func (s Shot) Captured() bool { return s.File != "" && s.Error == "" }

// Comparison is one "and now?" against the project's baseline.
type Comparison struct {
	ID         ID     `json:"id"`
	BaselineID ID     `json:"baselineId"`
	Status     Status `json:"status"`
	Error      string `json:"error,omitempty"`
	Label      string `json:"label,omitempty"`
	Pages      []Diff `json:"pages"`
	// ChangedPages and MaxChangedPercent are the headline: how many pages
	// moved, and how far the worst one moved. They are stored rather than
	// recomputed so listing does not have to walk every page of every run.
	ChangedPages      int     `json:"changedPages"`
	MaxChangedPercent float64 `json:"maxChangedPercent"`
	CreatedBy         string  `json:"createdBy,omitempty"`
	CreatedAt         int64   `json:"createdAt"`
	FinishedAt        int64   `json:"finishedAt,omitempty"`
}

// Diff is one page's before and after.
type Diff struct {
	Path string `json:"path"`
	// BeforeFile is the baseline's own image, referenced rather than copied. A
	// comparison holding its own copy would double the storage and let the two
	// drift apart.
	BeforeFile string `json:"beforeFile,omitempty"`
	AfterFile  string `json:"afterFile,omitempty"`
	DiffFile   string `json:"diffFile,omitempty"`
	BeforeURL  string `json:"beforeUrl,omitempty"`
	AfterURL   string `json:"afterUrl,omitempty"`
	DiffURL    string `json:"diffUrl,omitempty"`

	ChangedPercent float64 `json:"changedPercent"`
	ChangedPixels  int64   `json:"changedPixels"`
	Width          int     `json:"width,omitempty"`
	Height         int     `json:"height,omitempty"`
	// SizeChanged reports that the page's own dimensions moved between the two
	// captures. It is called out separately because the percentage understates
	// it: a page that grew 200px taller is wholly different below that point,
	// and a modest number alone reads like a modest change.
	SizeChanged bool   `json:"sizeChanged,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Changed reports whether this page moved enough to be worth attention.
func (d Diff) Changed() bool {
	return d.Error == "" && (d.SizeChanged || d.ChangedPercent >= ChangedPercentFloor)
}

// Overview is everything the project's visual panel renders in one request.
type Overview struct {
	Baseline    *Baseline    `json:"baseline,omitempty"`
	Comparisons []Comparison `json:"comparisons"`
	// Running reports a baseline or comparison in flight, so the panel can
	// poll rather than offer a button that is guaranteed to answer ErrBusy.
	Running bool `json:"running"`
}
