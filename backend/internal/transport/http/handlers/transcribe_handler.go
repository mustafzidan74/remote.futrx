package httphandlers

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

const (
	// audioFieldName is the multipart field carrying the recording. Every part
	// before it is treated as a text hint; it is the point of no return for
	// the streaming reader.
	audioFieldName = "audio"
	// maxFormFieldBytes bounds one text field so a client cannot spend the
	// whole upload budget on a "language" value.
	maxFormFieldBytes = 4 << 10
	// oversizedMessage is the one explanation for the byte ceiling, whether it
	// is caught up front or halfway through the copy.
	oversizedMessage = "the recording is too large (limit 25 MB)"
)

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

// handleTranscribe streams one recorded clip to the provider.
//
// It walks the multipart body part by part rather than calling
// ParseMultipartForm, because that helper spools any part over its memory
// budget into a temp file — and a recording of someone's voice must not land
// on this server's disk even briefly. Reading the parts in order keeps the
// audio in flight from the socket to the provider.
//
// The consequence is an ordering contract: the text fields have to arrive
// before the audio part, because once the audio is being streamed the reader
// has passed everything before it. The client sends them in that order; a
// request that does not simply transcribes without the hints.
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

	// The byte ceiling is the real defence; the browser-reported duration is
	// advisory and only shapes the error message and the audit line.
	//
	// A declared length over the cap is refused before a single byte is read,
	// which is what an honest client gets. MaxBytesReader is the backstop for
	// one that lies or streams without a length — that failure surfaces mid
	// copy and is recognized again in transcribeFailure.
	if r.ContentLength > servicetranscribe.MaxAudioBytes {
		httptransport.SendErr(w, http.StatusRequestEntityTooLarge, oversizedMessage)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, servicetranscribe.MaxAudioBytes)
	parts, err := r.MultipartReader()
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "expected a multipart upload")
		return
	}

	var (
		language string
		chatID   string
		duration time.Duration
	)
	for {
		part, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			httptransport.SendErr(w, http.StatusBadRequest, "no audio in request (field name: audio)")
			return
		}
		if err != nil {
			httptransport.SendErr(w, oversizedStatus(err), oversizedOrMalformed(err))
			return
		}
		if part.FormName() == audioFieldName {
			defer part.Close()
			h.transcribePart(w, r, email, part, servicetranscribe.Request{
				Filename: part.FileName(),
				MimeType: part.Header.Get("Content-Type"),
				Language: language,
				Duration: duration,
			}, chatID)
			return
		}
		value, err := readFormField(part)
		part.Close()
		if err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "malformed upload")
			return
		}
		switch part.FormName() {
		case "language":
			language = value
		case "chatId":
			chatID = value
		case "durationMs":
			duration = parseDurationMS(value)
		}
	}
}

// transcribePart runs the transcription for an audio part that is still being
// uploaded, then audits and answers.
func (h *TranscribeHandler) transcribePart(
	w http.ResponseWriter,
	r *http.Request,
	email string,
	audio io.Reader,
	request servicetranscribe.Request,
	chatID string,
) {
	request.Audio = audio
	result, err := h.transcription.Transcribe(r.Context(), email, request)
	h.audit.Record(r.Context(), serviceaudit.Result(
		serviceaudit.ActionChatTranscribe,
		serviceaudit.Target{Type: serviceaudit.TargetChat, ID: chatID},
		// Duration only: the clip and its transcript are the user's speech and
		// have no business in an append-only log.
		serviceaudit.Meta{"durationMs": request.Duration.Milliseconds()},
		err,
	))
	if err != nil {
		status, message := transcribeFailure(err)
		httptransport.SendErr(w, status, message)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, result)
}

// readFormField reads one small text part. The bound stops a client from
// spending the whole 25 MB budget on a "language" field.
func readFormField(part io.Reader) (string, error) {
	value, err := io.ReadAll(io.LimitReader(part, maxFormFieldBytes))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(value)), nil
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

// transcribeFailure maps a service failure onto the status and the message the
// caller is allowed to see.
//
// The sentinel errors are the caller's own doing and say so plainly. Anything
// else came back from the provider, and its prose is not safe to forward: an
// OpenAI 401 quotes a fragment of the operator's own API key and names the
// vendor, neither of which an ordinary member should learn from a failed
// dictation. Those are logged for the operator and flattened for the user,
// who gets the full detail only through the admin Test probe.
func transcribeFailure(err error) (int, string) {
	switch {
	case errors.Is(err, servicetranscribe.ErrDisabled):
		return http.StatusServiceUnavailable, err.Error()
	case errors.Is(err, servicetranscribe.ErrRateLimited):
		return http.StatusTooManyRequests, err.Error()
	case errors.Is(err, servicetranscribe.ErrTooLong),
		errors.Is(err, servicetranscribe.ErrEmptyAudio):
		return http.StatusBadRequest, err.Error()
	default:
		// A client that lied about its length trips the reader mid copy, and
		// the failure reaches here wrapped in the provider error. It is still
		// an oversized upload, not a provider outage.
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			return http.StatusRequestEntityTooLarge, oversizedMessage
		}
		log.Printf("transcribe: provider request failed: %v", err)
		return http.StatusBadGateway,
			"the transcription service could not be reached — ask an administrator to check the Voice input settings"
	}
}

// oversizedStatus separates "you sent too much" from "this is not a valid
// multipart body", which are different things for the caller to fix.
func oversizedStatus(err error) int {
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func oversizedOrMalformed(err error) string {
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		return oversizedMessage
	}
	return "the upload is malformed"
}

// parseDurationMS reads the browser-reported clip length. It clamps rather
// than trusts: an unbounded value multiplied into a Duration overflows into a
// negative that would slip past the ceiling check downstream.
func parseDurationMS(raw string) time.Duration {
	milliseconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || milliseconds < 0 {
		return 0
	}
	if milliseconds > maxDurationMS {
		return servicetranscribe.MaxAudioDuration + time.Second
	}
	return time.Duration(milliseconds) * time.Millisecond
}

// maxDurationMS is the largest value worth converting; anything above it is
// reported as "over the ceiling" without arithmetic.
const maxDurationMS = int64(servicetranscribe.MaxAudioDuration / time.Millisecond)
