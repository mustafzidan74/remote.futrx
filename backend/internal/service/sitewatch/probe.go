package sitewatch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Prober performs one HTTP request against one URL. It is an interface so the
// scheduler, the state machine, and the whole evaluation path can be tested
// without a socket.
type Prober interface {
	Probe(ctx context.Context, endpoint Endpoint, site Site) Probe
}

const (
	// requestTimeout bounds one URL's whole request, connection and body
	// included. Fifteen seconds is past any reasonable page and short enough
	// that a wedged origin cannot hold a scheduler slot for a minute.
	requestTimeout = 15 * time.Second
	// bodyLimit is how much of a response the keyword check reads. Keywords
	// live in the markup, not in a 4 MB hero image, and reading a whole page
	// for a substring is exactly the cost this watcher exists to avoid.
	bodyLimit = 256 << 10
	// maxRedirects is the redirect budget. Client sites redirect apex to www
	// and http to https; a chain longer than this is a loop.
	maxRedirects = 8
	// errorLimit keeps a verbose transport error out of the table.
	errorLimit = 200
	// userAgent is deliberately browser-shaped. Go's default agent is
	// blocked outright by a large share of managed WAFs (Cloudflare,
	// Sucuri, Wordfence), which would make this watcher report healthy
	// client sites as down.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/126.0.0.0 Safari/537.36 remote.futrx-sitewatch/1.0"
)

// httpProber is the production transport.
type httpProber struct {
	client *http.Client
	now    func() time.Time
}

// newHTTPProber builds the shared client. One client for every site is
// deliberate: it lets the connection pool keep a TLS session per host between
// intervals, which is most of what makes a five-minute check cheap.
func newHTTPProber(now func() time.Time) *httpProber {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The fleet is many hosts checked rarely, not one host checked often.
	transport.MaxIdleConns = 64
	transport.MaxIdleConnsPerHost = 2
	transport.IdleConnTimeout = 90 * time.Second
	return &httpProber{
		client: &http.Client{
			Timeout:   requestTimeout,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		now: now,
	}
}

// Probe requests one URL and reports what came back.
//
// The method is the site's choice with two overrides: a keyword check forces
// GET because HEAD has no body to search, and a HEAD that the origin refuses
// is retried once as GET. Refusing HEAD is common enough on managed WordPress
// and on CDNs that treating it as an outage would make the feature useless.
func (p *httpProber) Probe(ctx context.Context, endpoint Endpoint, site Site) Probe {
	method := site.Method
	if method != MethodGET {
		method = MethodHEAD
	}
	if endpoint.Checks.wantsBody() {
		method = MethodGET
	}

	started := p.now()
	probe := p.request(ctx, method, endpoint, site)
	if method == MethodHEAD && headRefused(probe) {
		probe = p.request(ctx, MethodGET, endpoint, site)
	}
	probe.Duration = p.now().Sub(started)
	return probe
}

// headRefused reports whether a response means "this origin does not do HEAD"
// rather than "this site is broken". The 4xx codes below are what CDNs and
// application firewalls answer a HEAD with; a 404 is not among them, because
// a 404 on HEAD is a genuinely missing page.
func headRefused(probe Probe) bool {
	if probe.Err != nil {
		return false
	}
	switch probe.StatusCode {
	case http.StatusMethodNotAllowed,
		http.StatusNotImplemented,
		http.StatusForbidden,
		http.StatusBadRequest,
		http.StatusUpgradeRequired,
		http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func (p *httpProber) request(ctx context.Context, method Method, endpoint Endpoint, site Site) Probe {
	out := Probe{Method: method}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, string(method), endpoint.URL, nil)
	if err != nil {
		out.Err = err
		out.ErrText = sanitizeError(err, endpoint.URL)
		return out
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// Identity encoding keeps the keyword check honest without a gzip reader:
	// a compressed body would have to be inflated before a substring means
	// anything, and inflating every page is the cost this feature avoids.
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache")
	for name, value := range site.Headers {
		request.Header.Set(name, value)
	}

	response, err := p.client.Do(request)
	if err != nil {
		out.Err = err
		out.ErrText = sanitizeError(err, endpoint.URL)
		return out
	}
	defer response.Body.Close()

	out.StatusCode = response.StatusCode
	out.SizeBytes = response.ContentLength
	if response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
		out.TLSExpiresAt = response.TLS.PeerCertificates[0].NotAfter
	}

	if method == MethodHEAD {
		// Nothing to read, but the body must still be drained and closed for
		// the connection to go back to the pool.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
		if out.SizeBytes < 0 {
			out.SizeBytes = 0
		}
		return out
	}

	body, readErr := io.ReadAll(io.LimitReader(response.Body, bodyLimit))
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		out.Err = readErr
		out.ErrText = sanitizeError(readErr, endpoint.URL)
		return out
	}
	out.Body = string(body)
	out.SizeBytes = int64(len(body))
	return out
}

// sanitizeError renders a transport failure as one short line with the URL
// taken out of it. The text reaches a settings table, a Telegram message, and
// the server log, and a bare url.Error repeats the whole target three times.
func sanitizeError(err error, endpoint string) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	text := strings.TrimSpace(err.Error())
	if endpoint != "" {
		text = strings.ReplaceAll(text, endpoint, "the URL")
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(text, "Client.Timeout"):
		text = "timed out after " + formatDuration(requestTimeout)
	case errors.Is(err, context.Canceled):
		text = "the check was cancelled"
	}
	return truncate(text, errorLimit)
}
