package httphandlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// SiteWatchService is the HTTP layer's narrow view of the client-site
// watcher. Every read takes the caller identity because visibility is part of
// the answer: a member sees only sites linked to a project they belong to.
type SiteWatchService interface {
	Available() bool
	List(ctx context.Context, callerEmail string, isAdmin bool) ([]servicesitewatch.View, error)
	Get(ctx context.Context, id servicesitewatch.ID, callerEmail string, isAdmin bool) (servicesitewatch.View, error)
	History(ctx context.Context, id servicesitewatch.ID, callerEmail string, isAdmin bool) ([]servicesitewatch.Record, error)
	CheckNow(ctx context.Context, id servicesitewatch.ID, callerEmail string, isAdmin bool) (servicesitewatch.Report, error)
	Create(ctx context.Context, input servicesitewatch.Input) (servicesitewatch.View, error)
	Update(ctx context.Context, id servicesitewatch.ID, input servicesitewatch.Input) (servicesitewatch.View, error)
	Delete(ctx context.Context, id servicesitewatch.ID) error
	Import(ctx context.Context, input servicesitewatch.ImportInput) (servicesitewatch.ImportResult, error)
	Candidates(ctx context.Context) ([]servicesitewatch.Candidate, error)
}

// SiteWatchHandler serves Settings → Insights → Client sites.
//
// Reading is open to any signed-in user, filtered by the service to what they
// may see. Every write is admin-only: the watched list is operator policy,
// and a member who could add a site could point this server's bandwidth at
// anything. "Check now" is the one exception among the POSTs — it performs no
// configuration change and runs only against a site the caller can already
// see.
type SiteWatchHandler struct {
	sites  SiteWatchService
	caller CallerResolver
}

func NewSiteWatchHandler(sites SiteWatchService, auth *serviceauth.Service) *SiteWatchHandler {
	return &SiteWatchHandler{
		sites:  sites,
		caller: httptransport.NewPrincipalResolver(auth),
	}
}

func (h *SiteWatchHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/sitewatch/sites", h.handleCollection)
	mux.HandleFunc("/api/sitewatch/sites/", h.handleItem)
	mux.HandleFunc("/api/admin/sitewatch/import", h.handleImport)
	mux.HandleFunc("/api/admin/sitewatch/candidates", h.handleCandidates)
}

func (h *SiteWatchHandler) handleCollection(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	email, isAdmin, ok := h.caller_(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		views, err := h.sites.List(r.Context(), email, isAdmin)
		if err != nil {
			h.sendError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, siteCollection(views))

	case http.MethodPost:
		if !isAdmin {
			httptransport.SendErr(w, http.StatusForbidden, "admin only")
			return
		}
		var input servicesitewatch.Input
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.sites.Create(r.Context(), input)
		if err != nil {
			h.sendError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusCreated, view)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleItem routes /api/sitewatch/sites/{id} and its two sub-resources.
func (h *SiteWatchHandler) handleItem(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	email, isAdmin, ok := h.caller_(w, r)
	if !ok {
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/sitewatch/sites/")
	id, action, _ := strings.Cut(rest, "/")
	siteID := servicesitewatch.ID(id)
	if !servicesitewatch.ValidID(siteID) {
		httptransport.SendErr(w, http.StatusNotFound, "site not found")
		return
	}

	switch action {
	case "":
		h.handleSite(w, r, siteID, email, isAdmin)
	case "check":
		if r.Method != http.MethodPost {
			httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		report, err := h.sites.CheckNow(r.Context(), siteID, email, isAdmin)
		if err != nil {
			h.sendError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, report)
	case "history":
		if r.Method != http.MethodGet {
			httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		records, err := h.sites.History(r.Context(), siteID, email, isAdmin)
		if err != nil {
			h.sendError(w, err)
			return
		}
		if records == nil {
			records = []servicesitewatch.Record{}
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]any{"checks": records})
	default:
		httptransport.SendErr(w, http.StatusNotFound, "not found")
	}
}

func (h *SiteWatchHandler) handleSite(
	w http.ResponseWriter,
	r *http.Request,
	id servicesitewatch.ID,
	email string,
	isAdmin bool,
) {
	switch r.Method {
	case http.MethodGet:
		view, err := h.sites.Get(r.Context(), id, email, isAdmin)
		if err != nil {
			h.sendError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	case http.MethodPut:
		if !isAdmin {
			httptransport.SendErr(w, http.StatusForbidden, "admin only")
			return
		}
		var input servicesitewatch.Input
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.sites.Update(r.Context(), id, input)
		if err != nil {
			h.sendError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	case http.MethodDelete:
		if !isAdmin {
			httptransport.SendErr(w, http.StatusForbidden, "admin only")
			return
		}
		if err := h.sites.Delete(r.Context(), id); err != nil {
			h.sendError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *SiteWatchHandler) handleImport(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input servicesitewatch.ImportInput
	if err := readJSONBody(r, &input); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	result, err := h.sites.Import(r.Context(), input)
	if err != nil {
		h.sendError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// handleCandidates offers what a project-derived import would create, so the
// operator sees the list before anything is written.
func (h *SiteWatchHandler) handleCandidates(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	found, err := h.sites.Candidates(r.Context())
	if err != nil {
		h.sendError(w, err)
		return
	}
	rows := make([]map[string]string, 0, len(found))
	for _, candidate := range found {
		rows = append(rows, map[string]string{
			"projectId":   candidate.ProjectID,
			"projectName": candidate.ProjectName,
			"url":         candidate.Domain,
			"secretKey":   candidate.SecretKey,
		})
	}
	httptransport.SendJSON(w, http.StatusOK, map[string]any{"candidates": rows})
}

func (h *SiteWatchHandler) available(w http.ResponseWriter) bool {
	if h == nil || h.sites == nil || !h.sites.Available() {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "client site monitoring is unavailable")
		return false
	}
	return true
}

// caller_ resolves the signed-in principal, answering 401 when there is none.
func (h *SiteWatchHandler) caller_(w http.ResponseWriter, r *http.Request) (string, bool, bool) {
	if h.caller == nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return "", false, false
	}
	email, isAdmin, err := h.caller.EmailAndAdmin(r.Context(), r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return "", false, false
	}
	return email, isAdmin, true
}

func (h *SiteWatchHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	_, isAdmin, ok := h.caller_(w, r)
	if !ok {
		return false
	}
	if !isAdmin {
		httptransport.SendErr(w, http.StatusForbidden, "admin only")
		return false
	}
	return true
}

// sendError maps the service's sentinels onto status codes. An invisible site
// and a missing one are both 404 on purpose: a member must not be able to
// discover which ids exist by probing.
func (h *SiteWatchHandler) sendError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicesitewatch.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, "site not found")
	case errors.Is(err, servicesitewatch.ErrInvalidSite):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, servicesitewatch.ErrTooManySites):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, servicesitewatch.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}

// siteCollection echoes the limits alongside the rows so the panel never
// hard-codes a bound the server owns.
func siteCollection(views []servicesitewatch.View) map[string]any {
	if views == nil {
		views = []servicesitewatch.View{}
	}
	return map[string]any{
		"sites":              views,
		"maxSites":           servicesitewatch.MaxSites,
		"minIntervalMinutes": servicesitewatch.MinIntervalMinutes,
		"maxIntervalMinutes": servicesitewatch.MaxIntervalMinutes,
		"maxExtraUrls":       servicesitewatch.MaxExtraURLs,
	}
}
