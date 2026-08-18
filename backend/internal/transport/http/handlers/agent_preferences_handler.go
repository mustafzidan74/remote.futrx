package httphandlers

import (
	"context"
	"errors"
	"net/http"

	serviceagentprefs "github.com/futrx-com/remote.futrx.com/internal/service/agentprefs"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// AgentPreferencesService is the HTTP layer's narrow view of the platform
// reply-preference document.
type AgentPreferencesService interface {
	Get(ctx context.Context) (serviceagentprefs.Preferences, error)
	Update(
		ctx context.Context,
		input serviceagentprefs.UpdateInput,
		actor string,
	) (serviceagentprefs.Preferences, error)
}

// AgentPreferencesHandler serves the platform-wide agent reply preferences.
//
// Both verbs are admin-only. Unlike playbooks there is no member-readable
// route: a member never needs to read the document, because the preference
// reaches them through the agent's instructions rather than through the UI.
type AgentPreferencesHandler struct {
	prefs  AgentPreferencesService
	caller CallerResolver
}

func NewAgentPreferencesHandler(
	prefs AgentPreferencesService,
	auth *serviceauth.Service,
) *AgentPreferencesHandler {
	return &AgentPreferencesHandler{
		prefs:  prefs,
		caller: httptransport.NewPrincipalResolver(auth),
	}
}

func (h *AgentPreferencesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/agent-preferences", h.handleDocument)
}

func (h *AgentPreferencesHandler) handleDocument(w http.ResponseWriter, r *http.Request) {
	if h.prefs == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "agent preferences are unavailable")
		return
	}
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		prefs, err := h.prefs.Get(r.Context())
		if err != nil {
			httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		httptransport.SendJSON(w, http.StatusOK, prefs)

	case http.MethodPut:
		var input serviceagentprefs.UpdateInput
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		prefs, err := h.prefs.Update(r.Context(), input, email)
		switch {
		case errors.Is(err, serviceagentprefs.ErrInvalidPreferences):
			httptransport.SendErr(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, serviceagentprefs.ErrUnavailable):
			httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
		case err != nil:
			httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
		default:
			httptransport.SendJSON(w, http.StatusOK, prefs)
		}

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// requireAdmin answers 401 for an unauthenticated caller and 403 for a signed
// in non-admin, so a member can tell "sign in again" from "not for you".
func (h *AgentPreferencesHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.caller == nil {
		httptransport.SendErr(w, http.StatusForbidden, "admin only")
		return "", false
	}
	email, isAdmin, err := h.caller.EmailAndAdmin(r.Context(), r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	if !isAdmin {
		httptransport.SendErr(w, http.StatusForbidden, "admin only")
		return "", false
	}
	return email, true
}
