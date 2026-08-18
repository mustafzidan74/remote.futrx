package portal

import (
	"sync"
	"time"
)

const (
	// limiterWindow and limiterBudget bound how often one client address may
	// present a portal token. A client refreshing the page is unaffected;
	// someone walking the 256-bit token space is stopped long before it
	// matters.
	limiterWindow = time.Minute
	limiterBudget = 20
	// limiterMaxKeys caps the tracking table so a spray of forged
	// X-Forwarded-For values cannot grow it without bound.
	limiterMaxKeys = 4096
)

// limiter is a fixed-window counter per key. It is deliberately tiny: the
// platform has one process, one portal route, and no need for a shared
// rate-limit store.
type limiter struct {
	window time.Duration
	budget int
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	windowStart time.Time
	count       int
}

func newLimiter(window time.Duration, budget int, now func() time.Time) *limiter {
	if now == nil {
		now = time.Now
	}
	return &limiter{
		window:  window,
		budget:  budget,
		now:     now,
		buckets: map[string]*bucket{},
	}
}

// allow records one attempt for key and reports whether it may proceed. An
// empty key is never throttled: it means the transport could not identify the
// caller, and dropping every such request would be a self-inflicted outage.
func (l *limiter) allow(key string) bool {
	if l == nil || key == "" {
		return true
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	current, ok := l.buckets[key]
	if !ok || now.Sub(current.windowStart) >= l.window {
		l.pruneLocked(now)
		l.buckets[key] = &bucket{windowStart: now, count: 1}
		return true
	}
	current.count++
	return current.count <= l.budget
}

// pruneLocked drops expired buckets, and — if the table is still oversized —
// everything, since a fixed window makes a full reset cheap and harmless.
func (l *limiter) pruneLocked(now time.Time) {
	for key, entry := range l.buckets {
		if now.Sub(entry.windowStart) >= l.window {
			delete(l.buckets, key)
		}
	}
	if len(l.buckets) > limiterMaxKeys {
		l.buckets = map[string]*bucket{}
	}
}
