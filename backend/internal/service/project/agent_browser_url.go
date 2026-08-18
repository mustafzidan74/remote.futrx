package project

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ErrInvalidBrowserURL reports an address the Agent Browser is not allowed to
// be driven to.
var ErrInvalidBrowserURL = errors.New("invalid agent browser url")

// ErrAgentBrowserNotReady reports that the shared browser core is not running,
// so there is nothing to navigate.
var ErrAgentBrowserNotReady = errors.New("agent browser is not running")

// illegalURLChars never appear in a legitimate absolute URL. They are how a
// caller would try to smuggle a second word into the DevTools request, so the
// check rejects rather than escapes.
const illegalURLChars = " \t\r\n\"'`<>\\"

// agentBrowserLoopbackHosts are the in-container hosts a preview may be
// reached on. The Agent Browser runs inside the project's own container, so
// loopback there is the project's own app, not the platform.
var agentBrowserLoopbackHosts = map[string]struct{}{
	"127.0.0.1": {},
	"localhost": {},
	"::1":       {},
}

// ValidateAgentBrowserURL is the policy behind "open this in the Agent
// Browser". The browser shares a container with the project's own code and
// carries the user's signed-in sessions, so the set of addresses the platform
// will drive it to is deliberately tiny:
//
//   - http or https only, so no file://, data:, or javascript: target;
//   - either container loopback (the project's dev server, reached from inside
//     the container so the platform login is never involved), or the project's
//     own public preview host "<slug>--<port>.dev.<publicHostname>";
//   - no embedded credentials, and never another project's host.
//
// It returns the URL to hand to the DevTools endpoint.
func ValidateAgentBrowserURL(rawURL, slug, publicHostname string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("%w: url is required", ErrInvalidBrowserURL)
	}
	if strings.ContainsAny(trimmed, illegalURLChars) {
		return "", fmt.Errorf("%w: url contains illegal characters", ErrInvalidBrowserURL)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidBrowserURL, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("%w: only http and https are allowed", ErrInvalidBrowserURL)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%w: embedded credentials are not allowed", ErrInvalidBrowserURL)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("%w: url has no host", ErrInvalidBrowserURL)
	}
	if port := parsed.Port(); port != "" {
		number, convErr := strconv.Atoi(port)
		if convErr != nil || number < 1 || number > 65535 {
			return "", fmt.Errorf("%w: %q is not a valid port", ErrInvalidBrowserURL, port)
		}
	}
	if _, ok := agentBrowserLoopbackHosts[host]; ok {
		return parsed.String(), nil
	}
	if isProjectPreviewHost(host, slug, publicHostname) {
		return parsed.String(), nil
	}
	return "", fmt.Errorf(
		"%w: %q is neither container loopback nor this project's preview host",
		ErrInvalidBrowserURL, host,
	)
}

// isProjectPreviewHost reports whether host is this project's own preview
// hostname, "<slug>--<port>.dev.<publicHostname>". Another project's slug is
// rejected: the browser holds this project's sessions.
func isProjectPreviewHost(host, slug, publicHostname string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	publicHostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(publicHostname), "."))
	if slug == "" || publicHostname == "" {
		return false
	}
	suffix := ".dev." + publicHostname
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	name, port, found := strings.Cut(strings.TrimSuffix(host, suffix), "--")
	if !found || name != slug {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 1 && number <= 65535
}

// AgentBrowserLoopbackURL builds the in-container address for one of the
// project's listening ports. Callers use it so the browser reaches the app
// directly instead of bouncing through the authenticated public edge.
func AgentBrowserLoopbackURL(port int) (string, error) {
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("%w: %d is not a valid port", ErrInvalidBrowserURL, port)
	}
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/", nil
}
