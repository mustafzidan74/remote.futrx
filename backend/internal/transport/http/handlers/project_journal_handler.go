package httphandlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	servicejournal "github.com/futrx-com/remote.futrx.com/internal/service/journal"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// WithJournal enables the per-project change history under
// /api/projects/{id}/journal. Without it the route reports 503.
func (h *ProjectHandler) WithJournal(journal *servicejournal.Service) *ProjectHandler {
	h.journal = journal
	return h
}

// handleJournal answers the project's change history.
//
//	GET /api/projects/{id}/journal?q=&from=&to=&limit=&cursor=
//	GET /api/projects/{id}/journal/export   → the same history as Markdown
//
// Membership is already enforced by HandleResource for every subpath.
func (h *ProjectHandler) handleJournal(w http.ResponseWriter, r *http.Request, id serviceproject.ID, parts []string) {
	if h.journal == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "the project journal is not configured on this server")
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if len(parts) >= 3 && parts[2] == "export" {
		h.exportJournal(w, r, id)
		return
	}

	query := r.URL.Query()
	from, err := journalInstant(query.Get("from"))
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "from must be an RFC3339 timestamp")
		return
	}
	to, err := journalInstant(query.Get("to"))
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "to must be an RFC3339 timestamp")
		return
	}
	limit, _ := strconv.Atoi(query.Get("limit"))

	page, err := h.journal.Query(r.Context(), servicejournal.Query{
		ProjectID: string(id),
		Text:      strings.TrimSpace(query.Get("q")),
		From:      from,
		To:        to,
		Limit:     limit,
		Cursor:    strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
		return
	}
	httptransport.SendJSON(w, http.StatusOK, page)
}

func (h *ProjectHandler) exportJournal(w http.ResponseWriter, r *http.Request, id serviceproject.ID) {
	markdown, err := h.journal.Export(r.Context(), string(id))
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="project-journal.md"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(markdown))
}

// journalInstant reads a filter bound. Empty means "no bound"; anything else
// has to be a timestamp, so a half-typed date is refused rather than silently
// widening the range.
func journalInstant(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0, err
	}
	return at.UnixMilli(), nil
}
