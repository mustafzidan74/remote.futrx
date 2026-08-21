package providerpool

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Reading a provider's own rate-limit headers.
//
// Every vendor invented its own spelling, so this file is a small pile of
// shapes rather than one elegant parser. That is the honest structure of the
// problem: there is no standard here, and pretending otherwise would just
// move the special cases somewhere less obvious.
//
// Four families are handled:
//
//	OpenAI / Groq / Mistral   x-ratelimit-{limit,remaining,reset}-{requests,tokens}
//	Cerebras                  ...-{requests,tokens}-{minute,day} — same prefix, window suffix
//	OpenRouter                x-ratelimit-{limit,remaining,reset} — no per-resource split
//	Anthropic                 anthropic-ratelimit-{requests,tokens}-{limit,remaining,reset}
//
// plus `retry-after`, which everyone sends on a 429 and which is the single
// most useful number of the lot.
//
// Anything unrecognized is simply absent from the result. An absent header is
// never read as a zero — "the provider told us nothing" and "the provider
// told us nothing is left" are opposite facts.

// ParseRateLimitHeaders reads whatever one response can tell us about the
// caller's remaining allowance. It is exported so the header shapes can be
// pinned by tests per vendor.
func ParseRateLimitHeaders(header http.Header, now time.Time) Reported {
	if header == nil {
		return Reported{}
	}
	var reported Reported

	// OpenAI / Groq / Mistral / GitHub Models: split by resource.
	reported.LimitRequests = headerInt(header, "x-ratelimit-limit-requests")
	reported.RemainingRequests = headerInt(header, "x-ratelimit-remaining-requests")
	reported.LimitTokens = headerInt(header, "x-ratelimit-limit-tokens")
	reported.RemainingTokens = headerInt(header, "x-ratelimit-remaining-tokens")

	// Cerebras splits the same names by window. The minute window is the one
	// that maps onto RPM/TPM; the day window feeds the daily meter, which is
	// the only place any vendor reports a daily figure at all.
	if reported.RemainingRequests == nil {
		reported.LimitRequests = headerInt(header, "x-ratelimit-limit-requests-minute")
		reported.RemainingRequests = headerInt(header, "x-ratelimit-remaining-requests-minute")
	}
	if reported.RemainingTokens == nil {
		reported.LimitTokens = headerInt(header, "x-ratelimit-limit-tokens-minute")
		reported.RemainingTokens = headerInt(header, "x-ratelimit-remaining-tokens-minute")
	}
	reported.LimitDaily = headerInt(header, "x-ratelimit-limit-requests-day")
	reported.RemainingDaily = headerInt(header, "x-ratelimit-remaining-requests-day")

	// Anthropic's own spelling.
	if reported.RemainingRequests == nil {
		reported.LimitRequests = headerInt(header, "anthropic-ratelimit-requests-limit")
		reported.RemainingRequests = headerInt(header, "anthropic-ratelimit-requests-remaining")
	}
	if reported.RemainingTokens == nil {
		reported.LimitTokens = headerInt(header, "anthropic-ratelimit-tokens-limit")
		reported.RemainingTokens = headerInt(header, "anthropic-ratelimit-tokens-remaining")
	}

	// OpenRouter reports one undifferentiated request budget. It is read as a
	// request allowance because that is what OpenRouter throttles on; its
	// credit balance lives on a separate endpoint rather than in a header, so
	// there is nothing to read here for it.
	if reported.RemainingRequests == nil {
		reported.LimitRequests = headerInt(header, "x-ratelimit-limit")
		reported.RemainingRequests = headerInt(header, "x-ratelimit-remaining")
	}

	reported.ResetAt = firstResetAt(header, now,
		"x-ratelimit-reset-requests",
		"x-ratelimit-reset-requests-minute",
		"anthropic-ratelimit-requests-reset",
		"x-ratelimit-reset-tokens",
		"x-ratelimit-reset-tokens-minute",
		"anthropic-ratelimit-tokens-reset",
		"x-ratelimit-reset",
		"x-ratelimit-reset-requests-day",
	)
	if reported.ResetAt == 0 {
		if wait, ok := RetryAfter(header, now); ok {
			reported.ResetAt = now.Add(wait).UnixMilli()
		}
	}
	return reported
}

// RetryAfter reads the one header every vendor agrees on. It accepts both
// spellings the RFC allows — a number of seconds and an HTTP date — and
// refuses anything that would put the retry in the past.
func RetryAfter(header http.Header, now time.Time) (time.Duration, bool) {
	if header == nil {
		return 0, false
	}
	raw := strings.TrimSpace(header.Get("retry-after"))
	if raw == "" {
		// Some gateways send the OpenAI-style per-resource reset instead.
		raw = strings.TrimSpace(header.Get("x-ratelimit-reset-requests"))
	}
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		// A bare number this large is a unix timestamp, not a wait. Vendors
		// do this, and reading it as seconds would sleep for a century.
		if seconds > unixSecondsFloor {
			return until(absoluteFromNumber(seconds), now)
		}
		if seconds <= 0 {
			return 0, false
		}
		return time.Duration(seconds * float64(time.Second)), true
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return duration, true
	}
	if at, err := http.ParseTime(raw); err == nil {
		return until(at.UnixMilli(), now)
	}
	return 0, false
}

// unixSecondsFloor separates "a number of seconds to wait" from "a unix
// timestamp". 10^9 seconds is thirty-odd years, which no vendor is asking us
// to wait, and 10^9 as a timestamp is 2001 — so anything above it is a date.
const unixSecondsFloor = 1e9

// absoluteFromNumber reads a bare number as a unix timestamp, in whichever of
// the two units it is obviously in. Milliseconds are what OpenRouter sends.
func absoluteFromNumber(value float64) int64 {
	if value > 1e11 {
		return int64(value)
	}
	return int64(value * 1000)
}

func until(atMillis int64, now time.Time) (time.Duration, bool) {
	if atMillis <= 0 {
		return 0, false
	}
	wait := time.UnixMilli(atMillis).Sub(now)
	if wait <= 0 {
		return 0, false
	}
	return wait, true
}

// firstResetAt returns the soonest reset any of the named headers describes,
// as a unix-millisecond instant.
func firstResetAt(header http.Header, now time.Time, names ...string) int64 {
	var soonest int64
	for _, name := range names {
		at, ok := resetAt(header, name, now)
		if !ok {
			continue
		}
		if soonest == 0 || at < soonest {
			soonest = at
		}
	}
	return soonest
}

// resetAt reads one reset header in any of the four spellings vendors use: a
// Go-style duration ("2m59.56s", Groq), a bare count of seconds, a unix
// timestamp in seconds or milliseconds (OpenRouter), or an RFC 3339 instant
// (Anthropic).
func resetAt(header http.Header, name string, now time.Time) (int64, bool) {
	raw := strings.TrimSpace(header.Get(name))
	if raw == "" {
		return 0, false
	}
	if number, err := strconv.ParseFloat(raw, 64); err == nil {
		if number > unixSecondsFloor {
			return absoluteFromNumber(number), true
		}
		if number <= 0 {
			return 0, false
		}
		return now.Add(time.Duration(number * float64(time.Second))).UnixMilli(), true
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return now.Add(duration).UnixMilli(), true
	}
	if at, err := time.Parse(time.RFC3339, raw); err == nil {
		return at.UnixMilli(), true
	}
	if at, err := http.ParseTime(raw); err == nil {
		return at.UnixMilli(), true
	}
	return 0, false
}

// headerInt reads a non-negative integer header. Vendors occasionally send
// "1.2M" style abbreviations for a limit; those are read rather than dropped,
// because a limit we cannot parse turns a reported meter back into a counted
// one for no good reason.
func headerInt(header http.Header, name string) *int {
	raw := strings.TrimSpace(header.Get(name))
	if raw == "" {
		return nil
	}
	if value, err := strconv.Atoi(raw); err == nil && value >= 0 {
		return &value
	}
	if value, ok := parseAbbreviated(raw); ok {
		return &value
	}
	return nil
}

// parseAbbreviated reads "1.2M", "60k" and friends.
func parseAbbreviated(raw string) (int, bool) {
	lower := strings.ToLower(raw)
	multiplier := 1
	switch {
	case strings.HasSuffix(lower, "k"):
		multiplier = 1_000
	case strings.HasSuffix(lower, "m"):
		multiplier = 1_000_000
	case strings.HasSuffix(lower, "b"):
		multiplier = 1_000_000_000
	default:
		return 0, false
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(lower[:len(lower)-1]), 64)
	if err != nil || number < 0 {
		return 0, false
	}
	return int(number * float64(multiplier)), true
}
