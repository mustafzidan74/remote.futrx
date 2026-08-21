package providerpool

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Usage tracking.
//
// Two things are counted, and the difference between them is the whole point
// of this file:
//
//   - What we spent. Every request this platform makes is counted locally,
//     into a minute window, a UTC-day window, and a UTC-month total. That is
//     always available and always ours.
//   - What the provider says is left. Most vendors return rate-limit headers
//     on every response. Those numbers are better than ours — they know about
//     requests made from somewhere else with the same key, and they know when
//     their own window resets — so when they are present they win, and the
//     meter is labelled "reported by provider" rather than "counted locally".
//
// The day and month windows are UTC. Vendors reset on their own schedules
// (Google's free tier rolls over on Pacific time, for one), so a locally
// counted daily meter is an estimate by construction. This is stated in the
// UI and in the docs rather than papered over.

// Event names for the ledger.
const (
	// EventRequest is one completion attempt against one provider.
	EventRequest = "request"
	// EventFailover records that a provider refused and the pool moved on.
	// It carries both ids so the log answers "what did it fall back to".
	EventFailover = "failover"
	// EventCooldown records a provider being put to sleep for a while.
	EventCooldown = "cooldown"
)

// Source labels where a number came from, so the UI never presents a guess as
// a fact.
const (
	// SourceCounted means "this platform added it up".
	SourceCounted = "counted"
	// SourceReported means "the provider's own rate-limit headers said so".
	SourceReported = "reported"
)

// UsageRecord is one line of DATA_DIR/providerpool/usage-YYYY-MM.jsonl.
type UsageRecord struct {
	At               int64  `json:"at"`
	Event            string `json:"event"`
	ProviderID       string `json:"providerId"`
	Model            string `json:"model,omitempty"`
	Job              string `json:"job,omitempty"`
	OK               bool   `json:"ok"`
	Status           int    `json:"status,omitempty"`
	PromptTokens     int    `json:"promptTokens,omitempty"`
	CompletionTokens int    `json:"completionTokens,omitempty"`
	LatencyMS        int64  `json:"latencyMs,omitempty"`
	Error            string `json:"error,omitempty"`
	// NextProviderID names where a failover went. Empty means the pool ran
	// out of candidates.
	NextProviderID string `json:"nextProviderId,omitempty"`
	// CooldownMS is how long a cooldown event put the provider to sleep.
	CooldownMS int64 `json:"cooldownMs,omitempty"`
	// TokenSource marks whether the token counts on this line came from the
	// provider's own usage block or from our estimate.
	TokenSource string `json:"tokenSource,omitempty"`
}

// Tokens is the total this record spent.
func (r UsageRecord) Tokens() int { return r.PromptTokens + r.CompletionTokens }

// MinuteKey, DayKey and MonthKey are the three window keys, all in UTC.
func MinuteKey(at time.Time) string { return at.UTC().Format("2006-01-02T15:04") }
func DayKey(at time.Time) string    { return at.UTC().Format("2006-01-02") }
func MonthKey(at time.Time) string  { return at.UTC().Format("2006-01") }

// Reported is what a provider's own headers last told us. Every field is
// nullable because vendors publish wildly different subsets, and an absent
// header must not read as a zero.
type Reported struct {
	RemainingRequests *int  `json:"remainingRequests,omitempty"`
	RemainingTokens   *int  `json:"remainingTokens,omitempty"`
	LimitRequests     *int  `json:"limitRequests,omitempty"`
	LimitTokens       *int  `json:"limitTokens,omitempty"`
	RemainingDaily    *int  `json:"remainingDaily,omitempty"`
	LimitDaily        *int  `json:"limitDaily,omitempty"`
	ResetAt           int64 `json:"resetAt,omitempty"`
	// At is when these numbers were read. A reading older than
	// reportedStaleAfter is dropped rather than shown: a "remaining: 0" from
	// two hours ago is not a reason to skip a provider now.
	At int64 `json:"at,omitempty"`
}

// Empty reports whether the provider told us nothing usable.
func (r Reported) Empty() bool {
	return r.RemainingRequests == nil && r.RemainingTokens == nil &&
		r.RemainingDaily == nil && r.ResetAt == 0
}

// reportedStaleAfter is how long a header reading stays interesting. The
// windows these numbers describe are minutes long, so anything older than a
// few of them says nothing about now.
const reportedStaleAfter = 10 * time.Minute

// counters is one provider's live state. All access goes through the tracker
// mutex; nothing here is safe on its own.
type counters struct {
	minuteKey      string
	minuteRequests int
	minuteTokens   int

	dayKey      string
	dayRequests int
	dayTokens   int

	monthKey      string
	monthRequests int
	monthTokens   int

	errors      int
	lastError   string
	lastErrorAt int64
	lastUsedAt  int64

	// failures counts consecutive refusals, which is what picks the next
	// cooldown length. A success zeroes it.
	failures      int
	cooldownUntil time.Time

	reported Reported
}

// roll advances the three windows to now, zeroing whichever ones have turned
// over. It is called on every read and every write, so a provider that has
// been idle across a day boundary reports today's zero rather than
// yesterday's total.
func (c *counters) roll(now time.Time) {
	if key := MinuteKey(now); key != c.minuteKey {
		c.minuteKey = key
		c.minuteRequests = 0
		c.minuteTokens = 0
	}
	if key := DayKey(now); key != c.dayKey {
		c.dayKey = key
		c.dayRequests = 0
		c.dayTokens = 0
	}
	if key := MonthKey(now); key != c.monthKey {
		c.monthKey = key
		c.monthRequests = 0
		c.monthTokens = 0
	}
}

// tracker owns every provider's counters.
type tracker struct {
	mu    sync.Mutex
	now   func() time.Time
	state map[string]*counters
}

func newTracker(now func() time.Time) *tracker {
	if now == nil {
		now = time.Now
	}
	return &tracker{now: now, state: map[string]*counters{}}
}

func (t *tracker) countersFor(id string) *counters {
	entry, found := t.state[id]
	if !found {
		entry = &counters{}
		t.state[id] = entry
	}
	entry.roll(t.now())
	return entry
}

// record folds one completed attempt into the counters. Tokens are added to
// every window; a failed attempt still counts as a request, because the
// vendor counted it too.
func (t *tracker) record(id string, tokens int, ok bool, failure string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.countersFor(id)
	now := t.now()
	entry.minuteRequests++
	entry.dayRequests++
	entry.monthRequests++
	if tokens > 0 {
		entry.minuteTokens += tokens
		entry.dayTokens += tokens
		entry.monthTokens += tokens
	}
	entry.lastUsedAt = now.UnixMilli()
	if ok {
		entry.failures = 0
		return
	}
	entry.errors++
	entry.lastError = failure
	entry.lastErrorAt = now.UnixMilli()
}

// replay folds a ledger line read at startup back into the counters, without
// touching the failure or cooldown state — a cooldown from before a restart
// is not worth reinstating, and a stale "last error" is noise.
func (t *tracker) replay(record UsageRecord) {
	if record.Event != EventRequest || record.ProviderID == "" {
		return
	}
	at := time.UnixMilli(record.At).UTC()
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, found := t.state[record.ProviderID]
	if !found {
		entry = &counters{}
		t.state[record.ProviderID] = entry
	}
	// Replay writes into the window the record belongs to, not the window we
	// are in now, so a line from yesterday lands in nothing.
	now := t.now()
	entry.roll(now)
	if DayKey(at) == entry.dayKey {
		entry.dayRequests++
		entry.dayTokens += record.Tokens()
	}
	if MonthKey(at) == entry.monthKey {
		entry.monthRequests++
		entry.monthTokens += record.Tokens()
	}
	if record.At > entry.lastUsedAt {
		entry.lastUsedAt = record.At
	}
}

// observe stores what a provider's headers reported.
func (t *tracker) observe(id string, reported Reported) {
	if reported.Empty() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.countersFor(id)
	reported.At = t.now().UnixMilli()
	entry.reported = reported
}

// snapshot copies one provider's counters out for reading.
func (t *tracker) snapshot(id string) counters {
	t.mu.Lock()
	defer t.mu.Unlock()
	return *t.countersFor(id)
}

// forget drops a deleted provider's counters so a re-created id starts clean.
func (t *tracker) forget(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.state, id)
}

/* ------------------------------------------------------------------ *
 * Cooldown state machine
 * ------------------------------------------------------------------ */

// cooldownSteps is the exponential back-off a refusing provider gets: long
// enough that a minute-window quota has certainly rolled over by the second
// step, and capped so a provider that was briefly unhappy is not written off
// for the rest of the day.
var cooldownSteps = []time.Duration{30 * time.Second, 5 * time.Minute, 30 * time.Minute}

// cooldownFor returns the sleep length for the nth consecutive failure. The
// count is one-based: the first failure gets the first step, and anything
// past the last step repeats it.
func cooldownFor(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	if failures > len(cooldownSteps) {
		return cooldownSteps[len(cooldownSteps)-1]
	}
	return cooldownSteps[failures-1]
}

// cool puts a provider to sleep after a refusal and reports how long for. A
// retry-after from the vendor wins when it is longer than our own step: they
// know when their window opens and we are guessing.
func (t *tracker) cool(id string, retryAfter time.Duration) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.countersFor(id)
	entry.failures++
	sleep := cooldownFor(entry.failures)
	if retryAfter > sleep {
		sleep = retryAfter
	}
	entry.cooldownUntil = t.now().Add(sleep)
	return sleep
}

// cooling reports whether a provider is asleep, and clears an expired
// cooldown as it passes: the next caller after the window is the one that
// retries, so there is no timer and no goroutine.
func (t *tracker) cooling(id string) (bool, time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.countersFor(id)
	if entry.cooldownUntil.IsZero() {
		return false, time.Time{}
	}
	if t.now().Before(entry.cooldownUntil) {
		return true, entry.cooldownUntil
	}
	entry.cooldownUntil = time.Time{}
	return false, time.Time{}
}

// revive clears a cooldown and the failure count outright. A successful call
// does this, and so does an operator pressing Test: they have just told us
// something changed.
func (t *tracker) revive(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.countersFor(id)
	entry.failures = 0
	entry.cooldownUntil = time.Time{}
}

/* ------------------------------------------------------------------ *
 * The read model
 * ------------------------------------------------------------------ */

// Meter is one usage bar: what was spent, what the cap is if one is known,
// and where each of those two numbers came from.
type Meter struct {
	Used int `json:"used"`
	// Limit is nil when nothing documents a cap for this window. The UI draws
	// an empty track and prints the raw count rather than a percentage.
	Limit *int `json:"limit,omitempty"`
	// Percent is nil for the same reason. It is clamped to 100: a provider
	// that let us past its documented cap is at 100%, not at 140%.
	Percent *int `json:"percent,omitempty"`
	// Source is SourceCounted or SourceReported.
	Source string `json:"source"`
}

func meter(used int, limit *int, source string) Meter {
	out := Meter{Used: used, Limit: limit, Source: source}
	if limit != nil && *limit > 0 {
		percent := used * 100 / *limit
		if percent > 100 {
			percent = 100
		}
		out.Percent = &percent
	}
	return out
}

// Status is the dot beside a provider's name.
type Status string

const (
	StatusReady    Status = "ready"
	StatusCooling  Status = "cooling"
	StatusNoKey    Status = "no-key"
	StatusDisabled Status = "disabled"
	// StatusExhausted is a provider that is enabled and keyed but is over a
	// counted or reported limit for the current window.
	StatusExhausted Status = "exhausted"
)

// Usage is one provider's consumption, as the panel and the dashboard card
// read it.
type Usage struct {
	RequestsToday Meter `json:"requestsToday"`
	TokensToday   Meter `json:"tokensToday"`
	TokensMonth   Meter `json:"tokensMonth"`
	// RequestsMinute and TokensMinute drive the "is this provider free right
	// now" decision. They are shown as a detail line rather than a bar.
	RequestsMinute Meter `json:"requestsMinute"`
	TokensMinute   Meter `json:"tokensMinute"`

	Errors        int    `json:"errors"`
	LastError     string `json:"lastError,omitempty"`
	LastErrorAt   int64  `json:"lastErrorAt,omitempty"`
	LastUsedAt    int64  `json:"lastUsedAt,omitempty"`
	CooldownUntil int64  `json:"cooldownUntil,omitempty"`
	// ReportedResetAt is when the provider said its window reopens.
	ReportedResetAt int64 `json:"reportedResetAt,omitempty"`
}

// usageFor renders one provider's counters against its documented limits.
func usageFor(provider Provider, entry counters, now time.Time) Usage {
	fresh := entry.reported.At > 0 &&
		now.Sub(time.UnixMilli(entry.reported.At)) < reportedStaleAfter

	requestsMinute := meter(entry.minuteRequests, provider.Limits.RPM, SourceCounted)
	if fresh && entry.reported.RemainingRequests != nil {
		limit := provider.Limits.RPM
		if entry.reported.LimitRequests != nil {
			limit = entry.reported.LimitRequests
		}
		if limit != nil {
			used := *limit - *entry.reported.RemainingRequests
			if used < 0 {
				used = 0
			}
			requestsMinute = meter(used, limit, SourceReported)
		}
	}

	tokensMinute := meter(entry.minuteTokens, provider.Limits.TPM, SourceCounted)
	if fresh && entry.reported.RemainingTokens != nil {
		limit := provider.Limits.TPM
		if entry.reported.LimitTokens != nil {
			limit = entry.reported.LimitTokens
		}
		if limit != nil {
			used := *limit - *entry.reported.RemainingTokens
			if used < 0 {
				used = 0
			}
			tokensMinute = meter(used, limit, SourceReported)
		}
	}

	requestsToday := meter(entry.dayRequests, provider.Limits.RPD, SourceCounted)
	if fresh && entry.reported.RemainingDaily != nil {
		limit := provider.Limits.RPD
		if entry.reported.LimitDaily != nil {
			limit = entry.reported.LimitDaily
		}
		if limit != nil {
			used := *limit - *entry.reported.RemainingDaily
			if used < 0 {
				used = 0
			}
			requestsToday = meter(used, limit, SourceReported)
		}
	}

	usage := Usage{
		RequestsToday:  requestsToday,
		TokensToday:    meter(entry.dayTokens, provider.Limits.TPD, SourceCounted),
		TokensMonth:    meter(entry.monthTokens, provider.Limits.MonthlyTokens, SourceCounted),
		RequestsMinute: requestsMinute,
		TokensMinute:   tokensMinute,
		Errors:         entry.errors,
		LastError:      entry.lastError,
		LastErrorAt:    entry.lastErrorAt,
		LastUsedAt:     entry.lastUsedAt,
	}
	if !entry.cooldownUntil.IsZero() && now.Before(entry.cooldownUntil) {
		usage.CooldownUntil = entry.cooldownUntil.UnixMilli()
	}
	if fresh {
		usage.ReportedResetAt = entry.reported.ResetAt
	}
	return usage
}

// exhausted reports whether any window this provider documents is already
// full. It is the "skip a provider that is over a counted limit" rule, and it
// is deliberately conservative: a window with no documented cap can never
// exhaust, because we would be inventing the number we compared against.
func exhausted(usage Usage) bool {
	for _, m := range []Meter{
		usage.RequestsMinute, usage.TokensMinute,
		usage.RequestsToday, usage.TokensToday, usage.TokensMonth,
	} {
		if m.Limit != nil && m.Used >= *m.Limit {
			return true
		}
	}
	return false
}

// statusOf is the dot the panel draws.
func statusOf(provider Provider, usage Usage, cooling bool) Status {
	switch {
	case !provider.Enabled:
		return StatusDisabled
	case !provider.HasKey():
		return StatusNoKey
	case cooling:
		return StatusCooling
	case exhausted(usage):
		return StatusExhausted
	default:
		return StatusReady
	}
}

/* ------------------------------------------------------------------ *
 * Token estimation
 * ------------------------------------------------------------------ */

// estimateTokens is the fallback when a provider's response carries no usage
// block. Four characters per token is the usual rule of thumb for English and
// is wrong for Arabic and for code — which is exactly why a meter fed by this
// number is labelled "counted locally" rather than presented as fact.
func estimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	return len([]rune(trimmed))/4 + 1
}

// topByConsumption orders provider ids by how full their tightest meter is,
// fullest first. The dashboard card shows the head of this list, because the
// provider about to run out is the one worth a glance.
func topByConsumption(views []ProviderView, limit int) []ProviderView {
	ranked := make([]ProviderView, 0, len(views))
	for _, view := range views {
		if view.Status == StatusDisabled || view.Status == StatusNoKey {
			continue
		}
		ranked = append(ranked, view)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return peakPercent(ranked[i].Usage) > peakPercent(ranked[j].Usage)
	})
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

// peakPercent is the fullest documented window a provider has. A provider
// with no documented limits at all scores -1, so it sorts below anything we
// can actually say something about.
func peakPercent(usage Usage) int {
	peak := -1
	for _, m := range []Meter{usage.RequestsToday, usage.TokensToday, usage.TokensMonth} {
		if m.Percent != nil && *m.Percent > peak {
			peak = *m.Percent
		}
	}
	return peak
}
