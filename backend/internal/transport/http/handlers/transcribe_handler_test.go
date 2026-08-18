package httphandlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
)

// transcriptionServiceStub records what the handler forwarded so the auth,
// size, and field plumbing can be asserted without a provider.
type transcriptionServiceStub struct {
	config     servicetranscribe.PublicConfig
	client     servicetranscribe.ClientConfig
	saveInput  servicetranscribe.UpdateInput
	saveErr    error
	request    servicetranscribe.Request
	requestKey string
	audio      []byte
	result     servicetranscribe.Result
	err        error
	calls      int
	tested     bool
}

func (s *transcriptionServiceStub) PublicConfig() servicetranscribe.PublicConfig { return s.config }

func (s *transcriptionServiceStub) ClientConfig() servicetranscribe.ClientConfig { return s.client }

func (s *transcriptionServiceStub) Save(
	_ context.Context,
	input servicetranscribe.UpdateInput,
) (servicetranscribe.PublicConfig, error) {
	s.saveInput = input
	if s.saveErr != nil {
		return servicetranscribe.PublicConfig{}, s.saveErr
	}
	return s.config, nil
}

func (s *transcriptionServiceStub) Transcribe(
	_ context.Context,
	user string,
	req servicetranscribe.Request,
) (servicetranscribe.Result, error) {
	s.calls++
	s.requestKey = user
	s.request = req
	if req.Audio != nil {
		s.audio, _ = io.ReadAll(req.Audio)
	}
	return s.result, s.err
}

func (s *transcriptionServiceStub) Test(context.Context) servicetranscribe.TestResult {
	s.tested = true
	return servicetranscribe.TestResult{OK: true, Model: servicetranscribe.DefaultModel, DurationMS: 120}
}

// recordingAuditor captures audit entries so a test can assert what the log
// would have kept — and, more importantly, what it would not have.
type recordingAuditor struct {
	entries []serviceaudit.Entry
}

func (r *recordingAuditor) Record(_ context.Context, entry serviceaudit.Entry) {
	r.entries = append(r.entries, entry)
}

func newTranscribeHandler(
	service TranscriptionService,
	caller transcriptionCaller,
) *TranscribeHandler {
	return NewTranscribeHandler(nil, nil).withDependencies(service, caller)
}

// withDependencies swaps the collaborators the constructor would have built
// from a live auth service.
func (h *TranscribeHandler) withDependencies(
	service TranscriptionService,
	caller transcriptionCaller,
) *TranscribeHandler {
	h.transcription = service
	h.caller = caller
	return h
}

// audioRequest builds the multipart upload the composer sends. The text hints
// come first because the handler streams the audio part rather than buffering
// the form, so anything after the recording is never read.
func audioRequest(t *testing.T, audio []byte, language string, durationMS int64) *http.Request {
	t.Helper()
	return audioRequestWithFields(t, audio, [][2]string{
		{"language", language},
		{"durationMs", strconv.FormatInt(durationMS, 10)},
		{"chatId", "chat-123"},
	})
}

func audioRequestWithFields(t *testing.T, audio []byte, fields [][2]string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("audio", "dictation.webm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(audio); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/transcribe", body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	return request
}

func TestTranscribeRequiresASession(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		method     string
		caller     callerStub
		wantStatus int
	}{
		{
			name:       "anonymous dictation",
			target:     "/api/transcribe",
			method:     http.MethodPost,
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "anonymous client config",
			target:     "/api/transcribe/config",
			method:     http.MethodGet,
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "anonymous settings read",
			target:     "/api/admin/transcription",
			method:     http.MethodGet,
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "member settings read",
			target:     "/api/admin/transcription",
			method:     http.MethodGet,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "member settings write",
			target:     "/api/admin/transcription",
			method:     http.MethodPut,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "member probe",
			target:     "/api/admin/transcription/test",
			method:     http.MethodPost,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &transcriptionServiceStub{}
			handler := newTranscribeHandler(service, tt.caller)
			mux := http.NewServeMux()
			handler.RegisterRoutes(mux)

			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.target, nil))

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tt.wantStatus, recorder.Body)
			}
			if service.calls != 0 || service.tested {
				t.Fatal("an unauthorized request must never reach the service")
			}
		})
	}
}

func TestTranscribeForwardsTheClipAndTheCallersIdentity(t *testing.T) {
	service := &transcriptionServiceStub{
		result: servicetranscribe.Result{Text: "مرحبا", Model: servicetranscribe.DefaultModel, Language: "ar"},
	}
	handler := newTranscribeHandler(service, callerStub{email: "user@example.com"})

	recorder := httptest.NewRecorder()
	handler.handleTranscribe(recorder, audioRequest(t, []byte("opus-bytes"), "ar-EG", 4200))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	var got servicetranscribe.Result
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Text != "مرحبا" {
		t.Fatalf("text = %q", got.Text)
	}
	if service.requestKey != "user@example.com" {
		t.Fatalf("rate-limit key = %q, want the caller's email", service.requestKey)
	}
	if service.request.Language != "ar-EG" {
		t.Fatalf("language = %q, want the tag the composer picked", service.request.Language)
	}
	if service.request.Duration != 4200*time.Millisecond {
		t.Fatalf("duration = %s, want 4.2s", service.request.Duration)
	}
	if string(service.audio) != "opus-bytes" {
		t.Fatalf("audio = %q, want the uploaded bytes", service.audio)
	}
}

func TestTranscribeRejectsAnUploadOverTheByteCeiling(t *testing.T) {
	service := &transcriptionServiceStub{}
	handler := newTranscribeHandler(service, callerStub{email: "user@example.com"})

	oversized := bytes.Repeat([]byte("a"), int(servicetranscribe.MaxAudioBytes)+1024)
	recorder := httptest.NewRecorder()
	handler.handleTranscribe(recorder, audioRequest(t, oversized, "en-US", 1000))

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (%s)", recorder.Code, recorder.Body)
	}
	if service.calls != 0 {
		t.Fatal("an oversized upload must be cut off before it reaches the provider")
	}
}

func TestTranscribeRejectsARequestWithNoAudioPart(t *testing.T) {
	service := &transcriptionServiceStub{}
	handler := newTranscribeHandler(service, callerStub{email: "user@example.com"})

	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	if err := form.WriteField("language", "ar-EG"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/transcribe", body)
	request.Header.Set("Content-Type", form.FormDataContentType())

	recorder := httptest.NewRecorder()
	handler.handleTranscribe(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", recorder.Code, recorder.Body)
	}
	if service.calls != 0 {
		t.Fatal("a request without audio must not reach the service")
	}
}

func TestTranscribeMapsServiceFailuresOntoStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "off", err: servicetranscribe.ErrDisabled, wantStatus: http.StatusServiceUnavailable},
		{name: "rate limited", err: servicetranscribe.ErrRateLimited, wantStatus: http.StatusTooManyRequests},
		{name: "too long", err: servicetranscribe.ErrTooLong, wantStatus: http.StatusBadRequest},
		{name: "no audio", err: servicetranscribe.ErrEmptyAudio, wantStatus: http.StatusBadRequest},
		{name: "provider unreachable", err: errProviderLeak, wantStatus: http.StatusBadGateway},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &transcriptionServiceStub{err: tt.err}
			handler := newTranscribeHandler(service, callerStub{email: "user@example.com"})

			recorder := httptest.NewRecorder()
			handler.handleTranscribe(recorder, audioRequest(t, []byte("bytes"), "ar-EG", 1000))

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tt.wantStatus, recorder.Body)
			}
		})
	}
}

// The audit trail must prove a dictation happened without preserving what was
// said: duration in, transcript and audio out.
func TestTranscribeAuditsDurationOnlyAndNeverTheTranscript(t *testing.T) {
	service := &transcriptionServiceStub{
		result: servicetranscribe.Result{Text: "the secret passphrase is hunter2"},
	}
	recorder := &recordingAuditor{}
	handler := newTranscribeHandler(service, callerStub{email: "user@example.com"}).WithAudit(recorder)

	response := httptest.NewRecorder()
	handler.handleTranscribe(response, audioRequest(t, []byte("opus-bytes"), "ar-EG", 7500))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", response.Code, response.Body)
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("recorded %d entries, want exactly 1", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if entry.Action != "chat.transcribe" || !entry.OK {
		t.Fatalf("entry = %+v, want a successful chat.transcribe", entry)
	}
	if entry.Meta["durationMs"] != int64(7500) {
		t.Fatalf("durationMs = %v, want 7500", entry.Meta["durationMs"])
	}
	if len(entry.Meta) != 1 {
		t.Fatalf("meta = %v, want the duration and nothing else", entry.Meta)
	}
	serialized, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"hunter2", "opus-bytes"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("audit entry leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestSettingsSaveRoundTripsTheMaskedViewAndRejectsBadInput(t *testing.T) {
	service := &transcriptionServiceStub{
		config: servicetranscribe.Config{
			Enabled:         true,
			Provider:        servicetranscribe.ProviderOpenAI,
			APIKey:          "sk-proj-abcdwxyz",
			Model:           servicetranscribe.DefaultModel,
			DefaultLanguage: "ar-EG",
		}.Public(),
	}
	handler := newTranscribeHandler(service, callerStub{email: "admin@example.com", isAdmin: true})

	recorder := httptest.NewRecorder()
	handler.handleSettings(recorder, httptest.NewRequest(
		http.MethodPut,
		"/api/admin/transcription",
		strings.NewReader(`{"enabled":true,"apiKey":"sk-proj-abcdwxyz","model":"gpt-4o-mini-transcribe","defaultLanguage":"ar-EG"}`),
	))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	if service.saveInput.APIKey != "sk-proj-abcdwxyz" || !service.saveInput.Enabled {
		t.Fatalf("save input = %+v", service.saveInput)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "sk-proj-abcdwxyz") {
		t.Fatalf("the response echoed the API key: %s", body)
	}
	if !strings.Contains(body, "wxyz") {
		t.Fatalf("the response should carry the masked tail: %s", body)
	}

	recorder = httptest.NewRecorder()
	handler.handleSettings(recorder, httptest.NewRequest(
		http.MethodPut, "/api/admin/transcription", strings.NewReader("not json"),
	))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400", recorder.Code)
	}
}

func TestSettingsSaveReportsAnInvalidConfigurationAsABadRequest(t *testing.T) {
	service := &transcriptionServiceStub{saveErr: servicetranscribe.ErrInvalidConfig}
	handler := newTranscribeHandler(service, callerStub{email: "admin@example.com", isAdmin: true})

	recorder := httptest.NewRecorder()
	handler.handleSettings(recorder, httptest.NewRequest(
		http.MethodPut, "/api/admin/transcription", strings.NewReader(`{"enabled":true}`),
	))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", recorder.Code, recorder.Body)
	}
}

func TestClientConfigTellsTheComposerTheLimitsWithoutAnyProviderDetail(t *testing.T) {
	service := &transcriptionServiceStub{client: servicetranscribe.Config{
		Enabled:         true,
		APIKey:          "sk-proj-secret",
		DefaultLanguage: "ar-EG",
	}.Client()}
	handler := newTranscribeHandler(service, callerStub{email: "member@example.com"})

	recorder := httptest.NewRecorder()
	handler.handleClientConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/transcribe/config", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	var got servicetranscribe.ClientConfig
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.MaxBytes != servicetranscribe.MaxAudioBytes || got.MaxSeconds != 300 {
		t.Fatalf("client config = %+v", got)
	}
	if strings.Contains(recorder.Body.String(), "sk-proj") ||
		strings.Contains(recorder.Body.String(), servicetranscribe.ProviderOpenAI) {
		t.Fatalf("the composer view leaked provider detail: %s", recorder.Body)
	}
}

func TestUnavailableServiceReportsServiceUnavailableRatherThanPanicking(t *testing.T) {
	handler := newTranscribeHandler(nil, callerStub{email: "admin@example.com", isAdmin: true})

	for _, target := range []struct {
		name    string
		request *http.Request
		serve   func(http.ResponseWriter, *http.Request)
	}{
		{
			name:    "dictation",
			request: httptest.NewRequest(http.MethodPost, "/api/transcribe", nil),
			serve:   handler.handleTranscribe,
		},
		{
			name:    "client config",
			request: httptest.NewRequest(http.MethodGet, "/api/transcribe/config", nil),
			serve:   handler.handleClientConfig,
		},
		{
			name:    "settings",
			request: httptest.NewRequest(http.MethodGet, "/api/admin/transcription", nil),
			serve:   handler.handleSettings,
		},
	} {
		t.Run(target.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			target.serve(recorder, target.request)
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", recorder.Code)
			}
		})
	}
}

// errProviderLeak stands in for a provider failure whose prose quotes the
// operator's key and names the vendor — exactly what OpenAI's 401 body does.
var errProviderLeak = errors.New(
	"transcription provider returned 401: Incorrect API key provided: sk-proj-abcd1234. " +
		"You can find your API key at platform.openai.com/api-keys",
)

// A member who mistypes nothing and simply hits a bad key must not learn the
// operator's credentials or which vendor is behind the button. The masked
// client config would be pointless if the next failed request spelled it out.
func TestTranscribeNeverForwardsProviderProseToTheCaller(t *testing.T) {
	service := &transcriptionServiceStub{err: errProviderLeak}
	handler := newTranscribeHandler(service, callerStub{email: "member@example.com"})

	recorder := httptest.NewRecorder()
	handler.handleTranscribe(recorder, audioRequest(t, []byte("bytes"), "ar-EG", 1000))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"sk-proj", "platform.openai.com", "openai", "API key"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("the response leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "could not be reached") {
		t.Fatalf("the response should still say what went wrong: %s", body)
	}
}

// The sentinel failures are the caller's own doing, so those keep their plain
// explanation rather than being flattened along with the provider errors.
func TestTranscribeKeepsTheExplanationForFailuresTheCallerCanAddress(t *testing.T) {
	service := &transcriptionServiceStub{err: servicetranscribe.ErrRateLimited}
	handler := newTranscribeHandler(service, callerStub{email: "member@example.com"})

	recorder := httptest.NewRecorder()
	handler.handleTranscribe(recorder, audioRequest(t, []byte("bytes"), "ar-EG", 1000))

	if !strings.Contains(recorder.Body.String(), "too many transcription requests") {
		t.Fatalf("body = %s, want the rate-limit explanation kept", recorder.Body)
	}
}

// The handler streams the audio part instead of buffering the form, so the
// text hints only count when they arrive first. A client that puts them after
// the recording still gets a transcription — just without the hints.
func TestTranscribeReadsOnlyTheHintsThatPrecedeTheAudio(t *testing.T) {
	service := &transcriptionServiceStub{result: servicetranscribe.Result{Text: "ok"}}
	handler := newTranscribeHandler(service, callerStub{email: "user@example.com"})

	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, err := form.CreateFormFile("audio", "dictation.webm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("opus")); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("language", "ar-EG"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/transcribe", body)
	request.Header.Set("Content-Type", form.FormDataContentType())

	recorder := httptest.NewRecorder()
	handler.handleTranscribe(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	if service.request.Language != "" {
		t.Fatalf("language = %q, want empty: it arrived after the audio", service.request.Language)
	}
}

func TestParseDurationClampsInsteadOfOverflowing(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "ordinary", raw: "4200", want: 4200 * time.Millisecond},
		{name: "empty", raw: "", want: 0},
		{name: "not a number", raw: "abc", want: 0},
		{name: "negative", raw: "-5", want: 0},
		{
			name: "max int64 would wrap the multiply",
			raw:  "9223372036854775807",
			want: servicetranscribe.MaxAudioDuration + time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDurationMS(tt.raw)
			if got != tt.want {
				t.Fatalf("parseDurationMS(%q) = %s, want %s", tt.raw, got, tt.want)
			}
			if got < 0 {
				t.Fatalf("parseDurationMS(%q) went negative: %s", tt.raw, got)
			}
		})
	}
}
