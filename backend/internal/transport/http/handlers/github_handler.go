package httphandlers

// The two halves of the GitHub integration's HTTP surface, which have almost
// nothing in common except the service behind them:
//
//   - the panel, under /api/projects/{id}/github/*, reached only through the
//     project route that has already established membership;
//   - the inbound webhook, at /hooks/github/{projectId}, which is public,
//     session-less, and authenticated by an HMAC signature instead.
//
// They live in one file because splitting them would hide the fact that the
// same service is reachable two ways, and the second way is reachable by
// anyone on the internet.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// GitHubService is the transport's narrow view of the integration. The stored
// webhook secret never crosses it: the only method that can return one is
// SaveSettings, and only for the call that just minted it.
type GitHubService interface {
	Available() bool
	Status(ctx context.Context, id serviceproject.ID) (servicegithub.Status, error)
	Link(ctx context.Context, id serviceproject.ID, in servicegithub.LinkInput, actor string) (serviceproject.Meta, error)
	Unlink(ctx context.Context, id serviceproject.ID) error
	Clone(ctx context.Context, id serviceproject.ID) error
	CreatePR(ctx context.Context, id serviceproject.ID, in servicegithub.CreatePRInput) (servicegithub.CreatePRResult, error)
	ListPullRequests(ctx context.Context, id serviceproject.ID) ([]servicegithub.PullRequest, error)
	ImportComments(ctx context.Context, id serviceproject.ID, number int, in servicegithub.ImportInput, actor string) (servicegithub.ImportResult, error)
	Settings(ctx context.Context, id serviceproject.ID) (servicegithub.PublicSettings, error)
	SaveSettings(ctx context.Context, id serviceproject.ID, in servicegithub.SettingsInput, actor string) (servicegithub.PublicSettings, error)
	HandleDelivery(ctx context.Context, id serviceproject.ID, req servicegithub.DeliveryRequest) (servicegithub.DeliveryOutcome, error)
}

// webhookRateLimit and webhookRateWindow bound how many deliveries one client
// address may present. GitHub retries a failed delivery a handful of times and
// a busy repository might fire a few events a minute; sixty leaves ample room
// while stopping an anonymous caller from using this route to make the box do
// HMAC work on demand.
const (
	webhookRateLimit  = 60
	webhookRateWindow = time.Minute
)

// GitHubHandler serves both halves. The panel routes are mounted by the
// project handler, which has already resolved membership; only the webhook
// route is registered here, because only it is its own entry point.
type GitHubHandler struct {
	github  GitHubService
	limiter *fixedWindowLimiter
	audit   serviceaudit.Recorder
}

func NewGitHubHandler(github GitHubService, _ *serviceauth.Service) *GitHubHandler {
	if github == nil {
		return nil
	}
	return &GitHubHandler{
		github:  github,
		limiter: newFixedWindowLimiter(webhookRateLimit, webhookRateWindow),
		audit:   serviceaudit.Nop{},
	}
}

// WithAudit attaches the audit recorder used for rejected deliveries the
// service never sees (rate limit, malformed path).
func (h *GitHubHandler) WithAudit(recorder serviceaudit.Recorder) *GitHubHandler {
	if h != nil && recorder != nil {
		h.audit = recorder
	}
	return h
}

// WithClock replaces the clock behind the rate limiter's window.
func (h *GitHubHandler) WithClock(now func() time.Time) *GitHubHandler {
	if h != nil && now != nil {
		h.limiter.now = now
	}
	return h
}

func (h *GitHubHandler) RegisterRoutes(mux *http.ServeMux) {
	// Public on purpose, exactly like /portal and /healthz: the auth
	// middleware gates /api and /ws only, so this route reaches the handler
	// with no session and authenticates itself with the HMAC signature.
	mux.HandleFunc(servicegithub.WebhookPath, h.handleWebhook)
}

/* ------------------------------------------------------------------ *
 * Inbound webhook
 * ------------------------------------------------------------------ */

// handleWebhook is the one endpoint in this feature an anonymous caller can
// reach. It does as little as possible before the signature is checked: parse
// the project id out of the path, refuse anything but POST, spend one rate
// limit token, read a capped body, and hand the raw bytes to the service.
func (h *GitHubHandler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := serviceproject.ID(strings.Trim(strings.TrimPrefix(r.URL.Path, servicegithub.WebhookPath), "/"))
	if !h.limiter.allow(webhookRateKey(r)) {
		// Recorded, because a throttled webhook endpoint is either a
		// misconfigured repository retrying in a loop or somebody probing it,
		// and both are things an operator needs to be able to see afterwards.
		h.audit.Record(r.Context(), serviceaudit.Result(
			serviceaudit.ActionGitHubWebhookRejected,
			serviceaudit.Target{Type: serviceaudit.TargetProject, ID: string(id)},
			serviceaudit.Meta{"reason": "rate limited", "event": r.Header.Get(servicegithub.EventHeader)},
			servicegithub.ErrRateLimited,
		))
		w.Header().Set("Retry-After", strconv.Itoa(int(webhookRateWindow.Seconds())))
		httptransport.SendErr(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	// The id grammar is checked before anything is loaded, so a path-traversal
	// attempt never reaches a store that would turn it into a filename.
	if !serviceproject.ValidID(id) {
		httptransport.SendErr(w, http.StatusNotFound, "unknown webhook")
		return
	}
	if h.github == nil || !h.github.Available() {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicegithub.ErrUnavailable.Error())
		return
	}

	// One byte over the cap is read on purpose: it is how a body that is
	// exactly at the limit is told apart from one that exceeds it.
	body, err := io.ReadAll(io.LimitReader(r.Body, servicegithub.MaxPayloadBytes+1))
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "could not read the request body")
		return
	}
	if len(body) > servicegithub.MaxPayloadBytes {
		httptransport.SendErr(w, http.StatusRequestEntityTooLarge, servicegithub.ErrPayloadTooLarge.Error())
		return
	}

	outcome, err := h.github.HandleDelivery(r.Context(), id, servicegithub.DeliveryRequest{
		Event:     r.Header.Get(servicegithub.EventHeader),
		ID:        r.Header.Get(servicegithub.DeliveryHeader),
		Signature: r.Header.Get(servicegithub.SignatureHeader),
		Body:      body,
	})
	if err != nil {
		// Every rejection answers the same way and says as little as possible.
		// A caller probing this endpoint must not learn from the response
		// whether a project exists, whether it is linked, or whether a secret
		// is configured — only that this delivery was not accepted.
		switch {
		case errors.Is(err, servicegithub.ErrPayloadTooLarge):
			httptransport.SendErr(w, http.StatusRequestEntityTooLarge, err.Error())
		case errors.Is(err, servicegithub.ErrUnavailable):
			httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
		default:
			httptransport.SendErr(w, http.StatusUnauthorized, "signature verification failed")
		}
		return
	}
	httptransport.SendJSON(w, http.StatusAccepted, outcome)
}

// webhookRateKey is the identity the delivery budget is spent against.
//
// It deliberately does not use httptransport.ClientIP, which reads the
// *left-most* X-Forwarded-For entry. Caddy is not configured with
// trusted_proxies, so it appends the real peer rather than replacing the
// header: the left-most value is whatever the caller sent, and rotating it per
// request would defeat the budget entirely. Since this route is reachable by
// the whole internet and every rejection past it writes an audit line, the
// budget has to key on something the caller cannot forge — the right-most hop,
// which is the address Caddy itself observed, falling back to the socket peer.
func webhookRateKey(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		hops := strings.Split(forwarded, ",")
		if last := strings.TrimSpace(hops[len(hops)-1]); last != "" {
			return last
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

/* ------------------------------------------------------------------ *
 * Panel routes
 * ------------------------------------------------------------------ */

// HandleProjectResource serves /api/projects/{id}/github[/...].
//
// It is called from the project handler, which has already established that
// the caller is an administrator or a member of this project. The one extra
// authorization decision made here is the autoRun toggle, which is
// administrator-only: turning it on lets text from the public internet start
// an agent with root inside the container, and that is not a member's call.
func (h *GitHubHandler) HandleProjectResource(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	rest string,
	email string,
	isAdmin bool,
) {
	if h == nil || h.github == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicegithub.ErrUnavailable.Error())
		return
	}
	segments := githubPathSegments(rest)

	switch {
	case len(segments) == 0:
		h.handleLink(w, r, id, email)
	case segments[0] == "settings" && len(segments) == 1:
		h.handleSettings(w, r, id, email, isAdmin)
	case segments[0] == "clone" && len(segments) == 1:
		h.handleClone(w, r, id)
	case segments[0] == "pr" && len(segments) == 1:
		h.handleCreatePR(w, r, id)
	case segments[0] == "prs" && len(segments) == 1:
		h.handleListPRs(w, r, id)
	case segments[0] == "prs" && len(segments) == 3 && segments[2] == "import-comments":
		h.handleImportComments(w, r, id, segments[1], email)
	default:
		httptransport.SendErr(w, http.StatusNotFound, "unknown GitHub action")
	}
}

// githubPathSegments turns "prs/12/import-comments" into its segments,
// dropping the empty ones a trailing slash produces.
func githubPathSegments(rest string) []string {
	segments := []string{}
	for _, part := range strings.Split(strings.Trim(rest, "/"), "/") {
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}

func (h *GitHubHandler) handleLink(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	email string,
) {
	switch r.Method {
	case http.MethodGet:
		status, err := h.github.Status(r.Context(), id)
		if err != nil {
			sendGitHubError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, status)
	case http.MethodPut:
		var body servicegithub.LinkInput
		if err := decodeGitHubBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		if _, err := h.github.Link(r.Context(), id, body, email); err != nil {
			sendGitHubError(w, err)
			return
		}
		status, err := h.github.Status(r.Context(), id)
		if err != nil {
			sendGitHubError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, status)
	case http.MethodDelete:
		if err := h.github.Unlink(r.Context(), id); err != nil {
			sendGitHubError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *GitHubHandler) handleSettings(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	email string,
	isAdmin bool,
) {
	switch r.Method {
	case http.MethodGet:
		settings, err := h.github.Settings(r.Context(), id)
		if err != nil {
			sendGitHubError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var body servicegithub.SettingsInput
		if err := decodeGitHubBody(r, &body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		// Arming inbound automation is the one edit a member may not make.
		// Everything else on this document — the label, the comment-back
		// toggle, rotating or disabling the secret — is ordinary project
		// configuration.
		if body.AutoRun != nil && *body.AutoRun && !isAdmin {
			httptransport.SendErr(w, http.StatusForbidden,
				"only an administrator can turn on automatic runs from GitHub events")
			return
		}
		settings, err := h.github.SaveSettings(r.Context(), id, body, email)
		if err != nil {
			sendGitHubError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, settings)
	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *GitHubHandler) handleClone(w http.ResponseWriter, r *http.Request, id serviceproject.ID) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := h.github.Clone(r.Context(), id); err != nil {
		sendGitHubError(w, err)
		return
	}
	status, err := h.github.Status(r.Context(), id)
	if err != nil {
		sendGitHubError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, status)
}

// dirtyWorkspaceResponse is the 409 body the commit dialog is built from. It
// carries the default message so the browser never has to compose one and the
// two can never disagree about the date.
type dirtyWorkspaceResponse struct {
	Error                string `json:"error"`
	Dirty                bool   `json:"dirty"`
	DefaultCommitMessage string `json:"defaultCommitMessage"`
	DirtyCount           int    `json:"dirtyCount"`
}

func (h *GitHubHandler) handleCreatePR(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body servicegithub.CreatePRInput
	if err := decodeGitHubBody(r, &body); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	result, err := h.github.CreatePR(r.Context(), id, body)
	if errors.Is(err, servicegithub.ErrDirtyWorkspace) {
		status, statusErr := h.github.Status(r.Context(), id)
		response := dirtyWorkspaceResponse{Error: err.Error(), Dirty: true}
		if statusErr == nil {
			response.DefaultCommitMessage = status.DefaultCommitMessage
			response.DirtyCount = status.DirtyCount
		}
		httptransport.SendJSON(w, http.StatusConflict, response)
		return
	}
	if err != nil {
		sendGitHubError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusCreated, result)
}

func (h *GitHubHandler) handleListPRs(w http.ResponseWriter, r *http.Request, id serviceproject.ID) {
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	list, err := h.github.ListPullRequests(r.Context(), id)
	if err != nil {
		sendGitHubError(w, err)
		return
	}
	if list == nil {
		list = []servicegithub.PullRequest{}
	}
	httptransport.SendJSON(w, http.StatusOK, list)
}

func (h *GitHubHandler) handleImportComments(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	rawNumber string,
	email string,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	number, err := strconv.Atoi(rawNumber)
	if err != nil || number <= 0 {
		httptransport.SendErr(w, http.StatusBadRequest, servicegithub.ErrInvalidNumber.Error())
		return
	}
	var body servicegithub.ImportInput
	if err := decodeGitHubBody(r, &body); err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	result, err := h.github.ImportComments(r.Context(), id, number, body, email)
	if err != nil {
		sendGitHubError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// decodeGitHubBody reads a small JSON body. An empty body decodes to the zero
// value rather than failing, because several of these routes take no fields.
func decodeGitHubBody(r *http.Request, target any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}

// sendGitHubError maps the service's error vocabulary onto status codes.
// Everything unmapped is a 500 with the message, which is what an unexpected
// git or gh failure is.
func sendGitHubError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicegithub.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, serviceproject.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, serviceproject.ErrInvalidID),
		errors.Is(err, serviceproject.ErrInvalidGitHubRepo),
		errors.Is(err, servicegithub.ErrInvalidRepo),
		errors.Is(err, servicegithub.ErrInvalidBranch),
		errors.Is(err, servicegithub.ErrInvalidNumber),
		errors.Is(err, servicegithub.ErrChatRequired),
		errors.Is(err, servicegithub.ErrTitleRequired):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, servicegithub.ErrChatMismatch):
		httptransport.SendErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, servicegithub.ErrAuth):
		// 424: the request was well formed and authorized here; the
		// dependency it needs (a GitHub credential in the container) is not
		// configured.
		httptransport.SendErr(w, http.StatusFailedDependency, err.Error())
	case errors.Is(err, servicegithub.ErrNotLinked),
		errors.Is(err, servicegithub.ErrAlreadyLinked),
		errors.Is(err, servicegithub.ErrNotRunning),
		errors.Is(err, servicegithub.ErrWorkspaceNotEmpty),
		errors.Is(err, servicegithub.ErrNotRepository),
		errors.Is(err, servicegithub.ErrDirtyWorkspace),
		errors.Is(err, servicegithub.ErrHeadIsBase),
		errors.Is(err, servicegithub.ErrNothingToPush),
		errors.Is(err, servicegithub.ErrNoComments),
		errors.Is(err, servicegithub.ErrRepoUnreachable):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
