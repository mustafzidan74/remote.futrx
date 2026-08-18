package httphandlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// screenshotsResponse is what the preview panel lists: the stored captures,
// newest first, plus whether a "send it to my phone" button has anywhere to
// send to.
type screenshotsResponse struct {
	Screenshots []servicescreenshot.Screenshot `json:"screenshots"`
	// Notifications reports whether any sink is configured, so the UI can hide
	// the send buttons rather than offer a guaranteed failure.
	Notifications bool `json:"notifications"`
}

// handleScreenshot serves POST /api/projects/{id}/screenshot: capture one
// preview port now. Project membership was already established by
// HandleResource.
func (h *ProjectHandler) handleScreenshot(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	email string,
) {
	if h.screenshots == nil || !h.screenshots.CanCapture() {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicescreenshot.ErrUnavailable.Error())
		return
	}
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body servicescreenshot.CaptureInput
	err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	if err != nil && !errors.Is(err, io.EOF) {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	result, err := h.screenshots.Capture(r.Context(), id, body, email)
	if err != nil {
		sendScreenshotError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// handleScreenshots serves /api/projects/{id}/screenshots[/{sid}.png]:
// the list, and the session-gated read of one stored PNG.
func (h *ProjectHandler) handleScreenshots(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	parts []string,
) {
	if h.screenshots == nil || !h.screenshots.Available() {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicescreenshot.ErrUnavailable.Error())
		return
	}

	rest := ""
	if len(parts) >= 3 {
		rest = strings.Trim(parts[2], "/")
	}
	if rest == "" {
		if r.Method != http.MethodGet {
			httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		records, err := h.screenshots.List(r.Context(), projectID)
		if err != nil {
			sendScreenshotError(w, err)
			return
		}
		if records == nil {
			records = []servicescreenshot.Screenshot{}
		}
		httptransport.SendJSON(w, http.StatusOK, screenshotsResponse{
			Screenshots:   records,
			Notifications: h.screenshots.NotificationsConfigured(),
		})
		return
	}

	// r.URL.Path is already decoded; nothing here decodes a second time.
	if id, ok := strings.CutSuffix(rest, "/send"); ok {
		h.sendScreenshot(w, r, projectID, id)
		return
	}
	screenshotID, ok := screenshotIDFromPath(rest)
	if !ok {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid screenshot id")
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	data, record, err := h.screenshots.Open(r.Context(), projectID, screenshotID)
	if err != nil {
		sendScreenshotError(w, err)
		return
	}
	writeScreenshot(w, data, record)
}

// sendScreenshot serves POST .../screenshots/{sid}/send: push a capture that
// already exists through the notification sinks.
func (h *ProjectHandler) sendScreenshot(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	rawID string,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if rawID == "" || strings.ContainsAny(rawID, `/\.`) {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid screenshot id")
		return
	}
	result, err := h.screenshots.Send(r.Context(), projectID, servicescreenshot.ID(rawID))
	if err != nil {
		sendScreenshotError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// screenshotIDFromPath strips the ".png" the route carries so the URL looks
// like a file to a browser, an image tag, and a chat client's link preview.
func screenshotIDFromPath(segment string) (servicescreenshot.ID, bool) {
	trimmed := strings.TrimSuffix(segment, ".png")
	if trimmed == "" || trimmed == segment || strings.ContainsAny(trimmed, "/\\.") {
		return "", false
	}
	return servicescreenshot.ID(trimmed), true
}

// writeScreenshot sends the PNG with caching that matches what it is: an
// immutable artifact of one moment, but a private one, so it is cached by the
// browser and by nothing in between.
func writeScreenshot(w http.ResponseWriter, data []byte, record servicescreenshot.Screenshot) {
	w.Header().Set("Content-Type", servicescreenshot.MIMEType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=3600, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Never let a stored image be interpreted as a document in the platform's
	// own origin.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", `inline; filename="`+record.File+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func sendScreenshotError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicescreenshot.ErrInvalidPort),
		errors.Is(err, servicescreenshot.ErrInvalidPath),
		errors.Is(err, servicescreenshot.ErrInvalidSize):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, servicescreenshot.ErrNotRunning),
		errors.Is(err, servicescreenshot.ErrToolingMissing),
		errors.Is(err, servicescreenshot.ErrTooLarge),
		errors.Is(err, servicescreenshot.ErrNotAnImage),
		errors.Is(err, servicescreenshot.ErrNoNotification):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, servicescreenshot.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, servicescreenshot.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		sendProjectError(w, err)
	}
}
