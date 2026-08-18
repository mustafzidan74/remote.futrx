package health

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// probeTimeout bounds one preview request. Three seconds is long enough for a
// dev server that is merely busy and short enough that a whole sweep cannot
// stall behind one wedged container.
const probeTimeout = 3 * time.Second

// Prober asks whether a project's own application answers on its first
// non-platform port. It is an interface so tests can decide the answer without
// a network, and so a deployment that fronts containers differently can swap
// the transport.
type Prober interface {
	Probe(ctx context.Context, slug string, port int) Probe
}

// httpProber issues the probe the way Caddy would: straight at
// http://<slug>.lxd:<port>/ over the LXD bridge, from the host.
type httpProber struct {
	client *http.Client
}

func newHTTPProber() *httpProber {
	return &httpProber{
		client: &http.Client{
			Timeout: probeTimeout,
			// A redirect is a working app; following it would probe a
			// different host and could leave the container entirely.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (p *httpProber) Probe(ctx context.Context, slug string, port int) Probe {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	url := "http://" + slug + ".lxd:" + strconv.Itoa(port) + "/"
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return Probe{Attempted: true, Port: port, Err: err}
	}
	response, err := p.client.Do(request)
	if err != nil {
		return Probe{Attempted: true, Port: port, Err: err}
	}
	defer response.Body.Close()
	return Probe{Attempted: true, Port: port, StatusCode: response.StatusCode}
}
