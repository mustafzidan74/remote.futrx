// Package screenshot owns "share a picture of the preview": a headless
// Playwright capture taken inside a project's container, stored on the host,
// and optionally pushed out through the notification sinks.
//
// It exists because the two ways of showing someone a running preview are both
// wrong for a quick "look at this". A public share link hands out live access
// to the app for a day; a manual screen grab leaves the platform entirely and
// carries no project context. A stored PNG is neither: it is a frozen, dated
// artifact of one port at one moment, readable only by the project's members
// (or, for a chat sink that cannot carry pictures, through one deliberately
// minted 24-hour link).
package screenshot

import (
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

// ID identifies one stored capture inside a project.
type ID string

const (
	// RetentionCount is how many captures one project keeps. The oldest
	// records and their PNGs are evicted after every successful capture.
	RetentionCount = 20

	// DefaultWidth and DefaultHeight are the viewport used when the caller
	// asks for none: a desktop layout that still fits a phone screen when the
	// picture is forwarded to a chat app.
	DefaultWidth  = 1280
	DefaultHeight = 800

	// MinWidth/MaxWidth and MinHeight/MaxHeight bound the viewport. The
	// ceiling is what keeps one request from asking the container for a
	// hundred-megabyte page bitmap.
	MinWidth  = 320
	MaxWidth  = 3840
	MinHeight = 240
	MaxHeight = 2160

	// MinPort and MaxPort mirror the preview host regex in Caddy: a port
	// outside it has no preview to photograph.
	MinPort = 1024
	MaxPort = 65535

	// MaxPathLength bounds the in-app path a caller may point the browser at.
	MaxPathLength = 512

	// MaxBytes rejects a capture too large to be worth storing or sending.
	// A full-page shot of a long marketing site is a few megabytes.
	MaxBytes = 16 << 20

	// CaptureTimeout bounds one capture end to end: launching Chromium,
	// loading the page, writing the file, and pulling it back to the host.
	CaptureTimeout = 30 * time.Second

	// PublicLinkTTL is how long the minted, login-less link stays valid. It
	// matches the preview share window so an operator has one number to
	// remember.
	PublicLinkTTL = 24 * time.Hour

	// TokenBytes is the entropy behind a public link before base64 encoding.
	TokenBytes = 32

	// MIMEType is the only format captures are stored in.
	MIMEType = "image/png"
)

var (
	ErrUnavailable    = errors.New("screenshots are not configured on this server")
	ErrInvalidPort    = errors.New("preview port must be between 1024 and 65535")
	ErrInvalidPath    = errors.New("screenshot path must start with / and must not contain '..'")
	ErrInvalidSize    = errors.New("screenshot viewport must be 320-3840 wide and 240-2160 tall")
	ErrNotRunning     = errors.New("the project container is not running")
	ErrNotFound       = errors.New("screenshot not found")
	ErrTooLarge       = errors.New("the captured image is larger than the 16 MB ceiling")
	ErrNotAnImage     = errors.New("the capture did not produce a PNG")
	ErrLinkExpired    = errors.New("this screenshot link has expired")
	ErrNoNotification = errors.New("no notification sink is configured to send the screenshot to")
)

// ErrToolingMissing reports that the container image has no Playwright or no
// Chromium. It is a 409 with a hint rather than a 500: the operator fixes it
// by rebuilding the base image, not by retrying.
var ErrToolingMissing = errors.New(
	"Playwright is not installed in this container: rebuild the project base image " +
		"(it ships playwright + chromium), or run `npx playwright install --with-deps chromium` inside the container",
)

// CaptureInput is the caller-supplied half of a capture request.
type CaptureInput struct {
	Port int `json:"port"`
	// Path is the in-app path, always rooted. It may carry a query string.
	Path     string `json:"path,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	FullPage bool   `json:"fullPage,omitempty"`
	// Notify asks the service to push the picture through the configured
	// notification sinks as well as storing it.
	Notify bool `json:"notify,omitempty"`
}

// Normalize fills in the defaults and rejects everything the container should
// never be asked for. It is pure so the transport can rely on the service
// having validated, and tests can drive it directly.
func (in CaptureInput) Normalize() (CaptureInput, error) {
	out := in
	if out.Port < MinPort || out.Port > MaxPort {
		return CaptureInput{}, ErrInvalidPort
	}
	path := strings.TrimSpace(out.Path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") ||
		strings.Contains(path, "..") ||
		len(path) > MaxPathLength ||
		strings.ContainsAny(path, " \t\r\n\"'\\") {
		return CaptureInput{}, ErrInvalidPath
	}
	out.Path = path

	if out.Width == 0 {
		out.Width = DefaultWidth
	}
	if out.Height == 0 {
		out.Height = DefaultHeight
	}
	if out.Width < MinWidth || out.Width > MaxWidth || out.Height < MinHeight || out.Height > MaxHeight {
		return CaptureInput{}, ErrInvalidSize
	}
	return out, nil
}

// Screenshot is the stored record of one capture. The PNG itself sits beside
// it under DATA_DIR/screenshots/<projectID>/<file>.
//
// LinkTokenHash is the SHA-256 digest of the public link's token, never the
// token: a copy of DATA_DIR cannot be turned back into a working link.
type Screenshot struct {
	ID   ID     `json:"id"`
	File string `json:"file"`
	// URL is the session-gated read route, filled in when a record is served.
	URL           string `json:"url,omitempty"`
	Port          int    `json:"port"`
	Path          string `json:"path"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	FullPage      bool   `json:"fullPage,omitempty"`
	Bytes         int64  `json:"bytes"`
	CreatedBy     string `json:"createdBy,omitempty"`
	CreatedAt     int64  `json:"createdAt"`
	LinkTokenHash string `json:"linkTokenHash,omitempty"`
	LinkExpiresAt int64  `json:"linkExpiresAt,omitempty"`
}

// LinkActive reports whether the record's public link can still serve bytes at
// nowMilli.
func (s Screenshot) LinkActive(nowMilli int64) bool {
	return s.LinkTokenHash != "" && s.LinkExpiresAt > nowMilli
}

// Public strips the link digest from a record leaving the project. The digest
// is not a secret, but it is not the caller's business either.
func (s Screenshot) Public() Screenshot {
	s.LinkTokenHash = ""
	return s
}

// CaptureResult is what a successful capture answers with: the stored record
// plus, when the caller asked to notify, one row per sink.
type CaptureResult struct {
	Screenshot Screenshot       `json:"screenshot"`
	Delivered  []DeliveryResult `json:"delivered,omitempty"`
	// Notifications reports whether any sink is configured, so the card that
	// renders the capture knows whether to offer a "send it" button at all.
	Notifications bool `json:"notifications"`
	// PublicURL is set only when a sink needed a login-less link. It is shown
	// once, like a preview share token.
	PublicURL string `json:"publicUrl,omitempty"`
}

// DeliveryResult mirrors one notification sink's outcome.
type DeliveryResult struct {
	Sink      string `json:"sink"`
	Delivered bool   `json:"delivered"`
	Error     string `json:"error,omitempty"`
}

// LoopbackURL is the address the in-container browser is pointed at. It is
// loopback on purpose: Chromium runs in the same container as the app, so this
// reaches the dev server directly and never meets the platform's sign-in page
// or the edge's share-token check.
func LoopbackURL(port int, path string) string {
	if path == "" {
		path = "/"
	}
	return "http://127.0.0.1:" + itoa(port) + path
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := [8]byte{}
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// pngSignature is the fixed 8-byte header every PNG starts with.
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// DecodePNGSize reads the dimensions out of a PNG's IHDR chunk. It doubles as
// the format check on bytes that came back through a container CLI: anything
// that is not a PNG (a shell error, a truncated pull) fails here rather than
// being stored as a broken image.
func DecodePNGSize(data []byte) (int, int, error) {
	const headerLength = 24 // signature + length + "IHDR" + width + height
	if len(data) < headerLength || string(data[:len(pngSignature)]) != string(pngSignature) {
		return 0, 0, ErrNotAnImage
	}
	if string(data[12:16]) != "IHDR" {
		return 0, 0, ErrNotAnImage
	}
	width := int(binary.BigEndian.Uint32(data[16:20]))
	height := int(binary.BigEndian.Uint32(data[20:24]))
	if width <= 0 || height <= 0 {
		return 0, 0, ErrNotAnImage
	}
	return width, height, nil
}
