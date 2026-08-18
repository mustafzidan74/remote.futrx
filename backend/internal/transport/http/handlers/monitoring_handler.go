package httphandlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// MonitoringService is the HTTP layer's narrow view of the monitoring
// service. Only masked configuration ever crosses this boundary.
type MonitoringService interface {
	Report(ctx context.Context, proxied bool) servicemonitoring.Report
	PublicConfig() servicemonitoring.PublicConfig
	Save(ctx context.Context, input servicemonitoring.UpdateInput) (servicemonitoring.PublicConfig, error)
	Ping(ctx context.Context) (servicemonitoring.PingResult, error)
}

// monitoringCaller resolves the authenticated principal for the admin half of
// this handler. It exists so the admin gate can be exercised without building
// a full auth service.
type monitoringCaller interface {
	EmailAndAdmin(ctx context.Context, r *http.Request) (string, bool, error)
}

// healthzRateLimit is the ceiling for unauthenticated /healthz hits from one
// IP. External monitors poll once a minute; sixty leaves room for several of
// them plus an operator with curl, and stops the endpoint from being turned
// into a way to make this box talk to LXD on demand.
const (
	healthzRateLimit  = 60
	healthzRateWindow = time.Minute
)

// MonitoringHandler serves both halves of external uptime monitoring: the
// public health endpoint an HTTP monitor polls, and the admin settings for
// the outbound heartbeat. They share a handler because they share a service
// and a feature; they share nothing else — /healthz is the one route here
// that takes no session at all.
type MonitoringHandler struct {
	monitoring MonitoringService
	caller     monitoringCaller
	limiter    *fixedWindowLimiter
}

func NewMonitoringHandler(
	monitoring MonitoringService,
	auth *serviceauth.Service,
) *MonitoringHandler {
	return &MonitoringHandler{
		monitoring: monitoring,
		caller:     httptransport.NewPrincipalResolver(auth),
		limiter:    newFixedWindowLimiter(healthzRateLimit, healthzRateWindow),
	}
}

// WithClock replaces the clock behind the rate limiter's window.
func (h *MonitoringHandler) WithClock(now func() time.Time) *MonitoringHandler {
	h.limiter.now = now
	return h
}

func (h *MonitoringHandler) RegisterRoutes(mux *http.ServeMux) {
	// Public on purpose. The auth middleware gates /api and /ws only, so this
	// route reaches the handler with no session, exactly like /portal.
	mux.HandleFunc(servicemonitoring.HealthPath, h.handleHealthz)
	mux.HandleFunc("/api/admin/monitoring", h.handleSettings)
	mux.HandleFunc("/api/admin/monitoring/ping", h.handlePing)
}

// handleHealthz answers the external monitor. It is the one endpoint on this
// server that an anonymous caller can reach, so it does three things and
// nothing else: rate-limit, read cached probes, and answer.
func (h *MonitoringHandler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// A monitor that caches a stale 200 is worse than no monitor at all.
	w.Header().Set("Cache-Control", "no-store")

	if !h.limiter.allow(httptransport.ClientIP(r)) {
		w.Header().Set("Retry-After", strconv.Itoa(int(healthzRateWindow.Seconds())))
		httptransport.SendErr(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	if h.monitoring == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "monitoring is unavailable")
		return
	}

	report := h.monitoring.Report(r.Context(), requestWasProxied(r))
	status := http.StatusOK
	if !report.Healthy() {
		status = http.StatusServiceUnavailable
	}
	// net/http drops the body of a HEAD response itself, so GET and HEAD take
	// the same path and can never disagree about the status code.
	httptransport.SendJSON(w, status, report)
}

// requestWasProxied reports whether Caddy forwarded this request. Only
// loopback reaches this backend, so these headers cannot be spoofed from
// outside, which makes their presence proof the public edge is alive.
func requestWasProxied(r *http.Request) bool {
	return r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Forwarded-Proto") != ""
}

func (h *MonitoringHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		httptransport.SendJSON(w, http.StatusOK, h.monitoring.PublicConfig())
	case http.MethodPut:
		var input servicemonitoring.UpdateInput
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid request")
			return
		}
		config, err := h.monitoring.Save(r.Context(), input)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, servicemonitoring.ErrInvalidConfig) {
				status = http.StatusBadRequest
			}
			httptransport.SendErr(w, status, err.Error())
			return
		}
		httptransport.SendJSON(w, http.StatusOK, config)
	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handlePing pushes one heartbeat now. It reports the delivery outcome rather
// than failing the request: "the monitoring service rejected us" is an answer
// the operator wants to read, not a 500.
func (h *MonitoringHandler) handlePing(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.monitoring.Ping(r.Context())
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, servicemonitoring.ErrInvalidConfig) {
			status = http.StatusBadRequest
		}
		httptransport.SendErr(w, status, err.Error())
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// authorize writes the failure response itself and reports whether the caller
// may proceed.
func (h *MonitoringHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.monitoring == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "monitoring is unavailable")
		return false
	}
	email, isAdmin, err := h.caller.EmailAndAdmin(r.Context(), r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	if !isAdmin {
		httptransport.SendErr(w, http.StatusForbidden, "admin only")
		return false
	}
	return true
}

// fixedWindowLimiter counts hits per key in tumbling windows. A fixed window
// admits a burst across a boundary that a token bucket would smooth, which is
// a fine trade for an endpoint whose only job is to be polled: the point is
// bounding the cost, not shaping the traffic.
type fixedWindowLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	windows map[string]*limiterWindow
}

type limiterWindow struct {
	startedAt time.Time
	count     int
}

// limiterCapacity bounds how many distinct keys are tracked. Reaching it
// triggers a sweep of expired windows; the map is the only thing an attacker
// with many source addresses could grow.
const limiterCapacity = 4096

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{
		limit:   limit,
		window:  window,
		now:     time.Now,
		windows: map[string]*limiterWindow{},
	}
}

func (l *fixedWindowLimiter) allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.windows) >= limiterCapacity {
		l.sweepLocked(now)
	}
	current, ok := l.windows[key]
	if !ok || now.Sub(current.startedAt) >= l.window {
		l.windows[key] = &limiterWindow{startedAt: now, count: 1}
		return true
	}
	if current.count >= l.limit {
		return false
	}
	current.count++
	return true
}

func (l *fixedWindowLimiter) sweepLocked(now time.Time) {
	for key, current := range l.windows {
		if now.Sub(current.startedAt) >= l.window {
			delete(l.windows, key)
		}
	}
}
