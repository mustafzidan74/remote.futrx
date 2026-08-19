package httphandlers

// The two halves of the MCP registry's HTTP surface:
//
//   - the platform registry under /api/admin/mcp-servers/*, admin-only,
//     because an entry there reaches every project in its scope and an MCP
//     server runs with the container's full privileges;
//   - the per-project view at /api/projects/{id}/mcp, reached only through
//     the project route that has already established membership, where a
//     member switches the available servers on or off and adds entries that
//     belong to their project alone.
//
// No route in this file returns a credential. The registry stores ${KEY}
// placeholders and the vault keys behind them, never a value, so there is
// nothing here to mask.

import (
	"context"
	"errors"
	"net/http"
	"strings"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// MCPService is the HTTP layer's narrow view of the registry.
type MCPService interface {
	List(ctx context.Context) ([]servicemcp.View, error)
	Create(ctx context.Context, server servicemcp.Server, actor string) (servicemcp.View, error)
	Update(ctx context.Context, name string, server servicemcp.Server, actor string) (servicemcp.View, error)
	Delete(ctx context.Context, name string) error
	Test(ctx context.Context, name, projectID string) (servicemcp.TestResult, error)
	ProjectSettings(ctx context.Context, projectID string) (servicemcp.ProjectView, error)
	SaveProjectSettings(ctx context.Context, projectID string, input servicemcp.ProjectInput, actor string) (servicemcp.ProjectView, error)
}

// MCPHandler serves /api/admin/mcp-servers and, through the project handler,
// /api/projects/{id}/mcp.
type MCPHandler struct {
	mcp    MCPService
	caller CallerResolver
}

func NewMCPHandler(mcp MCPService, auth *serviceauth.Service) *MCPHandler {
	return &MCPHandler{
		mcp:    mcp,
		caller: httptransport.NewPrincipalResolver(auth),
	}
}

func (h *MCPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/mcp-servers", h.handleCollection)
	mux.HandleFunc("/api/admin/mcp-servers/", h.handleItem)
}

// mcpServerRequest is the wire shape of a create or update. It mirrors the
// stored entry: there is no write-only field, because there is no secret.
type mcpServerRequest struct {
	Name        string            `json:"name"`
	Transport   string            `json:"transport"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Scope       servicemcp.Scope  `json:"scope"`
	Providers   []string          `json:"enabledForProviders"`
	Description string            `json:"description"`
	SecretRefs  []string          `json:"secretRefs"`
}

func (r mcpServerRequest) server() servicemcp.Server {
	return servicemcp.Server{
		Name:        strings.TrimSpace(r.Name),
		Transport:   servicemcp.Transport(strings.TrimSpace(r.Transport)),
		Command:     r.Command,
		Args:        r.Args,
		Env:         r.Env,
		URL:         r.URL,
		Headers:     r.Headers,
		Scope:       r.Scope,
		Providers:   r.Providers,
		Description: r.Description,
		SecretRefs:  r.SecretRefs,
	}
}

func (h *MCPHandler) handleCollection(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := h.mcp.List(r.Context())
		if err != nil {
			sendMCPError(w, err)
			return
		}
		if list == nil {
			list = []servicemcp.View{}
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]any{
			"servers":              list,
			"supportedProviders":   servicemcp.SupportedProviders(),
			"unsupportedProviders": servicemcp.UnsupportedProviders(),
		})

	case http.MethodPost:
		var body mcpServerRequest
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.mcp.Create(r.Context(), body.server(), email)
		if err != nil {
			sendMCPError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *MCPHandler) handleItem(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/mcp-servers/")
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	if name == "" {
		httptransport.SendErr(w, http.StatusBadRequest, "missing MCP server name")
		return
	}
	if len(parts) == 2 && parts[1] != "" {
		if parts[1] != "test" {
			httptransport.SendErr(w, http.StatusNotFound, "not found")
			return
		}
		h.handleTest(w, r, name)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var body mcpServerRequest
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.mcp.Update(r.Context(), name, body.server(), email)
		if err != nil {
			sendMCPError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	case http.MethodDelete:
		if err := h.mcp.Delete(r.Context(), name); err != nil {
			sendMCPError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleTest runs the probe inside one project's container. It is admin-only
// like the rest of this file: the probe starts a process of the admin's own
// choosing inside somebody else's project.
func (h *MCPHandler) handleTest(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		ProjectID string `json:"projectId"`
	}
	if err := readJSONBody(r, &body); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	result, err := h.mcp.Test(r.Context(), name, strings.TrimSpace(body.ProjectID))
	if err != nil {
		sendMCPError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// HandleProjectResource serves /api/projects/{id}/mcp.
//
// It is called from the project handler, which has already established that
// the caller is an administrator or a member of this project. Membership is
// the whole gate: everything a member can configure here, an agent in their
// own container could already start by hand, and the vault keys an entry may
// reference resolve only to values that project's container already receives.
func (h *MCPHandler) HandleProjectResource(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	rest string,
	email string,
) {
	if h == nil || h.mcp == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicemcp.ErrUnavailable.Error())
		return
	}
	if strings.Trim(rest, "/") != "" {
		httptransport.SendErr(w, http.StatusNotFound, "unknown MCP action")
		return
	}

	switch r.Method {
	case http.MethodGet:
		view, err := h.mcp.ProjectSettings(r.Context(), string(id))
		if err != nil {
			sendMCPError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, projectMCPPayload(view))

	case http.MethodPut:
		var body struct {
			Disabled []string           `json:"disabled"`
			Servers  []mcpServerRequest `json:"servers"`
		}
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		input := servicemcp.ProjectInput{Disabled: body.Disabled}
		for _, server := range body.Servers {
			input.Servers = append(input.Servers, server.server())
		}
		view, err := h.mcp.SaveProjectSettings(r.Context(), string(id), input, email)
		if err != nil {
			sendMCPError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, projectMCPPayload(view))

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// projectMCPPayload keeps the collections non-nil so the client never has to
// distinguish "no entries" from "field absent".
func projectMCPPayload(view servicemcp.ProjectView) servicemcp.ProjectView {
	if view.Available == nil {
		view.Available = []servicemcp.Entry{}
	}
	if view.SupportedProviders == nil {
		view.SupportedProviders = servicemcp.SupportedProviders()
	}
	if view.UnsupportedProviders == nil {
		view.UnsupportedProviders = servicemcp.UnsupportedProviders()
	}
	return view
}

// requireAdmin resolves the caller and refuses anyone who is not an admin.
func (h *MCPHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h == nil || h.mcp == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicemcp.ErrUnavailable.Error())
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

// sendMCPError maps registry errors onto status codes.
func sendMCPError(w http.ResponseWriter, err error) {
	var unresolved servicemcp.ErrUnresolvedSecret
	switch {
	case errors.Is(err, servicemcp.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, servicemcp.ErrExists):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, servicemcp.ErrUnavailable),
		errors.Is(err, servicemcp.ErrProbeFailed):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.As(err, &unresolved),
		errors.Is(err, servicemcp.ErrInvalidName),
		errors.Is(err, servicemcp.ErrInvalidKind),
		errors.Is(err, servicemcp.ErrNoCommand),
		errors.Is(err, servicemcp.ErrNoURL),
		errors.Is(err, servicemcp.ErrInvalidArg),
		errors.Is(err, servicemcp.ErrInvalidEnv),
		errors.Is(err, servicemcp.ErrInvalidHeader),
		errors.Is(err, servicemcp.ErrInvalidScope),
		errors.Is(err, servicemcp.ErrTooLarge),
		errors.Is(err, servicemcp.ErrProvider),
		errors.Is(err, servicemcp.ErrSecretRef),
		errors.Is(err, servicemcp.ErrNoProject):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
