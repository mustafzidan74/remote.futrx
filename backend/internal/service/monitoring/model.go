// Package monitoring exposes the platform's own liveness to the outside
// world. A box cannot alert about its own death from inside, so this package
// serves two halves of the same job: a cheap public /healthz an external HTTP
// monitor can poll, and an outbound heartbeat the platform pushes to a
// dead-man switch service while it is healthy.
//
// Both halves are deliberately shallow. The health report answers from cached
// probes so an unauthenticated request can never make the box do real work,
// and the heartbeat is fire and forget: a failure is logged once and retried
// no sooner than the configured interval.
package monitoring

import (
	"net/url"
	"strings"
	"time"
)

// Status is one component's verdict, and the report's own roll-up.
type Status string

const (
	// StatusOK means the component answered.
	StatusOK Status = "ok"
	// StatusDegraded means the component was asked and did not answer.
	StatusDegraded Status = "degraded"
	// StatusSkipped means this deployment has no such component wired, which
	// is what a dev box without LXD gets. It is never a failure.
	StatusSkipped Status = "skipped"
)

// Checks is the per-component breakdown. Its JSON shape is a public contract:
// external monitors match on it, so field names and values are load bearing.
type Checks struct {
	// Backend is the file-backed store under DATA_DIR. Without it the
	// platform cannot read or write a single thing it owns.
	Backend Status `json:"backend"`
	// LXD is the container daemon. Without it no project can start.
	LXD Status `json:"lxd"`
	// Caddy is the public HTTPS edge.
	Caddy Status `json:"caddy"`
}

// Report is the /healthz body. It is served to unauthenticated callers, so it
// carries the version and nothing else that describes this host: no hostname,
// no paths, no error strings from the probes. Details come from a fixed
// vocabulary for the same reason.
type Report struct {
	Status  Status   `json:"status"`
	Version string   `json:"version"`
	Checks  Checks   `json:"checks"`
	Details []string `json:"details,omitempty"`
}

// Healthy reports whether the platform can serve. Only the store and LXD
// count: a degraded edge check cannot be the reason a request that arrived
// through the edge is answered with a failure.
func (r Report) Healthy() bool {
	return r.Status == StatusOK
}

// Detail strings are a closed vocabulary. /healthz is public, so a probe's
// own error text — which can name binaries, paths, and hostnames — never
// reaches the wire.
const (
	DetailStore = "the platform data store is not writable"
	DetailLXD   = "the LXD daemon did not answer"
	DetailCaddy = "the public HTTPS edge is not accepting connections"
)

// Interval bounds for the heartbeat. A minute is the fastest any free dead
// man switch service accepts; an hour is the slowest that still catches a
// crash the same working day.
const (
	MinIntervalMinutes     = 1
	MaxIntervalMinutes     = 60
	DefaultIntervalMinutes = 5
)

// Ping outcome values recorded on the configuration.
const (
	PingOK     = "ok"
	PingFailed = "failed"
)

// HealthPath is the public health endpoint, named here so the admin page can
// show an operator exactly what to paste into an external monitor.
const HealthPath = "/healthz"

// Config is the monitoring configuration persisted at
// DATA_DIR/monitoring.json. The heartbeat URL is a bearer credential — anyone
// holding it can tell the monitoring service this box is alive — so the file
// is mode 0600 and the URL is never echoed back over the API.
type Config struct {
	Enabled         bool   `json:"enabled"`
	HeartbeatURL    string `json:"heartbeatUrl,omitempty"`
	IntervalMinutes int    `json:"intervalMinutes,omitempty"`

	// LastPingAt is when the ticker last *attempted* a push, successful or
	// not. It doubles as the throttle: a failed heartbeat waits a full
	// interval like a successful one, so a broken URL cannot spin.
	LastPingAt     int64  `json:"lastPingAt,omitempty"`
	LastPingStatus string `json:"lastPingStatus,omitempty"`
	LastPingError  string `json:"lastPingError,omitempty"`

	UpdatedAt int64 `json:"updatedAt,omitempty"`
}

// DefaultConfig is what a server starts with: no heartbeat, and the
// five-minute cadence pre-filled so enabling it is a one-field decision.
func DefaultConfig() Config {
	return Config{IntervalMinutes: DefaultIntervalMinutes}
}

// Normalize trims user-entered values and clamps the interval, so everything
// downstream can assume a sane document.
func (c Config) Normalize() Config {
	c.HeartbeatURL = strings.TrimSpace(c.HeartbeatURL)
	c.LastPingStatus = strings.TrimSpace(c.LastPingStatus)
	switch {
	case c.IntervalMinutes < MinIntervalMinutes:
		c.IntervalMinutes = DefaultIntervalMinutes
	case c.IntervalMinutes > MaxIntervalMinutes:
		c.IntervalMinutes = MaxIntervalMinutes
	}
	return c
}

// Configured reports whether the heartbeat has a target to push to.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.HeartbeatURL) != ""
}

// Active reports whether the ticker should be pushing at all.
func (c Config) Active() bool {
	return c.Enabled && c.Configured()
}

// Interval is the configured cadence as a duration.
func (c Config) Interval() time.Duration {
	return time.Duration(c.Normalize().IntervalMinutes) * time.Minute
}

// PublicConfig is the admin-facing view. The heartbeat URL is a credential,
// so the caller sees only whether one is stored and enough of it to recognize
// which service it points at.
type PublicConfig struct {
	Enabled            bool   `json:"enabled"`
	Configured         bool   `json:"configured"`
	HeartbeatURLMasked string `json:"heartbeatUrlMasked,omitempty"`
	HeartbeatHost      string `json:"heartbeatHost,omitempty"`
	IntervalMinutes    int    `json:"intervalMinutes"`
	MinIntervalMinutes int    `json:"minIntervalMinutes"`
	MaxIntervalMinutes int    `json:"maxIntervalMinutes"`
	LastPingAt         int64  `json:"lastPingAt,omitempty"`
	LastPingStatus     string `json:"lastPingStatus,omitempty"`
	LastPingError      string `json:"lastPingError,omitempty"`
	UpdatedAt          int64  `json:"updatedAt,omitempty"`
	// HealthPath is echoed so the panel can render the exact URL an operator
	// pastes into UptimeRobot without hard-coding it in two places.
	HealthPath string `json:"healthPath"`
}

// Public renders the admin-facing view of a configuration.
func (c Config) Public() PublicConfig {
	c = c.Normalize()
	return PublicConfig{
		Enabled:            c.Enabled,
		Configured:         c.Configured(),
		HeartbeatURLMasked: MaskURL(c.HeartbeatURL),
		HeartbeatHost:      URLHost(c.HeartbeatURL),
		IntervalMinutes:    c.IntervalMinutes,
		MinIntervalMinutes: MinIntervalMinutes,
		MaxIntervalMinutes: MaxIntervalMinutes,
		LastPingAt:         c.LastPingAt,
		LastPingStatus:     c.LastPingStatus,
		LastPingError:      c.LastPingError,
		UpdatedAt:          c.UpdatedAt,
		HealthPath:         HealthPath,
	}
}

const maskPrefix = "••••"

// URLHost is the host an operator recognizes the service by (hc-ping.com,
// uptimerobot.com, …). It is not the secret; the token is.
func URLHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host
}

// MaskURL renders a stored heartbeat URL as its host plus the last four
// characters of its token: enough to tell two healthchecks.io URLs apart,
// never enough to ping either of them.
func MaskURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return maskPrefix
	}
	tail := ""
	if runes := []rune(urlToken(parsed)); len(runes) > 4 {
		tail = string(runes[len(runes)-4:])
	}
	return parsed.Host + "/" + maskPrefix + tail
}

// urlToken is the secret-bearing part of a heartbeat URL: the last path
// segment, or the query string when the path carries nothing.
func urlToken(parsed *url.URL) string {
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	last := segments[len(segments)-1]
	if last == "" {
		return parsed.RawQuery
	}
	return last
}

// UpdateInput is the admin PUT body. The heartbeat URL follows the same
// write-only semantics as every other stored credential: an empty string
// keeps what is stored (the client only ever saw a mask), and the explicit
// clear flag removes it.
type UpdateInput struct {
	Enabled         bool   `json:"enabled"`
	HeartbeatURL    string `json:"heartbeatUrl"`
	ClearHeartbeat  bool   `json:"clearHeartbeatUrl"`
	IntervalMinutes int    `json:"intervalMinutes"`
}

// Apply folds an update onto the stored configuration, preserving the URL the
// caller did not resubmit. Retargeting the heartbeat resets the last-ping
// record, so a stale "delivered" can never vouch for a URL nothing has tried.
func (c Config) Apply(input UpdateInput) Config {
	current := c.Normalize()
	next := Config{
		Enabled:         input.Enabled,
		HeartbeatURL:    current.HeartbeatURL,
		IntervalMinutes: input.IntervalMinutes,
		LastPingAt:      current.LastPingAt,
		LastPingStatus:  current.LastPingStatus,
		LastPingError:   current.LastPingError,
	}
	switch {
	case input.ClearHeartbeat:
		// Removing the target removes the claim with it: a heartbeat with
		// nowhere to go is not "on", it is unconfigured. Turning the toggle
		// off here is what keeps "remove the URL" from failing validation.
		next.HeartbeatURL = ""
		next.Enabled = false
	case strings.TrimSpace(input.HeartbeatURL) != "":
		next.HeartbeatURL = strings.TrimSpace(input.HeartbeatURL)
	}
	if next.HeartbeatURL != current.HeartbeatURL {
		next.LastPingAt, next.LastPingStatus, next.LastPingError = 0, "", ""
	}
	return next
}
