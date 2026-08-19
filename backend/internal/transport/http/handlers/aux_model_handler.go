package httphandlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// AuxModelService is the HTTP layer's narrow view of the auxiliary model.
// Only masked or member-safe configuration crosses this boundary: the stored
// key and the raw configuration never leave the service package.
type AuxModelService interface {
	PublicConfig() serviceauxmodel.PublicConfig
	ClientConfig() serviceauxmodel.ClientConfig
	Save(ctx context.Context, input serviceauxmodel.UpdateInput) (serviceauxmodel.PublicConfig, error)
	Test(ctx context.Context) serviceauxmodel.TestResult
	Translate(ctx context.Context, target serviceauxmodel.TranslationTarget, text string) (string, error)
}

// auxModelCaller resolves the authenticated principal. It exists so the auth
// gates can be exercised without building a full auth service.
type auxModelCaller interface {
	EmailAndAdmin(ctx context.Context, r *http.Request) (string, bool, error)
}

// maxTranslationBytes bounds one translation request body. It is a little
// over the snippet body limit so any template a user can save can also be
// translated, and small enough that this route cannot be used to push a book
// through somebody's local model.
const maxTranslationBytes = 32 << 10

// AuxModelHandler serves the auxiliary model's three audiences: the admin
// panel that configures it, every signed-in user asking whether a given job
// is available at all, and the one job a person triggers by hand — translating
// a client message.
type AuxModelHandler struct {
	aux    AuxModelService
	caller auxModelCaller
}

func NewAuxModelHandler(aux AuxModelService, auth *serviceauth.Service) *AuxModelHandler {
	return &AuxModelHandler{
		aux:    aux,
		caller: httptransport.NewPrincipalResolver(auth),
	}
}

func (h *AuxModelHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/aux-model/config", h.handleClientConfig)
	mux.HandleFunc("/api/aux-model/translate", h.handleTranslate)
	mux.HandleFunc("/api/admin/aux-model", h.handleSettings)
	mux.HandleFunc("/api/admin/aux-model/test", h.handleTest)
}

// handleClientConfig tells the browser which buttons are worth rendering. Any
// signed-in user may read it; it names no endpoint, no model, and no key.
func (h *AuxModelHandler) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	httptransport.SendJSON(w, http.StatusOK, h.aux.ClientConfig())
}

// handleTranslate renders one client message in the other language.
//
// It is the only auxiliary-model job with no silent fallback, because it is
// the only one a person asks for directly: a button that quietly did nothing
// would be worse than an error saying the model is unavailable.
func (h *AuxModelHandler) handleTranslate(w http.ResponseWriter, r *http.Request) {
	if !h.authenticate(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Text   string `json:"text"`
		Target string `json:"target"`
	}
	if err := readJSONBodySized(r, &body, maxTranslationBytes); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		httptransport.SendErr(w, http.StatusBadRequest, "there is nothing to translate")
		return
	}
	target := serviceauxmodel.NormalizeTarget(body.Target)
	text, err := h.aux.Translate(r.Context(), target, body.Text)
	if err != nil {
		sendAuxModelFailure(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, map[string]string{
		"text":   text,
		"target": string(target),
	})
}

func (h *AuxModelHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		httptransport.SendJSON(w, http.StatusOK, h.aux.PublicConfig())
	case http.MethodPut:
		var input serviceauxmodel.UpdateInput
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid request")
			return
		}
		config, err := h.aux.Save(r.Context(), input)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, serviceauxmodel.ErrInvalidConfig) {
				status = http.StatusBadRequest
			}
			httptransport.SendErr(w, status, err.Error())
			return
		}
		httptransport.SendJSON(w, http.StatusOK, config)
	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleTest runs one real completion and reports the round trip. Like the
// monitoring ping it answers 200 with the failure inside the body: "your
// endpoint refused us" is the answer an operator wants to read, not a 500.
func (h *AuxModelHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	httptransport.SendJSON(w, http.StatusOK, h.aux.Test(r.Context()))
}

// readJSONBodySized is readJSONBody with a caller-chosen ceiling. A client
// message is longer than the 64 KB every other settings body fits in, and the
// ceiling is what keeps this route from becoming a way to hand somebody's
// local model an arbitrarily large document.
func readJSONBodySized(r *http.Request, v any, limit int64) error {
	body := http.MaxBytesReader(nil, r.Body, limit)
	defer body.Close()
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

// authenticate gates the member-facing routes: a session is enough.
func (h *AuxModelHandler) authenticate(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.aux == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "the auxiliary model is unavailable")
		return false
	}
	email, _, err := h.caller.EmailAndAdmin(r.Context(), r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	return true
}

// authorize gates the admin routes. It writes the failure response itself and
// reports whether the caller may proceed.
func (h *AuxModelHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.aux == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "the auxiliary model is unavailable")
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

// sendAuxModelFailure maps a job failure onto a status an operator can act
// on: 503 when the model is off or being left alone, 502 when the endpoint
// itself misbehaved.
func sendAuxModelFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serviceauxmodel.ErrDisabled):
		httptransport.SendErr(w, http.StatusServiceUnavailable,
			"the auxiliary model is not switched on for this job")
	case errors.Is(err, serviceauxmodel.ErrBreakerOpen):
		httptransport.SendErr(w, http.StatusServiceUnavailable,
			"the auxiliary model is paused after repeated failures — check Settings → Local / auxiliary model")
	case errors.Is(err, serviceauxmodel.ErrEmptyInput):
		// The wrapped text names what was missing, which is the whole value of
		// this branch: "this chat has no message to name it after yet" is
		// something a person can act on.
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	default:
		httptransport.SendErr(w, http.StatusBadGateway, err.Error())
	}
}
