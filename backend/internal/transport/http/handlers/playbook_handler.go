package httphandlers

import (
	"context"
	"errors"
	"net/http"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	serviceplaybooks "github.com/futrx-com/remote.futrx.com/internal/service/playbooks"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// PlaybookService is the HTTP layer's narrow view of the playbook library.
type PlaybookService interface {
	List(ctx context.Context) ([]serviceplaybooks.Playbook, error)
	Replace(ctx context.Context, list []serviceplaybooks.Playbook, actor string) ([]serviceplaybooks.Playbook, error)
}

// PlaybookHandler serves the composer's one-click prompt templates.
//
// Reading is open to any signed-in user — the middleware already gated
// /api/* — because every member needs the menu. Writing is admin-only and
// replaces the whole document, which is what the Settings page submits.
type PlaybookHandler struct {
	playbooks PlaybookService
	caller    CallerResolver
}

func NewPlaybookHandler(playbooks PlaybookService, auth *serviceauth.Service) *PlaybookHandler {
	return &PlaybookHandler{
		playbooks: playbooks,
		caller:    httptransport.NewPrincipalResolver(auth),
	}
}

func (h *PlaybookHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/playbooks", h.handleCollection)
	mux.HandleFunc("/api/admin/playbooks", h.handleAdminCollection)
}

func (h *PlaybookHandler) handleCollection(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	email, _, err := h.caller.EmailAndAdmin(r.Context(), r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	h.sendList(w, r)
}

func (h *PlaybookHandler) handleAdminCollection(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.sendList(w, r)

	case http.MethodPut:
		var body struct {
			Playbooks []serviceplaybooks.Playbook `json:"playbooks"`
		}
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		email, _, _ := h.caller.EmailAndAdmin(r.Context(), r)
		list, err := h.playbooks.Replace(r.Context(), body.Playbooks, email)
		switch {
		case errors.Is(err, serviceplaybooks.ErrInvalidPlaybooks):
			httptransport.SendErr(w, http.StatusBadRequest, err.Error())
		case err != nil:
			httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
		default:
			httptransport.SendJSON(w, http.StatusOK, playbookCollection(list))
		}

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *PlaybookHandler) sendList(w http.ResponseWriter, r *http.Request) {
	list, err := h.playbooks.List(r.Context())
	if err != nil {
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	httptransport.SendJSON(w, http.StatusOK, playbookCollection(list))
}

func (h *PlaybookHandler) available(w http.ResponseWriter) bool {
	if h.playbooks == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "playbooks are unavailable")
		return false
	}
	return true
}

func (h *PlaybookHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.caller == nil {
		httptransport.SendErr(w, http.StatusForbidden, "admin only")
		return false
	}
	email, isAdmin, err := h.caller.EmailAndAdmin(r.Context(), r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	if !isAdmin {
		httptransport.SendErr(w, http.StatusForbidden, "admin only")
		return false
	}
	return true
}

func playbookCollection(list []serviceplaybooks.Playbook) map[string]any {
	if list == nil {
		list = []serviceplaybooks.Playbook{}
	}
	return map[string]any{"playbooks": list}
}
