package sitewatch

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUnavailable is returned when this deployment has no sitewatch store.
	ErrUnavailable = errors.New("client site monitoring is unavailable")
	// ErrNotFound is returned for an unknown or invisible site. The two are
	// deliberately the same answer: a member must not learn that a site they
	// cannot see exists.
	ErrNotFound = errors.New("site not found")
	// ErrInvalidSite is returned for input the operator can fix.
	ErrInvalidSite = errors.New("invalid site")
	// ErrTooManySites is returned once the global cap is reached.
	ErrTooManySites = errors.New("site limit reached")
)

const (
	// tickInterval is how often the scheduler asks "is anything due?". The
	// cadence has minute granularity, so a coarse tick is plenty and keeps an
	// idle box quiet.
	tickInterval = 15 * time.Second
	// batchLimit bounds one tick's work. At the fastest cadence a full fleet
	// needs about seventeen checks per tick, so this leaves headroom without
	// ever letting a backlog turn into a burst.
	batchLimit = 25
	// probeWorkers is how many sites are checked at once. Each one is a
	// single request that mostly waits, so the bound is about politeness to
	// the host's uplink rather than about CPU.
	probeWorkers = 8
	// sweepTimeout bounds one whole tick.
	sweepTimeout = 2 * time.Minute
)

// Store persists the site catalog and the per-site check history.
type Store interface {
	Load(ctx context.Context) ([]Site, error)
	Save(ctx context.Context, sites []Site) error
	LoadHistory(ctx context.Context, id ID) ([]Record, error)
	// AppendHistory adds one record and trims the file to MaxHistoryRecords.
	AppendHistory(ctx context.Context, id ID, record Record) error
	DeleteHistory(ctx context.Context, id ID) error
}

// Access answers "may this person see this project?". It is the visibility
// rule for a linked site; an unlinked site is admin-only and never reaches
// this port.
type Access interface {
	HasProjectAccess(ctx context.Context, projectID, email string) (bool, error)
}

// Catalog suggests sites from what the platform already knows: the domains
// stored in projects' own secrets. It is optional — without it the bulk
// import can still take a pasted list.
type Catalog interface {
	Candidates(ctx context.Context) ([]Candidate, error)
}

// Alerter receives the settled state changes worth telling a human about. It
// is implemented in the composition package, the only place allowed to bridge
// this service and the notification service.
type Alerter interface {
	SiteStateChanged(ctx context.Context, site Site, alert Alert)
}

// Dependencies groups the collaborators. Only Store is required: without it
// the service reports unavailable and never schedules anything.
type Dependencies struct {
	Store   Store
	Access  Access
	Catalog Catalog
	Alerter Alerter
}

// Option customizes the service.
type Option func(*Service)

// WithProber replaces the HTTP transport. Tests answer without a network.
func WithProber(prober Prober) Option {
	return func(s *Service) {
		if prober != nil {
			s.prober = prober
		}
	}
}

// WithClock replaces the clock behind the schedule, the timestamps, and the
// uptime windows.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithTickInterval replaces how often the scheduler wakes up.
func WithTickInterval(interval time.Duration) Option {
	return func(s *Service) {
		if interval > 0 {
			s.tick = interval
		}
	}
}

// state is everything the watcher has learned about one site. It lives in
// memory: the history file is the durable copy, and it is read once at boot
// so the table and the uptime percentages never touch the disk again.
type state struct {
	machine stateMachine
	history []Record
	// dueAt is the jittered instant the scheduler next looks, zero for a site
	// that has not been armed.
	dueAt int64
	last  Record
	// reasons explain the most recent measurement, most severe first.
	reasons      []string
	tlsExpiresAt int64
	tlsDaysLeft  *int
	// tlsAlerted keeps the certificate warning to one message per crossing.
	tlsAlerted bool
}

// Service owns the site catalog, the scheduler, and the in-memory history.
type Service struct {
	store   Store
	access  Access
	catalog Catalog
	alerter Alerter
	prober  Prober
	now     func() time.Time
	tick    time.Duration

	mu     sync.Mutex
	sites  []Site
	states map[ID]*state

	startOnce sync.Once
	stopOnce  sync.Once
	stopped   chan struct{}
}

// New loads the catalog and every site's history. A store that cannot be read
// degrades to an empty catalog rather than stopping the server: a monitoring
// feature must never be the reason the platform will not boot.
func New(ctx context.Context, deps Dependencies, options ...Option) *Service {
	service := &Service{
		store:   deps.Store,
		access:  deps.Access,
		catalog: deps.Catalog,
		alerter: deps.Alerter,
		now:     time.Now,
		tick:    tickInterval,
		states:  map[ID]*state{},
		stopped: make(chan struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	if service.prober == nil {
		service.prober = newHTTPProber(func() time.Time { return service.now() })
	}
	if deps.Store == nil {
		return service
	}
	sites, err := deps.Store.Load(ctx)
	if err != nil {
		log.Printf("sitewatch: reading the site catalog failed, nothing is watched: %v", err)
		return service
	}
	service.sites = normalizeAll(sites)
	now := service.now()
	for _, site := range service.sites {
		current := &state{}
		if history, historyErr := deps.Store.LoadHistory(ctx, site.ID); historyErr != nil {
			log.Printf("sitewatch: reading history for %s failed: %v", site.Name(), historyErr)
		} else {
			current.history = history
			current.adoptHistory()
		}
		// Boot spreads the whole fleet across one interval. Without it every
		// restarted backend would fire two hundred requests in the same
		// second, which is the shape of an outbound DoS.
		current.dueAt = spreadDue(site, now)
		service.states[site.ID] = current
	}
	return service
}

// adoptHistory seeds the live row from the durable log, so a restart does not
// blank the table or re-announce an outage that is still running.
func (s *state) adoptHistory() {
	if len(s.history) == 0 {
		return
	}
	last := s.history[len(s.history)-1]
	s.last = last
	s.tlsExpiresAt = last.TLSExpiresAt
	if last.Error != "" {
		s.reasons = []string{last.Error}
	}
	// The debouncer is restored from the tail so a site that was already down
	// stays down, and a recovery after a restart still reports the outage it
	// ended. The `since` walk stops at the first record that disagrees.
	s.machine.published = last.Status
	s.machine.since = last.At
	for index := len(s.history) - 2; index >= 0; index-- {
		if s.history[index].Status != last.Status {
			break
		}
		s.machine.since = s.history[index].At
	}
}

// Available reports whether this deployment can watch anything at all.
func (s *Service) Available() bool { return s != nil && s.store != nil }

// Start launches the scheduler. It is idempotent, and the loop exits when ctx
// is cancelled or Stop is called.
func (s *Service) Start(ctx context.Context) {
	if !s.Available() {
		return
	}
	s.startOnce.Do(func() {
		log.Printf("sitewatch: watching %d client site(s)", len(s.sites))
		go s.loop(ctx)
	})
}

// Stop shuts the scheduler down. Safe to call more than once.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopped) })
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopped:
			return
		case <-ticker.C:
			s.Sweep(ctx)
		}
	}
}

// Sweep checks everything that is due. It is exported so a test can drive the
// scheduler with a fake clock instead of waiting for a tick.
func (s *Service) Sweep(ctx context.Context) {
	if !s.Available() {
		return
	}
	due := s.due()
	if len(due) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, sweepTimeout)
	defer cancel()

	workers := probeWorkers
	if len(due) < workers {
		workers = len(due)
	}
	queue := make(chan Site)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for site := range queue {
				s.check(ctx, site)
			}
		}()
	}
	for _, site := range due {
		select {
		case <-ctx.Done():
		case queue <- site:
			continue
		}
		break
	}
	close(queue)
	wait.Wait()
}

// due picks this tick's batch under the lock and returns copies, so the
// probes below run without holding anything.
func (s *Service) due() []Site {
	s.mu.Lock()
	defer s.mu.Unlock()
	dueAt := make(map[ID]int64, len(s.states))
	for id, current := range s.states {
		dueAt[id] = current.dueAt
	}
	return Due(s.sites, dueAt, s.now(), batchLimit)
}

// check runs one site's endpoints, records the result, and raises whatever
// the state change deserves.
func (s *Service) check(ctx context.Context, site Site) {
	results, record := s.measure(ctx, site)
	alerts := s.commit(site, results, record)
	if s.alerter == nil || !site.Notify {
		return
	}
	for _, alert := range alerts {
		s.alerter.SiteStateChanged(ctx, site, alert)
	}
}

// measure probes every URL of one site and folds the outcomes into the row's
// status. It touches no shared state, so it is safe to run concurrently.
func (s *Service) measure(ctx context.Context, site Site) ([]EndpointResult, Record) {
	endpoints := site.endpoints()
	results := make([]EndpointResult, 0, len(endpoints))
	started := s.now()
	for _, endpoint := range endpoints {
		probe := s.prober.Probe(ctx, endpoint, site)
		verdict := Evaluate(endpoint.Checks, probe, s.now())
		result := EndpointResult{
			Label:       endpoint.Label,
			URL:         endpoint.URL,
			Method:      string(probe.Method),
			Status:      verdict.Status,
			Code:        probe.StatusCode,
			DurationMs:  probe.Duration.Milliseconds(),
			SizeBytes:   probe.SizeBytes,
			TLSDaysLeft: verdict.TLSDaysLeft,
			Reasons:     verdict.Reasons,
		}
		if !probe.TLSExpiresAt.IsZero() {
			result.TLSExpiresAt = probe.TLSExpiresAt.UnixMilli()
		}
		if probe.Err != nil {
			result.Error = probe.ErrText
		}
		results = append(results, result)
	}

	status, reasons := Combine(results)
	record := Record{
		At:         started.UnixMilli(),
		Status:     status,
		DurationMs: s.now().Sub(started).Milliseconds(),
	}
	if len(results) > 0 {
		primary := results[0]
		record.Code = primary.Code
		record.SizeBytes = primary.SizeBytes
		record.TLSExpiresAt = primary.TLSExpiresAt
	}
	if len(reasons) > 0 {
		record.Error = truncate(strings.Join(reasons, "; "), 400)
	}
	return results, record
}

// commit folds one measurement into the site's live state and reports the
// messages it earned. The lock is held for the bookkeeping only; the store
// write and the alerts happen outside it.
func (s *Service) commit(site Site, results []EndpointResult, record Record) []Alert {
	s.mu.Lock()
	current, ok := s.states[site.ID]
	if !ok {
		// The site was deleted while its check was in flight. Recording it
		// would resurrect a row nothing owns.
		s.mu.Unlock()
		return nil
	}
	published, transition := current.machine.Observe(record.Status, record.At)
	current.last = record
	current.reasons = collectReasons(results)
	current.tlsExpiresAt, current.tlsDaysLeft = tlsFrom(results)
	current.history = append(current.history, record)
	if len(current.history) > MaxHistoryRecords {
		current.history = current.history[len(current.history)-MaxHistoryRecords:]
	}
	current.dueAt = nextDue(site, s.now())

	view := s.viewLocked(site, current)
	view.Status = published
	alerts := alertsFor(site, view, transition, results, current)
	s.mu.Unlock()

	if s.store != nil {
		// Deliberately not the caller's context: a "Check now" whose HTTP
		// request was abandoned still produced a measurement, and losing it
		// would put a hole in the uptime history.
		if err := s.store.AppendHistory(context.Background(), site.ID, record); err != nil {
			log.Printf("sitewatch: recording a check for %s failed: %v", site.Name(), err)
		}
	}
	return alerts
}

// alertsFor turns a settled transition (and the certificate reading beside
// it) into outbound messages. The caller holds the lock, because the TLS
// latch it flips lives on the state.
func alertsFor(site Site, view View, transition Transition, results []EndpointResult, current *state) []Alert {
	alerts := make([]Alert, 0, 2)
	if transition.Alert {
		switch transition.To {
		case StatusDown:
			alerts = append(alerts, Alert{
				Kind:      AlertDown,
				Status:    StatusDown,
				Summary:   DownSummary(site, view),
				At:        view.LastCheckedAt,
				DedupeKey: dedupeKey(site.ID, "down", view.ChangedAt),
			})
		case StatusSlow:
			alerts = append(alerts, Alert{
				Kind:      AlertSlow,
				Status:    StatusSlow,
				Summary:   SlowSummary(site, view),
				At:        view.LastCheckedAt,
				DedupeKey: dedupeKey(site.ID, "slow", view.ChangedAt),
			})
		case StatusUp:
			alerts = append(alerts, Alert{
				Kind:      AlertRecovered,
				Status:    StatusUp,
				Summary:   RecoveredSummary(site, transition.Duration),
				At:        view.LastCheckedAt,
				DedupeKey: dedupeKey(site.ID, "recovered", view.ChangedAt),
			})
		}
	}

	// The certificate warning is its own axis: a site can be perfectly up and
	// three days from a browser-wide TLS error. One message per crossing, and
	// the latch clears once the certificate is renewed.
	expiring := false
	for _, result := range results {
		if result.TLSDaysLeft == nil {
			continue
		}
		if warnDays := endpointWarnDays(site, result.URL); warnDays > 0 && *result.TLSDaysLeft <= warnDays {
			expiring = true
		}
	}
	switch {
	case expiring && !current.tlsAlerted:
		current.tlsAlerted = true
		days := 0
		if current.tlsDaysLeft != nil {
			days = *current.tlsDaysLeft
		}
		alerts = append(alerts, Alert{
			Kind:      AlertTLS,
			Status:    view.Status,
			Summary:   TLSSummary(site, days),
			At:        view.LastCheckedAt,
			DedupeKey: dedupeKey(site.ID, "tls", int64(days)),
		})
	case !expiring:
		current.tlsAlerted = false
	}
	return alerts
}

// endpointWarnDays finds the certificate rule that applies to one URL of a
// site, so a stricter rule on the checkout page is honoured.
func endpointWarnDays(site Site, endpointURL string) int {
	for _, extra := range site.ExtraURLs {
		if extra.URL == endpointURL {
			return extra.Checks.TLS.WarnDays
		}
	}
	return site.Checks.TLS.WarnDays
}

func dedupeKey(id ID, kind string, at int64) string {
	return "sitewatch:" + string(id) + ":" + kind + ":" + strconv.FormatInt(at, 10)
}

func collectReasons(results []EndpointResult) []string {
	_, reasons := Combine(results)
	if len(reasons) == 0 {
		return nil
	}
	return reasons
}

// tlsFrom reports the soonest certificate expiry across a site's endpoints,
// which is the one that will break it first.
func tlsFrom(results []EndpointResult) (int64, *int) {
	var expiresAt int64
	var daysLeft *int
	for _, result := range results {
		if result.TLSExpiresAt == 0 {
			continue
		}
		if expiresAt == 0 || result.TLSExpiresAt < expiresAt {
			expiresAt = result.TLSExpiresAt
			daysLeft = result.TLSDaysLeft
		}
	}
	return expiresAt, daysLeft
}

// endpoints is the site's primary URL followed by its extras, as one list the
// prober can walk.
func (s Site) endpoints() []Endpoint {
	out := make([]Endpoint, 0, 1+len(s.ExtraURLs))
	out = append(out, Endpoint{Label: "", URL: s.URL, Checks: s.Checks})
	out = append(out, s.ExtraURLs...)
	return out
}

// spreadDue arms a site somewhere inside its first interval, which is what
// keeps a restart from checking the whole fleet at once.
func spreadDue(site Site, now time.Time) int64 {
	interval := site.Interval()
	return now.Add(time.Duration(rand.Int64N(int64(interval) + 1))).UnixMilli()
}

// soonDue arms a freshly added site within the next few seconds, so the
// operator sees a verdict without waiting out an interval.
func soonDue(now time.Time) int64 {
	return now.Add(time.Duration(rand.Int64N(int64(10 * time.Second)))).UnixMilli()
}

// nextDue schedules the check after this one, jittered by a tenth of the
// interval either side of its nominal time.
func nextDue(site Site, now time.Time) int64 {
	interval := site.Interval()
	spread := interval / 5
	if spread <= 0 {
		return now.Add(interval).UnixMilli()
	}
	return now.Add(interval - spread/2 + time.Duration(rand.Int64N(int64(spread)+1))).UnixMilli()
}

// List returns every site the caller may see, ordered by label. Admins see
// everything; a member sees only sites linked to a project they belong to,
// and an unlinked site is admin-only.
func (s *Service) List(ctx context.Context, callerEmail string, isAdmin bool) ([]View, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}
	sites := s.snapshotSites()
	visible := make([]Site, 0, len(sites))
	for _, site := range sites {
		ok, err := s.visible(ctx, site, callerEmail, isAdmin)
		if err != nil {
			return nil, err
		}
		if ok {
			visible = append(visible, site)
		}
	}
	return s.views(visible), nil
}

// NotGreen is the home dashboard's read: the visible sites that are not
// currently up. It is a filter over List so the two can never disagree about
// what "not green" means.
func (s *Service) NotGreen(ctx context.Context, callerEmail string, isAdmin bool) ([]View, error) {
	all, err := s.List(ctx, callerEmail, isAdmin)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(all))
	for _, view := range all {
		if view.Enabled && !view.Status.Green() && view.Status != StatusUnknown {
			out = append(out, view)
		}
	}
	return out, nil
}

// Get returns one site the caller may see.
func (s *Service) Get(ctx context.Context, id ID, callerEmail string, isAdmin bool) (View, error) {
	site, err := s.authorize(ctx, id, callerEmail, isAdmin)
	if err != nil {
		return View{}, err
	}
	views := s.views([]Site{site})
	return views[0], nil
}

// History returns one site's raw checks, oldest first.
func (s *Service) History(ctx context.Context, id ID, callerEmail string, isAdmin bool) ([]Record, error) {
	if _, err := s.authorize(ctx, id, callerEmail, isAdmin); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.states[id]
	if !ok {
		return []Record{}, nil
	}
	return append([]Record(nil), current.history...), nil
}

// CheckNow runs one site's checks synchronously and reports the raw per-URL
// results. It goes through the same measure/commit path as the scheduler, so
// pressing the button can move the row and raise an alert exactly as a timed
// check would.
func (s *Service) CheckNow(ctx context.Context, id ID, callerEmail string, isAdmin bool) (Report, error) {
	site, err := s.authorize(ctx, id, callerEmail, isAdmin)
	if err != nil {
		return Report{}, err
	}
	results, record := s.measure(ctx, site)
	alerts := s.commit(site, results, record)
	if s.alerter != nil && site.Notify {
		for _, alert := range alerts {
			s.alerter.SiteStateChanged(ctx, site, alert)
		}
	}
	views := s.views([]Site{site})
	return Report{Site: views[0], Endpoints: results, CheckedAt: record.At}, nil
}

// Create adds a site. Admin-only; the handler enforces that.
func (s *Service) Create(ctx context.Context, input Input) (View, error) {
	if !s.Available() {
		return View{}, ErrUnavailable
	}
	site := input.site(Site{})
	normalized, ok := NormalizeURL(site.URL)
	if !ok {
		return View{}, fmt.Errorf("%w: the site URL must be an absolute http(s) URL with a hostname", ErrInvalidSite)
	}
	site.URL = normalized
	if err := validate(site); err != nil {
		return View{}, err
	}
	now := s.now()
	site.ID = newID()
	site.CreatedAt = now.UnixMilli()
	site.UpdatedAt = site.CreatedAt

	s.mu.Lock()
	if len(s.sites) >= MaxSites {
		s.mu.Unlock()
		return View{}, fmt.Errorf("%w: this server watches at most %d client sites", ErrTooManySites, MaxSites)
	}
	for _, existing := range s.sites {
		if SameTarget(existing.URL, site.URL) {
			s.mu.Unlock()
			return View{}, fmt.Errorf("%w: %s is already watched", ErrInvalidSite, site.Host())
		}
	}
	s.sites = append(s.sites, site)
	s.states[site.ID] = &state{dueAt: soonDue(now)}
	sites := append([]Site(nil), s.sites...)
	s.mu.Unlock()

	if err := s.store.Save(ctx, sites); err != nil {
		s.forget(site.ID)
		return View{}, fmt.Errorf("save client sites: %w", err)
	}
	views := s.views([]Site{site})
	return views[0], nil
}

// Update replaces one site's configuration, keeping its history.
func (s *Service) Update(ctx context.Context, id ID, input Input) (View, error) {
	if !s.Available() {
		return View{}, ErrUnavailable
	}
	s.mu.Lock()
	index := -1
	for position, existing := range s.sites {
		if existing.ID == id {
			index = position
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return View{}, ErrNotFound
	}
	next := input.site(s.sites[index])
	normalized, ok := NormalizeURL(next.URL)
	if !ok {
		s.mu.Unlock()
		return View{}, fmt.Errorf("%w: the site URL must be an absolute http(s) URL with a hostname", ErrInvalidSite)
	}
	next.URL = normalized
	if err := validate(next); err != nil {
		s.mu.Unlock()
		return View{}, err
	}
	for position, existing := range s.sites {
		if position != index && SameTarget(existing.URL, next.URL) {
			s.mu.Unlock()
			return View{}, fmt.Errorf("%w: %s is already watched", ErrInvalidSite, next.Host())
		}
	}
	previous := s.sites[index]
	next.UpdatedAt = s.now().UnixMilli()
	s.sites[index] = next
	if current, tracked := s.states[id]; tracked {
		// A retargeted or re-enabled site is scheduled again promptly: the
		// operator who just changed it is looking at the row.
		if previous.URL != next.URL || (!previous.Enabled && next.Enabled) {
			current.dueAt = soonDue(s.now())
		}
	} else {
		s.states[id] = &state{dueAt: soonDue(s.now())}
	}
	sites := append([]Site(nil), s.sites...)
	s.mu.Unlock()

	if err := s.store.Save(ctx, sites); err != nil {
		return View{}, fmt.Errorf("save client sites: %w", err)
	}
	views := s.views([]Site{next})
	return views[0], nil
}

// Delete removes a site and its history.
func (s *Service) Delete(ctx context.Context, id ID) error {
	if !s.Available() {
		return ErrUnavailable
	}
	s.mu.Lock()
	index := -1
	for position, existing := range s.sites {
		if existing.ID == id {
			index = position
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return ErrNotFound
	}
	s.sites = append(s.sites[:index], s.sites[index+1:]...)
	delete(s.states, id)
	sites := append([]Site(nil), s.sites...)
	s.mu.Unlock()

	if err := s.store.Save(ctx, sites); err != nil {
		return fmt.Errorf("save client sites: %w", err)
	}
	if err := s.store.DeleteHistory(ctx, id); err != nil {
		log.Printf("sitewatch: deleting history for %s failed: %v", id, err)
	}
	return nil
}

// Import adds many sites at once, from a pasted list and optionally from the
// domains the projects' own secrets already hold. Everything it creates gets
// the defaults, so a forty-line paste is one action rather than forty forms.
func (s *Service) Import(ctx context.Context, input ImportInput) (ImportResult, error) {
	if !s.Available() {
		return ImportResult{}, ErrUnavailable
	}
	result := ImportResult{Created: []View{}}
	type candidate struct {
		url       string
		label     string
		projectID string
	}
	candidates := make([]candidate, 0, 16)
	for _, raw := range ParseURLList(input.URLs) {
		candidates = append(candidates, candidate{url: raw, projectID: strings.TrimSpace(input.ProjectID)})
	}
	if input.FromProjects && s.catalog != nil {
		discovered, err := s.catalog.Candidates(ctx)
		if err != nil {
			return ImportResult{}, fmt.Errorf("read project domains: %w", err)
		}
		for _, item := range discovered {
			normalized, ok := NormalizeURL(item.Domain)
			if !ok {
				result.Skipped = append(result.Skipped, ImportSkipped{
					URL:    item.Domain,
					Reason: item.SecretKey + " is not a usable URL",
				})
				continue
			}
			candidates = append(candidates, candidate{
				url:       normalized,
				label:     item.ProjectName,
				projectID: item.ProjectID,
			})
		}
	}

	for _, item := range candidates {
		view, err := s.Create(ctx, Input{
			Label:           item.label,
			URL:             item.url,
			Enabled:         true,
			IntervalMinutes: DefaultIntervalMinutes,
			Checks:          DefaultChecks(),
			ProjectID:       item.projectID,
			Notify:          input.Notify,
			Method:          MethodHEAD,
		})
		switch {
		case errors.Is(err, ErrTooManySites):
			result.Skipped = append(result.Skipped, ImportSkipped{
				URL:    item.url,
				Reason: fmt.Sprintf("the %d-site limit is already reached", MaxSites),
			})
			return result, nil
		case errors.Is(err, ErrInvalidSite):
			result.Skipped = append(result.Skipped, ImportSkipped{URL: item.url, Reason: reasonText(err)})
		case err != nil:
			return result, err
		default:
			result.Created = append(result.Created, view)
		}
	}
	return result, nil
}

// reasonText strips the sentinel prefix so the skipped list reads as a
// sentence rather than as an error chain.
func reasonText(err error) string {
	text := err.Error()
	for _, prefix := range []string{ErrInvalidSite.Error() + ": ", ErrTooManySites.Error() + ": "} {
		text = strings.TrimPrefix(text, prefix)
	}
	return text
}

// Candidates lists what a project-derived import would offer, so the panel can
// show the operator the list before creating anything.
func (s *Service) Candidates(ctx context.Context) ([]Candidate, error) {
	if s == nil || s.catalog == nil {
		return []Candidate{}, nil
	}
	found, err := s.catalog.Candidates(ctx)
	if err != nil {
		return nil, err
	}
	watched := s.snapshotSites()
	out := make([]Candidate, 0, len(found))
	for _, item := range found {
		normalized, ok := NormalizeURL(item.Domain)
		if !ok {
			continue
		}
		duplicate := false
		for _, site := range watched {
			if SameTarget(site.URL, normalized) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		item.Domain = normalized
		out = append(out, item)
	}
	return out, nil
}

// authorize resolves a site the caller may act on, reporting "not found" for
// one they may not: a member must not be able to probe which ids exist.
func (s *Service) authorize(ctx context.Context, id ID, callerEmail string, isAdmin bool) (Site, error) {
	if !s.Available() {
		return Site{}, ErrUnavailable
	}
	if !ValidID(id) {
		return Site{}, ErrNotFound
	}
	s.mu.Lock()
	var site Site
	found := false
	for _, existing := range s.sites {
		if existing.ID == id {
			site, found = existing, true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		return Site{}, ErrNotFound
	}
	ok, err := s.visible(ctx, site, callerEmail, isAdmin)
	if err != nil {
		return Site{}, err
	}
	if !ok {
		return Site{}, ErrNotFound
	}
	return site, nil
}

// visible is the whole access rule: admins see every site, and everybody else
// sees a site only through a project they are a member of.
func (s *Service) visible(ctx context.Context, site Site, callerEmail string, isAdmin bool) (bool, error) {
	if isAdmin {
		return true, nil
	}
	if site.ProjectID == "" || s.access == nil {
		return false, nil
	}
	email := strings.ToLower(strings.TrimSpace(callerEmail))
	if email == "" {
		return false, nil
	}
	return s.access.HasProjectAccess(ctx, site.ProjectID, email)
}

func (s *Service) snapshotSites() []Site {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Site(nil), s.sites...)
}

// views renders rows for the given sites, newest measurement included.
func (s *Service) views(sites []Site) []View {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]View, 0, len(sites))
	for _, site := range sites {
		out = append(out, s.viewLocked(site, s.states[site.ID]))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name()) < strings.ToLower(out[j].Name())
	})
	return out
}

// viewLocked renders one row. The caller must hold the mutex.
func (s *Service) viewLocked(site Site, current *state) View {
	view := View{Site: site, Status: StatusUnknown}
	if current == nil {
		return view
	}
	view.Status = current.machine.status()
	view.ChangedAt = current.machine.since
	view.LastCheckedAt = current.last.At
	view.LastDurationMs = current.last.DurationMs
	view.LastCode = current.last.Code
	view.LastSizeBytes = current.last.SizeBytes
	view.NextCheckAt = current.dueAt
	view.TLSExpiresAt = current.tlsExpiresAt
	view.TLSDaysLeft = current.tlsDaysLeft
	view.Uptime = ComputeUptime(current.history, s.now())
	view.Spark = Spark(current.history, SparkPoints)
	if len(current.reasons) > 0 {
		view.LastError = truncate(strings.Join(current.reasons, "; "), 400)
	}
	return view
}

// forget drops a site that failed to persist, so an in-memory row never
// outlives the write that was supposed to create it.
func (s *Service) forget(id ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for position, existing := range s.sites {
		if existing.ID == id {
			s.sites = append(s.sites[:position], s.sites[position+1:]...)
			break
		}
	}
	delete(s.states, id)
}

func validate(site Site) error {
	if strings.TrimSpace(site.URL) == "" {
		return fmt.Errorf("%w: a URL is required", ErrInvalidSite)
	}
	if site.IntervalMinutes < MinIntervalMinutes || site.IntervalMinutes > MaxIntervalMinutes {
		return fmt.Errorf(
			"%w: the check interval must be between %d and %d minutes",
			ErrInvalidSite, MinIntervalMinutes, MaxIntervalMinutes,
		)
	}
	if len(site.ExtraURLs) > MaxExtraURLs {
		return fmt.Errorf("%w: a site may carry at most %d extra URLs", ErrInvalidSite, MaxExtraURLs)
	}
	for index, extra := range site.ExtraURLs {
		normalized, ok := NormalizeURL(extra.URL)
		if !ok {
			return fmt.Errorf("%w: extra URL %d is not an absolute http(s) URL", ErrInvalidSite, index+1)
		}
		site.ExtraURLs[index].URL = normalized
	}
	return nil
}

func normalizeAll(sites []Site) []Site {
	out := make([]Site, 0, len(sites))
	seen := map[ID]struct{}{}
	for _, site := range sites {
		site = site.Normalize()
		if !ValidID(site.ID) {
			continue
		}
		if _, dupe := seen[site.ID]; dupe {
			continue
		}
		if _, ok := NormalizeURL(site.URL); !ok {
			continue
		}
		seen[site.ID] = struct{}{}
		out = append(out, site)
		if len(out) >= MaxSites {
			break
		}
	}
	return out
}

func newID() ID {
	var bytes [12]byte
	if _, err := crand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate site id: %v", err))
	}
	return ID(hex.EncodeToString(bytes[:]))
}
