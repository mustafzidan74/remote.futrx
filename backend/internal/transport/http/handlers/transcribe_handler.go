package httphandlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// multipartMemory is how much of the upload is parsed in memory before the
// rest spills to a temp file. The audio part is streamed out again
// immediately, so this only needs to cover the small text fields.
const multipartMemory = 1 << 20

// TranscriptionService is the HTTP layer's narrow view of the transcription
// service. Only masked or client-safe configuration crosses this boundary.
type TranscriptionService interface {
	PublicConfig() servicetranscribe.PublicConfig
	ClientConfig() servicetranscribe.ClientConfig
	Save(ctx context.Context, input servicetranscribe.UpdateInput) (servicetranscribe.PublicConfig, error)
	Transcribe(ctx context.Context, user string, req servicetranscribe.Request) (servicetranscribe.Result, error)
	Test(ctx context.Context) servicetranscribe.TestResult
}

// transcriptionCaller resolves the authenticated principal. It exists so the
// auth gates can be exercised without building a full auth service.
type transcriptionCaller interface {
	EmailAndAdmin(ctx context.Context, r *http.Request) (string, bool, error)
}

// TranscribeHandler serves the user-facing dictation endpoint and the admin
// settings panel behind it.
type TranscribeHandler struct {
	transcription TranscriptionService
	caller        transcriptionCaller
	audit         serviceaudit.Recorder
}

func NewTranscribeHandler(
	transcription TranscriptionService,
	auth *serviceauth.Service,
) *TranscribeHandler {
	return &TranscribeHandler{
		transcription: transcription,
		caller:        httptransport.NewPrincipalResolver(auth),
		audit:         serviceaudit.Nop{},
	}
}

// WithAudit attaches the audit recorder. Only the clip's duration is ever
// recorded — never the audio, never the text it became.
func (h *TranscribeHandler) WithAudit(recorder serviceaudit.Recorder) *TranscribeHandler {
	if h != nil {
		h.audit = serviceaudit.RecorderOrNop(recorder)
	}
	return h
}

func (h *TranscribeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/transcribe", h.handleTranscribe)
	mux.HandleFunc("/api/transcribe/config", h.handleClientConfig)
	mux.HandleFunc("/api/admin/transcription", h.handleSettings)
	mux.HandleFunc("/api/admin/transcription/test", h.handleTest)
}

// handleClientConfig tells the composer whether the mic button should offer
// the server option at all. Any signed-in user may read it; it carries no
// provider identity and no key material.
func (h *TranscribeHandler) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	if h.transcription == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "transcription is unavailable")
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.requireUser(w, r); !ok {
		return
	}
	httptransport.SendJSON(w, http.StatusOK, h.transcription.ClientConfig())
}

// handleTranscribe streams one recorded clip to the provider. The audio never
// touches the platform's own storage: the multipart part is handed straight to
// the service, which forwards it.
func (h *TranscribeHandler) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if h.transcription == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "transcription is unavailable")
		return
	}
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	email, ok := h.requireUser(w, r)
	if !ok {
		return
	}

	// The byte ceiling is the real defence; the browser-reported duration
	// below is advisory and only shapes the error message and the audit line.
	r.Body = http.MaxBytesReader(w, r.Body, servicetranscribe.MaxAudioBytes)
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		httptransport.SendErr(w, http.StatusRequestEntityTooLarge,
			"the recording is too large or malformed (limit 25 MB)")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "no audio in request (field name: audio)")
		return
	}
	defer file.Close()

	duration := parseDurationMS(r.FormValue("durationMs"))
	result, err := h.transcription.Transcribe(r.Context(), email, servicetranscribe.Request{
		Audio:    file,
		Filename: header.Filename,
		MimeType: header.Header.Get("Content-Type"),
		Language: r.FormValue("language"),
		Duration: duration,
	})
	h.audit.Record(r.Context(), serviceaudit.Result(
		serviceaudit.ActionChatTranscribe,
		serviceaudit.Target{Type: serviceaudit.TargetChat, ID: r.FormValue("chatId")},
		// Duration only: the clip and its transcript are the user's speech and
		// have no business in an append-only log.
		serviceaudit.Meta{"durationMs": duration.Milliseconds()},
		err,
	))
	if err != nil {
		httptransport.SendErr(w, transcribeStatus(err), err.Error())
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

func (h *TranscribeHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		httptransport.SendJSON(w, http.StatusOK, h.transcription.PublicConfig())
	case http.MethodPut:
		var input servicetranscribe.UpdateInput
		if err := readJSONBody(r, &input); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid request")
			return
		}
		config, err := h.transcription.Save(r.Context(), input)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, servicetranscribe.ErrInvalidConfig) {
				status = http.StatusBadRequest
			}
			httptransport.SendErr(w, status, err.Error())
			return
		}
		h.audit.Record(r.Context(), serviceaudit.Success(
			serviceaudit.ActionSettingsTranscription,
			serviceaudit.Target{Type: serviceaudit.TargetServer, ID: "transcription"},
			serviceaudit.Meta{"enabled": config.Enabled, "model": config.Model},
		))
		httptransport.SendJSON(w, http.StatusOK, config)
	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *TranscribeHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	httptransport.SendJSON(w, http.StatusOK, h.transcription.Test(r.Context()))
}

// requireUser resolves the caller and writes the failure response itself. The
// auth middleware already gates /api, so this is about identity for rate
// limiting and auditing rather than a second gate.
func (h *TranscribeHandler) requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	email, _, err := h.caller.EmailAndAdmin(r.Context(), r)
	if err != nil || email == "" {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	return email, true
}

// authorizeAdmin writes the failure response itself and reports whether the
// caller may proceed.
func (h *TranscribeHandler) authorizeAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.transcription == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable, "transcription is unavailable")
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

// transcribeStatus maps a service failure onto the status the composer knows
// how to explain. Anything unrecognized is a provider or network problem,
// which is a gateway failure rather than the user's fault.
func transcribeStatus(err error) int {
	switch {
	case errors.Is(err, servicetranscribe.ErrDisabled):
		return http.StatusServiceUnavailable
	case errors.Is(err, servicetranscribe.ErrRateLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, servicetranscribe.ErrTooLong),
		errors.Is(err, servicetranscribe.ErrEmptyAudio):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

func parseDurationMS(raw string) time.Duration {
	milliseconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || milliseconds < 0 {
		return 0
	}
	return time.Duration(milliseconds) * time.Millisecond
}
