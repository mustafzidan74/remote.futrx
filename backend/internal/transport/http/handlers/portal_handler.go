package httphandlers

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	serviceportal "github.com/futrx-com/remote.futrx.com/internal/service/portal"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

//go:embed portal_page.gohtml
var portalPageSource string

// portalPageTemplate renders the public page. html/template escapes every
// interpolated value in its context, which is what makes an operator note (or
// a commit subject an agent wrote) safe to display verbatim.
var portalPageTemplate = template.Must(template.New("portal").Parse(portalPageSource))

// PortalService is the transport layer's view of the portal service.
type PortalService interface {
	Get(ctx context.Context, projectID serviceproject.ID) (serviceportal.Settings, error)
	Save(
		ctx context.Context,
		projectID serviceproject.ID,
		input serviceportal.UpdateInput,
	) (serviceportal.Settings, error)
	View(
		ctx context.Context,
		projectID serviceproject.ID,
		token string,
		clientKey string,
	) (serviceportal.Page, error)
}

// PortalHandler serves the one public, session-less route in the application:
// GET /portal/{projectId}?t=<token>. The member-facing settings live under
// /api/projects/{id}/portal and are dispatched by ProjectHandler, which has
// already established membership.
type PortalHandler struct {
	portal PortalService
}

func NewPortalHandler(portal PortalService) *PortalHandler {
	return &PortalHandler{portal: portal}
}

func (h *PortalHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/portal/", h.handlePage)
}

func (h *PortalHandler) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.sendPortalError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	if h.portal == nil {
		h.sendPortalError(w, http.StatusNotFound, notAvailableMessage)
		return
	}
	rawID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/portal/"), "/")
	projectID, err := url.PathUnescape(rawID)
	if err != nil || strings.Contains(projectID, "/") {
		h.sendPortalError(w, http.StatusNotFound, notAvailableMessage)
		return
	}

	page, err := h.portal.View(
		r.Context(),
		serviceproject.ID(projectID),
		r.URL.Query().Get("t"),
		httptransport.ClientIP(r),
	)
	switch {
	case errors.Is(err, serviceportal.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		h.sendPortalError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a minute.")
		return
	case err != nil:
		h.sendPortalError(w, http.StatusNotFound, notAvailableMessage)
		return
	}

	var rendered bytes.Buffer
	if err := portalPageTemplate.Execute(&rendered, page); err != nil {
		h.sendPortalError(w, http.StatusInternalServerError, "This page could not be rendered.")
		return
	}
	// A portal link is a bearer credential in a URL: keep it out of caches,
	// search engines, and the Referer header of anything the visitor clicks.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(rendered.Bytes())
}

// notAvailableMessage is the single answer to a missing project, a disabled
// portal, and a wrong token alike.
const notAvailableMessage = "This client portal is not available. Ask for a fresh link."

func (h *PortalHandler) sendPortalError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message + "\n"))
}

// handleProjectPortal serves /api/projects/{id}/portal. ProjectHandler has
// already resolved the project and checked that the caller is a member or an
// admin, so this only has to read or write.
func (h *ProjectHandler) handleProjectPortal(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
) {
	if h.portal == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, serviceportal.ErrUnavailable.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := h.portal.Get(r.Context(), id)
		if err != nil {
			sendPortalSettingsError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var input serviceportal.UpdateInput
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		settings, err := h.portal.Save(r.Context(), id, input)
		if err != nil {
			sendPortalSettingsError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, settings)
	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func sendPortalSettingsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serviceportal.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, serviceportal.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	default:
		sendProjectError(w, err)
	}
}
