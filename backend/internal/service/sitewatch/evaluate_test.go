package sitewatch

import (
	"errors"
	"testing"
	"time"
)

var evalNow = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

func TestEvaluateAppliesEveryCheck(t *testing.T) {
	tests := []struct {
		name        string
		checks      Checks
		probe       Probe
		wantStatus  Status
		wantReason  string
		wantTLSDays *int
		wantWarn    bool
	}{
		{
			name:       "a transport failure is down",
			checks:     DefaultChecks(),
			probe:      Probe{Err: errors.New("dial tcp: connection refused"), ErrText: "connection refused"},
			wantStatus: StatusDown,
			wantReason: "connection refused",
		},
		{
			name:       "a 2xx with no expectation is up",
			checks:     DefaultChecks(),
			probe:      Probe{StatusCode: 200, Duration: 300 * time.Millisecond},
			wantStatus: StatusUp,
		},
		{
			name:       "a redirect with no expectation is up",
			checks:     DefaultChecks(),
			probe:      Probe{StatusCode: 301},
			wantStatus: StatusUp,
		},
		{
			name:       "a 502 is down",
			checks:     DefaultChecks(),
			probe:      Probe{StatusCode: 502},
			wantStatus: StatusDown,
			wantReason: "answered HTTP 502",
		},
		{
			name:       "an exact expectation refuses another success code",
			checks:     Checks{Status: StatusCheck{Expect: 200}},
			probe:      Probe{StatusCode: 204},
			wantStatus: StatusDown,
			wantReason: "answered HTTP 204, expected 200",
		},
		{
			name:       "a missing keyword is down",
			checks:     Checks{Keyword: &KeywordCheck{MustContain: "Add to cart"}},
			probe:      Probe{StatusCode: 200, Body: "<html>maintenance</html>"},
			wantStatus: StatusDown,
			wantReason: `the page no longer contains "Add to cart"`,
		},
		{
			name:       "the keyword match ignores case",
			checks:     Checks{Keyword: &KeywordCheck{MustContain: "Add to cart"}},
			probe:      Probe{StatusCode: 200, Body: "<button>ADD TO CART</button>"},
			wantStatus: StatusUp,
		},
		{
			name:       "a forbidden keyword is down",
			checks:     Checks{Keyword: &KeywordCheck{MustNotContain: "Error establishing"}},
			probe:      Probe{StatusCode: 200, Body: "Error establishing a database connection"},
			wantStatus: StatusDown,
			wantReason: `the page now contains "Error establishing"`,
		},
		{
			name:       "over the response budget is slow, not down",
			checks:     Checks{MaxResponseMs: 2000},
			probe:      Probe{StatusCode: 200, Duration: 3400 * time.Millisecond},
			wantStatus: StatusSlow,
			wantReason: "answered in 3.4 s, over the 2 s budget",
		},
		{
			name:       "inside the response budget is up",
			checks:     Checks{MaxResponseMs: 2000},
			probe:      Probe{StatusCode: 200, Duration: 1999 * time.Millisecond},
			wantStatus: StatusUp,
		},
		{
			name:       "a broken page that is also slow reports down",
			checks:     Checks{MaxResponseMs: 100},
			probe:      Probe{StatusCode: 500, Duration: 4 * time.Second},
			wantStatus: StatusDown,
			wantReason: "answered HTTP 500",
		},
		{
			name:        "a certificate inside the warning window flags but stays up",
			checks:      Checks{TLS: TLSCheck{WarnDays: 21}},
			probe:       Probe{StatusCode: 200, TLSExpiresAt: evalNow.Add(9 * 24 * time.Hour)},
			wantStatus:  StatusUp,
			wantTLSDays: intPtr(9),
			wantWarn:    true,
		},
		{
			name:        "a certificate outside the warning window is silent",
			checks:      Checks{TLS: TLSCheck{WarnDays: 21}},
			probe:       Probe{StatusCode: 200, TLSExpiresAt: evalNow.Add(60 * 24 * time.Hour)},
			wantStatus:  StatusUp,
			wantTLSDays: intPtr(60),
		},
		{
			name:        "an expired certificate is down",
			checks:      Checks{TLS: TLSCheck{WarnDays: 21}},
			probe:       Probe{StatusCode: 200, TLSExpiresAt: evalNow.Add(-time.Hour)},
			wantStatus:  StatusDown,
			wantReason:  "the TLS certificate has expired",
			wantTLSDays: intPtr(-1),
			wantWarn:    true,
		},
		{
			name:        "warnDays zero disables the certificate rule",
			checks:      Checks{TLS: TLSCheck{WarnDays: 0}},
			probe:       Probe{StatusCode: 200, TLSExpiresAt: evalNow.Add(2 * 24 * time.Hour)},
			wantStatus:  StatusUp,
			wantTLSDays: intPtr(2),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Evaluate(test.checks, test.probe, evalNow)
			if got.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q (reasons %v)", got.Status, test.wantStatus, got.Reasons)
			}
			if test.wantReason != "" && !containsReason(got.Reasons, test.wantReason) {
				t.Fatalf("reasons = %v, want one of them to be %q", got.Reasons, test.wantReason)
			}
			if test.wantTLSDays == nil && got.TLSDaysLeft != nil {
				t.Fatalf("tls days = %d, want none", *got.TLSDaysLeft)
			}
			if test.wantTLSDays != nil {
				if got.TLSDaysLeft == nil {
					t.Fatalf("tls days = none, want %d", *test.wantTLSDays)
				}
				if *got.TLSDaysLeft != *test.wantTLSDays {
					t.Fatalf("tls days = %d, want %d", *got.TLSDaysLeft, *test.wantTLSDays)
				}
			}
			if got.TLSExpiring != test.wantWarn {
				t.Fatalf("tls expiring = %t, want %t", got.TLSExpiring, test.wantWarn)
			}
		})
	}
}

func TestCombineTakesTheWorstEndpointAndNamesIt(t *testing.T) {
	status, reasons := Combine([]EndpointResult{
		{URL: "https://shop.example.com/", Status: StatusUp},
		{Label: "Checkout", URL: "https://shop.example.com/checkout", Status: StatusDown, Reasons: []string{"answered HTTP 500"}},
		{Label: "Login", URL: "https://shop.example.com/login", Status: StatusSlow, Reasons: []string{"answered in 4 s, over the 2 s budget"}},
	})
	if status != StatusDown {
		t.Fatalf("status = %q, want down", status)
	}
	if len(reasons) != 2 {
		t.Fatalf("reasons = %v, want two", reasons)
	}
	if reasons[0] != "Checkout: answered HTTP 500" {
		t.Fatalf("reasons[0] = %q, want the label prefixed", reasons[0])
	}
}

func TestStateMachineNeedsTwoConsecutiveChecksEachWay(t *testing.T) {
	var machine stateMachine
	at := int64(1_000)
	step := func(observed Status) (Status, Transition) {
		at += 60_000
		return machine.Observe(observed, at)
	}

	// A first healthy reading publishes at once: nothing recovered, so it is
	// not an alert either.
	status, transition := step(StatusUp)
	if status != StatusUp {
		t.Fatalf("first reading = %q, want up", status)
	}
	if transition.Alert {
		t.Fatal("the first healthy reading must not alert")
	}

	// One bad check is a blip and changes nothing.
	status, transition = step(StatusDown)
	if status != StatusUp || transition.Alert {
		t.Fatalf("after one failure: status %q alert %t, want up and silence", status, transition.Alert)
	}

	// The second consecutive failure publishes and alerts.
	downAt := at + 60_000
	status, transition = step(StatusDown)
	if status != StatusDown {
		t.Fatalf("after two failures: status = %q, want down", status)
	}
	if !transition.Alert || transition.To != StatusDown {
		t.Fatalf("second failure transition = %+v, want a down alert", transition)
	}
	if machine.since != downAt {
		t.Fatalf("since = %d, want the instant of the second failure %d", machine.since, downAt)
	}

	// Staying down says nothing more.
	if _, transition = step(StatusDown); transition.Alert {
		t.Fatal("a site that stays down must not alert again")
	}

	// One good check is not a recovery.
	status, transition = step(StatusUp)
	if status != StatusDown || transition.Alert {
		t.Fatalf("after one good check: status %q alert %t, want still down", status, transition.Alert)
	}

	// The second good check recovers, and reports how long the outage was.
	status, transition = step(StatusUp)
	if status != StatusUp {
		t.Fatalf("after two good checks: status = %q, want up", status)
	}
	if !transition.Alert || transition.From != StatusDown {
		t.Fatalf("recovery transition = %+v, want an alert out of down", transition)
	}
	if transition.Duration != 3*time.Minute {
		t.Fatalf("outage duration = %s, want 3m", transition.Duration)
	}
}

func TestStateMachineHoldsABrandNewSiteUntilItFailsTwice(t *testing.T) {
	var machine stateMachine
	if status, transition := machine.Observe(StatusDown, 1_000); status != StatusUnknown || transition.Alert {
		t.Fatalf("first ever reading: status %q alert %t, want unknown and silence", status, transition.Alert)
	}
	status, transition := machine.Observe(StatusDown, 61_000)
	if status != StatusDown || !transition.Alert {
		t.Fatalf("second reading: status %q alert %t, want down and an alert", status, transition.Alert)
	}
	if transition.Duration != 0 {
		t.Fatalf("duration = %s, want zero: there was no previous state to have lasted", transition.Duration)
	}
}

func TestStateMachineIgnoresFlappingBetweenTwoBadStates(t *testing.T) {
	var machine stateMachine
	machine.published, machine.since = StatusUp, 1_000
	for index, observed := range []Status{StatusDown, StatusSlow, StatusDown, StatusSlow} {
		status, transition := machine.Observe(observed, int64(2_000+index*1_000))
		if status != StatusUp || transition.Alert {
			t.Fatalf("step %d: status %q alert %t, want up and silence while it flaps", index, status, transition.Alert)
		}
	}
}

func TestComputeUptimeOverAHistoryFixture(t *testing.T) {
	now := time.Date(2025, 6, 30, 12, 0, 0, 0, time.UTC)
	minute := func(offset time.Duration, status Status) Record {
		return Record{At: now.Add(-offset).UnixMilli(), Status: status}
	}
	// Two failures in the last hour, one more six days back, and one 20 days
	// back: each window sees a different denominator.
	history := []Record{
		minute(25*24*time.Hour, StatusUp),
		minute(20*24*time.Hour, StatusDown),
		minute(6*24*time.Hour, StatusDown),
		minute(6*24*time.Hour, StatusUp),
		minute(2*time.Hour, StatusUp),
		minute(30*time.Minute, StatusDown),
		minute(20*time.Minute, StatusDown),
		minute(10*time.Minute, StatusSlow),
		minute(5*time.Minute, StatusUp),
	}

	got := ComputeUptime(history, now)
	if got.Checks != len(history) {
		t.Fatalf("checks = %d, want %d", got.Checks, len(history))
	}
	// Day window: 5 checks, 3 of them served (slow counts as served).
	assertPercent(t, "day", got.Day, 60)
	// Week window: 7 checks, 4 served.
	assertPercent(t, "week", got.Week, 57.14)
	// Month window: all 9, 5 served.
	assertPercent(t, "month", got.Month, 55.56)
	if got.Since != history[0].At {
		t.Fatalf("since = %d, want the oldest record %d", got.Since, history[0].At)
	}
}

func TestComputeUptimeReportsNothingForAnEmptyWindow(t *testing.T) {
	now := time.Date(2025, 6, 30, 12, 0, 0, 0, time.UTC)
	// One check, 10 days old: the day and week windows have nothing at all,
	// and "no data" must never render as 0% availability.
	got := ComputeUptime([]Record{{At: now.Add(-10 * 24 * time.Hour).UnixMilli(), Status: StatusUp}}, now)
	if got.Day != nil {
		t.Fatalf("day = %v, want nothing", *got.Day)
	}
	if got.Week != nil {
		t.Fatalf("week = %v, want nothing", *got.Week)
	}
	assertPercent(t, "month", got.Month, 100)

	if empty := ComputeUptime(nil, now); empty.Checks != 0 || empty.Month != nil {
		t.Fatalf("empty history = %+v, want no checks and no percentages", empty)
	}
}

func TestSparkZeroesTheFailedChecksAndKeepsTheNewest(t *testing.T) {
	history := []Record{
		{Status: StatusUp, DurationMs: 100},
		{Status: StatusDown, DurationMs: 15_000},
		{Status: StatusSlow, DurationMs: 3_000},
		{Status: StatusUp, DurationMs: 220},
	}
	got := Spark(history, 3)
	want := []int64{0, 3_000, 220}
	if len(got) != len(want) {
		t.Fatalf("spark = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("spark = %v, want %v", got, want)
		}
	}
}

func TestDueSelectsArmedSitesOldestFirst(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	sites := []Site{
		{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", Enabled: true, IntervalMinutes: 5},
		{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", Enabled: true, IntervalMinutes: 5},
		{ID: "cccccccccccccccccccccccc", Enabled: true, IntervalMinutes: 5},
		{ID: "dddddddddddddddddddddddd", Enabled: false, IntervalMinutes: 5},
		{ID: "eeeeeeeeeeeeeeeeeeeeeeee", Enabled: true, IntervalMinutes: 5},
	}
	dueAt := map[ID]int64{
		"aaaaaaaaaaaaaaaaaaaaaaaa": now.Add(-time.Minute).UnixMilli(),
		"bbbbbbbbbbbbbbbbbbbbbbbb": now.Add(time.Minute).UnixMilli(), // not yet
		"cccccccccccccccccccccccc": now.Add(-time.Hour).UnixMilli(),  // most overdue
		"dddddddddddddddddddddddd": now.Add(-time.Hour).UnixMilli(),  // disabled
		// e is never armed at all.
	}

	got := Due(sites, dueAt, now, 0)
	if len(got) != 2 {
		t.Fatalf("due = %d sites, want 2 (%v)", len(got), ids(got))
	}
	if got[0].ID != "cccccccccccccccccccccccc" || got[1].ID != "aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("due order = %v, want the most overdue first", ids(got))
	}

	if limited := Due(sites, dueAt, now, 1); len(limited) != 1 || limited[0].ID != "cccccccccccccccccccccccc" {
		t.Fatalf("limited due = %v, want only the most overdue", ids(limited))
	}

	// A site exactly at its instant is due; one a millisecond early is not.
	edge := map[ID]int64{"aaaaaaaaaaaaaaaaaaaaaaaa": now.UnixMilli()}
	if len(Due(sites[:1], edge, now, 0)) != 1 {
		t.Fatal("a site due exactly now must be selected")
	}
	if len(Due(sites[:1], edge, now.Add(-time.Millisecond), 0)) != 0 {
		t.Fatal("a site due next millisecond must not be selected")
	}
}

func TestAlertSummariesReadLikeTheDocumentedExamples(t *testing.T) {
	site := Site{Label: "", URL: "https://shop.example.com/"}
	down := DownSummary(site, View{LastCode: 502, LastDurationMs: 1200})
	if down != "🔴 shop.example.com — 502 in 1.2 s" {
		t.Fatalf("down summary = %q", down)
	}
	if got := RecoveredSummary(site, 12*time.Minute); got != "🟢 shop.example.com — back after 12 m" {
		t.Fatalf("recovered summary = %q", got)
	}
	if got := TLSSummary(site, 9); got != "🟡 shop.example.com — certificate expires in 9 days" {
		t.Fatalf("tls summary = %q", got)
	}
	if got := TLSSummary(site, 1); got != "🟡 shop.example.com — certificate expires in 1 day" {
		t.Fatalf("singular tls summary = %q", got)
	}
	slow := SlowSummary(
		Site{Label: "Shop", URL: "https://shop.example.com/", Checks: Checks{MaxResponseMs: 2000}},
		View{LastDurationMs: 3400},
	)
	if slow != "🟠 Shop — 3.4 s, over the 2 s budget" {
		t.Fatalf("slow summary = %q", slow)
	}
	// A site with no response at all still says something useful.
	if got := DownSummary(site, View{LastError: "connection refused"}); got != "🔴 shop.example.com — connection refused" {
		t.Fatalf("errored down summary = %q", got)
	}
}

func TestParseURLListNormalizesAndDeduplicates(t *testing.T) {
	got := ParseURLList(`
		shop.example.com
		https://shop.example.com/    # already listed, different scheme spelling
		http://blog.example.com/posts
		# a whole comment line
		not a url
		localhost:3000
		app.example.com, cdn.example.com
	`)
	want := []string{
		"https://shop.example.com/",
		"http://blog.example.com/posts",
		"https://app.example.com/",
		"https://cdn.example.com/",
	}
	if len(got) != len(want) {
		t.Fatalf("parsed = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("parsed = %v, want %v", got, want)
		}
	}
}

func TestSameTargetIgnoresSchemeAndTrailingSlash(t *testing.T) {
	tests := []struct {
		left, right string
		want        bool
	}{
		{"https://shop.example.com/", "http://shop.example.com", true},
		{"https://Shop.Example.com/", "https://shop.example.com/", true},
		{"https://shop.example.com/checkout", "https://shop.example.com/", false},
		{"https://shop.example.com/", "https://blog.example.com/", false},
	}
	for _, test := range tests {
		if got := SameTarget(test.left, test.right); got != test.want {
			t.Fatalf("SameTarget(%q, %q) = %t, want %t", test.left, test.right, got, test.want)
		}
	}
}

func assertPercent(t *testing.T, window string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nothing, want %.2f", window, want)
	}
	if *got != want {
		t.Fatalf("%s = %.2f, want %.2f", window, *got, want)
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func ids(sites []Site) []ID {
	out := make([]ID, 0, len(sites))
	for _, site := range sites {
		out = append(out, site.ID)
	}
	return out
}

func intPtr(value int) *int { return &value }
