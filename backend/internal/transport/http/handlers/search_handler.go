package httphandlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicesearch "github.com/futrx-com/remote.futrx.com/internal/service/search"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// SearchService is the HTTP layer's narrow view of the chat search index.
type SearchService interface {
	Search(ctx context.Context, query servicesearch.Query) ([]servicesearch.Result, error)
	Stats() servicesearch.Stats
}

// SearchHandler serves full-text search over chat transcripts.
//
// Every signed-in user may call it — the middleware already gated /api — but
// the service filters every hit through the caller's chat membership, so the
// route is not a way around project access control.
type SearchHandler struct {
	search SearchService
	caller CallerResolver
}

func NewSearchHandler(search SearchService, auth *serviceauth.Service) *SearchHandler {
	return &SearchHandler{
		search: search,
		caller: httptransport.NewPrincipalResolver(auth),
	}
}

func (h *SearchHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/search", h.handleSearch)
}

func (h *SearchHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	if h.search == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "search is unavailable")
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	email, isAdmin := "", true
	if h.caller != nil {
		var err error
		email, isAdmin, err = h.caller.EmailAndAdmin(r.Context(), r)
		if err != nil || email == "" {
			httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
	}

	query := servicesearch.Query{
		Text:      r.URL.Query().Get("q"),
		ProjectID: r.URL.Query().Get("projectId"),
		Limit:     parseSearchLimit(r.URL.Query().Get("limit")),
		Email:     email,
		IsAdmin:   isAdmin,
	}
	results, err := h.search.Search(r.Context(), query)
	switch {
	case errors.Is(err, servicesearch.ErrQueryTooShort):
		// An empty result is the honest answer to a too-short query: the
		// browser types into this endpoint on every keystroke, and a 400 would
		// only make the composer's console noisy.
		httptransport.SendJSON(w, http.StatusOK, searchPayload(nil, h.search.Stats()))
	case err != nil:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	default:
		httptransport.SendJSON(w, http.StatusOK, searchPayload(results, h.search.Stats()))
	}
}

func searchPayload(results []servicesearch.Result, stats servicesearch.Stats) map[string]any {
	if results == nil {
		results = []servicesearch.Result{}
	}
	return map[string]any{
		"results": results,
		// `truncated` tells the browser that older history fell out of the
		// memory-bounded index, so "no results" can be shown honestly.
		"truncated": stats.Evicted > 0,
	}
}

func parseSearchLimit(raw string) int {
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0
	}
	return limit
}
