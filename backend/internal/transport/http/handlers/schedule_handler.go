package httphandlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	serviceschedulecapability "github.com/futrx-com/remote.futrx.com/internal/service/schedulecapability"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

const scheduleRequestLimit = 64 << 10

// ScheduleService is the HTTP layer's intentionally narrow view of the
// scheduled-task service.
type ScheduleService interface {
	List(ctx context.Context, callerEmail string, isAdmin bool) ([]serviceschedule.Task, error)
	Get(
		ctx context.Context,
		id serviceschedule.ID,
		callerEmail string,
		isAdmin bool,
	) (serviceschedule.Task, error)
	Create(
		ctx context.Context,
		input serviceschedule.CreateInput,
		ownerEmail string,
		isAdmin bool,
	) (serviceschedule.Task, error)
	Update(
		ctx context.Context,
		id serviceschedule.ID,
		input serviceschedule.UpdateInput,
		callerEmail string,
		isAdmin bool,
	) (serviceschedule.Task, error)
	Delete(
		ctx context.Context,
		id serviceschedule.ID,
		callerEmail string,
		isAdmin bool,
	) error
	RunNow(
		ctx context.Context,
		id serviceschedule.ID,
		callerEmail string,
		isAdmin bool,
	) (serviceschedule.Task, error)
	CompleteClaim(
		ctx context.Context,
		id serviceschedule.ID,
		activeRunID string,
	) (serviceschedule.Task, error)
	History(
		ctx context.Context,
		id serviceschedule.ID,
		callerEmail string,
		isAdmin bool,
	) ([]serviceschedule.RunRecord, error)
	RunDiff(
		ctx context.Context,
		id serviceschedule.ID,
		runID string,
		callerEmail string,
		isAdmin bool,
	) (serviceschedule.RunDiff, error)
}

type ScheduleCapabilityResolver interface {
	Resolve(token string) (serviceschedulecapability.Grant, error)
}

type ScheduleHandler struct {
	schedules    ScheduleService
	capabilities ScheduleCapabilityResolver
	auth         *serviceauth.Service
}

func NewScheduleHandler(
	schedules ScheduleService,
	capabilities ScheduleCapabilityResolver,
	auth *serviceauth.Service,
) *ScheduleHandler {
	return &ScheduleHandler{
		schedules:    schedules,
		capabilities: capabilities,
		auth:         auth,
	}
}

func (h *ScheduleHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/schedules/", h.HandleUserResource)
	mux.HandleFunc("/agent-api/schedules", h.HandleAgentCollection)
	mux.HandleFunc("/agent-api/schedules/", h.HandleAgentResource)
}

// HandleChatCollection is called by ChatHandler only after it has resolved
// the chat and authorized the caller. This keeps /api/chats/{id}/schedules
// under the existing chat route without registering an overlapping ServeMux
// pattern.
func (h *ScheduleHandler) HandleChatCollection(
	w http.ResponseWriter,
	r *http.Request,
	chat servicechat.Meta,
	callerEmail string,
	isAdmin bool,
) {
	if h == nil || h.schedules == nil {
		httptransport.SendErr(w, http.StatusNotFound, "not found")
		return
	}
	if chat.ProjectID == "" {
		sendScheduleError(w, serviceschedule.ErrProjectRequired)
		return
	}

	switch r.Method {
	case http.MethodGet:
		tasks, err := h.schedules.List(r.Context(), callerEmail, isAdmin)
		if err != nil {
			sendScheduleError(w, err)
			return
		}
		httptransport.SendJSON(
			w,
			http.StatusOK,
			scheduleResponses(filterSchedulesForChat(tasks, chat.ID)),
		)

	case http.MethodPost:
		input, err := decodeScheduleCreate(r)
		if err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, err.Error())
			return
		}
		input.ChatID = chat.ID
		input.ProjectID = serviceproject.ID(chat.ProjectID)
		task, err := h.schedules.Create(r.Context(), input, callerEmail, isAdmin)
		if err != nil {
			sendScheduleError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusCreated, newScheduleResponse(task))

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ScheduleHandler) HandleUserResource(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.schedules == nil {
		httptransport.SendErr(w, http.StatusNotFound, "not found")
		return
	}
	callerEmail, isAdmin, err := h.caller(r)
	if err != nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id, action, rest, ok := parseScheduleResourcePath(r.URL.Path, "/api/schedules/")
	if !ok {
		httptransport.SendErr(w, http.StatusNotFound, "not found")
		return
	}
	// History is a read-only view of the same task, so it hangs off the user
	// API only: an agent grant has no business paging a task's past runs.
	if action == "history" {
		h.handleHistory(w, r, id, rest, callerEmail, isAdmin)
		return
	}
	h.handleResource(w, r, id, action, callerEmail, isAdmin, callerKindUser)
}

// handleHistory serves GET /api/schedules/{id}/history and
// GET /api/schedules/{id}/history/{runId}/diff.
func (h *ScheduleHandler) handleHistory(
	w http.ResponseWriter,
	r *http.Request,
	id serviceschedule.ID,
	rest []string,
	callerEmail string,
	isAdmin bool,
) {
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch {
	case len(rest) == 0:
		records, err := h.schedules.History(r.Context(), id, callerEmail, isAdmin)
		if err != nil {
			sendScheduleError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, scheduleHistoryResponse(records))

	case len(rest) == 2 && rest[1] == "diff":
		diff, err := h.schedules.RunDiff(r.Context(), id, rest[0], callerEmail, isAdmin)
		if err != nil {
			sendScheduleError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, diff)

	default:
		httptransport.SendErr(w, http.StatusNotFound, "not found")
	}
}

func (h *ScheduleHandler) HandleAgentCollection(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/agent-api/schedules" {
		httptransport.SendErr(w, http.StatusNotFound, "not found")
		return
	}
	grant, ok := h.resolveAgentGrant(w, r)
	if !ok {
		return
	}
	if grant.Scope != serviceschedulecapability.ScopeManage {
		httptransport.SendErr(w, http.StatusForbidden, "schedule capability does not allow management")
		return
	}

	switch r.Method {
	case http.MethodGet:
		tasks, err := h.schedules.List(
			r.Context(),
			grant.OwnerEmail,
			grant.IsAdmin,
		)
		if err != nil {
			sendScheduleError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, scheduleResponses(filterSchedulesForGrant(tasks, grant)))

	case http.MethodPost:
		input, err := decodeScheduleCreate(r)
		if err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, err.Error())
			return
		}
		input.ChatID = grant.ChatID
		input.ProjectID = grant.ProjectID
		// The enforced arm step: agent-minted tasks start disabled and wait
		// for a user to arm them from the Schedules drawer.
		input.CreatedByAgent = true
		task, err := h.schedules.Create(
			r.Context(),
			input,
			grant.OwnerEmail,
			grant.IsAdmin,
		)
		if err != nil {
			sendScheduleError(w, err)
			return
		}
		if !taskMatchesGrant(task, grant) {
			httptransport.SendErr(w, http.StatusForbidden, "scheduled task is outside this capability")
			return
		}
		httptransport.SendJSON(w, http.StatusCreated, newScheduleResponse(task))

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ScheduleHandler) HandleAgentResource(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.resolveAgentGrant(w, r)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/agent-api/schedules/")
	if rest == "current/complete" {
		h.handleAgentComplete(w, r, grant)
		return
	}
	if grant.Scope != serviceschedulecapability.ScopeManage {
		httptransport.SendErr(w, http.StatusForbidden, "schedule capability does not allow management")
		return
	}

	id, action, tail, valid := parseScheduleResourcePath(
		r.URL.Path,
		"/agent-api/schedules/",
	)
	if !valid || len(tail) > 0 {
		httptransport.SendErr(w, http.StatusNotFound, "not found")
		return
	}
	task, err := h.schedules.Get(
		r.Context(),
		id,
		grant.OwnerEmail,
		grant.IsAdmin,
	)
	if err != nil {
		sendScheduleError(w, err)
		return
	}
	if !taskMatchesGrant(task, grant) {
		httptransport.SendErr(w, http.StatusForbidden, "scheduled task is outside this capability")
		return
	}
	h.handleResource(w, r, id, action, grant.OwnerEmail, grant.IsAdmin, callerKindAgent)
}

// callerKind separates the human session API from the agent capability API so
// agent turns keep a narrower write surface.
type callerKind int

const (
	callerKindUser callerKind = iota
	callerKindAgent
)

func (h *ScheduleHandler) handleAgentComplete(
	w http.ResponseWriter,
	r *http.Request,
	grant serviceschedulecapability.Grant,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if grant.Scope != serviceschedulecapability.ScopeCompleteSelf ||
		grant.ScheduledTaskID == "" ||
		grant.ScheduledRunID == "" {
		httptransport.SendErr(w, http.StatusForbidden, "schedule capability cannot complete this task")
		return
	}
	id := serviceschedule.ID(grant.ScheduledTaskID)
	// The capability itself is already locked to this owner, chat, project,
	// and task. Bypass a fresh membership check so an authorized scheduled
	// run owned by an administrator can still complete itself.
	task, err := h.schedules.Get(r.Context(), id, grant.OwnerEmail, true)
	if err != nil {
		sendScheduleError(w, err)
		return
	}
	if !taskMatchesGrant(task, grant) {
		httptransport.SendErr(w, http.StatusForbidden, "scheduled task is outside this capability")
		return
	}
	task, err = h.schedules.CompleteClaim(
		r.Context(),
		id,
		grant.ScheduledRunID,
	)
	if err != nil {
		sendScheduleError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, newScheduleResponse(task))
}

func (h *ScheduleHandler) handleResource(
	w http.ResponseWriter,
	r *http.Request,
	id serviceschedule.ID,
	action string,
	callerEmail string,
	isAdmin bool,
	caller callerKind,
) {
	switch {
	case action == "" && r.Method == http.MethodPatch:
		input, err := decodeScheduleUpdate(r, caller)
		if err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if caller == callerKindAgent && input.Enabled != nil && *input.Enabled {
			// Arming is the human half of the agent-create handshake; an
			// agent turn may pause but never enable.
			sendScheduleError(w, serviceschedule.ErrArmRequiresUser)
			return
		}
		task, err := h.schedules.Update(r.Context(), id, input, callerEmail, isAdmin)
		if err != nil {
			sendScheduleError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, newScheduleResponse(task))

	case action == "" && r.Method == http.MethodDelete:
		if err := h.schedules.Delete(r.Context(), id, callerEmail, isAdmin); err != nil {
			sendScheduleError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case action == "run" && r.Method == http.MethodPost:
		task, err := h.schedules.RunNow(r.Context(), id, callerEmail, isAdmin)
		if err != nil {
			sendScheduleError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, newScheduleResponse(task))

	case action != "" && action != "run":
		httptransport.SendErr(w, http.StatusNotFound, "not found")

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ScheduleHandler) resolveAgentGrant(
	w http.ResponseWriter,
	r *http.Request,
) (serviceschedulecapability.Grant, bool) {
	if h == nil || h.schedules == nil || h.capabilities == nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "invalid schedule capability")
		return serviceschedulecapability.Grant{}, false
	}
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		httptransport.SendErr(w, http.StatusUnauthorized, "invalid schedule capability")
		return serviceschedulecapability.Grant{}, false
	}
	grant, err := h.capabilities.Resolve(token)
	if err != nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "invalid schedule capability")
		return serviceschedulecapability.Grant{}, false
	}
	return grant, true
}

func (h *ScheduleHandler) caller(r *http.Request) (string, bool, error) {
	if h.auth == nil {
		return "", true, nil
	}
	return callerStateFromRequest(r.Context(), r, h.auth)
}

type scheduleCreateRequest struct {
	Name     string                        `json:"name"`
	Prompt   string                        `json:"prompt"`
	Kind     serviceschedule.Kind          `json:"kind"`
	At       json.RawMessage               `json:"at"`
	Cron     string                        `json:"cron"`
	Timezone string                        `json:"timezone"`
	MaxRuns  int                           `json:"maxRuns"`
	Overlap  serviceschedule.OverlapPolicy `json:"overlapPolicy"`
	Next     []serviceschedule.ChainLink   `json:"next"`
	// Condition is the optional pre-run gate. A kind-less object clears it.
	Condition *serviceschedule.Condition `json:"condition"`
}

func decodeScheduleCreate(r *http.Request) (serviceschedule.CreateInput, error) {
	var body scheduleCreateRequest
	if err := decodeScheduleJSON(r, &body); err != nil {
		return serviceschedule.CreateInput{}, err
	}
	at, err := decodeScheduleAt(body.At)
	if err != nil {
		return serviceschedule.CreateInput{}, err
	}
	return serviceschedule.CreateInput{
		Name:      body.Name,
		Prompt:    body.Prompt,
		Kind:      body.Kind,
		At:        at,
		Cron:      body.Cron,
		Timezone:  body.Timezone,
		MaxRuns:   body.MaxRuns,
		Overlap:   body.Overlap,
		Next:      body.Next,
		Condition: body.Condition,
	}, nil
}

type scheduleUpdateRequest struct {
	Name      *string                        `json:"name"`
	Prompt    *string                        `json:"prompt"`
	Kind      *serviceschedule.Kind          `json:"kind"`
	At        json.RawMessage                `json:"at"`
	Cron      *string                        `json:"cron"`
	Timezone  *string                        `json:"timezone"`
	MaxRuns   *int                           `json:"maxRuns"`
	Overlap   *serviceschedule.OverlapPolicy `json:"overlapPolicy"`
	Enabled   *bool                          `json:"enabled"`
	Next      *[]serviceschedule.ChainLink   `json:"next"`
	Condition *serviceschedule.Condition     `json:"condition"`
}

// decodeScheduleUpdate accepts the full definition for the user API. The
// agent capability API stays pause/resume-shaped: definition edits from an
// agent would bypass the arm step a user granted to a different prompt.
func decodeScheduleUpdate(
	r *http.Request,
	caller callerKind,
) (serviceschedule.UpdateInput, error) {
	var body scheduleUpdateRequest
	if err := decodeScheduleJSON(r, &body); err != nil {
		return serviceschedule.UpdateInput{}, err
	}
	if caller == callerKindAgent {
		if body.Enabled == nil {
			return serviceschedule.UpdateInput{}, errors.New("enabled is required")
		}
		if body.Name != nil || body.Prompt != nil || body.Kind != nil ||
			len(body.At) > 0 || body.Cron != nil || body.Timezone != nil ||
			body.MaxRuns != nil || body.Overlap != nil ||
			body.Next != nil || body.Condition != nil {
			return serviceschedule.UpdateInput{}, errors.New(
				"agent updates may only pause or resume; delete and recreate to redefine",
			)
		}
		return serviceschedule.UpdateInput{Enabled: body.Enabled}, nil
	}

	input := serviceschedule.UpdateInput{
		Name:      body.Name,
		Prompt:    body.Prompt,
		Kind:      body.Kind,
		Cron:      body.Cron,
		Timezone:  body.Timezone,
		MaxRuns:   body.MaxRuns,
		Overlap:   body.Overlap,
		Enabled:   body.Enabled,
		Next:      body.Next,
		Condition: body.Condition,
	}
	if len(body.At) > 0 {
		at, err := decodeScheduleAt(body.At)
		if err != nil {
			return serviceschedule.UpdateInput{}, err
		}
		input.At = &at
	}
	if input.Name == nil && input.Prompt == nil && input.Kind == nil &&
		input.At == nil && input.Cron == nil && input.Timezone == nil &&
		input.MaxRuns == nil && input.Overlap == nil && input.Enabled == nil &&
		input.Next == nil && input.Condition == nil {
		return serviceschedule.UpdateInput{}, errors.New("no fields to update")
	}
	return input, nil
}

func decodeScheduleJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, scheduleRequestLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid json")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid json")
	}
	return nil
}

func decodeScheduleAt(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var millis int64
	if err := json.Unmarshal(raw, &millis); err == nil {
		return millis, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, errors.New("at must be Unix milliseconds or an RFC3339 timestamp")
	}
	value = strings.TrimSpace(value)
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0, errors.New("at must be Unix milliseconds or an RFC3339 timestamp")
	}
	return parsed.UnixMilli(), nil
}

// parseScheduleResourcePath splits `{id}`, `{id}/{action}`, and the deeper
// `{id}/history/{runId}/diff` form. Everything after the action is returned
// untouched so each action decides what its own tail means.
func parseScheduleResourcePath(
	path, prefix string,
) (serviceschedule.ID, string, []string, bool) {
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" || rest == path || strings.HasSuffix(rest, "/") {
		return "", "", nil, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) > 4 {
		return "", "", nil, false
	}
	for _, part := range parts {
		if part == "" {
			return "", "", nil, false
		}
	}
	id := serviceschedule.ID(parts[0])
	if len(parts) == 1 {
		return id, "", nil, true
	}
	return id, parts[1], parts[2:], true
}

func bearerToken(header string) (string, bool) {
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") ||
		strings.TrimSpace(fields[1]) == "" {
		return "", false
	}
	return fields[1], true
}

func filterSchedulesForChat(
	tasks []serviceschedule.Task,
	chatID servicechat.ID,
) []serviceschedule.Task {
	filtered := make([]serviceschedule.Task, 0, len(tasks))
	for _, task := range tasks {
		if task.ChatID == chatID {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func filterSchedulesForGrant(
	tasks []serviceschedule.Task,
	grant serviceschedulecapability.Grant,
) []serviceschedule.Task {
	filtered := make([]serviceschedule.Task, 0, len(tasks))
	for _, task := range tasks {
		if taskMatchesGrant(task, grant) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func taskMatchesGrant(
	task serviceschedule.Task,
	grant serviceschedulecapability.Grant,
) bool {
	return strings.EqualFold(task.OwnerEmail, grant.OwnerEmail) &&
		task.ChatID == grant.ChatID &&
		task.ProjectID == grant.ProjectID
}

type scheduleResponse struct {
	ID             serviceschedule.ID            `json:"id"`
	Name           string                        `json:"name"`
	OwnerEmail     string                        `json:"ownerEmail"`
	ProjectID      serviceproject.ID             `json:"projectId"`
	ChatID         servicechat.ID                `json:"chatId"`
	Prompt         string                        `json:"prompt"`
	Kind           serviceschedule.Kind          `json:"kind"`
	At             int64                         `json:"at,omitempty"`
	Cron           string                        `json:"cron,omitempty"`
	Timezone       string                        `json:"timezone"`
	Enabled        bool                          `json:"enabled"`
	Status         serviceschedule.Status        `json:"status"`
	NextRunAt      int64                         `json:"nextRunAt,omitempty"`
	RunCount       int                           `json:"runCount"`
	MaxRuns        int                           `json:"maxRuns,omitempty"`
	LastRunAt      int64                         `json:"lastRunAt,omitempty"`
	LastRunStatus  serviceschedule.RunStatus     `json:"lastRunStatus,omitempty"`
	LastError      string                        `json:"lastError,omitempty"`
	LastRunResult  string                        `json:"lastRunResult,omitempty"`
	OverlapPolicy  serviceschedule.OverlapPolicy `json:"overlapPolicy,omitempty"`
	Next           []serviceschedule.ChainLink   `json:"next,omitempty"`
	Condition      *serviceschedule.Condition    `json:"condition,omitempty"`
	CreatedByAgent bool                          `json:"createdByAgent,omitempty"`
	CreatedAt      int64                         `json:"createdAt"`
	UpdatedAt      int64                         `json:"updatedAt"`
}

func newScheduleResponse(task serviceschedule.Task) scheduleResponse {
	return scheduleResponse{
		ID:             task.ID,
		Name:           task.Name,
		OwnerEmail:     task.OwnerEmail,
		ProjectID:      task.ProjectID,
		ChatID:         task.ChatID,
		Prompt:         task.Prompt,
		Kind:           task.Kind,
		At:             task.At,
		Cron:           task.Cron,
		Timezone:       task.Timezone,
		Enabled:        task.Enabled,
		Status:         task.Status,
		NextRunAt:      task.NextRunAt,
		RunCount:       task.RunCount,
		MaxRuns:        task.MaxRuns,
		LastRunAt:      task.LastRunAt,
		LastRunStatus:  task.LastStatus,
		LastError:      task.LastError,
		LastRunResult:  task.LastResult,
		OverlapPolicy:  task.Overlap,
		Next:           task.Next,
		Condition:      task.Condition,
		CreatedByAgent: task.CreatedByAgent,
		CreatedAt:      task.CreatedAt,
		UpdatedAt:      task.UpdatedAt,
	}
}

func scheduleResponses(tasks []serviceschedule.Task) []scheduleResponse {
	responses := make([]scheduleResponse, 0, len(tasks))
	for _, task := range tasks {
		responses = append(responses, newScheduleResponse(task))
	}
	return responses
}

// scheduleHistoryEntry flattens a stored run for the History drawer. Duration
// is precomputed so every client renders the same number.
type scheduleHistoryEntry struct {
	serviceschedule.RunRecord
	DurationMs int64 `json:"durationMs"`
}

func scheduleHistoryResponse(records []serviceschedule.RunRecord) []scheduleHistoryEntry {
	entries := make([]scheduleHistoryEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, scheduleHistoryEntry{
			RunRecord:  record,
			DurationMs: record.DurationMs(),
		})
	}
	return entries
}

func sendScheduleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serviceschedule.ErrInvalidID),
		errors.Is(err, serviceschedule.ErrNameRequired),
		errors.Is(err, serviceschedule.ErrPromptRequired),
		errors.Is(err, serviceschedule.ErrPromptTooLarge),
		errors.Is(err, serviceschedule.ErrInvalidKind),
		errors.Is(err, serviceschedule.ErrInvalidTime),
		errors.Is(err, serviceschedule.ErrInvalidCron),
		errors.Is(err, serviceschedule.ErrInvalidTimezone),
		errors.Is(err, serviceschedule.ErrInvalidMaxRuns),
		errors.Is(err, serviceschedule.ErrInvalidOverlap),
		errors.Is(err, serviceschedule.ErrProjectRequired),
		errors.Is(err, serviceschedule.ErrProjectMismatch),
		errors.Is(err, serviceschedule.ErrIntervalTooSmall),
		errors.Is(err, serviceschedule.ErrChainCycle),
		errors.Is(err, serviceschedule.ErrChainTooDeep),
		errors.Is(err, serviceschedule.ErrChainTooManyLinks),
		errors.Is(err, serviceschedule.ErrChainCrossProject),
		errors.Is(err, serviceschedule.ErrInvalidChainWhen),
		errors.Is(err, serviceschedule.ErrInvalidChainDelay),
		errors.Is(err, serviceschedule.ErrGateInvalidKind),
		errors.Is(err, serviceschedule.ErrGatePatternRequired),
		errors.Is(err, serviceschedule.ErrGateInvalidPattern),
		errors.Is(err, serviceschedule.ErrGateInvalidReference),
		errors.Is(err, serviceschedule.ErrGateInvalidURL),
		errors.Is(err, serviceschedule.ErrGateInvalidExpect),
		errors.Is(err, serviceschedule.ErrGateCommandRequired),
		errors.Is(err, serviceschedule.ErrGateWeekdaysRequired),
		errors.Is(err, serviceschedule.ErrGateInvalidMinutes):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, serviceschedule.ErrNotFound),
		errors.Is(err, serviceschedule.ErrRunNotFound),
		errors.Is(err, serviceschedule.ErrChainTargetNotFound),
		errors.Is(err, serviceschedule.ErrChatNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, serviceschedule.ErrAccessDenied),
		errors.Is(err, serviceschedule.ErrOwnerUnregistered),
		errors.Is(err, serviceschedule.ErrArmRequiresUser):
		httptransport.SendErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, serviceschedule.ErrAlreadyExists),
		errors.Is(err, serviceschedule.ErrExecutorBusy),
		errors.Is(err, serviceschedule.ErrProjectQuota):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
