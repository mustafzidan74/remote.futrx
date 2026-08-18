package monitoring

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrInvalidConfig is returned for configurations the operator can fix.
var ErrInvalidConfig = errors.New("invalid monitoring settings")

const (
	// lxdProbeTimeout bounds one `lxc info`. An unauthenticated request must
	// never wait on a wedged daemon, and two seconds is already generous for
	// a query the daemon answers from memory.
	lxdProbeTimeout = 2 * time.Second
	// edgeProbeTimeout bounds one TCP dial at the public HTTPS port.
	edgeProbeTimeout = 2 * time.Second
	// probeTTL is how long a probe result stands in for the next one. It is
	// what keeps /healthz cheap enough to expose without a session.
	probeTTL = time.Minute
	// heartbeatTimeout bounds one outbound heartbeat push.
	heartbeatTimeout = 10 * time.Second
	// tickInterval is how often the heartbeat loop wakes up to ask whether a
	// push is due. The schedule has minute granularity, so a coarse tick is
	// plenty and keeps an idle server quiet.
	tickInterval = 30 * time.Second
	// pingErrorLimit keeps a provider's error page out of the settings panel.
	pingErrorLimit = 300
)

// Store persists the single global monitoring document and reports whether
// the file-backed store underneath it still works.
type Store interface {
	Load(ctx context.Context) (Config, error)
	Save(ctx context.Context, cfg Config) error
	// Probe reports whether DATA_DIR is readable and writable. It is the
	// "backend" check of the health report: every other store on this box
	// lives in the same directory, so one answer covers them all.
	Probe(ctx context.Context) error
}

// LXD is the container daemon probe. The lxc CLI client satisfies it
// directly, so nothing has to adapt between them.
type LXD interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// EdgeProbe reports whether the public HTTPS edge accepts connections. It is
// an interface so tests can answer without a socket.
type EdgeProbe interface {
	Probe(ctx context.Context) error
}

// Announcer receives the one lifecycle event this service emits: the backend
// process came up. It is implemented in the composition package, the only
// place allowed to bridge monitoring and the notification service.
type Announcer interface {
	PlatformStarted(ctx context.Context, version string)
}

// Dependencies groups the collaborators the service can work with. Every one
// of them is optional: a nil store leaves settings unavailable, a nil LXD
// reports the container check as skipped, and a nil announcer means a restart
// is simply not announced.
type Dependencies struct {
	Store     Store
	LXD       LXD
	Announcer Announcer
	// Version is stamped into the health report and the boot announcement. It
	// is the only fact /healthz reveals about this host.
	Version string
}

// Service owns the cached health probes, the configuration cache, and the
// heartbeat loop.
type Service struct {
	store     Store
	lxd       LXD
	edge      EdgeProbe
	announcer Announcer
	version   string
	client    *http.Client
	now       func() time.Time
	tick      time.Duration
	ttl       time.Duration

	lxdCache  probeCache
	edgeCache probeCache

	mu     sync.RWMutex
	config Config

	startOnce sync.Once
	stopOnce  sync.Once
	stopped   chan struct{}
}

type Option func(*Service)

// WithClock replaces the clock behind the probe cache, the heartbeat
// schedule, and UpdatedAt.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithHTTPClient replaces the heartbeat transport. Tests point it at an
// httptest server.
func WithHTTPClient(client *http.Client) Option {
	return func(s *Service) {
		if client != nil {
			s.client = client
		}
	}
}

// WithEdgeProbe replaces the public-edge check.
func WithEdgeProbe(probe EdgeProbe) Option {
	return func(s *Service) {
		if probe != nil {
			s.edge = probe
		}
	}
}

// WithTickInterval replaces how often the heartbeat loop wakes up. Tests use
// it to avoid waiting for the production tick.
func WithTickInterval(interval time.Duration) Option {
	return func(s *Service) {
		if interval > 0 {
			s.tick = interval
		}
	}
}

// WithProbeTTL replaces how long a health probe result is reused.
func WithProbeTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.ttl = ttl
		}
	}
}

// New loads the stored configuration. A missing or unreadable document
// degrades to defaults, so a monitoring problem can never keep the server
// from booting — which would be a spectacular way for a health feature to
// fail. Nothing is probed or pushed until Start is called.
func New(ctx context.Context, deps Dependencies, options ...Option) *Service {
	service := &Service{
		store:     deps.Store,
		lxd:       deps.LXD,
		edge:      newTCPEdgeProbe(),
		announcer: deps.Announcer,
		version:   strings.TrimSpace(deps.Version),
		client:    &http.Client{Timeout: heartbeatTimeout},
		now:       time.Now,
		tick:      tickInterval,
		ttl:       probeTTL,
		config:    DefaultConfig(),
		stopped:   make(chan struct{}),
	}
	if deps.Store != nil {
		loaded, err := deps.Store.Load(ctx)
		if err != nil {
			log.Printf("monitoring: reading stored settings failed, the heartbeat stays off: %v", err)
		} else {
			service.config = loaded.Normalize()
		}
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Start announces the restart and launches the heartbeat loop. It is
// idempotent, and the loop exits when ctx is cancelled or Stop is called.
func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		s.announceStart(ctx)
		go s.loop(ctx)
	})
}

// Stop shuts the heartbeat loop down. Safe to call more than once.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopped) })
}

// announceStart publishes the "we came up" event. A crash-restart is
// otherwise invisible from a phone: the box is reachable again before anyone
// notices it went away, so the restart itself is the signal worth sending.
func (s *Service) announceStart(ctx context.Context) {
	if s.announcer == nil {
		return
	}
	version := s.version
	if version == "" {
		version = "unknown"
	}
	s.announcer.PlatformStarted(ctx, version)
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
			s.heartbeatTick(ctx)
		}
	}
}

// heartbeatTick pushes at most one heartbeat. It is the whole schedule: the
// loop around it only decides how often to ask.
func (s *Service) heartbeatTick(ctx context.Context) {
	cfg := s.Config()
	if !cfg.Active() || !s.pushDue(cfg) {
		return
	}
	// The heartbeat is a liveness claim, so it must not be made on behalf of
	// a platform that cannot serve. Staying silent is what makes the external
	// monitor raise the alarm.
	if report := s.Report(ctx, false); !report.Healthy() {
		log.Printf("monitoring: skipping the heartbeat, the platform reports %s", report.Status)
		return
	}
	s.push(ctx, cfg)
}

// pushDue reports whether a full interval has passed since the last attempt.
// Attempts count whether or not they succeeded, which is what keeps a broken
// URL from being retried in a tight loop.
func (s *Service) pushDue(cfg Config) bool {
	if cfg.LastPingAt <= 0 {
		return true
	}
	return !s.now().Before(time.UnixMilli(cfg.LastPingAt).Add(cfg.Interval()))
}

// PingResult reports one heartbeat push, for the admin "Ping now" action.
type PingResult struct {
	Delivered bool   `json:"delivered"`
	At        int64  `json:"at"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// Ping pushes one heartbeat immediately and reports the outcome. Unlike the
// ticker it does not consult the health report: an operator pressing the
// button is testing the URL, not claiming the box is well.
func (s *Service) Ping(ctx context.Context) (PingResult, error) {
	if s == nil {
		return PingResult{}, errors.New("monitoring is unavailable")
	}
	cfg := s.Config()
	if !cfg.Configured() {
		return PingResult{}, fmt.Errorf("%w: no heartbeat URL is stored", ErrInvalidConfig)
	}
	return s.push(ctx, cfg), nil
}

// push performs one heartbeat GET and records the outcome on the stored
// configuration, so the panel can show what happened and the ticker knows
// when it may try again.
func (s *Service) push(ctx context.Context, cfg Config) PingResult {
	at := s.now()
	err := s.get(ctx, cfg.HeartbeatURL)
	result := PingResult{Delivered: err == nil, At: at.UnixMilli(), Status: PingOK}
	if err != nil {
		result.Status = PingFailed
		result.Error = truncate(pingErrorText(err, cfg.HeartbeatURL), pingErrorLimit)
		log.Printf("monitoring: heartbeat push to %s failed: %s", MaskURL(cfg.HeartbeatURL), result.Error)
	}
	s.recordPing(ctx, result)
	return result
}

func (s *Service) get(ctx context.Context, endpoint string) error {
	ctx, cancel := context.WithTimeout(ctx, heartbeatTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("the heartbeat URL responded %d", response.StatusCode)
	}
	return nil
}

// recordPing folds an attempt into the stored configuration. A store failure
// is logged rather than surfaced: the push itself already happened, and the
// operator's question is whether it landed.
func (s *Service) recordPing(ctx context.Context, result PingResult) {
	s.mu.Lock()
	next := s.config
	next.LastPingAt = result.At
	next.LastPingStatus = result.Status
	next.LastPingError = result.Error
	s.config = next
	s.mu.Unlock()

	if s.store == nil {
		return
	}
	if err := s.store.Save(ctx, next); err != nil {
		log.Printf("monitoring: recording the heartbeat outcome failed: %v", err)
	}
}

// Config returns the live configuration, heartbeat URL included. It must not
// be exposed over HTTP.
func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// PublicConfig returns the admin-facing, URL-masked view.
func (s *Service) PublicConfig() PublicConfig {
	return s.Config().Public()
}

// Save validates and persists an update, then arms the new schedule.
func (s *Service) Save(ctx context.Context, input UpdateInput) (PublicConfig, error) {
	if s == nil {
		return PublicConfig{}, errors.New("monitoring is unavailable")
	}
	// An omitted interval means "leave it at the default" rather than "zero",
	// so a minimal client body is still a valid one.
	if input.IntervalMinutes == 0 {
		input.IntervalMinutes = DefaultIntervalMinutes
	}
	if input.IntervalMinutes < MinIntervalMinutes || input.IntervalMinutes > MaxIntervalMinutes {
		return PublicConfig{}, fmt.Errorf(
			"%w: the heartbeat interval must be between %d and %d minutes",
			ErrInvalidConfig, MinIntervalMinutes, MaxIntervalMinutes,
		)
	}
	next := s.Config().Apply(input)
	if err := validate(next); err != nil {
		return PublicConfig{}, err
	}
	next.UpdatedAt = s.now().UnixMilli()

	if s.store != nil {
		if err := s.store.Save(ctx, next); err != nil {
			return PublicConfig{}, fmt.Errorf("save monitoring settings: %w", err)
		}
	}
	s.mu.Lock()
	s.config = next
	s.mu.Unlock()
	return next.Public(), nil
}

func validate(cfg Config) error {
	cfg = cfg.Normalize()
	if cfg.HeartbeatURL != "" {
		parsed, err := url.Parse(cfg.HeartbeatURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%w: the heartbeat URL must be an absolute http(s) URL", ErrInvalidConfig)
		}
	}
	if cfg.Enabled && cfg.HeartbeatURL == "" {
		return fmt.Errorf("%w: paste a heartbeat URL before turning the heartbeat on", ErrInvalidConfig)
	}
	return nil
}

// pingErrorText renders a transport failure without the URL in it. The URL is
// the credential, and this string reaches an admin page and the server log.
func pingErrorText(err error, endpoint string) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	text := err.Error()
	if endpoint != "" {
		text = strings.ReplaceAll(text, endpoint, MaskURL(endpoint))
	}
	return strings.TrimSpace(text)
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
