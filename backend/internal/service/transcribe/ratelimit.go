package transcribe

import (
	"sync"
	"time"
)

// DefaultRateLimit is how many transcriptions one user may start per window.
// Dictation is bursty — a user correcting themselves fires several short
// clips in a row — so the ceiling is generous; it exists to stop a stuck
// client from spending the operator's provider budget in a loop.
const (
	DefaultRateLimit  = 30
	DefaultRateWindow = time.Minute
)

// rateLimiter is a per-user sliding window over request timestamps. The user
// set on one server is small (invited users only), so a map of slices is the
// right size of solution; entries are pruned as they are touched.
type rateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	hits map[string][]time.Time
}

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	if limit <= 0 {
		limit = DefaultRateLimit
	}
	if window <= 0 {
		window = DefaultRateWindow
	}
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{limit: limit, window: window, now: now, hits: map[string][]time.Time{}}
}

// allow records an attempt and reports whether it fits inside the window. A
// refused attempt is not recorded, so a client that backs off recovers as soon
// as the oldest hit ages out instead of being held down by its own retries.
func (l *rateLimiter) allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, hit := range l.hits[key] {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
