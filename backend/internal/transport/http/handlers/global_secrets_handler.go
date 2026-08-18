package httphandlers

// Admin HTTP surface for the platform secrets vault. Everything here is
// admin-only: a vault entry reaches every project in its scope, and any agent
// running in one of those containers can read it.
//
// Values are write-only. A GET returns a mask and metadata, a write with a
// blank value keeps whatever is stored, and only an explicit `clear` removes
// it. No handler in this file puts a value in a response, an error, or a log.

import (
	"context"
	"errors"
	"net/http"
	"strings"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// GlobalSecretsService is the HTTP layer's narrow view of the vault.
type GlobalSecretsService interface {
	List(ctx context.Context) ([]serviceglobalsecrets.View, error)
	Create(ctx context.Context, input serviceglobalsecrets.Input, actor string) (serviceglobalsecrets.View, error)
	Update(ctx context.Context, key string, input serviceglobalsecrets.Input, actor string) (serviceglobalsecrets.View, error)
	Delete(ctx context.Context, key string) error
	TestSSH(ctx context.Context, key string) (serviceglobalsecrets.TestResult, error)
}

// GlobalSecretsHandler serves /api/admin/secrets.
type GlobalSecretsHandler struct {
	secrets GlobalSecretsService
	caller  CallerResolver
}

func NewGlobalSecretsHandler(secrets GlobalSecretsService, auth *serviceauth.Service) *GlobalSecretsHandler {
	return &GlobalSecretsHandler{
		secrets: secrets,
		caller:  httptransport.NewPrincipalResolver(auth),
	}
}

func (h *GlobalSecretsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/secrets", h.handleCollection)
	mux.HandleFunc("/api/admin/secrets/", h.handleItem)
}

// secretRequest is the wire shape of a create or update. Value is optional on
// every write; Clear is the only way to remove a stored value.
type secretRequest struct {
	Key         string                          `json:"key"`
	Kind        string                          `json:"kind"`
	Value       string                          `json:"value"`
	Path        string                          `json:"path"`
	SSH         *serviceglobalsecrets.SSHTarget `json:"ssh"`
	Scope       serviceglobalsecrets.Scope      `json:"scope"`
	Description string                          `json:"description"`
	Clear       bool                            `json:"clear"`
}

func (r secretRequest) input() serviceglobalsecrets.Input {
	return serviceglobalsecrets.Input{
		Key:         strings.TrimSpace(r.Key),
		Kind:        serviceglobalsecrets.Kind(strings.TrimSpace(r.Kind)),
		Value:       r.Value,
		Path:        r.Path,
		SSH:         r.SSH,
		Scope:       r.Scope,
		Description: r.Description,
		Clear:       r.Clear,
	}
}

func (h *GlobalSecretsHandler) handleCollection(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := h.secrets.List(r.Context())
		if err != nil {
			sendSecretError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, secretCollection(list))

	case http.MethodPost:
		var body secretRequest
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.secrets.Create(r.Context(), body.input(), email)
		if err != nil {
			sendSecretError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *GlobalSecretsHandler) handleItem(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/secrets/")
	parts := strings.SplitN(rest, "/", 2)
	key := parts[0]
	if key == "" {
		httptransport.SendErr(w, http.StatusBadRequest, "missing secret key")
		return
	}
	if len(parts) == 2 && parts[1] != "" {
		if parts[1] != "test" {
			httptransport.SendErr(w, http.StatusNotFound, "not found")
			return
		}
		h.handleTest(w, r, key)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var body secretRequest
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.secrets.Update(r.Context(), key, body.input(), email)
		if err != nil {
			sendSecretError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	case http.MethodDelete:
		if err := h.secrets.Delete(r.Context(), key); err != nil {
			sendSecretError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *GlobalSecretsHandler) handleTest(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.secrets.TestSSH(r.Context(), key)
	if err != nil {
		sendSecretError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// requireAdmin resolves the caller and refuses anyone who is not an admin.
// The vault has no member-facing route: members see what their own project
// inherits through /api/projects/{id}/secrets instead.
func (h *GlobalSecretsHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.secrets == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "secrets vault is unavailable")
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

func secretCollection(list []serviceglobalsecrets.View) map[string]any {
	if list == nil {
		list = []serviceglobalsecrets.View{}
	}
	return map[string]any{"secrets": list}
}

// sendSecretError maps vault errors onto status codes. Every message here is
// about shape or identity, never about a value.
func sendSecretError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serviceglobalsecrets.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, "secret not found")
	case errors.Is(err, serviceglobalsecrets.ErrExists):
		httptransport.SendErr(w, http.StatusConflict, "a secret with that key already exists")
	case errors.Is(err, serviceglobalsecrets.ErrUnavailable),
		errors.Is(err, serviceglobalsecrets.ErrProbeUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, serviceglobalsecrets.ErrInvalidKey),
		errors.Is(err, serviceglobalsecrets.ErrInvalidKind),
		errors.Is(err, serviceglobalsecrets.ErrInvalidPath),
		errors.Is(err, serviceglobalsecrets.ErrInvalidScope),
		errors.Is(err, serviceglobalsecrets.ErrInvalidSSHTarget),
		errors.Is(err, serviceglobalsecrets.ErrInvalidKnownHosts),
		errors.Is(err, serviceglobalsecrets.ErrMultilineEnvValue),
		errors.Is(err, serviceglobalsecrets.ErrValueTooLarge),
		errors.Is(err, serviceglobalsecrets.ErrWrongKind),
		errors.Is(err, serviceglobalsecrets.ErrNoValue):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
