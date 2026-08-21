package providerpool

import (
	"net/http"
	"testing"
	"time"
)

// The header shapes, one vendor at a time.
//
// There is no standard here, so these cases are the specification: each one
// is a real spelling one of the seeded providers uses. A vendor that changes
// its spelling should break exactly one of these rather than quietly falling
// back to local counting everywhere.

func headerFrom(pairs map[string]string) http.Header {
	header := http.Header{}
	for name, value := range pairs {
		header.Set(name, value)
	}
	return header
}

func TestParseRateLimitHeadersPerVendorShape(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name              string
		header            http.Header
		wantRemainingReq  *int
		wantRemainingTok  *int
		wantLimitRequests *int
		wantRemainingDay  *int
		wantResetIn       time.Duration
	}{
		{
			name: "OpenAI and Groq split the budget by resource and express the reset as a duration",
			header: headerFrom(map[string]string{
				"x-ratelimit-limit-requests":     "14400",
				"x-ratelimit-remaining-requests": "14370",
				"x-ratelimit-limit-tokens":       "18000",
				"x-ratelimit-remaining-tokens":   "17997",
				"x-ratelimit-reset-requests":     "2m59.56s",
				"x-ratelimit-reset-tokens":       "7.66s",
			}),
			wantRemainingReq:  intp(14370),
			wantRemainingTok:  intp(17997),
			wantLimitRequests: intp(14400),
			// The soonest of the two resets is the one that matters.
			wantResetIn: 7660 * time.Millisecond,
		},
		{
			name: "Cerebras splits the same names by window and is the only vendor reporting a daily figure",
			header: headerFrom(map[string]string{
				"x-ratelimit-limit-requests-minute":     "30",
				"x-ratelimit-remaining-requests-minute": "29",
				"x-ratelimit-limit-tokens-minute":       "60000",
				"x-ratelimit-remaining-tokens-minute":   "59000",
				"x-ratelimit-limit-requests-day":        "14400",
				"x-ratelimit-remaining-requests-day":    "14000",
				"x-ratelimit-reset-requests-minute":     "60",
			}),
			wantRemainingReq:  intp(29),
			wantRemainingTok:  intp(59000),
			wantLimitRequests: intp(30),
			wantRemainingDay:  intp(14000),
			wantResetIn:       time.Minute,
		},
		{
			name: "OpenRouter reports one undifferentiated budget and a unix-millisecond reset",
			header: headerFrom(map[string]string{
				"x-ratelimit-limit":     "50",
				"x-ratelimit-remaining": "37",
				"x-ratelimit-reset":     "1787392800000",
			}),
			wantRemainingReq:  intp(37),
			wantLimitRequests: intp(50),
			// 1787392800000 ms is 2026-08-22T10:00:00Z plus nothing; the case
			// below pins the arithmetic rather than the constant.
		},
		{
			name: "Anthropic uses its own prefix and an RFC 3339 instant",
			header: headerFrom(map[string]string{
				"anthropic-ratelimit-requests-limit":     "50",
				"anthropic-ratelimit-requests-remaining": "42",
				"anthropic-ratelimit-tokens-limit":       "40000",
				"anthropic-ratelimit-tokens-remaining":   "39000",
				"anthropic-ratelimit-requests-reset":     "2026-08-22T10:00:30Z",
			}),
			wantRemainingReq:  intp(42),
			wantRemainingTok:  intp(39000),
			wantLimitRequests: intp(50),
			wantResetIn:       30 * time.Second,
		},
		{
			name: "Gemini publishes no rate-limit headers, only retry-after on a refusal",
			header: headerFrom(map[string]string{
				"retry-after": "45",
			}),
			wantResetIn: 45 * time.Second,
		},
		{
			name:   "a response with nothing to say yields nothing rather than zeroes",
			header: headerFrom(map[string]string{}),
		},
		{
			name: "an abbreviated limit is read rather than dropped",
			header: headerFrom(map[string]string{
				"x-ratelimit-limit-tokens":     "1.2M",
				"x-ratelimit-remaining-tokens": "900k",
			}),
			wantRemainingTok: intp(900000),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseRateLimitHeaders(test.header, now)

			assertIntPtr(t, "remaining requests", got.RemainingRequests, test.wantRemainingReq)
			assertIntPtr(t, "remaining tokens", got.RemainingTokens, test.wantRemainingTok)
			assertIntPtr(t, "remaining daily", got.RemainingDaily, test.wantRemainingDay)
			if test.wantLimitRequests != nil {
				assertIntPtr(t, "limit requests", got.LimitRequests, test.wantLimitRequests)
			}
			if test.wantResetIn > 0 {
				want := now.Add(test.wantResetIn).UnixMilli()
				if got.ResetAt != want {
					t.Fatalf("resetAt = %d, want %d (%s from now)", got.ResetAt, want, test.wantResetIn)
				}
			}
		})
	}
}

func assertIntPtr(t *testing.T, label string, got, want *int) {
	t.Helper()
	switch {
	case want == nil && got == nil:
	case want == nil:
		t.Fatalf("%s = %d, want nothing — an absent header must never read as a number", label, *got)
	case got == nil:
		t.Fatalf("%s was dropped, want %d", label, *want)
	case *got != *want:
		t.Fatalf("%s = %d, want %d", label, *got, *want)
	}
}

func TestRetryAfterReadsEverySpellingAndRefusesThePast(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		value  string
		want   time.Duration
		wantOK bool
	}{
		{name: "a plain count of seconds", value: "30", want: 30 * time.Second, wantOK: true},
		{name: "a fractional count of seconds", value: "1.5", want: 1500 * time.Millisecond, wantOK: true},
		{name: "a Go-style duration, which Groq sends", value: "2m30s", want: 150 * time.Second, wantOK: true},
		{name: "an HTTP date", value: "Sat, 22 Aug 2026 10:01:00 GMT", want: time.Minute, wantOK: true},
		{
			name:   "a unix timestamp is a date, not a wait of thirty thousand years",
			value:  "1787392860",
			want:   time.Minute,
			wantOK: true,
		},
		{name: "an HTTP date in the past yields nothing", value: "Sat, 22 Aug 2026 09:00:00 GMT"},
		{name: "zero seconds yields nothing", value: "0"},
		{name: "nonsense yields nothing", value: "soon"},
		{name: "an absent header yields nothing", value: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := http.Header{}
			if test.value != "" {
				header.Set("retry-after", test.value)
			}
			got, ok := RetryAfter(header, now)
			if ok != test.wantOK {
				t.Fatalf("RetryAfter(%q) ok = %v, want %v (got %s)", test.value, ok, test.wantOK, got)
			}
			if !test.wantOK {
				return
			}
			if got != test.want {
				t.Fatalf("RetryAfter(%q) = %s, want %s", test.value, got, test.want)
			}
		})
	}
}

func TestOpenRouterUnixMillisecondResetIsReadAsAnInstant(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reset := now.Add(90 * time.Second)

	header := http.Header{}
	header.Set("x-ratelimit-remaining", "3")
	header.Set("x-ratelimit-reset", formatMillis(reset))

	got := ParseRateLimitHeaders(header, now)
	if got.ResetAt != reset.UnixMilli() {
		t.Fatalf("resetAt = %d, want the instant %d that OpenRouter named", got.ResetAt, reset.UnixMilli())
	}
}

func formatMillis(at time.Time) string {
	millis := at.UnixMilli()
	digits := []byte{}
	for millis > 0 {
		digits = append([]byte{byte('0' + millis%10)}, digits...)
		millis /= 10
	}
	return string(digits)
}
