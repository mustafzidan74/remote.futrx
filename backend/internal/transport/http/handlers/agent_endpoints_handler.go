package httphandlers

// The HTTP surface of the third-party agent endpoint register.
//
// It has two halves with deliberately different gates:
//
//   - /api/admin/agent-endpoints/* is admin-only. An entry there decides
//     which outside company sees a client's source code, and the Test action
//     starts an agent CLI inside somebody else's project container.
//   - /api/agent-endpoints is readable by any signed-in user, because the
//     composer has to list what a chat may be pointed at. It returns labels,
//     CLIs, and model ids only — never a base URL, a key reference, or a key.
//
// No route in this file returns a credential. The register stores the *name*
// of a Secrets-vault key and never its value, so there is nothing here to
// mask; the one place a value exists is the run and probe path inside the
// service.

import (
	"context"
	"errors"
	"net/http"
	"strings"

	serviceendpoints "github.com/futrx-com/remote.futrx.com/internal/service/agentendpoints"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// AgentEndpointsService is the HTTP layer's narrow view of the register.
type AgentEndpointsService interface {
	List(ctx context.Context) ([]serviceendpoints.View, error)
	Choices(ctx context.Context) ([]serviceendpoints.Choice, error)
	Create(ctx context.Context, endpoint serviceendpoints.Endpoint, actor string) (serviceendpoints.View, error)
	Update(ctx context.Context, id string, endpoint serviceendpoints.Endpoint, actor string) (serviceendpoints.View, error)
	Delete(ctx context.Context, id string) error
	SetEnabled(ctx context.Context, id string, enabled bool, actor string) (serviceendpoints.View, error)
	Test(ctx context.Context, id, projectID, model string) (serviceendpoints.TestResult, error)
}

// AgentEndpointsHandler serves both halves described above.
type AgentEndpointsHandler struct {
	endpoints AgentEndpointsService
	caller    CallerResolver
}

func NewAgentEndpointsHandler(
	endpoints AgentEndpointsService,
	auth *serviceauth.Service,
) *AgentEndpointsHandler {
	return &AgentEndpointsHandler{
		endpoints: endpoints,
		caller:    httptransport.NewPrincipalResolver(auth),
	}
}

func (h *AgentEndpointsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/agent-endpoints", h.handleChoices)
	mux.HandleFunc("/api/admin/agent-endpoints", h.handleCollection)
	mux.HandleFunc("/api/admin/agent-endpoints/", h.handleItem)
}

// agentEndpointRequest is the wire shape of a create or update. It mirrors the
// stored profile: there is no write-only field, because there is no secret.
type agentEndpointRequest struct {
	ID        string                   `json:"id"`
	Label     string                   `json:"label"`
	CLI       string                   `json:"cli"`
	BaseURL   string                   `json:"baseUrl"`
	APIKeyRef string                   `json:"apiKeyRef"`
	Models    []serviceendpoints.Model `json:"models"`
	Headers   map[string]string        `json:"headers"`
	WireAPI   string                   `json:"wireApi"`
	Notes     string                   `json:"notes"`
	Enabled   bool                     `json:"enabled"`
}

func (r agentEndpointRequest) endpoint() serviceendpoints.Endpoint {
	return serviceendpoints.Endpoint{
		ID:        strings.TrimSpace(r.ID),
		Label:     r.Label,
		CLI:       serviceendpoints.CLI(strings.TrimSpace(r.CLI)),
		BaseURL:   r.BaseURL,
		APIKeyRef: r.APIKeyRef,
		Models:    r.Models,
		Headers:   r.Headers,
		WireAPI:   r.WireAPI,
		Notes:     r.Notes,
		Enabled:   r.Enabled,
	}
}

// handleChoices serves the composer's read: what a chat may be pointed at.
//
// Any signed-in user may call it. It names which vendors and models this
// deployment offers — the same thing the composer's picker would show — and
// nothing about how to reach them.
func (h *AgentEndpointsHandler) handleChoices(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.endpoints == nil {
		// A deployment without the register is not an error for this route:
		// the composer simply shows no third-party section.
		httptransport.SendJSON(w, http.StatusOK, map[string]any{
			"endpoints":       []serviceendpoints.Choice{},
			"supportedCLIs":   serviceendpoints.SupportedCLIs(),
			"unsupportedCLIs": serviceendpoints.UnsupportedCLIs(),
		})
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	choices, err := h.endpoints.Choices(r.Context())
	if err != nil {
		sendAgentEndpointError(w, err)
		return
	}
	if choices == nil {
		choices = []serviceendpoints.Choice{}
	}
	httptransport.SendJSON(w, http.StatusOK, map[string]any{
		"endpoints":       choices,
		"supportedCLIs":   serviceendpoints.SupportedCLIs(),
		"unsupportedCLIs": serviceendpoints.UnsupportedCLIs(),
	})
}

func (h *AgentEndpointsHandler) handleCollection(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := h.endpoints.List(r.Context())
		if err != nil {
			sendAgentEndpointError(w, err)
			return
		}
		if list == nil {
			list = []serviceendpoints.View{}
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]any{
			"endpoints":       list,
			"supportedCLIs":   serviceendpoints.SupportedCLIs(),
			"unsupportedCLIs": serviceendpoints.UnsupportedCLIs(),
			"wireApis":        serviceendpoints.WireAPIs(),
		})

	case http.MethodPost:
		var body agentEndpointRequest
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.endpoints.Create(r.Context(), body.endpoint(), email)
		if err != nil {
			sendAgentEndpointError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *AgentEndpointsHandler) handleItem(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/agent-endpoints/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		httptransport.SendErr(w, http.StatusBadRequest, "missing agent endpoint id")
		return
	}
	if len(parts) == 2 && parts[1] != "" {
		switch parts[1] {
		case "test":
			h.handleTest(w, r, id)
		case "enabled":
			h.handleEnabled(w, r, id, email)
		default:
			httptransport.SendErr(w, http.StatusNotFound, "not found")
		}
		return
	}

	switch r.Method {
	case http.MethodPut:
		var body agentEndpointRequest
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.endpoints.Update(r.Context(), id, body.endpoint(), email)
		if err != nil {
			sendAgentEndpointError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	case http.MethodDelete:
		if err := h.endpoints.Delete(r.Context(), id); err != nil {
			sendAgentEndpointError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleEnabled flips one profile's switch without restating the rest of it.
func (h *AgentEndpointsHandler) handleEnabled(
	w http.ResponseWriter,
	r *http.Request,
	id, email string,
) {
	if r.Method != http.MethodPut {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSONBody(r, &body); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	view, err := h.endpoints.SetEnabled(r.Context(), id, body.Enabled, email)
	if err != nil {
		sendAgentEndpointError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, view)
}

// handleTest runs a two-word prompt through the real CLI inside one project's
// container. It is admin-only like the rest of the admin half: the probe
// starts an agent process of the admin's choosing inside somebody else's
// project, and it spends the operator's money at a third party.
func (h *AgentEndpointsHandler) handleTest(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		ProjectID string `json:"projectId"`
		Model     string `json:"model"`
	}
	if err := readJSONBody(r, &body); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	result, err := h.endpoints.Test(
		r.Context(),
		id,
		strings.TrimSpace(body.ProjectID),
		strings.TrimSpace(body.Model),
	)
	if err != nil {
		sendAgentEndpointError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// requireAdmin resolves the caller and refuses anyone who is not an admin.
func (h *AgentEndpointsHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h == nil || h.endpoints == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, serviceendpoints.ErrUnavailable.Error())
		return "", false
	}
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

// sendAgentEndpointError maps register errors onto status codes.
func sendAgentEndpointError(w http.ResponseWriter, err error) {
	var unresolved serviceendpoints.ErrKeyUnresolved
	switch {
	case errors.Is(err, serviceendpoints.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, serviceendpoints.ErrExists):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, serviceendpoints.ErrUnavailable),
		errors.Is(err, serviceendpoints.ErrProbeFailed):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.As(err, &unresolved),
		errors.Is(err, serviceendpoints.ErrInvalidID),
		errors.Is(err, serviceendpoints.ErrInvalidLabel),
		errors.Is(err, serviceendpoints.ErrInvalidCLI),
		errors.Is(err, serviceendpoints.ErrInvalidURL),
		errors.Is(err, serviceendpoints.ErrInvalidKeyRef),
		errors.Is(err, serviceendpoints.ErrInvalidModel),
		errors.Is(err, serviceendpoints.ErrInvalidHeader),
		errors.Is(err, serviceendpoints.ErrDisabled),
		errors.Is(err, serviceendpoints.ErrTooLarge),
		errors.Is(err, serviceendpoints.ErrNoProject):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
