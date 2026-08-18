package health

import (
	"context"
	"log"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceresources "github.com/futrx-com/remote.futrx.com/internal/service/resources"
)

// DefaultInterval is how often the monitor sweeps the fleet when the operator
// sets no HEALTH_MONITOR_INTERVAL.
const DefaultInterval = time.Minute

// checkTimeout bounds one project's whole sweep: a handful of LXD queries, one
// listener scan, and one preview probe. A container wedged badly enough to
// hang all of them must not hold up the projects behind it.
const checkTimeout = 20 * time.Second

// Projects is the monitor's view of the project catalog.
type Projects interface {
	List(ctx context.Context) ([]serviceproject.Meta, error)
}

// ContainerVitals is the cheap half of a container inspection: lifecycle
// state, configured limits, and the live counters. It deliberately excludes
// the agent-version and credential probes of a full inspection, which shell
// into the container several times and have no business running on a timer.
type ContainerVitals interface {
	Vitals(ctx context.Context, containerName string) (serviceproject.ContainerInspect, error)
}

// Listeners discovers reachable TCP listeners inside a container. It is the
// same scanner the preview picker uses.
type Listeners interface {
	List(ctx context.Context, containerName string) ([]serviceproject.ContainerApp, error)
}

// Publisher broadcasts one project's health to workspace subscribers. A nil
// health clears the row for a project that is no longer monitored.
type Publisher interface {
	PublishProjectHealth(id serviceproject.ID, health *ProjectHealth)
}

// Alerter receives the transitions worth telling a human about. It is
// implemented in the composition package, the only place allowed to bridge the
// health and notification services.
type Alerter interface {
	ProjectHealthChanged(ctx context.Context, project serviceproject.Meta, health ProjectHealth)
}

// Dependencies groups the collaborators the monitor cannot work without. A nil
// Vitals leaves the service permanently disabled, which is what a deployment
// without LXD gets.
type Dependencies struct {
	Projects  Projects
	Vitals    ContainerVitals
	Listeners Listeners
	Publisher Publisher
	Alerter   Alerter
	// Interval is the sweep period. Zero selects DefaultInterval; a negative
	// value disables the monitor, which is how HEALTH_MONITOR_INTERVAL=0
	// reaches here.
	Interval time.Duration
}

// Service owns the sweep loop and the per-project cache that the API and the
// workspace socket read.
type Service struct {
	projects  Projects
	vitals    ContainerVitals
	listeners Listeners
	publisher Publisher
	alerter   Alerter
	prober    Prober
	interval  time.Duration
	now       func() time.Time

	mu      sync.Mutex
	entries map[serviceproject.ID]*entry
}

// entry is one project's cached verdict plus the state the next sweep needs:
// the debouncer, and the CPU counter to subtract from.
type entry struct {
	health  ProjectHealth
	machine stateMachine
	cpu     cpuSample
}

// cpuSample is a cumulative CPU-seconds reading and when it was taken. Two of
// them make a percentage.
type cpuSample struct {
	seconds int64
	at      time.Time
}

type Option func(*Service)

// WithProber replaces the preview transport. Tests use it to answer without a
// network.
func WithProber(prober Prober) Option {
	return func(s *Service) {
		if prober != nil {
			s.prober = prober
		}
	}
}

// WithClock replaces the clock behind LastCheckedAt and the CPU delta.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// New builds the monitor. Nothing is measured until Start is called.
func New(deps Dependencies, options ...Option) *Service {
	interval := deps.Interval
	if interval == 0 {
		interval = DefaultInterval
	}
	service := &Service{
		projects:  deps.Projects,
		vitals:    deps.Vitals,
		listeners: deps.Listeners,
		publisher: deps.Publisher,
		alerter:   deps.Alerter,
		prober:    newHTTPProber(),
		interval:  interval,
		now:       time.Now,
		entries:   map[serviceproject.ID]*entry{},
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Enabled reports whether the monitor will sweep at all. A disabled service
// still answers queries; it simply never has anything cached.
func (s *Service) Enabled() bool {
	return s != nil && s.projects != nil && s.vitals != nil && s.interval > 0
}

// Interval reports the configured sweep period.
func (s *Service) Interval() time.Duration {
	if s == nil {
		return 0
	}
	return s.interval
}

// Start runs the sweep loop until ctx is cancelled. It returns immediately,
// and does nothing at all when the monitor is disabled.
func (s *Service) Start(ctx context.Context) {
	if !s.Enabled() {
		log.Printf("health: project health monitor disabled")
		return
	}
	log.Printf("health: project health monitor sweeping every %s", s.interval)
	go s.loop(ctx)
}

// loop sweeps on a jittered period. The jitter matters after a host reboot:
// without it every restarted backend would query LXD on the same second as
// every other timer on the box.
func (s *Service) loop(ctx context.Context) {
	timer := time.NewTimer(s.nextDelay())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.Check(ctx)
			timer.Reset(s.nextDelay())
		}
	}
}

// nextDelay spreads a sweep across roughly a tenth of the interval either side
// of its nominal time.
func (s *Service) nextDelay() time.Duration {
	spread := s.interval / 5
	if spread <= 0 {
		return s.interval
	}
	return s.interval - spread/2 + time.Duration(rand.Int64N(int64(spread)+1))
}

// Check runs one full sweep: every running project is measured, and every
// project that is no longer running is forgotten.
func (s *Service) Check(ctx context.Context) {
	if s.projects == nil || s.vitals == nil {
		return
	}
	metas, err := s.projects.List(ctx)
	if err != nil {
		log.Printf("health: listing projects failed, skipping this sweep: %v", err)
		return
	}
	monitored := make(map[serviceproject.ID]struct{}, len(metas))
	for _, meta := range metas {
		if meta.Status != serviceproject.StatusRunning || meta.ContainerName == "" {
			continue
		}
		monitored[meta.ID] = struct{}{}
		s.checkProject(ctx, meta)
		if ctx.Err() != nil {
			return
		}
	}
	s.forget(monitored)
}

// checkProject measures one project and folds the result into its cache entry,
// broadcasting the row and raising an alert when the debounced status moves.
func (s *Service) checkProject(ctx context.Context, meta serviceproject.Meta) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	signals, health := s.measure(ctx, meta)
	result := Evaluate(signals)
	health.MemoryPct = result.MemoryPct
	health.PreviewOK = result.PreviewOK
	health.Reasons = result.Reasons
	health.LastCheckedAt = s.now().UnixMilli()

	alert := s.commit(meta.ID, result.Status, &health)

	if s.publisher != nil {
		published := health
		s.publisher.PublishProjectHealth(meta.ID, &published)
	}
	if alert && s.alerter != nil {
		s.alerter.ProjectHealthChanged(ctx, meta, health)
	}
}

// measure collects the raw signals for one project and pre-fills the parts of
// the health row that come straight off the container rather than out of the
// threshold rules.
func (s *Service) measure(
	ctx context.Context,
	meta serviceproject.Meta,
) (Signals, ProjectHealth) {
	health := ProjectHealth{ProjectID: string(meta.ID)}
	signals := Signals{
		ProjectStatus:  meta.Status,
		ContainerState: serviceproject.ContainerStateUnknown,
	}

	info, err := s.vitals.Vitals(ctx, meta.ContainerName)
	if err != nil {
		// LXD being unreachable is not the project's fault: report unknown
		// rather than invent a failure.
		return signals, health
	}
	signals.ContainerState = info.State
	if info.State != serviceproject.ContainerStateRunning {
		return signals, health
	}

	signals.MemoryUsedBytes, signals.MemoryLimitBytes = memoryUsage(info)
	health.MemoryUsedBytes = signals.MemoryUsedBytes
	health.MemoryLimitBytes = signals.MemoryLimitBytes
	health.CPUPct = s.cpuPercent(meta.ID, info)
	health.DiskUsedPct = diskPercent(info)

	signals.Listeners = s.listenerPorts(ctx, meta.ContainerName)
	health.Listeners = signals.Listeners
	if port, ok := firstAppPort(signals.Listeners); ok && s.prober != nil {
		signals.Preview = s.prober.Probe(ctx, meta.Slug, port)
	}
	return signals, health
}

// commit stores the measurement under the debounced status and reports whether
// the change deserves a notification.
func (s *Service) commit(id serviceproject.ID, observed Status, health *ProjectHealth) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.entryLocked(id)
	status, alert := current.machine.Observe(observed)
	health.Status = status
	current.health = *health
	return alert
}

// entryLocked returns the cache entry for id, creating it on first sight. The
// caller must hold the mutex.
func (s *Service) entryLocked(id serviceproject.ID) *entry {
	current, ok := s.entries[id]
	if !ok {
		current = &entry{}
		s.entries[id] = current
	}
	return current
}

// listenerPorts scans the container for open ports, tolerating a scanner that
// is unavailable: a project with no listener report is still worth a memory
// verdict.
func (s *Service) listenerPorts(ctx context.Context, containerName string) []int {
	if s.listeners == nil {
		return nil
	}
	apps, err := s.listeners.List(ctx, containerName)
	if err != nil {
		return nil
	}
	ports := make([]int, 0, len(apps))
	for _, app := range apps {
		ports = append(ports, app.Port)
	}
	sort.Ints(ports)
	return ports
}

// cpuPercent turns two cumulative CPU-seconds readings into an average load
// across the container's cores. The first reading of a container has nothing
// to subtract from and reports nothing.
func (s *Service) cpuPercent(id serviceproject.ID, info serviceproject.ContainerInspect) *float64 {
	if info.Resources == nil {
		return nil
	}
	sample := cpuSample{seconds: info.Resources.CPUUsageSeconds, at: s.now()}

	s.mu.Lock()
	current := s.entryLocked(id)
	previous := current.cpu
	current.cpu = sample
	s.mu.Unlock()

	if previous.at.IsZero() || !sample.at.After(previous.at) || sample.seconds < previous.seconds {
		return nil
	}
	elapsed := sample.at.Sub(previous.at).Seconds()
	cores := float64(containerCores(info))
	percent := round1(float64(sample.seconds-previous.seconds) / elapsed / cores * 100)
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return &percent
}

// forget drops the cache entries of projects that stopped or were deleted, and
// tells subscribers to clear their rows, so a stopped project's sidebar dot
// falls back to its plain lifecycle status instead of freezing on a stale
// reading.
func (s *Service) forget(monitored map[serviceproject.ID]struct{}) {
	s.mu.Lock()
	stale := make([]serviceproject.ID, 0)
	for id := range s.entries {
		if _, live := monitored[id]; !live {
			stale = append(stale, id)
			delete(s.entries, id)
		}
	}
	s.mu.Unlock()

	if s.publisher == nil {
		return
	}
	for _, id := range stale {
		s.publisher.PublishProjectHealth(id, nil)
	}
}

// Snapshot returns the cached verdicts for the given projects, in the order
// asked for. Projects with nothing cached are omitted rather than reported as
// unknown, so a caller can tell "not monitored" from "monitored, unknowable".
func (s *Service) Snapshot(ids []serviceproject.ID) []ProjectHealth {
	out := make([]ProjectHealth, 0, len(ids))
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if current, ok := s.entries[id]; ok {
			out = append(out, current.health)
		}
	}
	return out
}

// memoryUsage reads the container's live memory consumption and the ceiling it
// is measured against. The cgroup total is preferred over the configured
// limit: it is what the kernel will actually enforce.
func memoryUsage(info serviceproject.ContainerInspect) (int64, int64) {
	if info.Resources == nil {
		return 0, 0
	}
	used := info.Resources.MemoryCurrentBytes
	limit := info.Resources.MemoryTotalBytes
	if limit <= 0 && info.Limits != nil {
		if parsed, err := serviceresources.ParseSize(info.Limits.Memory); err == nil {
			limit = int64(parsed)
		}
	}
	return used, limit
}

// diskPercent reports root-disk consumption against the project's quota. Most
// storage pools enforce none, in which case there is no percentage to report.
func diskPercent(info serviceproject.ContainerInspect) *float64 {
	if info.Resources == nil || info.Resources.DiskUsageBytes <= 0 || info.Limits == nil {
		return nil
	}
	quota, err := serviceresources.ParseSize(info.Limits.Disk)
	if err != nil || quota == 0 {
		return nil
	}
	percent := round1(float64(info.Resources.DiskUsageBytes) / float64(quota) * 100)
	return &percent
}

// containerCores is the divisor for the CPU percentage: the enforced core
// count, falling back to what the guest reports and finally to one, so the
// number is never divided by zero.
func containerCores(info serviceproject.ContainerInspect) int {
	if info.Limits != nil {
		if cores, err := strconv.Atoi(strings.TrimSpace(info.Limits.CPU)); err == nil && cores > 0 {
			return cores
		}
	}
	if info.OS != nil && info.OS.CPUCount > 0 {
		return info.OS.CPUCount
	}
	return 1
}
