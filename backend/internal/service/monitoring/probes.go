package monitoring

import (
	"context"
	"net"
	"sync"
	"time"
)

// edgeAddress is where Caddy terminates public HTTPS on this box. The backend
// is loopback-only, so dialing loopback is the same question as "is the edge
// listening".
const edgeAddress = "127.0.0.1:443"

// Report answers the /healthz question. It never blocks on more than one
// probe per TTL window, so an unauthenticated caller — even a rate-limited
// flood of them — cannot turn the endpoint into a way to hammer LXD.
//
// proxied says the request arrived through Caddy (it carried the forwarding
// headers only Caddy can set, because only loopback reaches this backend).
// That is proof the edge is up, and it is cheaper and more truthful than any
// probe, so it short-circuits the edge check.
func (s *Service) Report(ctx context.Context, proxied bool) Report {
	if s == nil {
		return Report{Status: StatusDegraded, Checks: Checks{
			Backend: StatusDegraded, LXD: StatusSkipped, Caddy: StatusSkipped,
		}}
	}
	report := Report{
		Version: s.version,
		Checks: Checks{
			Backend: s.checkStore(ctx),
			LXD:     s.checkLXD(ctx),
			Caddy:   s.checkEdge(ctx, proxied),
		},
	}
	if report.Version == "" {
		report.Version = "dev"
	}

	// Only the store and LXD decide the roll-up, and only when they were asked
	// and failed. A degraded edge check is worth reporting but cannot be the
	// reason a request that arrived through that edge is answered with a
	// failure, and StatusSkipped is a fact about the deployment — no LXD on
	// this box — rather than a fault in it.
	report.Status = StatusOK
	if report.Checks.Backend == StatusDegraded {
		report.Status = StatusDegraded
		report.Details = append(report.Details, DetailStore)
	}
	if report.Checks.LXD == StatusDegraded {
		report.Status = StatusDegraded
		report.Details = append(report.Details, DetailLXD)
	}
	if report.Checks.Caddy == StatusDegraded {
		report.Details = append(report.Details, DetailCaddy)
	}
	return report
}

// checkStore asks whether DATA_DIR still accepts writes. It is not cached:
// the probe is a stat and a temp file on a local filesystem, and it is the
// one answer nobody wants a minute stale.
func (s *Service) checkStore(ctx context.Context) Status {
	if s.store == nil {
		return StatusSkipped
	}
	if err := s.store.Probe(ctx); err != nil {
		return StatusDegraded
	}
	return StatusOK
}

// checkLXD asks the daemon for its own state. `lxc info` with no container
// name is the cheapest question LXD answers, and the result stands for a full
// TTL so a polled endpoint costs one call a minute at most.
func (s *Service) checkLXD(ctx context.Context) Status {
	if s.lxd == nil {
		return StatusSkipped
	}
	err := s.lxdCache.value(s.now(), s.ttl, func() error {
		probeCtx, cancel := context.WithTimeout(ctx, lxdProbeTimeout)
		defer cancel()
		_, err := s.lxd.Run(probeCtx, "info")
		return err
	})
	if err != nil {
		return StatusDegraded
	}
	return StatusOK
}

func (s *Service) checkEdge(ctx context.Context, proxied bool) Status {
	if proxied {
		return StatusOK
	}
	if s.edge == nil {
		return StatusSkipped
	}
	err := s.edgeCache.value(s.now(), s.ttl, func() error {
		probeCtx, cancel := context.WithTimeout(ctx, edgeProbeTimeout)
		defer cancel()
		return s.edge.Probe(probeCtx)
	})
	if err != nil {
		return StatusDegraded
	}
	return StatusOK
}

// probeCache remembers one probe's answer for a TTL. The mutex is held across
// the probe itself, so a burst of concurrent callers produces one call and
// then shares its result rather than stampeding the thing being probed.
type probeCache struct {
	mu    sync.Mutex
	at    time.Time
	err   error
	valid bool
}

func (c *probeCache) value(now time.Time, ttl time.Duration, run func() error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && now.Sub(c.at) < ttl {
		return c.err
	}
	c.err = run()
	c.at = now
	c.valid = true
	return c.err
}

// tcpEdgeProbe answers "is anything listening on the public HTTPS port" with
// a connect and an immediate close. It deliberately speaks no TLS: a
// handshake would cost a certificate decision and tell us nothing more.
type tcpEdgeProbe struct {
	address string
}

func newTCPEdgeProbe() tcpEdgeProbe {
	return tcpEdgeProbe{address: edgeAddress}
}

func (p tcpEdgeProbe) Probe(ctx context.Context) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", p.address)
	if err != nil {
		return err
	}
	return conn.Close()
}
