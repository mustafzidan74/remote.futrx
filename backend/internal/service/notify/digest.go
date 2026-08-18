package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	// The digest is scheduled in an operator-chosen IANA zone. Embedding the
	// zone database keeps that working on a minimal container image that ships
	// no /usr/share/zoneinfo.
	_ "time/tzdata"
)

// DefaultDigestTimezone is the zone a fresh install schedules the weekly
// digest in.
const DefaultDigestTimezone = "Africa/Cairo"

// DefaultDigestHour is the local hour of day the digest is sent at.
const DefaultDigestHour = 9

// digestWindowDays is how far back a digest aggregates.
const digestWindowDays = 7

// digestProjectsInMessage bounds how many per-project figures reach the
// message body; the rest collapse into a "+N more" tail.
const digestProjectsInMessage = 5

// DigestConfig schedules the weekly cost-and-usage roll-up.
type DigestConfig struct {
	Enabled bool `json:"enabled"`
	// Weekday is time.Weekday: 0 = Sunday.
	Weekday int `json:"weekday"`
	// Hour is the local hour of day, 0-23, in Timezone.
	Hour int `json:"hour"`
	// Timezone is an IANA name. An unknown name falls back to UTC at send
	// time rather than failing the schedule.
	Timezone string `json:"timezone,omitempty"`
	// LastSentAt is the scheduled occurrence the last digest covered, which
	// is what makes the loop idempotent across restarts and ticks.
	LastSentAt int64 `json:"lastDigestSentAt,omitempty"`
}

// PublicDigest is the admin-facing view. The digest holds no secrets, so it
// is echoed back whole.
type PublicDigest struct {
	Enabled    bool   `json:"enabled"`
	Weekday    int    `json:"weekday"`
	Hour       int    `json:"hour"`
	Timezone   string `json:"timezone"`
	LastSentAt int64  `json:"lastDigestSentAt,omitempty"`
}

// DigestInput is the admin PUT body for the digest schedule. LastSentAt is
// not writable: only a delivery moves it.
type DigestInput struct {
	Enabled  bool   `json:"enabled"`
	Weekday  int    `json:"weekday"`
	Hour     int    `json:"hour"`
	Timezone string `json:"timezone"`
}

// DefaultDigestConfig is the schedule a fresh install starts with: off, but
// already pointed at Sunday 09:00 Africa/Cairo.
func DefaultDigestConfig() DigestConfig {
	return DigestConfig{
		Enabled:  false,
		Weekday:  int(time.Sunday),
		Hour:     DefaultDigestHour,
		Timezone: DefaultDigestTimezone,
	}
}

func (d DigestConfig) normalize() DigestConfig {
	d.Weekday = ((d.Weekday % 7) + 7) % 7
	if d.Hour < 0 {
		d.Hour = 0
	}
	if d.Hour > 23 {
		d.Hour = 23
	}
	d.Timezone = strings.TrimSpace(d.Timezone)
	if d.Timezone == "" {
		d.Timezone = DefaultDigestTimezone
	}
	return d
}

func (d DigestConfig) public() PublicDigest {
	d = d.normalize()
	return PublicDigest{
		Enabled:    d.Enabled,
		Weekday:    d.Weekday,
		Hour:       d.Hour,
		Timezone:   d.Timezone,
		LastSentAt: d.LastSentAt,
	}
}

// apply folds an update onto the stored schedule, keeping the delivery
// bookkeeping the operator does not own.
func (d DigestConfig) apply(input DigestInput) DigestConfig {
	next := DigestConfig{
		Enabled:    input.Enabled,
		Weekday:    input.Weekday,
		Hour:       input.Hour,
		Timezone:   input.Timezone,
		LastSentAt: d.LastSentAt,
	}
	return next.normalize()
}

// Location resolves the configured zone. An unknown name degrades to UTC so a
// typo delays a digest rather than stopping it forever.
func (d DigestConfig) Location() *time.Location {
	location, err := time.LoadLocation(d.normalize().Timezone)
	if err != nil || location == nil {
		return time.UTC
	}
	return location
}

// LastOccurrence is the most recent scheduled send time at or before now. It
// is the identity of a week's digest: two ticks inside the same week resolve
// to the same instant, which is what LastSentAt is compared against.
func (d DigestConfig) LastOccurrence(now time.Time) time.Time {
	d = d.normalize()
	location := d.Location()
	local := now.In(location)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), d.Hour, 0, 0, 0, location)
	back := (int(candidate.Weekday()) - d.Weekday + 7) % 7
	candidate = candidate.AddDate(0, 0, -back)
	if candidate.After(local) {
		candidate = candidate.AddDate(0, 0, -7)
	}
	return candidate
}

// DueAt reports the occurrence a digest is owed for, if any. A schedule that
// has never sent is armed rather than fired, so enabling the digest (or
// restarting the server) never dumps a surprise report into the chat.
func (d DigestConfig) DueAt(now time.Time) (time.Time, bool) {
	d = d.normalize()
	if !d.Enabled {
		return time.Time{}, false
	}
	occurrence := d.LastOccurrence(now)
	if d.LastSentAt <= 0 || d.LastSentAt >= occurrence.UnixMilli() {
		return occurrence, false
	}
	return occurrence, true
}

// DigestProject is one project's slice of a weekly digest.
type DigestProject struct {
	Name    string
	CostUSD float64
	Runs    int64
}

// Digest is the aggregate a source hands back for one window.
type Digest struct {
	From         int64
	To           int64
	TotalCostUSD float64
	Runs         int64
	Projects     []DigestProject
	TopModel     string
}

// Empty reports a window with nothing to say.
func (d Digest) Empty() bool { return d.Runs == 0 }

// DigestSource is the usage ledger as the notify service sees it. The adapter
// lives in the composition package, so notify never imports the usage service.
type DigestSource interface {
	// WeeklyDigest aggregates every project's runs in [from, to] unix millis.
	WeeklyDigest(ctx context.Context, from, to int64) (Digest, error)
}

// DigestWindow is the [from, to] pair covering the digestWindowDays before to.
func DigestWindow(to time.Time) (int64, int64) {
	return to.AddDate(0, 0, -digestWindowDays).UnixMilli(), to.UnixMilli()
}

// DigestSummary renders the one-line report body. It is plain text so every
// sink can carry it: Telegram escapes it, WhatsApp sends it as is, and the
// webhook ships it in the `summary` field.
func DigestSummary(digest Digest, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	var out strings.Builder
	out.WriteString("Weekly usage ")
	out.WriteString(FormatDigestRange(digest.From, digest.To, location))
	if digest.Empty() {
		out.WriteString(" — no agent runs.")
		return out.String()
	}
	out.WriteString(" — total ")
	out.WriteString(formatUSD(digest.TotalCostUSD))
	out.WriteString(fmt.Sprintf(" (%d run%s)", digest.Runs, plural(digest.Runs)))

	shown := digest.Projects
	overflow := 0
	if len(shown) > digestProjectsInMessage {
		overflow = len(shown) - digestProjectsInMessage
		shown = shown[:digestProjectsInMessage]
	}
	for _, project := range shown {
		out.WriteString(" · ")
		out.WriteString(project.Name)
		out.WriteString(" ")
		out.WriteString(formatUSD(project.CostUSD))
		out.WriteString(fmt.Sprintf(" (%d run%s)", project.Runs, plural(project.Runs)))
	}
	if overflow > 0 {
		out.WriteString(fmt.Sprintf(" · +%d more project%s", overflow, plural(int64(overflow))))
	}
	if model := strings.TrimSpace(digest.TopModel); model != "" {
		out.WriteString(" · top model ")
		out.WriteString(model)
	}
	return out.String()
}

// FormatDigestRange renders the covered days: "11–17 Aug", or with both month
// names when the window straddles a boundary. The end is the last day the
// window actually includes, not the exclusive bound.
func FormatDigestRange(fromMilli, toMilli int64, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	from := time.UnixMilli(fromMilli).In(location)
	last := time.UnixMilli(toMilli - 1).In(location)
	if toMilli <= fromMilli {
		last = from
	}
	if from.Year() != last.Year() {
		return from.Format("2 Jan 2006") + "–" + last.Format("2 Jan 2006")
	}
	if from.Month() != last.Month() {
		return from.Format("2 Jan") + "–" + last.Format("2 Jan")
	}
	return from.Format("2") + "–" + last.Format("2 Jan")
}

func formatUSD(value float64) string {
	if value >= 100 {
		return fmt.Sprintf("$%.0f", value)
	}
	return fmt.Sprintf("$%.2f", value)
}

func plural(count int64) string {
	if count == 1 {
		return ""
	}
	return "s"
}
