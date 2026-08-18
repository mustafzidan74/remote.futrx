package httphandlers

import (
	"net/http"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// ProjectHealthHandler serves the health monitor's cache for every project the
// caller can see, in one request. It exists so an operator (or a sidebar that
// has just booted) can read the whole fleet's traffic lights without a request
// per project, and without waiting for the workspace socket's next broadcast.
type ProjectHealthHandler struct {
	projects *serviceproject.Service
	health   *servicehealth.Service
	auth     *serviceauth.Service
}

// NewProjectHealthHandler wires the read-only health endpoint. A nil health
// service leaves the route reporting an empty, disabled monitor rather than
// disappearing, so a client never has to special-case its absence.
func NewProjectHealthHandler(
	projects *serviceproject.Service,
	health *servicehealth.Service,
	auth *serviceauth.Service,
) *ProjectHealthHandler {
	return &ProjectHealthHandler{projects: projects, health: health, auth: auth}
}

func (h *ProjectHealthHandler) RegisterRoutes(mux *http.ServeMux) {
	// More specific than the project handler's "/api/projects/" prefix, so
	// this wins the route without "health" ever being read as a project id.
	mux.HandleFunc("/api/projects/health", h.Handle)
}

// healthResponse is the endpoint's body. The monitor's own state travels with
// the rows so an operator who sees an empty list can tell "nothing is wrong"
// from "the monitor is switched off".
type healthResponse struct {
	Enabled    bool                          `json:"enabled"`
	IntervalMs int64                         `json:"intervalMs"`
	Projects   []servicehealth.ProjectHealth `json:"projects"`
}

func (h *ProjectHealthHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	email, isAdmin, err := h.caller(r)
	if err != nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.projects == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "projects are unavailable")
		return
	}

	// Visibility is resolved the same way GET /api/projects resolves it, so
	// the health list can never name a project the caller cannot already see.
	metas, err := h.projects.ListVisible(r.Context(), email, isAdmin)
	if err != nil {
		sendProjectError(w, err)
		return
	}
	ids := make([]serviceproject.ID, 0, len(metas))
	for _, meta := range metas {
		ids = append(ids, meta.ID)
	}

	httptransport.SendJSON(w, http.StatusOK, healthResponse{
		Enabled:    h.health.Enabled(),
		IntervalMs: h.health.Interval().Milliseconds(),
		Projects:   h.health.Snapshot(ids),
	})
}

func (h *ProjectHealthHandler) caller(r *http.Request) (string, bool, error) {
	if h.auth == nil {
		return "", true, nil
	}
	return callerStateFromRequest(r.Context(), r, h.auth)
}
