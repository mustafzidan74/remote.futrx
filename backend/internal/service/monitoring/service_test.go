package monitoring

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- fakes -----------------------------------------------------------------

type fakeStore struct {
	mu       sync.Mutex
	config   Config
	loadErr  error
	saveErr  error
	probeErr error
	saves    []Config
	probes   int
}

func (s *fakeStore) Load(context.Context) (Config, error) {
	if s.loadErr != nil {
		return Config{}, s.loadErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config, nil
}

func (s *fakeStore) Save(_ context.Context, cfg Config) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
	s.saves = append(s.saves, cfg)
	return nil
}

func (s *fakeStore) Probe(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probes++
	return s.probeErr
}

func (s *fakeStore) lastSave() (Config, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.saves) == 0 {
		return Config{}, false
	}
	return s.saves[len(s.saves)-1], true
}

type fakeLXD struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (l *fakeLXD) Run(context.Context, ...string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return "", l.err
}

func (l *fakeLXD) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

type fakeEdge struct {
	calls int
	err   error
}

func (e *fakeEdge) Probe(context.Context) error {
	e.calls++
	return e.err
}

type fakeAnnouncer struct {
	versions []string
}

func (a *fakeAnnouncer) PlatformStarted(_ context.Context, version string) {
	a.versions = append(a.versions, version)
}

// fakeClock is a hand-wound clock the ticker and the probe cache both read.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// --- health report ---------------------------------------------------------

func TestReportRollsUpOnlyTheChecksThatStopThePlatformServing(t *testing.T) {
	cases := []struct {
		name        string
		storeErr    error
		omitStore   bool
		lxdErr      error
		omitLXD     bool
		edgeErr     error
		wantStatus  Status
		wantChecks  Checks
		wantDetails []string
	}{
		{
			name:       "everything answers",
			wantStatus: StatusOK,
			wantChecks: Checks{Backend: StatusOK, LXD: StatusOK, Caddy: StatusOK},
		},
		{
			name:        "an unwritable data store is fatal",
			storeErr:    errors.New("read-only file system at /opt/remote.futrx/data"),
			wantStatus:  StatusDegraded,
			wantChecks:  Checks{Backend: StatusDegraded, LXD: StatusOK, Caddy: StatusOK},
			wantDetails: []string{DetailStore},
		},
		{
			name:        "a silent LXD daemon is fatal",
			lxdErr:      errors.New("exec: lxc: executable file not found in $PATH"),
			wantStatus:  StatusDegraded,
			wantChecks:  Checks{Backend: StatusOK, LXD: StatusDegraded, Caddy: StatusOK},
			wantDetails: []string{DetailLXD},
		},
		{
			name:        "a closed edge port is reported but not fatal",
			edgeErr:     errors.New("connection refused"),
			wantStatus:  StatusOK,
			wantChecks:  Checks{Backend: StatusOK, LXD: StatusOK, Caddy: StatusDegraded},
			wantDetails: []string{DetailCaddy},
		},
		{
			name:       "a deployment without LXD skips that check",
			omitLXD:    true,
			wantStatus: StatusOK,
			wantChecks: Checks{Backend: StatusOK, LXD: StatusSkipped, Caddy: StatusOK},
		},
		{
			name:       "a deployment without a store skips that check too",
			omitStore:  true,
			wantStatus: StatusOK,
			wantChecks: Checks{Backend: StatusSkipped, LXD: StatusOK, Caddy: StatusOK},
		},
		{
			name:        "both halves down reports both",
			storeErr:    errors.New("no space left on device"),
			lxdErr:      errors.New("lxd is not running"),
			wantStatus:  StatusDegraded,
			wantChecks:  Checks{Backend: StatusDegraded, LXD: StatusDegraded, Caddy: StatusOK},
			wantDetails: []string{DetailStore, DetailLXD},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			deps := Dependencies{Version: "v1.2.3"}
			if !testCase.omitStore {
				deps.Store = &fakeStore{config: DefaultConfig(), probeErr: testCase.storeErr}
			}
			if !testCase.omitLXD {
				deps.LXD = &fakeLXD{err: testCase.lxdErr}
			}
			service := New(context.Background(), deps, WithEdgeProbe(&fakeEdge{err: testCase.edgeErr}))

			report := service.Report(context.Background(), false)

			if report.Status != testCase.wantStatus {
				t.Fatalf("status = %q, want %q", report.Status, testCase.wantStatus)
			}
			if report.Checks != testCase.wantChecks {
				t.Fatalf("checks = %+v, want %+v", report.Checks, testCase.wantChecks)
			}
			if report.Version != "v1.2.3" {
				t.Fatalf("version = %q, want v1.2.3", report.Version)
			}
			if len(report.Details) != len(testCase.wantDetails) {
				t.Fatalf("details = %q, want %q", report.Details, testCase.wantDetails)
			}
			for i, want := range testCase.wantDetails {
				if report.Details[i] != want {
					t.Fatalf("detail %d = %q, want %q", i, report.Details[i], want)
				}
			}
			if report.Healthy() != (testCase.wantStatus == StatusOK) {
				t.Fatalf("Healthy() disagrees with status %q", report.Status)
			}
		})
	}
}

func TestReportDetailsNeverCarryAProbesOwnErrorText(t *testing.T) {
	const leak = "/opt/remote.futrx/data: read-only file system"
	store := &fakeStore{config: DefaultConfig(), probeErr: errors.New(leak)}
	service := New(context.Background(), Dependencies{
		Store:   store,
		LXD:     &fakeLXD{err: errors.New("dial unix /var/snap/lxd/common/lxd/unix.socket")},
		Version: "v1.2.3",
	}, WithEdgeProbe(&fakeEdge{}))

	report := service.Report(context.Background(), false)

	for _, detail := range report.Details {
		if strings.Contains(detail, "/opt/") || strings.Contains(detail, "/var/") {
			t.Fatalf("detail %q leaked a host path", detail)
		}
	}
}

func TestReportCachesTheLXDProbeForATTL(t *testing.T) {
	clock := newClock()
	lxd := &fakeLXD{}
	service := New(context.Background(), Dependencies{
		Store: &fakeStore{config: DefaultConfig()}, LXD: lxd,
	}, WithClock(clock.Now), WithEdgeProbe(&fakeEdge{}), WithProbeTTL(time.Minute))

	for i := 0; i < 20; i++ {
		service.Report(context.Background(), false)
	}
	if lxd.count() != 1 {
		t.Fatalf("twenty hits inside the TTL made %d lxc calls, want 1", lxd.count())
	}

	clock.advance(59 * time.Second)
	service.Report(context.Background(), false)
	if lxd.count() != 1 {
		t.Fatalf("a hit before the TTL expired made %d lxc calls, want 1", lxd.count())
	}

	clock.advance(2 * time.Second)
	service.Report(context.Background(), false)
	if lxd.count() != 2 {
		t.Fatalf("a hit after the TTL expired made %d lxc calls, want 2", lxd.count())
	}
}

func TestReportTrustsAProxiedRequestInsteadOfDialingTheEdge(t *testing.T) {
	edge := &fakeEdge{err: errors.New("connection refused")}
	service := New(context.Background(), Dependencies{
		Store: &fakeStore{config: DefaultConfig()}, LXD: &fakeLXD{},
	}, WithEdgeProbe(edge))

	report := service.Report(context.Background(), true)

	if report.Checks.Caddy != StatusOK {
		t.Fatalf("caddy check = %q, want ok for a proxied request", report.Checks.Caddy)
	}
	if edge.calls != 0 {
		t.Fatalf("edge probe ran %d times for a proxied request, want 0", edge.calls)
	}
}

// --- heartbeat -------------------------------------------------------------

// heartbeatServer counts pushes and answers with the given status.
func heartbeatServer(t *testing.T, status int) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func healthyService(t *testing.T, store *fakeStore, clock *fakeClock) *Service {
	t.Helper()
	return New(context.Background(), Dependencies{
		Store: store, LXD: &fakeLXD{}, Version: "v1.2.3",
	}, WithClock(clock.Now), WithEdgeProbe(&fakeEdge{}))
}

func TestHeartbeatTickPushesOncePerIntervalWhileHealthy(t *testing.T) {
	server, hits := heartbeatServer(t, http.StatusOK)
	clock := newClock()
	store := &fakeStore{config: Config{
		Enabled: true, HeartbeatURL: server.URL + "/ping/token1234", IntervalMinutes: 5,
	}}
	service := healthyService(t, store, clock)

	service.heartbeatTick(context.Background())
	if *hits != 1 {
		t.Fatalf("first tick pushed %d times, want 1", *hits)
	}
	saved, ok := store.lastSave()
	if !ok || saved.LastPingStatus != PingOK || saved.LastPingAt != clock.Now().UnixMilli() {
		t.Fatalf("first push was not recorded: %+v", saved)
	}

	// Inside the interval nothing happens, however often the loop wakes up.
	for i := 0; i < 8; i++ {
		clock.advance(30 * time.Second)
		if clock.Now().Sub(time.UnixMilli(saved.LastPingAt)) >= 5*time.Minute {
			break
		}
		service.heartbeatTick(context.Background())
	}
	if *hits != 1 {
		t.Fatalf("ticks inside the interval pushed %d times, want 1", *hits)
	}

	clock.advance(5 * time.Minute)
	service.heartbeatTick(context.Background())
	if *hits != 2 {
		t.Fatalf("a tick after the interval pushed %d times, want 2", *hits)
	}
}

func TestHeartbeatTickStaysSilentWhenTheHeartbeatIsOffOrUnconfigured(t *testing.T) {
	server, hits := heartbeatServer(t, http.StatusOK)
	cases := []struct {
		name   string
		config Config
	}{
		{name: "disabled", config: Config{HeartbeatURL: server.URL + "/ping", IntervalMinutes: 5}},
		{name: "no URL", config: Config{Enabled: true, IntervalMinutes: 5}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			before := *hits
			service := healthyService(t, &fakeStore{config: testCase.config}, newClock())
			service.heartbeatTick(context.Background())
			if *hits != before {
				t.Fatalf("pushed %d times, want %d", *hits, before)
			}
		})
	}
}

func TestHeartbeatTickStaysSilentWhileThePlatformIsUnhealthy(t *testing.T) {
	server, hits := heartbeatServer(t, http.StatusOK)
	clock := newClock()
	store := &fakeStore{config: Config{
		Enabled: true, HeartbeatURL: server.URL + "/ping/token1234", IntervalMinutes: 5,
	}}
	// A silent LXD daemon is exactly the failure an operator wants the
	// external monitor to notice, so the heartbeat must not vouch for it.
	service := New(context.Background(), Dependencies{
		Store: store, LXD: &fakeLXD{err: errors.New("lxd is not running")},
	}, WithClock(clock.Now), WithEdgeProbe(&fakeEdge{}))

	service.heartbeatTick(context.Background())

	if *hits != 0 {
		t.Fatalf("pushed %d times while degraded, want 0", *hits)
	}
}

func TestAFailedHeartbeatIsRecordedAndNotRetriedUntilTheNextInterval(t *testing.T) {
	server, hits := heartbeatServer(t, http.StatusInternalServerError)
	clock := newClock()
	store := &fakeStore{config: Config{
		Enabled: true, HeartbeatURL: server.URL + "/ping/token1234", IntervalMinutes: 5,
	}}
	service := healthyService(t, store, clock)

	service.heartbeatTick(context.Background())
	if *hits != 1 {
		t.Fatalf("first tick pushed %d times, want 1", *hits)
	}
	saved, _ := store.lastSave()
	if saved.LastPingStatus != PingFailed {
		t.Fatalf("last ping status = %q, want %q", saved.LastPingStatus, PingFailed)
	}
	if !strings.Contains(saved.LastPingError, "500") {
		t.Fatalf("last ping error = %q, want it to name the status code", saved.LastPingError)
	}

	// The retry budget is the interval itself: a broken URL must not spin.
	clock.advance(time.Second)
	service.heartbeatTick(context.Background())
	service.heartbeatTick(context.Background())
	if *hits != 1 {
		t.Fatalf("a failure was retried in a tight loop: %d pushes, want 1", *hits)
	}

	clock.advance(5 * time.Minute)
	service.heartbeatTick(context.Background())
	if *hits != 2 {
		t.Fatalf("the next interval did not retry: %d pushes, want 2", *hits)
	}
}

func TestPingPushesImmediatelyAndNeverLeaksTheURL(t *testing.T) {
	server, hits := heartbeatServer(t, http.StatusBadGateway)
	clock := newClock()
	endpoint := server.URL + "/ping/9f3a1c72-secret1234"
	store := &fakeStore{config: Config{Enabled: true, HeartbeatURL: endpoint, IntervalMinutes: 60}}
	service := healthyService(t, store, clock)

	result, err := service.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping returned %v", err)
	}
	if *hits != 1 {
		t.Fatalf("Ping pushed %d times, want 1", *hits)
	}
	if result.Delivered || result.Status != PingFailed {
		t.Fatalf("result = %+v, want a recorded failure", result)
	}
	if strings.Contains(result.Error, endpoint) || strings.Contains(result.Error, "secret1234") {
		t.Fatalf("ping error leaked the heartbeat URL: %q", result.Error)
	}
}

func TestPingWithoutAStoredURLIsARequestError(t *testing.T) {
	service := healthyService(t, &fakeStore{config: DefaultConfig()}, newClock())

	if _, err := service.Ping(context.Background()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

// --- settings --------------------------------------------------------------

func TestSaveRejectsWhatTheOperatorCanFix(t *testing.T) {
	cases := []struct {
		name    string
		input   UpdateInput
		wantErr bool
	}{
		{
			name:  "a plain healthchecks URL",
			input: UpdateInput{Enabled: true, HeartbeatURL: "https://hc-ping.com/token1234", IntervalMinutes: 5},
		},
		{
			name:  "an omitted interval takes the default",
			input: UpdateInput{HeartbeatURL: "https://hc-ping.com/token1234"},
		},
		{
			name:    "a relative URL",
			input:   UpdateInput{HeartbeatURL: "/ping", IntervalMinutes: 5},
			wantErr: true,
		},
		{
			name:    "a non-http scheme",
			input:   UpdateInput{HeartbeatURL: "ftp://hc-ping.com/token", IntervalMinutes: 5},
			wantErr: true,
		},
		{
			name:    "enabled with nothing to push to",
			input:   UpdateInput{Enabled: true, IntervalMinutes: 5},
			wantErr: true,
		},
		{
			name:    "an interval below the floor",
			input:   UpdateInput{HeartbeatURL: "https://hc-ping.com/token1234", IntervalMinutes: 0 - 1},
			wantErr: true,
		},
		{
			name:    "an interval above the ceiling",
			input:   UpdateInput{HeartbeatURL: "https://hc-ping.com/token1234", IntervalMinutes: 61},
			wantErr: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := healthyService(t, &fakeStore{config: DefaultConfig()}, newClock())
			_, err := service.Save(context.Background(), testCase.input)
			if testCase.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("err = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Save returned %v", err)
			}
		})
	}
}

func TestSavePersistsAndArmsTheNewSchedule(t *testing.T) {
	clock := newClock()
	store := &fakeStore{config: DefaultConfig()}
	service := healthyService(t, store, clock)

	public, err := service.Save(context.Background(), UpdateInput{
		Enabled:         true,
		HeartbeatURL:    "https://hc-ping.com/token1234",
		IntervalMinutes: 2,
	})
	if err != nil {
		t.Fatalf("Save returned %v", err)
	}
	if public.HeartbeatURLMasked != "hc-ping.com/••••1234" {
		t.Fatalf("response leaked or mangled the URL: %q", public.HeartbeatURLMasked)
	}
	if public.UpdatedAt != clock.Now().UnixMilli() {
		t.Fatalf("updatedAt = %d, want the clock", public.UpdatedAt)
	}
	saved, ok := store.lastSave()
	if !ok || saved.HeartbeatURL != "https://hc-ping.com/token1234" || saved.IntervalMinutes != 2 {
		t.Fatalf("stored document = %+v", saved)
	}
	if live := service.Config(); live.IntervalMinutes != 2 || !live.Active() {
		t.Fatalf("live configuration was not armed: %+v", live)
	}
}

func TestNewDegradesToDefaultsWhenTheStoreCannotBeRead(t *testing.T) {
	service := New(context.Background(), Dependencies{
		Store: &fakeStore{loadErr: errors.New("permission denied")},
	})

	if got := service.Config(); got.Enabled || got.IntervalMinutes != DefaultIntervalMinutes {
		t.Fatalf("config = %+v, want the defaults", got)
	}
}

// --- boot announcement -----------------------------------------------------

func TestStartAnnouncesTheRestartExactlyOnce(t *testing.T) {
	announcer := &fakeAnnouncer{}
	service := New(context.Background(), Dependencies{
		Store: &fakeStore{config: DefaultConfig()}, Announcer: announcer, Version: "v1.4.0",
	}, WithEdgeProbe(&fakeEdge{}))
	t.Cleanup(service.Stop)

	service.Start(context.Background())
	service.Start(context.Background())

	if len(announcer.versions) != 1 {
		t.Fatalf("announced %d times, want 1", len(announcer.versions))
	}
	if announcer.versions[0] != "v1.4.0" {
		t.Fatalf("announced version %q, want v1.4.0", announcer.versions[0])
	}
}

func TestStartAnnouncesAnUnstampedBuildAsUnknown(t *testing.T) {
	announcer := &fakeAnnouncer{}
	service := New(context.Background(), Dependencies{
		Store: &fakeStore{config: DefaultConfig()}, Announcer: announcer,
	}, WithEdgeProbe(&fakeEdge{}))
	t.Cleanup(service.Stop)

	service.Start(context.Background())

	if len(announcer.versions) != 1 || announcer.versions[0] != "unknown" {
		t.Fatalf("announced %q, want [unknown]", announcer.versions)
	}
}
