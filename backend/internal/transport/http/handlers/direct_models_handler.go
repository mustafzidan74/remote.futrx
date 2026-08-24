package httphandlers

import (
	"context"
	"net/http"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	serviceprompt "github.com/futrx-com/remote.futrx.com/internal/service/prompt"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// DirectModelsService lists the completion-API models a chat may be pointed
// at. Only the read side is exposed: these models are configured elsewhere —
// the pool in AI providers, the local one in the auxiliary-model page — and
// this route exists so the composer can offer what is already switched on.
type DirectModelsService interface {
	Choices(ctx context.Context) []serviceprompt.DirectChoice
}

// DirectModelsHandler serves the composer's list.
type DirectModelsHandler struct {
	direct DirectModelsService
	caller CallerResolver
}

func NewDirectModelsHandler(direct DirectModelsService, auth *serviceauth.Service) *DirectModelsHandler {
	return &DirectModelsHandler{
		direct: direct,
		caller: httptransport.NewPrincipalResolver(auth),
	}
}

func (h *DirectModelsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/direct-models", h.handleChoices)
}

// handleChoices answers any signed-in user.
//
// Nothing here is a secret: a label, a model id, and which of the two sources
// it came from. Base URLs, keys and quota state stay on the admin routes,
// where they were before.
func (h *DirectModelsHandler) handleChoices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h == nil || h.direct == nil {
		// Not an error: a deployment with no providers and no local model
		// simply shows no direct section in the picker.
		httptransport.SendJSON(w, http.StatusOK, map[string]any{"models": []serviceprompt.DirectChoice{}})
		return
	}
	if email, _, err := h.caller.EmailAndAdmin(r.Context(), r); err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	choices := h.direct.Choices(r.Context())
	if choices == nil {
		choices = []serviceprompt.DirectChoice{}
	}
	httptransport.SendJSON(w, http.StatusOK, map[string]any{"models": choices})
}
