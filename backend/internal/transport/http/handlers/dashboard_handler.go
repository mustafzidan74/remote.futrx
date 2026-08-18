package httphandlers

import (
	"context"
	"errors"
	"net/http"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicedashboard "github.com/futrx-com/remote.futrx.com/internal/service/dashboard"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// DashboardService is the HTTP layer's view of the home-screen aggregator.
type DashboardService interface {
	Snapshot(
		ctx context.Context,
		callerEmail string,
		isAdmin bool,
	) (servicedashboard.Snapshot, error)
}

// DashboardHandler serves the home screen in one request.
//
// It is the landing view, so it is the request most likely to be the first
// one a session makes. That is why the fan-out lives on the server: eight
// round trips before anything renders would make "what is happening?" the
// slowest question the product can be asked.
type DashboardHandler struct {
	dashboard DashboardService
	caller    CallerResolver
}

// NewDashboardHandler wires the read-only dashboard endpoint.
func NewDashboardHandler(
	dashboard DashboardService,
	auth *serviceauth.Service,
) *DashboardHandler {
	return &DashboardHandler{
		dashboard: dashboard,
		caller:    httptransport.NewPrincipalResolver(auth),
	}
}

func (h *DashboardHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dashboard", h.Handle)
}

func (h *DashboardHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h == nil || h.dashboard == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "dashboard unavailable")
		return
	}
	email, isAdmin, err := h.callerState(r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Scoping is the service's job, not this handler's: it asks every source
	// with the same (email, isAdmin) pair the individual endpoints take, so
	// the home screen can never name a project, chat, run or task the caller
	// could not already reach on its own endpoint.
	snapshot, err := h.dashboard.Snapshot(r.Context(), email, isAdmin)
	if err != nil {
		sendProjectError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, snapshot)
}

func (h *DashboardHandler) callerState(r *http.Request) (string, bool, error) {
	if h.caller == nil {
		return "", false, errors.New("no caller resolver")
	}
	return h.caller.EmailAndAdmin(r.Context(), r)
}
