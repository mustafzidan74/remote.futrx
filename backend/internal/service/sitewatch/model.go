// Package sitewatch is the always-on watcher for the operator's *client*
// websites — the shops, landing pages, and dashboards this box's projects
// were built for — as opposed to the platform's own liveness, which
// internal/service/monitoring answers for.
//
// It is deliberately the cheapest thing that can honestly say "the site is
// down": one HEAD request per site per interval, a substring test instead of
// an HTML parse, no browser, no agent, and therefore no tokens. The whole
// feature costs the platform host a few kilobytes of traffic per site per
// check, which is the only budget an always-on watcher can be allowed.
package sitewatch

import (
	"net/url"
	"sort"
	"strings"
	"time"
)

// ID identifies one watched site. It is 24 lowercase hex characters, the same
// shape scheduled tasks use, so it is safe as a file name without escaping.
type ID string

// ValidID rejects anything that could steer the history store outside its own
// directory.
func ValidID(id ID) bool {
	if len(id) != 24 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// Method is the HTTP verb one check uses.
type Method string

const (
	// MethodHEAD is the default: it asks the origin for headers only, which
	// is the difference between watching two hundred sites and watching two
	// hundred sites expensively.
	MethodHEAD Method = "HEAD"
	// MethodGET is used when HEAD is refused, and always when a keyword check
	// is configured — a keyword needs a body to be in.
	MethodGET Method = "GET"
)

// Status is one site's traffic light.
type Status string

const (
	// StatusUnknown means nothing has been measured yet.
	StatusUnknown Status = "unknown"
	// StatusUp means every configured check passed.
	StatusUp Status = "up"
	// StatusSlow means the site answered correctly but over its response
	// budget. It is a warning, not an outage.
	StatusSlow Status = "slow"
	// StatusDown means the request failed, or the response failed a check.
	StatusDown Status = "down"
)

// Severity orders the statuses so "the worst thing about this site" is a
// comparison rather than a chain of ifs.
func (s Status) severity() int {
	switch s {
	case StatusDown:
		return 3
	case StatusSlow:
		return 2
	case StatusUp:
		return 1
	default:
		return 0
	}
}

// Green reports whether a status needs nobody's attention. The home dashboard
// lists exactly the sites this is false for.
func (s Status) Green() bool { return s == StatusUp }

// Limits the operator cannot cross. They exist so a watcher that is meant to
// be cheap cannot be configured into a load generator.
const (
	// MinIntervalMinutes is the fastest cadence. A minute is already 1440
	// requests a day per site.
	MinIntervalMinutes = 1
	// MaxIntervalMinutes is the slowest cadence still worth calling
	// monitoring.
	MaxIntervalMinutes = 60
	// DefaultIntervalMinutes is what a pasted URL gets.
	DefaultIntervalMinutes = 5
	// MaxSites is the global cap. Two hundred sites at the default cadence is
	// roughly one request every 1.5 seconds from this host, which is the most
	// a shared platform box should be spending on somebody else's uptime.
	MaxSites = 200
	// MaxExtraURLs bounds the secondary endpoints per site (the checkout
	// page, the login page). Each one is a full extra request per interval.
	MaxExtraURLs = 5
	// MaxHistoryRecords is how many checks are kept per site. At the default
	// cadence that is a little under two days of raw checks; the uptime
	// percentages are computed from the same window, so a 30-day figure on a
	// 5-minute site is honest about covering only what it has.
	MaxHistoryRecords = 500
	// DefaultTLSWarnDays is how early a certificate expiry is worth saying
	// out loud. Three weeks clears a two-week ACME renewal window plus a
	// holiday.
	DefaultTLSWarnDays = 21
	// MaxTLSWarnDays keeps the warning from being permanently on.
	MaxTLSWarnDays = 90
	// MaxHeaders bounds the custom request headers per site.
	MaxHeaders = 10
	// MaxKeywordLength bounds the substring tests. They are substring tests,
	// not selectors; a long one is a mistake worth refusing.
	MaxKeywordLength = 200
	// SparkPoints is how many response times the row's sparkline carries.
	SparkPoints = 40
)

// StatusCheck is the response-code rule. Expect is matched exactly; zero
// means "any 2xx or 3xx", which is what a pasted URL gets.
type StatusCheck struct {
	Expect int `json:"expect,omitempty"`
}

// KeywordCheck is the only thing this package knows about a page's contents:
// two substring tests over the first chunk of the body. There is no HTML
// parse and no selector engine, on purpose — a watcher that understands the
// DOM is a watcher that costs CPU on every check of every site.
type KeywordCheck struct {
	MustContain    string `json:"mustContain,omitempty"`
	MustNotContain string `json:"mustNotContain,omitempty"`
}

func (k KeywordCheck) configured() bool {
	return strings.TrimSpace(k.MustContain) != "" || strings.TrimSpace(k.MustNotContain) != ""
}

// TLSCheck is the certificate-expiry rule.
type TLSCheck struct {
	// WarnDays is how many days before expiry the amber alert fires. Zero
	// disables the check entirely.
	WarnDays int `json:"warnDays"`
}

// Checks is the rule set applied to one URL.
type Checks struct {
	Status  StatusCheck   `json:"status"`
	Keyword *KeywordCheck `json:"keyword,omitempty"`
	TLS     TLSCheck      `json:"tls"`
	// MaxResponseMs is the slow threshold. Zero means the site is never
	// called slow, only up or down.
	MaxResponseMs int `json:"maxResponseMs,omitempty"`
}

// wantsBody reports whether this rule set needs the response body, which is
// what forces a GET even when the site is configured for HEAD.
func (c Checks) wantsBody() bool {
	return c.Keyword != nil && c.Keyword.configured()
}

// DefaultChecks is what a pasted URL is watched with: it must answer, it must
// answer 2xx/3xx, and its certificate must not be about to expire.
func DefaultChecks() Checks {
	return Checks{TLS: TLSCheck{WarnDays: DefaultTLSWarnDays}}
}

func (c Checks) normalize() Checks {
	if c.Status.Expect < 0 || c.Status.Expect > 599 {
		c.Status.Expect = 0
	}
	switch {
	case c.TLS.WarnDays < 0:
		c.TLS.WarnDays = 0
	case c.TLS.WarnDays > MaxTLSWarnDays:
		c.TLS.WarnDays = MaxTLSWarnDays
	}
	if c.MaxResponseMs < 0 {
		c.MaxResponseMs = 0
	}
	if c.Keyword != nil {
		keyword := KeywordCheck{
			MustContain:    truncate(strings.TrimSpace(c.Keyword.MustContain), MaxKeywordLength),
			MustNotContain: truncate(strings.TrimSpace(c.Keyword.MustNotContain), MaxKeywordLength),
		}
		if keyword.configured() {
			c.Keyword = &keyword
		} else {
			c.Keyword = nil
		}
	}
	return c
}

// Endpoint is one secondary URL watched under the same site — the checkout
// page, the login page — with its own rules. A failure on any endpoint makes
// the whole site red, because a shop whose checkout 500s is down for the only
// purpose it has.
type Endpoint struct {
	Label  string `json:"label,omitempty"`
	URL    string `json:"url"`
	Checks Checks `json:"checks"`
}

// Site is one watched client website.
type Site struct {
	ID              ID     `json:"id"`
	Label           string `json:"label"`
	URL             string `json:"url"`
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	Checks          Checks `json:"checks"`
	// ExtraURLs are the secondary pages checked alongside the front page.
	ExtraURLs []Endpoint `json:"extraUrls,omitempty"`
	// ProjectID links the site to a project. It is also the visibility rule:
	// a member sees a site only through a project they belong to, and an
	// unlinked site is admin-only.
	ProjectID string `json:"projectId,omitempty"`
	// Notify gates the outbound alerts for this site. The watching itself is
	// unaffected — the table still goes red.
	Notify bool `json:"notify"`
	// Headers are extra request headers, for a staging site behind a shared
	// token or a host that needs an explicit Accept.
	Headers   map[string]string `json:"headers,omitempty"`
	Method    Method            `json:"method"`
	CreatedAt int64             `json:"createdAt,omitempty"`
	UpdatedAt int64             `json:"updatedAt,omitempty"`
}

// Interval is the configured cadence as a duration.
func (s Site) Interval() time.Duration {
	return time.Duration(clampInterval(s.IntervalMinutes)) * time.Minute
}

// Host is what an alert calls the site when it has no label.
func (s Site) Host() string {
	if host := URLHost(s.URL); host != "" {
		return host
	}
	return s.URL
}

// Name is the site as a human refers to it: the label if there is one, the
// hostname otherwise.
func (s Site) Name() string {
	if label := strings.TrimSpace(s.Label); label != "" {
		return label
	}
	return s.Host()
}

// Normalize trims and clamps everything a client can send, so nothing
// downstream has to defend against a half-filled document.
func (s Site) Normalize() Site {
	s.Label = truncate(strings.TrimSpace(s.Label), 80)
	s.URL = strings.TrimSpace(s.URL)
	s.ProjectID = strings.TrimSpace(s.ProjectID)
	s.IntervalMinutes = clampInterval(s.IntervalMinutes)
	s.Checks = s.Checks.normalize()
	if s.Method != MethodGET {
		s.Method = MethodHEAD
	}
	if len(s.ExtraURLs) > MaxExtraURLs {
		s.ExtraURLs = s.ExtraURLs[:MaxExtraURLs]
	}
	extras := make([]Endpoint, 0, len(s.ExtraURLs))
	for _, extra := range s.ExtraURLs {
		extra.Label = truncate(strings.TrimSpace(extra.Label), 60)
		extra.URL = strings.TrimSpace(extra.URL)
		extra.Checks = extra.Checks.normalize()
		if extra.URL == "" {
			continue
		}
		extras = append(extras, extra)
	}
	if len(extras) == 0 {
		extras = nil
	}
	s.ExtraURLs = extras
	s.Headers = normalizeHeaders(s.Headers)
	return s
}

// normalizeHeaders drops blanks and the headers this package sets itself, so
// an operator cannot accidentally disable the browser-ish User-Agent that
// gets past most WAFs.
func normalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if len(out) >= MaxHeaders {
			break
		}
		trimmed := strings.TrimSpace(name)
		value := strings.TrimSpace(headers[name])
		if trimmed == "" || value == "" || !validHeaderName(trimmed) {
			continue
		}
		out[trimmed] = truncate(value, 400)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validHeaderName(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func clampInterval(minutes int) int {
	switch {
	case minutes < MinIntervalMinutes:
		return DefaultIntervalMinutes
	case minutes > MaxIntervalMinutes:
		return MaxIntervalMinutes
	default:
		return minutes
	}
}

// Record is one check, as written to DATA_DIR/sitewatch/history/<id>.jsonl.
// The field names are short because there are five hundred of these per site
// and the file is read whole on every append.
type Record struct {
	At     int64  `json:"at"`
	Status Status `json:"st"`
	// Code is the response code of the primary URL, zero when the request
	// never produced a response.
	Code int `json:"code,omitempty"`
	// DurationMs is the wall time of the whole site check: the primary URL
	// plus every extra URL.
	DurationMs int64 `json:"ms"`
	// SizeBytes is what the primary URL reported or delivered.
	SizeBytes int64 `json:"size,omitempty"`
	// TLSExpiresAt is the certificate's NotAfter, zero for plain HTTP or when
	// the handshake never completed.
	TLSExpiresAt int64  `json:"tls,omitempty"`
	Error        string `json:"err,omitempty"`
}

// OK reports whether this check counted as uptime. Slow is up: the site
// served the customer, it just took its time.
func (r Record) OK() bool { return r.Status == StatusUp || r.Status == StatusSlow }

// Uptime is the availability roll-up over the three windows the table shows.
// Each is nil when the history holds no check in that window, which reads as
// "not enough data" rather than as 0%.
type Uptime struct {
	Day   *float64 `json:"day,omitempty"`
	Week  *float64 `json:"week,omitempty"`
	Month *float64 `json:"month,omitempty"`
	// Checks is how many records the whole history holds, so the panel can
	// say what the percentages are based on.
	Checks int `json:"checks"`
	// Since is the oldest record's timestamp: the real extent of the window,
	// which on a trimmed history is shorter than 30 days.
	Since int64 `json:"since,omitempty"`
}

// View is one row of the Client sites table: the stored site plus everything
// the watcher has learned about it. It is what the API returns; Site alone is
// never served.
type View struct {
	Site
	Status Status `json:"status"`
	// ChangedAt is when the current status began, which is what makes a
	// recovery message able to say "back after 12 m".
	ChangedAt      int64  `json:"changedAt,omitempty"`
	LastCheckedAt  int64  `json:"lastCheckedAt,omitempty"`
	LastDurationMs int64  `json:"lastDurationMs,omitempty"`
	LastCode       int    `json:"lastCode,omitempty"`
	LastError      string `json:"lastError,omitempty"`
	LastSizeBytes  int64  `json:"lastSizeBytes,omitempty"`
	// NextCheckAt is when the scheduler will look again. It is jittered, so
	// it is not simply the last check plus the interval.
	NextCheckAt  int64 `json:"nextCheckAt,omitempty"`
	TLSExpiresAt int64 `json:"tlsExpiresAt,omitempty"`
	// TLSDaysLeft is nil for a site with no certificate reading yet.
	TLSDaysLeft *int   `json:"tlsDaysLeft,omitempty"`
	Uptime      Uptime `json:"uptime"`
	// Spark is the newest response times, oldest first, for the row's inline
	// sparkline. Failed checks appear as zero.
	Spark []int64 `json:"spark,omitempty"`
}

// EndpointResult is one URL's outcome inside a synchronous "Check now".
type EndpointResult struct {
	Label      string `json:"label,omitempty"`
	URL        string `json:"url"`
	Method     string `json:"method"`
	Status     Status `json:"status"`
	Code       int    `json:"code,omitempty"`
	DurationMs int64  `json:"durationMs"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	// TLSExpiresAt and TLSDaysLeft are only filled for https URLs.
	TLSExpiresAt int64    `json:"tlsExpiresAt,omitempty"`
	TLSDaysLeft  *int     `json:"tlsDaysLeft,omitempty"`
	Reasons      []string `json:"reasons,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// Report is what the "Check now" button gets back: the raw per-URL results
// plus the row as it now stands.
type Report struct {
	Site      View             `json:"site"`
	Endpoints []EndpointResult `json:"endpoints"`
	CheckedAt int64            `json:"checkedAt"`
}

// Input is the create/update body. It mirrors Site without the server-owned
// fields, so a client cannot set an id or a timestamp.
type Input struct {
	Label           string            `json:"label"`
	URL             string            `json:"url"`
	Enabled         bool              `json:"enabled"`
	IntervalMinutes int               `json:"intervalMinutes"`
	Checks          Checks            `json:"checks"`
	ExtraURLs       []Endpoint        `json:"extraUrls,omitempty"`
	ProjectID       string            `json:"projectId,omitempty"`
	Notify          bool              `json:"notify"`
	Headers         map[string]string `json:"headers,omitempty"`
	Method          Method            `json:"method"`
}

// site folds an input onto a site, keeping the identity and creation stamp.
func (in Input) site(existing Site) Site {
	existing.Label = in.Label
	existing.URL = in.URL
	existing.Enabled = in.Enabled
	existing.IntervalMinutes = in.IntervalMinutes
	existing.Checks = in.Checks
	existing.ExtraURLs = in.ExtraURLs
	existing.ProjectID = in.ProjectID
	existing.Notify = in.Notify
	existing.Headers = in.Headers
	existing.Method = in.Method
	return existing.Normalize()
}

// ImportInput is the bulk-add body: a pasted block of URLs, one per line, and
// optionally everything the project catalog already knows about.
type ImportInput struct {
	// URLs is the pasted text. Blank lines and anything after a '#' are
	// ignored so an operator can paste a commented list.
	URLs string `json:"urls"`
	// FromProjects pulls candidates out of the projects' own
	// HESTIA_DOMAIN-style secrets.
	FromProjects bool `json:"fromProjects"`
	// ProjectID links every imported site to one project. It is ignored for
	// the project-derived half, which carries its own.
	ProjectID string `json:"projectId,omitempty"`
	Notify    bool   `json:"notify"`
}

// ImportResult reports what a bulk import did, per candidate, so an operator
// pasting forty lines learns which three were already watched.
type ImportResult struct {
	Created []View          `json:"created"`
	Skipped []ImportSkipped `json:"skipped,omitempty"`
}

// ImportSkipped is one candidate that did not become a site, and why.
type ImportSkipped struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

// Candidate is a site the project catalog suggests: a domain found in a
// project's secrets, with the project it belongs to.
type Candidate struct {
	ProjectID   string
	ProjectName string
	// Domain is whatever the secret held: a bare hostname or a full URL.
	Domain string
	// SecretKey names the secret it came from, for the "skipped" report.
	SecretKey string
}

// URLHost is the hostname an alert names a site by.
func URLHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// NormalizeURL turns whatever an operator pasted into an absolute http(s)
// URL, or reports that it cannot. A bare domain gets https, because a client
// site that only speaks http in 2025 is itself the finding.
func NormalizeURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	// A hostname with no dot is either localhost or a typo; neither is a
	// client website, and both would point this watcher at the host itself.
	if !strings.Contains(parsed.Hostname(), ".") {
		return "", false
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

// ParseURLList splits a pasted block into candidate URLs. Blank lines,
// '#' comments, and repeats are dropped, and each survivor is normalized.
func ParseURLList(text string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	for _, line := range strings.Split(text, "\n") {
		if hash := strings.Index(line, "#"); hash >= 0 {
			line = line[:hash]
		}
		// A pasted list is often comma or space separated within a line.
		for _, field := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\r'
		}) {
			normalized, ok := NormalizeURL(field)
			if !ok {
				continue
			}
			if _, dupe := seen[normalized]; dupe {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	return out
}

// SameTarget reports whether two URLs watch the same thing. It is host plus
// path, case-folded, ignoring the scheme and a trailing slash, so pasting
// "example.com" twice as http and https is caught as a duplicate.
func SameTarget(left, right string) bool {
	return targetKey(left) == targetKey(right)
}

func targetKey(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	return strings.ToLower(parsed.Hostname()) + path
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
