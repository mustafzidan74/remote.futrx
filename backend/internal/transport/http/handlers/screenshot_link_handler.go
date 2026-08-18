package httphandlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// screenshotLinkPrefix is the login-less route. It sits outside /api on
// purpose: the auth middleware gates /api and /ws only, so this reaches its own
// token check the same way the client portal does.
const screenshotLinkPrefix = "/s/screenshot/"

// ScreenshotLinkHandler serves a stored capture to whoever holds its 24-hour
// token.
//
// This route exists for exactly one reason: a chat gateway that cannot carry
// binary content (CallMeBot's WhatsApp bridge is one GET with the message in
// its query string) can still show someone the picture. The token is the whole
// credential, so it is single-purpose — one image, one expiry, no listing, no
// project, no session.
type ScreenshotLinkHandler struct {
	screenshots ScreenshotLinkService
}

// ScreenshotLinkService is the narrow half of the screenshot service this
// route needs. Declaring it here keeps the handler testable without a store.
type ScreenshotLinkService interface {
	ResolveLink(ctx context.Context, token string) ([]byte, servicescreenshot.Screenshot, error)
}

func NewScreenshotLinkHandler(screenshots ScreenshotLinkService) *ScreenshotLinkHandler {
	if screenshots == nil {
		return nil
	}
	return &ScreenshotLinkHandler{screenshots: screenshots}
}

func (h *ScreenshotLinkHandler) RegisterRoutes(mux *http.ServeMux) {
	if h == nil {
		return
	}
	mux.HandleFunc(screenshotLinkPrefix, h.Handle)
}

func (h *ScreenshotLinkHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.screenshots == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "screenshots are unavailable")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token, ok := screenshotTokenFromPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, record, err := h.screenshots.ResolveLink(r.Context(), token)
	if err != nil {
		switch {
		case errors.Is(err, servicescreenshot.ErrLinkExpired):
			httptransport.SendErr(w, http.StatusGone, err.Error())
		default:
			http.NotFound(w, r)
		}
		return
	}
	writeScreenshot(w, data, record)
}

// screenshotTokenFromPath pulls "<token>" out of "/s/screenshot/<token>.png".
func screenshotTokenFromPath(path string) (string, bool) {
	rest := strings.TrimPrefix(path, screenshotLinkPrefix)
	if rest == path || rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	if !strings.HasSuffix(rest, ".png") {
		return "", false
	}
	// r.URL.Path arrives decoded, so the token is taken as-is.
	token := strings.TrimSuffix(rest, ".png")
	if token == "" {
		return "", false
	}
	return token, true
}
