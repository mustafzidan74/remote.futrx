package httphandlers

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// maxClientMessageLength bounds one outbound message. It matches the snippet
// body cap, so a template that stores cannot fail to send.
const maxClientMessageLength = 8000

// ClientMessageService is the HTTP layer's view of the outbound sinks. It is
// the notification service seen through one verb: deliver this text now, and
// say what happened.
type ClientMessageService interface {
	SinksConfigured() bool
	SendMessage(ctx context.Context, event servicenotify.Event) []servicenotify.SinkResult
}

// clientMessageResponse reports the fan-out, one row per sink, in the same
// shape the screenshot send already returns.
type clientMessageResponse struct {
	Configured bool                       `json:"configured"`
	Delivered  []servicenotify.SinkResult `json:"delivered"`
}

// handleClientMessage serves /api/projects/{id}/client-message.
//
// GET reports whether anything is configured, so the panel can hide a button
// rather than offer a guaranteed failure. POST hands the already-resolved text
// to the sinks: the placeholders were filled in by the composer that wrote it,
// and the server treats the body as opaque plain text.
//
// ProjectHandler has already resolved the project and checked that the caller
// is a member or an admin.
func (h *ProjectHandler) handleClientMessage(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
) {
	if h.clientMessages == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "notifications are not configured on this server")
		return
	}

	switch r.Method {
	case http.MethodGet:
		httptransport.SendJSON(w, http.StatusOK, clientMessageResponse{
			Configured: h.clientMessages.SinksConfigured(),
			Delivered:  []servicenotify.SinkResult{},
		})

	case http.MethodPost:
		var body struct {
			Text string `json:"text"`
			URL  string `json:"url"`
		}
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		text := strings.TrimSpace(body.Text)
		if text == "" {
			httptransport.SendErr(w, http.StatusBadRequest, "the message is empty")
			return
		}
		if utf8.RuneCountInString(text) > maxClientMessageLength {
			httptransport.SendErr(w, http.StatusBadRequest, "the message is too long to send")
			return
		}
		if !h.clientMessages.SinksConfigured() {
			httptransport.SendErr(
				w,
				http.StatusServiceUnavailable,
				"no notification sink is configured, so there is nowhere to send this",
			)
			return
		}

		project, err := h.projects.Get(r.Context(), id)
		if err != nil {
			sendProjectError(w, err)
			return
		}
		results := h.clientMessages.SendMessage(r.Context(), servicenotify.Event{
			ProjectID:   string(project.ID),
			ProjectSlug: project.Slug,
			ProjectName: project.Name,
			Summary:     text,
			URL:         strings.TrimSpace(body.URL),
		})
		if results == nil {
			results = []servicenotify.SinkResult{}
		}
		httptransport.SendJSON(w, http.StatusOK, clientMessageResponse{
			Configured: true,
			Delivered:  results,
		})

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
