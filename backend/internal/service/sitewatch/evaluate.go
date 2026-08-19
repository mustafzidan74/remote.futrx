package sitewatch

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Probe is one HTTP request's raw outcome, as the transport saw it. Keeping
// the transport's answer in a plain struct is what lets every rule below be
// tested without a socket.
type Probe struct {
	// Method is the verb that actually ran, which is GET whenever HEAD was
	// refused or a keyword needed a body.
	Method Method
	// Err is set when the request never produced a response: DNS failure,
	// connection refused, TLS failure, or the deadline.
	Err error
	// ErrText is the sanitized form of Err, already stripped of anything not
	// worth showing an operator.
	ErrText    string
	StatusCode int
	Duration   time.Duration
	SizeBytes  int64
	// Body is however much of the response was read, empty for a HEAD.
	Body string
	// TLSExpiresAt is the leaf certificate's NotAfter, zero for plain HTTP.
	TLSExpiresAt time.Time
}

// Verdict is what one URL's rules make of one probe.
type Verdict struct {
	Status  Status
	Reasons []string
	// TLSDaysLeft is nil when there was no certificate to read.
	TLSDaysLeft *int
	// TLSExpiring is true when the certificate is inside its warning window.
	TLSExpiring bool
}

// Evaluate applies one rule set to one probe. It is pure: the same probe
// always produces the same verdict, which is what keeps the scheduler a thin
// loop around it.
//
// Severity is resolved by worst-of rather than first-match, so a page that is
// both slow and missing its keyword is reported down with both reasons.
func Evaluate(checks Checks, probe Probe, now time.Time) Verdict {
	checks = checks.normalize()
	out := Verdict{Status: StatusUp}

	if probe.Err != nil {
		text := strings.TrimSpace(probe.ErrText)
		if text == "" {
			text = probe.Err.Error()
		}
		return Verdict{Status: StatusDown, Reasons: []string{text}}
	}

	var down, slow []string

	if reason, ok := statusReason(checks.Status, probe.StatusCode); !ok {
		down = append(down, reason)
	}
	if checks.Keyword != nil {
		down = append(down, keywordReasons(*checks.Keyword, probe.Body)...)
	}
	if checks.MaxResponseMs > 0 {
		elapsed := probe.Duration.Milliseconds()
		if elapsed > int64(checks.MaxResponseMs) {
			slow = append(slow, fmt.Sprintf(
				"answered in %s, over the %s budget",
				formatDuration(probe.Duration),
				formatMillis(int64(checks.MaxResponseMs)),
			))
		}
	}

	if !probe.TLSExpiresAt.IsZero() {
		days := daysUntil(probe.TLSExpiresAt, now)
		out.TLSDaysLeft = &days
		if checks.TLS.WarnDays > 0 && days <= checks.TLS.WarnDays {
			out.TLSExpiring = true
			// An expired certificate is not a warning: every browser refuses
			// the page, so the site is down for its visitors.
			if days <= 0 {
				down = append(down, "the TLS certificate has expired")
			}
		}
	}

	switch {
	case len(down) > 0:
		out.Status = StatusDown
	case len(slow) > 0:
		out.Status = StatusSlow
	}
	out.Reasons = append(down, slow...)
	return out
}

// statusReason applies the response-code rule. An unset expectation accepts
// anything below 400, which is what a pasted URL is watched with: redirects
// are followed by the client, so a 3xx here means a redirect the client chose
// not to follow rather than a broken site.
func statusReason(rule StatusCheck, code int) (string, bool) {
	if rule.Expect > 0 {
		if code == rule.Expect {
			return "", true
		}
		return fmt.Sprintf("answered HTTP %d, expected %d", code, rule.Expect), false
	}
	if code >= 200 && code < 400 {
		return "", true
	}
	return "answered HTTP " + strconv.Itoa(code), false
}

// keywordReasons applies the two substring tests. Matching is case-insensitive
// because "Add to cart" and "ADD TO CART" are the same page to a human, and
// the check exists to notice a white screen, not a rebrand.
func keywordReasons(rule KeywordCheck, body string) []string {
	reasons := make([]string, 0, 2)
	haystack := strings.ToLower(body)
	if want := strings.TrimSpace(rule.MustContain); want != "" {
		if !strings.Contains(haystack, strings.ToLower(want)) {
			reasons = append(reasons, "the page no longer contains "+strconv.Quote(want))
		}
	}
	if reject := strings.TrimSpace(rule.MustNotContain); reject != "" {
		if strings.Contains(haystack, strings.ToLower(reject)) {
			reasons = append(reasons, "the page now contains "+strconv.Quote(reject))
		}
	}
	return reasons
}

// Combine folds a site's primary verdict and its extra URLs' verdicts into
// one row status. The worst wins, and every reason is carried with the URL it
// came from so the table can explain a red dot without a second request.
func Combine(results []EndpointResult) (Status, []string) {
	status := StatusUnknown
	reasons := make([]string, 0, len(results))
	for _, result := range results {
		if result.Status.severity() > status.severity() {
			status = result.Status
		}
		for _, reason := range result.Reasons {
			label := strings.TrimSpace(result.Label)
			if label == "" {
				reasons = append(reasons, reason)
				continue
			}
			reasons = append(reasons, label+": "+reason)
		}
	}
	return status, reasons
}

// consecutiveChecks is how many checks in a row must agree before a site
// changes state. Two is the whole anti-flap policy: one dropped packet, one
// WAF hiccup, or one origin restart never wakes anybody, and a real outage is
// reported one interval later than it began.
const consecutiveChecks = 2

// stateMachine debounces one site's status and remembers when the current
// state began.
type stateMachine struct {
	// published is the state currently shown and last alerted on. The zero
	// value means the site has never been measured.
	published Status
	// since is when published began, in unix milliseconds.
	since     int64
	candidate Status
	streak    int
}

// Transition describes a settled state change: what it was, what it now is,
// and how long the old state had lasted.
type Transition struct {
	From     Status
	To       Status
	Since    int64
	Duration time.Duration
	// Alert is true when a human should hear about this change.
	Alert bool
}

// Observe folds one check into the machine and reports the state to publish
// now plus, when it moved, the transition.
//
// Unlike a first-reading-wins debouncer, the very first bad check does not
// publish either: a site added while its origin is restarting must fail twice
// before it is called down. A first *good* reading publishes immediately, so
// a healthy site shows green on its first check without waiting.
func (m *stateMachine) Observe(observed Status, at int64) (Status, Transition) {
	if observed == m.published {
		m.candidate, m.streak = "", 0
		return m.status(), Transition{}
	}
	if m.candidate == observed {
		m.streak++
	} else {
		m.candidate, m.streak = observed, 1
	}
	firstGoodReading := m.published == "" && observed == StatusUp
	if !firstGoodReading && m.streak < consecutiveChecks {
		return m.status(), Transition{}
	}

	previous, previousSince := m.published, m.since
	m.published, m.since = observed, at
	m.candidate, m.streak = "", 0

	transition := Transition{From: previous, To: observed, Since: previousSince, Alert: alertWorthy(previous, observed)}
	if previousSince > 0 && at > previousSince {
		transition.Duration = time.Duration(at-previousSince) * time.Millisecond
	}
	return m.status(), transition
}

// status renders the published state for a caller: a machine that has never
// settled on anything reports unknown rather than an empty string.
func (m *stateMachine) status() Status {
	if m.published == "" {
		return StatusUnknown
	}
	return m.published
}

// alertWorthy decides which settled changes become messages. Every step into
// a degraded state is one, and so is the recovery out of one. The first
// reading of a healthy site is not: nothing recovered, it was simply added.
func alertWorthy(previous, next Status) bool {
	switch next {
	case StatusDown, StatusSlow:
		return previous != next
	case StatusUp:
		return previous == StatusDown || previous == StatusSlow
	default:
		return false
	}
}

// UptimeWindows are the three periods the table reports, longest last.
var UptimeWindows = struct {
	Day   time.Duration
	Week  time.Duration
	Month time.Duration
}{
	Day:   24 * time.Hour,
	Week:  7 * 24 * time.Hour,
	Month: 30 * 24 * time.Hour,
}

// ComputeUptime is the availability arithmetic over a site's history: the
// share of checks in each window that served the customer.
//
// A window with no checks reports nothing rather than zero — "we have not
// looked" and "it was down the whole time" must never render the same.
func ComputeUptime(records []Record, now time.Time) Uptime {
	out := Uptime{Checks: len(records)}
	if len(records) == 0 {
		return out
	}
	oldest := records[0].At
	for _, record := range records {
		if record.At < oldest {
			oldest = record.At
		}
	}
	out.Since = oldest
	out.Day = uptimePercent(records, now, UptimeWindows.Day)
	out.Week = uptimePercent(records, now, UptimeWindows.Week)
	out.Month = uptimePercent(records, now, UptimeWindows.Month)
	return out
}

func uptimePercent(records []Record, now time.Time, window time.Duration) *float64 {
	from := now.Add(-window).UnixMilli()
	total, ok := 0, 0
	for _, record := range records {
		if record.At < from || record.At > now.UnixMilli() {
			continue
		}
		total++
		if record.OK() {
			ok++
		}
	}
	if total == 0 {
		return nil
	}
	percent := round2(float64(ok) / float64(total) * 100)
	return &percent
}

// Spark is the newest response times for the row's sparkline, oldest first.
// A failed check contributes a zero, which the sparkline draws as a gap
// rather than as a fast response.
func Spark(records []Record, points int) []int64 {
	if points <= 0 || len(records) == 0 {
		return nil
	}
	if len(records) > points {
		records = records[len(records)-points:]
	}
	out := make([]int64, 0, len(records))
	for _, record := range records {
		if !record.OK() {
			out = append(out, 0)
			continue
		}
		out = append(out, record.DurationMs)
	}
	return out
}

// Due selects the sites the scheduler should check now: enabled, and past the
// instant their jittered schedule named. A site with no scheduled instant has
// never been armed and is not due — arming happens when the site is first
// seen, which is what spreads a two-hundred-site fleet across its interval
// instead of firing all of it on the first tick.
//
// The result is ordered by how overdue each site is, so a batch cap always
// serves the longest-waiting sites first.
func Due(sites []Site, dueAt map[ID]int64, now time.Time, limit int) []Site {
	nowMilli := now.UnixMilli()
	type entry struct {
		site Site
		due  int64
	}
	pending := make([]entry, 0, len(sites))
	for _, site := range sites {
		if !site.Enabled {
			continue
		}
		due, armed := dueAt[site.ID]
		if !armed || due > nowMilli {
			continue
		}
		pending = append(pending, entry{site: site, due: due})
	}
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].due < pending[j].due })
	if limit > 0 && len(pending) > limit {
		pending = pending[:limit]
	}
	out := make([]Site, 0, len(pending))
	for _, item := range pending {
		out = append(out, item.site)
	}
	return out
}

// AlertKind names what an outbound message is about. The values reach the
// notification payload, so they are a public vocabulary.
type AlertKind string

const (
	AlertDown      AlertKind = "down"
	AlertRecovered AlertKind = "recovered"
	AlertSlow      AlertKind = "slow"
	AlertTLS       AlertKind = "tls"
)

// Alert is one outbound message about one site.
type Alert struct {
	Kind    AlertKind
	Status  Status
	Summary string
	At      int64
	// DedupeKey collapses repeat deliveries of the same state change.
	DedupeKey string
}

// DownSummary is the message a site going dark produces: what it is, what it
// answered, and how long it took to say so.
//
//	🔴 shop.example.com — 502 in 1.2 s
func DownSummary(site Site, view View) string {
	detail := strings.TrimSpace(view.LastError)
	if view.LastCode > 0 {
		detail = strconv.Itoa(view.LastCode)
	}
	if detail == "" {
		detail = "no response"
	}
	summary := "🔴 " + site.Name() + " — " + detail
	if view.LastDurationMs > 0 {
		summary += " in " + formatMillis(view.LastDurationMs)
	}
	return summary
}

// RecoveredSummary is the all-clear, with the outage it ends.
//
//	🟢 shop.example.com — back after 12 m
func RecoveredSummary(site Site, outage time.Duration) string {
	if outage <= 0 {
		return "🟢 " + site.Name() + " — back up"
	}
	return "🟢 " + site.Name() + " — back after " + formatDuration(outage)
}

// SlowSummary is the amber message for a site that answers but drags.
//
//	🟠 shop.example.com — 3.4 s, over the 2 s budget
func SlowSummary(site Site, view View) string {
	summary := "🟠 " + site.Name() + " — " + formatMillis(view.LastDurationMs)
	if budget := site.Checks.MaxResponseMs; budget > 0 {
		summary += ", over the " + formatMillis(int64(budget)) + " budget"
	}
	return summary
}

// TLSSummary is the certificate warning.
//
//	🟡 shop.example.com — certificate expires in 9 days
func TLSSummary(site Site, days int) string {
	switch {
	case days < 0:
		return "🟡 " + site.Name() + " — the TLS certificate expired " + plural(-days, "day") + " ago"
	case days == 0:
		return "🟡 " + site.Name() + " — the TLS certificate expires today"
	default:
		return "🟡 " + site.Name() + " — certificate expires in " + plural(days, "day")
	}
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

// daysUntil is whole days from now to the instant, rounded *down* rather than
// toward zero, so a certificate that expired an hour ago reports -1 day
// instead of the reassuring 0 that truncation would produce.
func daysUntil(at, now time.Time) int {
	return int(math.Floor(at.Sub(now).Hours() / 24))
}

func formatMillis(ms int64) string {
	return formatDuration(time.Duration(ms) * time.Millisecond)
}

// formatDuration renders a span the way an alert reads it out loud: sub-second
// as milliseconds, then seconds with one decimal, then whole minutes, hours,
// and days.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + " ms"
	case d < time.Minute:
		return strings.TrimSuffix(strconv.FormatFloat(d.Seconds(), 'f', 1, 64), ".0") + " s"
	case d < time.Hour:
		return strconv.FormatInt(int64(d.Minutes()), 10) + " m"
	case d < 24*time.Hour:
		return strconv.FormatInt(int64(d.Hours()), 10) + " h"
	default:
		return strconv.FormatInt(int64(d.Hours()/24), 10) + " d"
	}
}

// round2 keeps an uptime percentage at two decimals: enough to tell 99.95%
// from 100%, which is the difference an SLA argument turns on.
func round2(value float64) float64 {
	return float64(int64(value*100+0.5)) / 100
}
