package httphandlers

import (
	"context"
	"errors"
	"net/http"

	servicerouting "github.com/futrx-com/remote.futrx.com/internal/service/routing"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// ModelRoutingService is the transport's narrow view of the routing policy.
type ModelRoutingService interface {
	View(ctx context.Context) (servicerouting.View, error)
	Update(ctx context.Context, policy servicerouting.Policy, actor string) (servicerouting.View, error)
	Test(ctx context.Context, in servicerouting.TestInput) (servicerouting.Decision, error)
}

// ModelRoutingHandler serves the automatic model routing policy.
//
// The three admin routes own the policy; the fourth, /api/model-routing/preview,
// is open to any signed-in user because the composer pill has to tell whoever
// is typing which model their next turn will use. It evaluates the inputs the
// caller supplies rather than looking a chat up, so it grants no read access
// to anything a caller did not already have.
type ModelRoutingHandler struct {
	routing ModelRoutingService
	caller  CallerResolver
}

func NewModelRoutingHandler(routing ModelRoutingService, caller CallerResolver) *ModelRoutingHandler {
	return &ModelRoutingHandler{routing: routing, caller: caller}
}

func (h *ModelRoutingHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/model-routing", h.handlePolicy)
	mux.HandleFunc("/api/admin/model-routing/test", h.handleTest)
	mux.HandleFunc("/api/model-routing/preview", h.handlePreview)
}

func (h *ModelRoutingHandler) handlePolicy(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) || !h.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		view, err := h.routing.View(r.Context())
		if err != nil {
			sendRoutingError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	case http.MethodPut:
		var policy servicerouting.Policy
		if err := readJSONBody(r, &policy); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		email, _, _ := h.caller.EmailAndAdmin(r.Context(), r)
		view, err := h.routing.Update(r.Context(), policy, email)
		if err != nil {
			sendRoutingError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ModelRoutingHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) || !h.requireAdmin(w, r) {
		return
	}
	h.decide(w, r)
}

func (h *ModelRoutingHandler) handlePreview(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	if h.caller != nil {
		email, _, err := h.caller.EmailAndAdmin(r.Context(), r)
		if err != nil || email == "" {
			httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
	}
	h.decide(w, r)
}

func (h *ModelRoutingHandler) decide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in servicerouting.TestInput
	if err := readJSONBody(r, &in); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	decision, err := h.routing.Test(r.Context(), in)
	if err != nil {
		sendRoutingError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, decision)
}

func (h *ModelRoutingHandler) available(w http.ResponseWriter) bool {
	if h == nil || h.routing == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "model routing is unavailable")
		return false
	}
	return true
}

func (h *ModelRoutingHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
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

func sendRoutingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicerouting.ErrInvalidPolicy):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, servicerouting.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
