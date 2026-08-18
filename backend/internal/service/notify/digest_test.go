package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubDigestSource returns a fixed aggregate and counts how often it is asked.
type stubDigestSource struct {
	mu      sync.Mutex
	calls   int
	windows [][2]int64
	digest  Digest
	err     error
}

func (s *stubDigestSource) WeeklyDigest(_ context.Context, from, to int64) (Digest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.windows = append(s.windows, [2]int64{from, to})
	return s.digest, s.err
}

func (s *stubDigestSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// movableClock is a clock the test can advance while the digest loop reads it
// from its own goroutine.
type movableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movableClock) set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

func (c *movableClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func cairo(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(DefaultDigestTimezone)
	if err != nil {
		t.Fatalf("load %s: %v", DefaultDigestTimezone, err)
	}
	return location
}

func TestDefaultDigestConfigIsSundayNineAMCairo(t *testing.T) {
	digest := DefaultDigestConfig()
	if digest.Enabled {
		t.Fatal("the digest must start switched off")
	}
	if digest.Weekday != int(time.Sunday) {
		t.Fatalf("weekday = %d, want Sunday", digest.Weekday)
	}
	if digest.Hour != 9 {
		t.Fatalf("hour = %d, want 9", digest.Hour)
	}
	if digest.Timezone != "Africa/Cairo" {
		t.Fatalf("timezone = %q", digest.Timezone)
	}
}

func TestDigestLastOccurrence(t *testing.T) {
	location := cairo(t)
	config := DigestConfig{Enabled: true, Weekday: int(time.Sunday), Hour: 9, Timezone: DefaultDigestTimezone}

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "midweek looks back to the last Sunday",
			now:  time.Date(2026, 8, 12, 14, 30, 0, 0, location), // Wednesday
			want: time.Date(2026, 8, 9, 9, 0, 0, 0, location),
		},
		{
			name: "on the day but before the hour looks back a week",
			now:  time.Date(2026, 8, 16, 8, 59, 0, 0, location), // Sunday
			want: time.Date(2026, 8, 9, 9, 0, 0, 0, location),
		},
		{
			name: "on the day at the hour is the occurrence itself",
			now:  time.Date(2026, 8, 16, 9, 0, 0, 0, location),
			want: time.Date(2026, 8, 16, 9, 0, 0, 0, location),
		},
		{
			name: "the Saturday after is still that Sunday",
			now:  time.Date(2026, 8, 22, 23, 59, 0, 0, location),
			want: time.Date(2026, 8, 16, 9, 0, 0, 0, location),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := config.LastOccurrence(test.now)
			if !got.Equal(test.want) {
				t.Fatalf("LastOccurrence(%s) = %s, want %s", test.now, got, test.want)
			}
		})
	}
}

func TestDigestDueAt(t *testing.T) {
	location := cairo(t)
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, location) // Sunday, past the hour
	occurrence := time.Date(2026, 8, 16, 9, 0, 0, 0, location)
	base := DigestConfig{Enabled: true, Weekday: int(time.Sunday), Hour: 9, Timezone: DefaultDigestTimezone}

	tests := []struct {
		name       string
		config     DigestConfig
		wantDue    bool
		wantTarget time.Time
	}{
		{
			name:   "disabled is never due",
			config: func() DigestConfig { c := base; c.Enabled = false; return c }(),
		},
		{
			name:       "a never-sent schedule arms rather than fires",
			config:     base,
			wantDue:    false,
			wantTarget: occurrence,
		},
		{
			name: "last week's delivery makes this week due",
			config: func() DigestConfig {
				c := base
				c.LastSentAt = occurrence.AddDate(0, 0, -7).UnixMilli()
				return c
			}(),
			wantDue:    true,
			wantTarget: occurrence,
		},
		{
			name: "this week's delivery is not due again",
			config: func() DigestConfig {
				c := base
				c.LastSentAt = occurrence.UnixMilli()
				return c
			}(),
			wantDue:    false,
			wantTarget: occurrence,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, due := test.config.DueAt(now)
			if due != test.wantDue {
				t.Fatalf("due = %t, want %t", due, test.wantDue)
			}
			if test.wantDue && !target.Equal(test.wantTarget) {
				t.Fatalf("target = %s, want %s", target, test.wantTarget)
			}
		})
	}
}

func TestDigestSummaryReadsAsOneLine(t *testing.T) {
	location := cairo(t)
	from := time.Date(2026, 8, 11, 9, 0, 0, 0, location)
	to := time.Date(2026, 8, 18, 9, 0, 0, 0, location)

	summary := DigestSummary(Digest{
		From:         from.UnixMilli(),
		To:           to.UnixMilli(),
		TotalCostUSD: 12.3456,
		Runs:         56,
		Projects: []DigestProject{
			{Name: "shop", CostUSD: 7.1, Runs: 30},
			{Name: "wp-project", CostUSD: 5.24, Runs: 26},
		},
		TopModel: "claude-sonnet-4",
	}, location)

	for _, want := range []string{
		"Weekly usage 11–18 Aug",
		"total $12.35",
		"(56 runs)",
		"shop $7.10 (30 runs)",
		"wp-project $5.24 (26 runs)",
		"top model claude-sonnet-4",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q is missing %q", summary, want)
		}
	}
	if strings.Contains(summary, "\n") {
		t.Fatalf("the digest body must stay on one line: %q", summary)
	}
}

func TestDigestSummaryCollapsesLongProjectLists(t *testing.T) {
	location := time.UTC
	projects := make([]DigestProject, 0, 8)
	for i := 0; i < 8; i++ {
		projects = append(projects, DigestProject{Name: "p", CostUSD: 1, Runs: 1})
	}
	summary := DigestSummary(Digest{Runs: 8, Projects: projects}, location)
	if !strings.Contains(summary, "+3 more projects") {
		t.Fatalf("summary %q should collapse the tail", summary)
	}
}

func TestDigestSummaryHandlesAnEmptyWeek(t *testing.T) {
	summary := DigestSummary(Digest{
		From: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC).UnixMilli(),
		To:   time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC).UnixMilli(),
	}, time.UTC)
	if !strings.Contains(summary, "no agent runs") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestFormatDigestRange(t *testing.T) {
	location := time.UTC
	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want string
	}{
		{
			name: "inside one month",
			from: time.Date(2026, 8, 11, 9, 0, 0, 0, location),
			to:   time.Date(2026, 8, 18, 9, 0, 0, 0, location),
			want: "11–18 Aug",
		},
		{
			name: "across two months",
			from: time.Date(2026, 7, 28, 9, 0, 0, 0, location),
			to:   time.Date(2026, 8, 4, 9, 0, 0, 0, location),
			want: "28 Jul–4 Aug",
		},
		{
			name: "across two years",
			from: time.Date(2025, 12, 29, 9, 0, 0, 0, location),
			to:   time.Date(2026, 1, 5, 9, 0, 0, 0, location),
			want: "29 Dec 2025–5 Jan 2026",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := FormatDigestRange(test.from.UnixMilli(), test.to.UnixMilli(), location)
			if got != test.want {
				t.Fatalf("FormatDigestRange = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDigestTickSendsOncePerWeek(t *testing.T) {
	location := cairo(t)
	store := &memoryStore{config: DefaultConfig()}
	source := &stubDigestSource{digest: Digest{
		TotalCostUSD: 3, Runs: 2,
		Projects: []DigestProject{{Name: "shop", CostUSD: 3, Runs: 2}},
	}}
	sink := newRecordingSink("fake", 0)
	notifier := NewNotifier(nil, WithSinks(sink), WithBackoff(time.Millisecond))

	clock := &movableClock{now: time.Date(2026, 8, 16, 10, 0, 0, 0, location)}
	service := New(
		context.Background(),
		store,
		"https://remote.example.com",
		WithNotifier(notifier),
		WithDigestSource(source),
		WithClock(clock.read),
	)
	t.Cleanup(service.Stop)

	if _, err := service.Save(context.Background(), UpdateInput{
		Enabled:  true,
		Telegram: TelegramInput{BotToken: "token", ChatID: "chat"},
		Digest: DigestInput{
			Enabled: true, Weekday: int(time.Sunday), Hour: 9, Timezone: DefaultDigestTimezone,
		},
	}); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	// The first pass arms the schedule instead of firing, so an operator who
	// just turned the digest on does not immediately receive last week.
	service.digestTick(context.Background())
	if source.count() != 0 {
		t.Fatalf("aggregation ran %d times on the arming pass", source.count())
	}
	armed := service.Config().Digest.LastSentAt
	want := time.Date(2026, 8, 16, 9, 0, 0, 0, location).UnixMilli()
	if armed != want {
		t.Fatalf("armed at %d, want the current occurrence %d", armed, want)
	}

	// A week later exactly one digest goes out, however many times the loop
	// ticks inside that week.
	clock.set(time.Date(2026, 8, 23, 9, 30, 0, 0, location))
	service.digestTick(context.Background())
	service.digestTick(context.Background())
	clock.set(time.Date(2026, 8, 25, 18, 0, 0, 0, location))
	service.digestTick(context.Background())

	if source.count() != 1 {
		t.Fatalf("aggregation ran %d times, want exactly one digest for the week", source.count())
	}
	sink.waitForDelivery(t)
	_, delivered := sink.counts()
	if len(delivered) != 1 {
		t.Fatalf("delivered %d events, want 1", len(delivered))
	}
	if delivered[0].Event != KindDigest {
		t.Fatalf("event kind = %q", delivered[0].Event)
	}
	if !strings.Contains(delivered[0].Summary, "Weekly usage") {
		t.Fatalf("summary = %q", delivered[0].Summary)
	}
	if delivered[0].URL != "https://remote.example.com/" {
		t.Fatalf("url = %q", delivered[0].URL)
	}

	// The claim survives a restart: the stored document carries it.
	stored, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	sent := time.Date(2026, 8, 23, 9, 0, 0, 0, location).UnixMilli()
	if stored.Digest.LastSentAt != sent {
		t.Fatalf("stored lastDigestSentAt = %d, want %d", stored.Digest.LastSentAt, sent)
	}
}

func TestDigestTickAggregatesTheSevenDaysBeforeTheOccurrence(t *testing.T) {
	location := cairo(t)
	source := &stubDigestSource{}
	clock := &movableClock{now: time.Date(2026, 8, 23, 9, 30, 0, 0, location)}
	occurrence := time.Date(2026, 8, 23, 9, 0, 0, 0, location)

	service := New(
		context.Background(),
		&memoryStore{config: Config{
			Enabled:  true,
			Telegram: TelegramConfig{BotToken: "t", ChatID: "c"},
			Digest: DigestConfig{
				Enabled: true, Weekday: int(time.Sunday), Hour: 9, Timezone: DefaultDigestTimezone,
				LastSentAt: occurrence.AddDate(0, 0, -7).UnixMilli(),
			},
		}},
		"https://remote.example.com",
		WithNotifier(NewNotifier(nil, WithSinks(newRecordingSink("fake", 0)))),
		WithDigestSource(source),
		WithClock(clock.read),
	)
	t.Cleanup(service.Stop)

	service.digestTick(context.Background())

	if len(source.windows) != 1 {
		t.Fatalf("windows = %v", source.windows)
	}
	wantFrom := occurrence.AddDate(0, 0, -7).UnixMilli()
	if source.windows[0][0] != wantFrom || source.windows[0][1] != occurrence.UnixMilli() {
		t.Fatalf("window = %v, want [%d %d]", source.windows[0], wantFrom, occurrence.UnixMilli())
	}
}

func TestDigestTickKeepsTheClaimWhenAggregationFails(t *testing.T) {
	location := cairo(t)
	occurrence := time.Date(2026, 8, 23, 9, 0, 0, 0, location)
	source := &stubDigestSource{err: errors.New("ledger unreadable")}
	sink := newRecordingSink("fake", 0)

	service := New(
		context.Background(),
		&memoryStore{config: Config{
			Enabled:  true,
			Telegram: TelegramConfig{BotToken: "t", ChatID: "c"},
			Digest: DigestConfig{
				Enabled: true, Weekday: int(time.Sunday), Hour: 9, Timezone: DefaultDigestTimezone,
				LastSentAt: occurrence.AddDate(0, 0, -7).UnixMilli(),
			},
		}},
		"https://remote.example.com",
		WithNotifier(NewNotifier(nil, WithSinks(sink))),
		WithDigestSource(source),
		WithClock(func() time.Time { return time.Date(2026, 8, 23, 9, 30, 0, 0, location) }),
	)
	t.Cleanup(service.Stop)

	service.digestTick(context.Background())
	service.digestTick(context.Background())

	// A broken ledger must not turn into a retry storm against the sinks.
	if source.count() != 1 {
		t.Fatalf("aggregation ran %d times, want 1", source.count())
	}
	if attempts, _ := sink.counts(); attempts != 0 {
		t.Fatalf("sink saw %d attempts, want none", attempts)
	}
}

func TestSendDigestNowDoesNotConsumeTheWeeklySlot(t *testing.T) {
	location := cairo(t)
	source := &stubDigestSource{digest: Digest{TotalCostUSD: 1, Runs: 1}}
	sink := newRecordingSink("fake", 0)
	clock := &movableClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, location)}

	service := New(
		context.Background(),
		&memoryStore{config: Config{
			Enabled:  true,
			Telegram: TelegramConfig{BotToken: "t", ChatID: "c"},
			Digest: DigestConfig{
				Enabled: true, Weekday: int(time.Sunday), Hour: 9, Timezone: DefaultDigestTimezone,
				LastSentAt: time.Date(2026, 8, 16, 9, 0, 0, 0, location).UnixMilli(),
			},
		}},
		"https://remote.example.com",
		WithNotifier(NewNotifier(nil, WithSinks(sink))),
		WithDigestSource(source),
		WithClock(clock.read),
	)
	t.Cleanup(service.Stop)

	before := service.Config().Digest.LastSentAt
	results, err := service.SendDigestNow(context.Background())
	if err != nil {
		t.Fatalf("SendDigestNow() = %v", err)
	}
	if len(results) != 1 || !results[0].Delivered {
		t.Fatalf("results = %+v", results)
	}
	if after := service.Config().Digest.LastSentAt; after != before {
		t.Fatalf("lastDigestSentAt moved from %d to %d", before, after)
	}
}

func TestSendDigestNowWithoutALedgerReportsAnError(t *testing.T) {
	service := New(
		context.Background(),
		&memoryStore{config: DefaultConfig()},
		"https://remote.example.com",
		WithNotifier(NewNotifier(nil, WithSinks(newRecordingSink("fake", 0)))),
	)
	t.Cleanup(service.Stop)

	if _, err := service.SendDigestNow(context.Background()); err == nil {
		t.Fatal("expected an error when no usage ledger is wired up")
	}
}

func TestDigestScheduleRequiresNotificationsToBeOn(t *testing.T) {
	service := New(
		context.Background(),
		&memoryStore{config: DefaultConfig()},
		"https://remote.example.com",
		WithNotifier(NewNotifier(nil, WithSinks(newRecordingSink("fake", 0)))),
	)
	t.Cleanup(service.Stop)

	_, err := service.Save(context.Background(), UpdateInput{
		Enabled:  false,
		Telegram: TelegramInput{BotToken: "t", ChatID: "c"},
		Digest:   DigestInput{Enabled: true, Weekday: 0, Hour: 9, Timezone: DefaultDigestTimezone},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Save() = %v, want ErrInvalidConfig", err)
	}
}
