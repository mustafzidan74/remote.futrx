package httphandlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicesnippets "github.com/futrx-com/remote.futrx.com/internal/service/snippets"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// SnippetService is the HTTP layer's view of the personal snippet library.
type SnippetService interface {
	List(ctx context.Context, owner servicesnippets.Owner) ([]servicesnippets.Snippet, error)
	Create(ctx context.Context, owner servicesnippets.Owner, input servicesnippets.Input) (servicesnippets.Snippet, error)
	Update(ctx context.Context, owner servicesnippets.Owner, id string, input servicesnippets.Input) (servicesnippets.Snippet, error)
	Delete(ctx context.Context, owner servicesnippets.Owner, id string) error
	MarkUsed(ctx context.Context, owner servicesnippets.Owner, id string) (servicesnippets.Snippet, error)
	Import(
		ctx context.Context,
		owner servicesnippets.Owner,
		incoming []servicesnippets.Snippet,
		replace bool,
	) ([]servicesnippets.Snippet, error)
}

// snippetSessionResolver resolves the caller's session. It exists so the
// owner-only gate can be exercised without building a full auth service.
type snippetSessionResolver interface {
	Session(r *http.Request) (*serviceauth.Session, error)
}

// SnippetHandler serves each user's own prompt library and client message
// templates under /api/me/snippets.
//
// Every route derives the owner from the session and never from the path, so
// there is no request shape that reads or writes somebody else's library: an
// id that belongs to another user is simply "not found" here.
type SnippetHandler struct {
	snippets SnippetService
	sessions snippetSessionResolver
}

func NewSnippetHandler(snippets SnippetService, auth *serviceauth.Service) *SnippetHandler {
	return &SnippetHandler{
		snippets: snippets,
		sessions: httptransport.NewPrincipalResolver(auth),
	}
}

func (h *SnippetHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/me/snippets", h.handleCollection)
	mux.HandleFunc("/api/me/snippets/", h.handleItem)
}

func (h *SnippetHandler) handleCollection(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.owner(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := h.snippets.List(r.Context(), owner)
		if err != nil {
			sendSnippetError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, snippetCollection(list))

	case http.MethodPost:
		var input servicesnippets.Input
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		item, err := h.snippets.Create(r.Context(), owner, input)
		if err != nil {
			sendSnippetError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusCreated, item)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleItem serves /api/me/snippets/{id}, /{id}/use, and the /import verb.
func (h *SnippetHandler) handleItem(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.owner(w, r)
	if !ok {
		return
	}

	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/me/snippets/"), "/")
	decoded, err := url.PathUnescape(rest)
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid snippet id")
		return
	}
	parts := strings.SplitN(decoded, "/", 2)
	id := strings.TrimSpace(parts[0])
	if id == "" {
		httptransport.SendErr(w, http.StatusBadRequest, "missing snippet id")
		return
	}

	if id == "import" {
		h.handleImport(w, r, owner)
		return
	}

	if len(parts) == 2 && strings.Trim(parts[1], "/") == "use" {
		h.handleUse(w, r, owner, id)
		return
	}
	if len(parts) == 2 && strings.Trim(parts[1], "/") != "" {
		httptransport.SendErr(w, http.StatusNotFound, "unknown snippet route")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var input servicesnippets.Input
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		item, err := h.snippets.Update(r.Context(), owner, id, input)
		if err != nil {
			sendSnippetError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, item)

	case http.MethodDelete:
		if err := h.snippets.Delete(r.Context(), owner, id); err != nil {
			sendSnippetError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]any{"deleted": id})

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *SnippetHandler) handleUse(
	w http.ResponseWriter,
	r *http.Request,
	owner servicesnippets.Owner,
	id string,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	item, err := h.snippets.MarkUsed(r.Context(), owner, id)
	if err != nil {
		sendSnippetError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, item)
}

func (h *SnippetHandler) handleImport(
	w http.ResponseWriter,
	r *http.Request,
	owner servicesnippets.Owner,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Snippets []servicesnippets.Snippet `json:"snippets"`
		Replace  bool                      `json:"replace"`
	}
	if err := readJSONBody(r, &body); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	list, err := h.snippets.Import(r.Context(), owner, body.Snippets, body.Replace)
	if err != nil {
		sendSnippetError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, snippetCollection(list))
}

// owner resolves the caller's library key, answering 401 when there is none.
func (h *SnippetHandler) owner(w http.ResponseWriter, r *http.Request) (servicesnippets.Owner, bool) {
	if h.snippets == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicesnippets.ErrUnavailable.Error())
		return "", false
	}
	if h.sessions == nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	session, err := h.sessions.Session(r)
	if err != nil || session == nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	owner, err := servicesnippets.OwnerFromSession(session.Email, session.Sub)
	if err != nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	return owner, true
}

func snippetCollection(list []servicesnippets.Snippet) map[string]any {
	if list == nil {
		list = []servicesnippets.Snippet{}
	}
	return map[string]any{"snippets": list}
}

func sendSnippetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicesnippets.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, servicesnippets.ErrInvalidSnippet):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, servicesnippets.ErrInvalidOwner):
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
	case errors.Is(err, servicesnippets.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
