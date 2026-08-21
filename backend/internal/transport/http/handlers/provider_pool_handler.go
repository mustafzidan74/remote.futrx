package httphandlers

// The free-tier provider pool's HTTP surface, in two halves:
//
//   - /api/admin/providers/*, admin-only, where the registry is edited. An
//     entry there holds a credential and decides where the platform's own
//     text jobs are sent, so nothing below admin touches it.
//   - /api/providers/*, any signed-in member: the quota card the home
//     dashboard draws, and the bulk lane every future "generate a lot of
//     text" feature is meant to go through.
//
// No route in this file returns a credential. An inline key comes back as a
// mask and a vault reference comes back as a key *name*, which was never
// secret.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	serviceproviderpool "github.com/futrx-com/remote.futrx.com/internal/service/providerpool"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// ProviderPoolService is the HTTP layer's narrow view of the pool.
type ProviderPoolService interface {
	View() serviceproviderpool.PoolView
	Quota() serviceproviderpool.QuotaView
	Save(ctx context.Context, input serviceproviderpool.ProviderInput, actor string) (serviceproviderpool.PoolView, error)
	Delete(ctx context.Context, id, actor string) (serviceproviderpool.PoolView, error)
	Reorder(ctx context.Context, ids []string, actor string) (serviceproviderpool.PoolView, error)
	SaveSettings(ctx context.Context, input serviceproviderpool.SettingsInput, actor string) (serviceproviderpool.PoolView, error)
	Test(ctx context.Context, id string) serviceproviderpool.TestResult
	Bulk(ctx context.Context, input serviceproviderpool.BulkInput) (serviceproviderpool.Result, error)
}

// bulkRateLimit and bulkRateWindow bound how often one member may spend the
// operator's free tiers through the bulk lane. It is generous enough for a
// feature looping over a product catalogue and tight enough that a stuck
// client cannot burn a day's quota in a minute.
const (
	bulkRateLimit  = 30
	bulkRateWindow = time.Minute
	// maxBulkBytes bounds one bulk request body. The service applies the real
	// ceiling in tokens; this is the cheaper guard in front of it.
	maxBulkBytes = 64 << 10
)

// ProviderPoolHandler serves both halves.
type ProviderPoolHandler struct {
	pool    ProviderPoolService
	caller  CallerResolver
	audit   serviceaudit.Recorder
	limiter *fixedWindowLimiter
}

func NewProviderPoolHandler(pool ProviderPoolService, auth *serviceauth.Service) *ProviderPoolHandler {
	return &ProviderPoolHandler{
		pool:    pool,
		caller:  httptransport.NewPrincipalResolver(auth),
		audit:   serviceaudit.Nop{},
		limiter: newFixedWindowLimiter(bulkRateLimit, bulkRateWindow),
	}
}

// WithAudit attaches the audit recorder, so a bulk completion is recorded
// against the member who asked for it.
func (h *ProviderPoolHandler) WithAudit(recorder serviceaudit.Recorder) *ProviderPoolHandler {
	if h != nil && recorder != nil {
		h.audit = recorder
	}
	return h
}

func (h *ProviderPoolHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/providers", h.handleCollection)
	mux.HandleFunc("/api/admin/providers/", h.handleItem)
	mux.HandleFunc("/api/providers/quota", h.handleQuota)
	mux.HandleFunc("/api/providers/complete", h.handleComplete)
}

/* ------------------------------------------------------------------ *
 * Admin: the registry
 * ------------------------------------------------------------------ */

// providerRequest is the wire shape of a create or update. It mirrors the
// stored entry except for the credential, which is write-only.
type providerRequest struct {
	ID          string                      `json:"id"`
	Label       string                      `json:"label"`
	Kind        string                      `json:"kind"`
	BaseURL     string                      `json:"baseUrl"`
	APIKeyRef   string                      `json:"apiKeyRef"`
	APIKey      string                      `json:"apiKey"`
	ClearAPIKey bool                        `json:"clearApiKey"`
	Models      []serviceproviderpool.Model `json:"models"`
	Limits      serviceproviderpool.Limits  `json:"limits"`
	Priority    int                         `json:"priority"`
	Enabled     bool                        `json:"enabled"`
	Notes       string                      `json:"notes"`
}

func (r providerRequest) input() serviceproviderpool.ProviderInput {
	return serviceproviderpool.ProviderInput{
		ID:          r.ID,
		Label:       r.Label,
		Kind:        r.Kind,
		BaseURL:     r.BaseURL,
		APIKeyRef:   r.APIKeyRef,
		APIKey:      r.APIKey,
		ClearAPIKey: r.ClearAPIKey,
		Models:      r.Models,
		Limits:      r.Limits,
		Priority:    r.Priority,
		Enabled:     r.Enabled,
		Notes:       r.Notes,
	}
}

// handleCollection serves the whole panel: GET reads it, POST creates or
// updates one provider, PUT stores the pool's own policy.
func (h *ProviderPoolHandler) handleCollection(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		httptransport.SendJSON(w, http.StatusOK, h.pool.View())

	case http.MethodPost:
		var body providerRequest
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.pool.Save(r.Context(), body.input(), email)
		if err != nil {
			sendProviderPoolError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	case http.MethodPut:
		var body serviceproviderpool.SettingsInput
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.pool.SaveSettings(r.Context(), body, email)
		if err != nil {
			sendProviderPoolError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleItem serves /api/admin/providers/{id}, /{id}/test, and the one
// reserved verb /reorder. The reserved ids are refused by the service's own
// validation, so a provider can never shadow this route.
func (h *ProviderPoolHandler) handleItem(w http.ResponseWriter, r *http.Request) {
	email, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/providers/")
	parts := strings.SplitN(rest, "/", 2)
	id := strings.ToLower(strings.TrimSpace(parts[0]))
	if id == "" {
		httptransport.SendErr(w, http.StatusBadRequest, "missing provider id")
		return
	}

	if id == "reorder" {
		if r.Method != http.MethodPost {
			httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		view, err := h.pool.Reorder(r.Context(), body.IDs, email)
		if err != nil {
			sendProviderPoolError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)
		return
	}

	if len(parts) == 2 && strings.Trim(parts[1], "/") == "test" {
		if r.Method != http.MethodPost {
			httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// Like the auxiliary model's probe, a failed test answers 200 with
		// the failure inside the body: "your provider refused us" is the
		// answer an operator wants to read, not a 500.
		httptransport.SendJSON(w, http.StatusOK, h.pool.Test(r.Context(), id))
		return
	}

	switch r.Method {
	case http.MethodPut:
		var body providerRequest
		if err := readJSONBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		// The path wins over the body, so a mismatched id renames nothing.
		body.ID = id
		view, err := h.pool.Save(r.Context(), body.input(), email)
		if err != nil {
			sendProviderPoolError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	case http.MethodDelete:
		view, err := h.pool.Delete(r.Context(), id, email)
		if err != nil {
			sendProviderPoolError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, view)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

/* ------------------------------------------------------------------ *
 * Members: the quota card and the bulk lane
 * ------------------------------------------------------------------ */

// handleQuota serves the home dashboard's "Free quota" card. Any signed-in
// user may read it; it carries labels and meters, no endpoint and no key.
func (h *ProviderPoolHandler) handleQuota(w http.ResponseWriter, r *http.Request) {
	if !h.requireMember(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	httptransport.SendJSON(w, http.StatusOK, h.pool.Quota())
}

// handleComplete is the bulk lane: one entry point for every future feature
// that wants a lot of cheap text, so none of them grows its own key handling.
func (h *ProviderPoolHandler) handleComplete(w http.ResponseWriter, r *http.Request) {
	email, ok := h.member(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.limiter.allow(email) {
		httptransport.SendErr(w, http.StatusTooManyRequests,
			"too many bulk completions — slow down and try again shortly")
		return
	}

	var body struct {
		Job         string `json:"job"`
		Prompt      string `json:"prompt"`
		System      string `json:"system"`
		MaxTokens   int    `json:"maxTokens"`
		ProviderID  string `json:"providerId"`
		PreferModel string `json:"model"`
	}
	if err := readJSONBodySized(r, &body, maxBulkBytes); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	// The job field exists so this route can grow other lanes later without
	// a second endpoint. Today there is exactly one, and anything else is a
	// caller mistake worth naming rather than silently treating as bulk.
	if job := strings.TrimSpace(body.Job); job != "" && job != serviceproviderpool.BulkJob {
		httptransport.SendErr(w, http.StatusBadRequest,
			`the only job this route takes is "bulk"`)
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		httptransport.SendErr(w, http.StatusBadRequest, "there is nothing to send")
		return
	}

	result, err := h.pool.Bulk(r.Context(), serviceproviderpool.BulkInput{
		Prompt:      body.Prompt,
		System:      body.System,
		MaxTokens:   body.MaxTokens,
		ProviderID:  body.ProviderID,
		PreferModel: body.PreferModel,
	})
	h.recordBulk(r, result, err)
	if err != nil {
		sendProviderPoolError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// recordBulk writes the audit line. It carries the provider, the model and
// the token counts — never the prompt and never the answer, which is the
// same rule the rest of the audit log follows for user content.
func (h *ProviderPoolHandler) recordBulk(
	r *http.Request,
	result serviceproviderpool.Result,
	err error,
) {
	if h.audit == nil {
		return
	}
	meta := serviceaudit.Meta{
		"provider":         result.ProviderID,
		"model":            result.Model,
		"promptTokens":     result.PromptTokens,
		"completionTokens": result.CompletionTokens,
		"failovers":        result.Failovers,
	}
	h.audit.Record(r.Context(), serviceaudit.Result(
		serviceaudit.ActionProviderComplete,
		serviceaudit.Target{Type: serviceaudit.TargetProvider, ID: result.ProviderID},
		meta,
		err,
	))
}

/* ------------------------------------------------------------------ *
 * Gates and errors
 * ------------------------------------------------------------------ */

// requireAdmin gates the registry routes. It writes the failure itself and
// reports whether the caller may proceed.
func (h *ProviderPoolHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h == nil || h.pool == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "the provider pool is unavailable")
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

// member resolves the signed-in caller for the two member routes.
func (h *ProviderPoolHandler) member(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h == nil || h.pool == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "the provider pool is unavailable")
		return "", false
	}
	email, _, err := h.caller.EmailAndAdmin(r.Context(), r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	return email, true
}

func (h *ProviderPoolHandler) requireMember(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.member(w, r)
	return ok
}

// sendProviderPoolError maps a failure onto a status an operator or a caller
// can act on.
func sendProviderPoolError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serviceproviderpool.ErrInvalidProvider):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, serviceproviderpool.ErrUnknownProvider):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, serviceproviderpool.ErrPromptTooLarge):
		httptransport.SendErr(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, serviceproviderpool.ErrEmptyPrompt):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, serviceproviderpool.ErrNoProvider):
		// 503 rather than 502: nothing is broken, the pool simply has nothing
		// left to spend, and the caller should fall back rather than retry.
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
