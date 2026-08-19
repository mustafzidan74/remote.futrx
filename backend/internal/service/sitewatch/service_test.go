package sitewatch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// memoryStore is the file store's in-memory stand-in.
type memoryStore struct {
	mu      sync.Mutex
	sites   []Site
	history map[ID][]Record
	saveErr error
}

func newMemoryStore(sites ...Site) *memoryStore {
	return &memoryStore{sites: sites, history: map[ID][]Record{}}
}

func (s *memoryStore) Load(context.Context) ([]Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Site(nil), s.sites...), nil
}

func (s *memoryStore) Save(_ context.Context, sites []Site) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sites = append([]Site(nil), sites...)
	return nil
}

func (s *memoryStore) LoadHistory(_ context.Context, id ID) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.history[id]...), nil
}

func (s *memoryStore) AppendHistory(_ context.Context, id ID, record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history[id] = append(s.history[id], record)
	return nil
}

func (s *memoryStore) DeleteHistory(_ context.Context, id ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.history, id)
	return nil
}

// scriptedProber answers from a queue of probes, per URL.
type scriptedProber struct {
	mu       sync.Mutex
	answers  map[string][]Probe
	fallback Probe
	calls    []string
}

func (p *scriptedProber) Probe(_ context.Context, endpoint Endpoint, _ Site) Probe {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, endpoint.URL)
	queue := p.answers[endpoint.URL]
	if len(queue) == 0 {
		return p.fallback
	}
	next := queue[0]
	if len(queue) > 1 {
		p.answers[endpoint.URL] = queue[1:]
	}
	return next
}

type recordingAlerter struct {
	mu     sync.Mutex
	alerts []Alert
	sites  []Site
}

func (a *recordingAlerter) SiteStateChanged(_ context.Context, site Site, alert Alert) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sites = append(a.sites, site)
	a.alerts = append(a.alerts, alert)
}

func (a *recordingAlerter) kinds() []AlertKind {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]AlertKind, 0, len(a.alerts))
	for _, alert := range a.alerts {
		out = append(out, alert.Kind)
	}
	return out
}

type stubAccess struct {
	allowed map[string]string // projectID -> email
}

func (s stubAccess) HasProjectAccess(_ context.Context, projectID, email string) (bool, error) {
	return s.allowed[projectID] == email, nil
}

type stubCatalog struct {
	candidates []Candidate
	err        error
}

func (c stubCatalog) Candidates(context.Context) ([]Candidate, error) {
	return c.candidates, c.err
}

// clock is a hand-cranked wall clock.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func newClock(at time.Time) *clock { return &clock{at: at} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

const (
	adminEmail  = "admin@example.com"
	memberEmail = "member@example.com"
	siteAlpha   = ID("aaaaaaaaaaaaaaaaaaaaaaaa")
	siteBeta    = ID("bbbbbbbbbbbbbbbbbbbbbbbb")
)

func fixedSite(id ID, url string) Site {
	return Site{
		ID:              id,
		URL:             url,
		Enabled:         true,
		IntervalMinutes: 5,
		Checks:          DefaultChecks(),
		Notify:          true,
		Method:          MethodHEAD,
	}
}

func TestCheckNowAlertsOnlyAfterTwoConsecutiveFailuresAndOnceOnRecovery(t *testing.T) {
	tick := newClock(time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC))
	store := newMemoryStore(fixedSite(siteAlpha, "https://shop.example.com/"))
	prober := &scriptedProber{answers: map[string][]Probe{
		"https://shop.example.com/": {
			{StatusCode: 200, Duration: 120 * time.Millisecond},
			{StatusCode: 502, Duration: 1200 * time.Millisecond},
			{StatusCode: 502, Duration: 1200 * time.Millisecond},
			{StatusCode: 502, Duration: 1200 * time.Millisecond},
			{StatusCode: 200, Duration: 120 * time.Millisecond},
			{StatusCode: 200, Duration: 120 * time.Millisecond},
		},
	}}
	alerter := &recordingAlerter{}
	service := New(context.Background(), Dependencies{Store: store, Alerter: alerter},
		WithProber(prober), WithClock(tick.now))

	check := func() View {
		t.Helper()
		report, err := service.CheckNow(context.Background(), siteAlpha, adminEmail, true)
		if err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		tick.advance(5 * time.Minute)
		return report.Site
	}

	if view := check(); view.Status != StatusUp {
		t.Fatalf("first check status = %q, want up", view.Status)
	}
	if len(alerter.kinds()) != 0 {
		t.Fatalf("alerts after a healthy first check = %v, want none", alerter.kinds())
	}

	if view := check(); view.Status != StatusUp {
		t.Fatalf("after one failure status = %q, want still up", view.Status)
	}
	if len(alerter.kinds()) != 0 {
		t.Fatalf("alerts after one failure = %v, want none", alerter.kinds())
	}

	view := check()
	if view.Status != StatusDown {
		t.Fatalf("after two failures status = %q, want down", view.Status)
	}
	if got := alerter.kinds(); len(got) != 1 || got[0] != AlertDown {
		t.Fatalf("alerts after two failures = %v, want one down", got)
	}
	if summary := alerter.alerts[0].Summary; !strings.Contains(summary, "502") {
		t.Fatalf("down summary = %q, want the response code in it", summary)
	}

	check() // still down: silence
	if got := alerter.kinds(); len(got) != 1 {
		t.Fatalf("alerts while it stays down = %v, want no new message", got)
	}

	check() // one good check: not yet recovered
	if got := alerter.kinds(); len(got) != 1 {
		t.Fatalf("alerts after one good check = %v, want no recovery yet", got)
	}

	view = check()
	if view.Status != StatusUp {
		t.Fatalf("after two good checks status = %q, want up", view.Status)
	}
	got := alerter.kinds()
	if len(got) != 2 || got[1] != AlertRecovered {
		t.Fatalf("alerts = %v, want a recovery second", got)
	}
	if !strings.Contains(alerter.alerts[1].Summary, "back after") {
		t.Fatalf("recovery summary = %q, want the outage length", alerter.alerts[1].Summary)
	}
}

func TestCheckNowReportsEveryEndpointAndTakesTheWorst(t *testing.T) {
	tick := newClock(time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC))
	site := fixedSite(siteAlpha, "https://shop.example.com/")
	site.ExtraURLs = []Endpoint{{
		Label:  "Checkout",
		URL:    "https://shop.example.com/checkout",
		Checks: DefaultChecks(),
	}}
	store := newMemoryStore(site)
	prober := &scriptedProber{answers: map[string][]Probe{
		"https://shop.example.com/":         {{StatusCode: 200, Duration: 90 * time.Millisecond}},
		"https://shop.example.com/checkout": {{StatusCode: 500, Duration: 400 * time.Millisecond}},
	}}
	service := New(context.Background(), Dependencies{Store: store},
		WithProber(prober), WithClock(tick.now))

	report, err := service.CheckNow(context.Background(), siteAlpha, adminEmail, true)
	if err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if len(report.Endpoints) != 2 {
		t.Fatalf("endpoints = %d, want the primary plus one extra", len(report.Endpoints))
	}
	if report.Endpoints[0].Status != StatusUp || report.Endpoints[1].Status != StatusDown {
		t.Fatalf("endpoint statuses = %q/%q, want up then down",
			report.Endpoints[0].Status, report.Endpoints[1].Status)
	}
	// One check does not move the row yet, but the raw results are the point
	// of the button: they say exactly which page is broken.
	if !strings.Contains(report.Site.LastError, "Checkout") {
		t.Fatalf("row detail = %q, want the checkout page named", report.Site.LastError)
	}
	if len(store.history[siteAlpha]) != 1 {
		t.Fatalf("history = %d records, want the check recorded", len(store.history[siteAlpha]))
	}
}

func TestCheckNowRaisesTheCertificateWarningExactlyOnce(t *testing.T) {
	tick := newClock(time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC))
	store := newMemoryStore(fixedSite(siteAlpha, "https://shop.example.com/"))
	expiring := Probe{StatusCode: 200, Duration: 80 * time.Millisecond, TLSExpiresAt: tick.now().Add(9 * 24 * time.Hour)}
	renewed := Probe{StatusCode: 200, Duration: 80 * time.Millisecond, TLSExpiresAt: tick.now().Add(90 * 24 * time.Hour)}
	prober := &scriptedProber{answers: map[string][]Probe{
		"https://shop.example.com/": {expiring, expiring, renewed, expiring},
	}}
	alerter := &recordingAlerter{}
	service := New(context.Background(), Dependencies{Store: store, Alerter: alerter},
		WithProber(prober), WithClock(tick.now))

	for range 2 {
		if _, err := service.CheckNow(context.Background(), siteAlpha, adminEmail, true); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
	}
	if got := alerter.kinds(); len(got) != 1 || got[0] != AlertTLS {
		t.Fatalf("alerts = %v, want exactly one certificate warning", got)
	}

	// Renewal clears the latch; the next expiry warns again.
	if _, err := service.CheckNow(context.Background(), siteAlpha, adminEmail, true); err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if got := alerter.kinds(); len(got) != 1 {
		t.Fatalf("alerts after renewal = %v, want no new message", got)
	}
	if _, err := service.CheckNow(context.Background(), siteAlpha, adminEmail, true); err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if got := alerter.kinds(); len(got) != 2 || got[1] != AlertTLS {
		t.Fatalf("alerts = %v, want a second warning after the latch cleared", got)
	}
}

func TestNotifyOffKeepsTheRowRedAndTheSinksQuiet(t *testing.T) {
	tick := newClock(time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC))
	site := fixedSite(siteAlpha, "https://shop.example.com/")
	site.Notify = false
	store := newMemoryStore(site)
	prober := &scriptedProber{fallback: Probe{StatusCode: 500, Duration: 50 * time.Millisecond}}
	alerter := &recordingAlerter{}
	service := New(context.Background(), Dependencies{Store: store, Alerter: alerter},
		WithProber(prober), WithClock(tick.now))

	var last View
	for range 2 {
		report, err := service.CheckNow(context.Background(), siteAlpha, adminEmail, true)
		if err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		last = report.Site
		tick.advance(5 * time.Minute)
	}
	if last.Status != StatusDown {
		t.Fatalf("status = %q, want down: the watching itself is unaffected", last.Status)
	}
	if got := alerter.kinds(); len(got) != 0 {
		t.Fatalf("alerts = %v, want none for a site with notifications off", got)
	}
}

func TestSweepChecksOnlyWhatIsDue(t *testing.T) {
	start := time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)
	tick := newClock(start)
	store := newMemoryStore(
		fixedSite(siteAlpha, "https://alpha.example.com/"),
		fixedSite(siteBeta, "https://beta.example.com/"),
	)
	prober := &scriptedProber{fallback: Probe{StatusCode: 200, Duration: 40 * time.Millisecond}}
	service := New(context.Background(), Dependencies{Store: store},
		WithProber(prober), WithClock(tick.now))

	// Nothing is due at boot: every site is armed somewhere inside its first
	// interval, which is what spreads a restart's load.
	service.Sweep(context.Background())
	if len(prober.calls) != 0 {
		t.Fatalf("boot sweep probed %v, want nothing before the spread instant", prober.calls)
	}

	// One interval later every site's first instant has passed.
	tick.advance(6 * time.Minute)
	service.Sweep(context.Background())
	if len(prober.calls) != 2 {
		t.Fatalf("first due sweep probed %v, want both sites", prober.calls)
	}

	// Immediately afterwards nothing is due again.
	service.Sweep(context.Background())
	if len(prober.calls) != 2 {
		t.Fatalf("immediate re-sweep probed %v, want no repeat", prober.calls)
	}

	// A disabled site is never due, however overdue it looks.
	if _, err := service.Update(context.Background(), siteBeta, Input{
		URL:             "https://beta.example.com/",
		Enabled:         false,
		IntervalMinutes: 5,
		Checks:          DefaultChecks(),
		Method:          MethodHEAD,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	tick.advance(30 * time.Minute)
	service.Sweep(context.Background())
	for _, call := range prober.calls[2:] {
		if strings.Contains(call, "beta") {
			t.Fatalf("a disabled site was probed: %v", prober.calls)
		}
	}
}

func TestVisibilityHidesUnlinkedAndForeignSitesFromMembers(t *testing.T) {
	linked := fixedSite(siteAlpha, "https://client.example.com/")
	linked.ProjectID = "proj-1"
	unlinked := fixedSite(siteBeta, "https://internal.example.com/")
	store := newMemoryStore(linked, unlinked)
	service := New(context.Background(), Dependencies{
		Store:  store,
		Access: stubAccess{allowed: map[string]string{"proj-1": memberEmail}},
	}, WithProber(&scriptedProber{}))

	admin, err := service.List(context.Background(), adminEmail, true)
	if err != nil {
		t.Fatalf("admin List: %v", err)
	}
	if len(admin) != 2 {
		t.Fatalf("admin sees %d sites, want both", len(admin))
	}

	member, err := service.List(context.Background(), memberEmail, false)
	if err != nil {
		t.Fatalf("member List: %v", err)
	}
	if len(member) != 1 || member[0].ID != siteAlpha {
		t.Fatalf("member sees %v, want only the linked site", member)
	}

	stranger, err := service.List(context.Background(), "stranger@example.com", false)
	if err != nil {
		t.Fatalf("stranger List: %v", err)
	}
	if len(stranger) != 0 {
		t.Fatalf("stranger sees %v, want nothing", stranger)
	}

	// An invisible site is "not found", never "forbidden": a member must not
	// be able to discover which ids exist.
	if _, err := service.Get(context.Background(), siteBeta, memberEmail, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member Get on an unlinked site = %v, want ErrNotFound", err)
	}
	if _, err := service.CheckNow(context.Background(), siteBeta, memberEmail, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member CheckNow on an unlinked site = %v, want ErrNotFound", err)
	}
}

func TestCreateRejectsDuplicatesAndUnusableURLs(t *testing.T) {
	store := newMemoryStore()
	service := New(context.Background(), Dependencies{Store: store}, WithProber(&scriptedProber{}))

	created, err := service.Create(context.Background(), Input{
		URL:             "shop.example.com",
		Enabled:         true,
		IntervalMinutes: 5,
		Checks:          DefaultChecks(),
		Method:          MethodHEAD,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.URL != "https://shop.example.com/" {
		t.Fatalf("stored URL = %q, want the normalized https form", created.URL)
	}
	if created.Status != StatusUnknown {
		t.Fatalf("status = %q, want unknown before the first check", created.Status)
	}

	if _, err := service.Create(context.Background(), Input{
		URL:             "http://shop.example.com",
		IntervalMinutes: 5,
		Checks:          DefaultChecks(),
	}); !errors.Is(err, ErrInvalidSite) {
		t.Fatalf("duplicate Create = %v, want ErrInvalidSite", err)
	}

	if _, err := service.Create(context.Background(), Input{
		URL:             "ftp://files.example.com",
		IntervalMinutes: 5,
	}); !errors.Is(err, ErrInvalidSite) {
		t.Fatalf("non-http Create = %v, want ErrInvalidSite", err)
	}

	if _, err := service.Create(context.Background(), Input{
		URL:             "localhost:8080",
		IntervalMinutes: 5,
	}); !errors.Is(err, ErrInvalidSite) {
		t.Fatalf("hostname without a dot = %v, want ErrInvalidSite", err)
	}
}

func TestDeleteRemovesTheSiteAndItsHistory(t *testing.T) {
	store := newMemoryStore(fixedSite(siteAlpha, "https://shop.example.com/"))
	store.history[siteAlpha] = []Record{{At: 1, Status: StatusUp}}
	service := New(context.Background(), Dependencies{Store: store}, WithProber(&scriptedProber{}))

	if err := service.Delete(context.Background(), siteAlpha); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.history[siteAlpha]; ok {
		t.Fatal("history survived the delete")
	}
	if err := service.Delete(context.Background(), siteAlpha); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestImportTakesAPastedListAndTheProjectDomains(t *testing.T) {
	store := newMemoryStore(fixedSite(siteAlpha, "https://shop.example.com/"))
	service := New(context.Background(), Dependencies{
		Store: store,
		Catalog: stubCatalog{candidates: []Candidate{
			{ProjectID: "proj-1", ProjectName: "Acme shop", Domain: "acme.example.com", SecretKey: "HESTIA_DOMAIN"},
			{ProjectID: "proj-2", ProjectName: "Broken", Domain: "not a domain", SecretKey: "HESTIA_DOMAIN"},
		}},
	}, WithProber(&scriptedProber{}))

	result, err := service.Import(context.Background(), ImportInput{
		URLs:         "blog.example.com\nshop.example.com\n# a comment\n",
		FromProjects: true,
		Notify:       true,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Created) != 2 {
		t.Fatalf("created %d sites, want the blog and the project domain", len(result.Created))
	}
	created := map[string]View{}
	for _, view := range result.Created {
		created[view.URL] = view
	}
	if _, ok := created["https://blog.example.com/"]; !ok {
		t.Fatalf("created = %v, want the pasted blog", created)
	}
	acme, ok := created["https://acme.example.com/"]
	if !ok {
		t.Fatalf("created = %v, want the project domain", created)
	}
	if acme.ProjectID != "proj-1" || acme.Label != "Acme shop" {
		t.Fatalf("project-derived site = %+v, want it linked and labelled", acme.Site)
	}
	if !acme.Notify || acme.IntervalMinutes != DefaultIntervalMinutes {
		t.Fatalf("imported defaults = %+v, want notify on at the default cadence", acme.Site)
	}

	skipped := map[string]string{}
	for _, item := range result.Skipped {
		skipped[item.URL] = item.Reason
	}
	if _, ok := skipped["https://shop.example.com/"]; !ok {
		t.Fatalf("skipped = %v, want the already-watched site reported", skipped)
	}
	if _, ok := skipped["not a domain"]; !ok {
		t.Fatalf("skipped = %v, want the unusable secret reported", skipped)
	}
}

func TestCandidatesHideWhatIsAlreadyWatched(t *testing.T) {
	store := newMemoryStore(fixedSite(siteAlpha, "https://acme.example.com/"))
	service := New(context.Background(), Dependencies{
		Store: store,
		Catalog: stubCatalog{candidates: []Candidate{
			{ProjectID: "proj-1", Domain: "acme.example.com"},
			{ProjectID: "proj-2", Domain: "https://new.example.com/"},
		}},
	}, WithProber(&scriptedProber{}))

	got, err := service.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(got) != 1 || got[0].Domain != "https://new.example.com/" {
		t.Fatalf("candidates = %v, want only the unwatched one", got)
	}
}

func TestNotGreenIsWhatTheHomeScreenLists(t *testing.T) {
	tick := newClock(time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC))
	store := newMemoryStore(
		fixedSite(siteAlpha, "https://alpha.example.com/"),
		fixedSite(siteBeta, "https://beta.example.com/"),
	)
	prober := &scriptedProber{answers: map[string][]Probe{
		"https://alpha.example.com/": {{StatusCode: 200, Duration: 30 * time.Millisecond}},
		"https://beta.example.com/":  {{StatusCode: 503, Duration: 30 * time.Millisecond}},
	}}
	service := New(context.Background(), Dependencies{Store: store},
		WithProber(prober), WithClock(tick.now))

	for range 2 {
		for _, id := range []ID{siteAlpha, siteBeta} {
			if _, err := service.CheckNow(context.Background(), id, adminEmail, true); err != nil {
				t.Fatalf("CheckNow: %v", err)
			}
		}
		tick.advance(5 * time.Minute)
	}

	unwell, err := service.NotGreen(context.Background(), adminEmail, true)
	if err != nil {
		t.Fatalf("NotGreen: %v", err)
	}
	if len(unwell) != 1 || unwell[0].ID != siteBeta {
		t.Fatalf("not green = %v, want only the failing site", unwell)
	}
}

func TestAnUnavailableServiceAnswersRatherThanPanics(t *testing.T) {
	service := New(context.Background(), Dependencies{})
	if service.Available() {
		t.Fatal("a service with no store must report unavailable")
	}
	if _, err := service.List(context.Background(), adminEmail, true); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("List = %v, want ErrUnavailable", err)
	}
	if _, err := service.Create(context.Background(), Input{URL: "example.com"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Create = %v, want ErrUnavailable", err)
	}
	// Start and Sweep on an unavailable service must simply do nothing.
	service.Start(context.Background())
	service.Sweep(context.Background())
	service.Stop()
}

func TestHistoryIsBoundedAndSeedsTheRowAfterARestart(t *testing.T) {
	tick := newClock(time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC))
	store := newMemoryStore(fixedSite(siteAlpha, "https://shop.example.com/"))
	// A site that was already down when the process stopped.
	store.history[siteAlpha] = []Record{
		{At: tick.now().Add(-30 * time.Minute).UnixMilli(), Status: StatusUp, DurationMs: 40},
		{At: tick.now().Add(-20 * time.Minute).UnixMilli(), Status: StatusDown, Code: 502, DurationMs: 900, Error: "answered HTTP 502"},
		{At: tick.now().Add(-10 * time.Minute).UnixMilli(), Status: StatusDown, Code: 502, DurationMs: 900, Error: "answered HTTP 502"},
	}
	prober := &scriptedProber{fallback: Probe{StatusCode: 200, Duration: 40 * time.Millisecond}}
	alerter := &recordingAlerter{}
	service := New(context.Background(), Dependencies{Store: store, Alerter: alerter},
		WithProber(prober), WithClock(tick.now))

	views, err := service.List(context.Background(), adminEmail, true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 1 || views[0].Status != StatusDown {
		t.Fatalf("restored row = %+v, want it still down", views)
	}
	if views[0].Uptime.Checks != 3 {
		t.Fatalf("uptime checks = %d, want the loaded history", views[0].Uptime.Checks)
	}
	if views[0].ChangedAt != store.history[siteAlpha][1].At {
		t.Fatalf("changedAt = %d, want the start of the running outage", views[0].ChangedAt)
	}

	// Two good checks recover, and the message knows how long it was out.
	for range 2 {
		if _, err := service.CheckNow(context.Background(), siteAlpha, adminEmail, true); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		tick.advance(5 * time.Minute)
	}
	if got := alerter.kinds(); len(got) != 1 || got[0] != AlertRecovered {
		t.Fatalf("alerts = %v, want one recovery", got)
	}
	// The outage started 20 minutes before the process came back and ended
	// on the second good check five minutes later.
	if !strings.Contains(alerter.alerts[0].Summary, "back after 25 m") {
		t.Fatalf("recovery summary = %q, want the pre-restart outage length", alerter.alerts[0].Summary)
	}
}
