package transcribe

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// memoryStore is the in-process stand-in for filetranscribe.Store.
type memoryStore struct {
	config Config
	saved  Config
	loads  int
}

func (s *memoryStore) Load(context.Context) (Config, error) {
	s.loads++
	return s.config, nil
}

func (s *memoryStore) Save(_ context.Context, cfg Config) error {
	s.saved = cfg
	s.config = cfg
	return nil
}

// recordingProvider captures what the service asked the provider for.
type recordingProvider struct {
	request ProviderRequest
	audio   []byte
	text    string
	err     error
	calls   int
}

func (p *recordingProvider) Transcribe(_ context.Context, req ProviderRequest) (string, error) {
	p.calls++
	p.request = req
	if req.Audio != nil {
		p.audio, _ = io.ReadAll(req.Audio)
	}
	return p.text, p.err
}

func activeConfig() Config {
	return Config{
		Enabled:         true,
		Provider:        ProviderOpenAI,
		APIKey:          "sk-test",
		Model:           DefaultModel,
		DefaultLanguage: "ar-EG",
	}
}

func TestTranscribeStreamsTheClipWithTheReducedLanguageHint(t *testing.T) {
	provider := &recordingProvider{text: "  مرحبا بالعالم  "}
	service := New(context.Background(), &memoryStore{config: activeConfig()},
		WithTranscriber(provider))

	result, err := service.Transcribe(context.Background(), "User@Example.com", Request{
		Audio:    strings.NewReader("fake-opus-bytes"),
		Filename: "dictation.webm",
		MimeType: "audio/webm",
		Language: "ar-EG",
		Duration: 4 * time.Second,
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if result.Text != "مرحبا بالعالم" {
		t.Fatalf("Text = %q, want the trimmed transcript", result.Text)
	}
	if result.Model != DefaultModel || result.Language != "ar" {
		t.Fatalf("Result = %+v, want the configured model and the ar hint", result)
	}
	if provider.request.Language != "ar" {
		t.Fatalf("provider language = %q, want the ISO-639-1 subtag", provider.request.Language)
	}
	if provider.request.APIKey != "sk-test" || provider.request.Model != DefaultModel {
		t.Fatalf("provider request = %+v, want the stored credentials", provider.request)
	}
	if string(provider.audio) != "fake-opus-bytes" {
		t.Fatalf("provider audio = %q, want the uploaded bytes", provider.audio)
	}
}

func TestTranscribeRefusesWhatItCannotOrShouldNotRun(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		request Request
		wantErr error
	}{
		{
			name:    "off",
			config:  Config{Provider: ProviderOpenAI, APIKey: "sk-test", Model: DefaultModel},
			request: Request{Audio: strings.NewReader("x")},
			wantErr: ErrDisabled,
		},
		{
			name:    "on but no key",
			config:  Config{Enabled: true, Provider: ProviderOpenAI, Model: DefaultModel},
			request: Request{Audio: strings.NewReader("x")},
			wantErr: ErrDisabled,
		},
		{
			name:    "no audio",
			config:  activeConfig(),
			request: Request{},
			wantErr: ErrEmptyAudio,
		},
		{
			name:    "past the five minute ceiling",
			config:  activeConfig(),
			request: Request{Audio: strings.NewReader("x"), Duration: MaxAudioDuration + time.Second},
			wantErr: ErrTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &recordingProvider{}
			service := New(context.Background(), &memoryStore{config: tt.config},
				WithTranscriber(provider))

			_, err := service.Transcribe(context.Background(), "user@example.com", tt.request)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Transcribe error = %v, want %v", err, tt.wantErr)
			}
			if provider.calls != 0 {
				t.Fatal("a refused request must never reach the provider")
			}
		})
	}
}

// A stuck client retrying in a loop would otherwise spend the operator's
// provider budget, so the ceiling is per user and the refusal is not itself
// counted against them.
func TestTranscribeRateLimitsOneUserWithoutTouchingAnother(t *testing.T) {
	provider := &recordingProvider{text: "ok"}
	service := New(context.Background(), &memoryStore{config: activeConfig()},
		WithTranscriber(provider), WithRateLimit(2, time.Minute))

	clip := func() Request {
		return Request{Audio: strings.NewReader("bytes"), Language: "en-US"}
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := service.Transcribe(context.Background(), "a@example.com", clip()); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if _, err := service.Transcribe(context.Background(), "a@example.com", clip()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third attempt error = %v, want ErrRateLimited", err)
	}
	// The same person under a different capitalization is still that person.
	if _, err := service.Transcribe(context.Background(), "A@Example.com", clip()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("case-varied identity error = %v, want ErrRateLimited", err)
	}
	if _, err := service.Transcribe(context.Background(), "b@example.com", clip()); err != nil {
		t.Fatalf("a second user must not inherit the first user's ceiling: %v", err)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls = %d, want 3 (the two allowed plus the second user)", provider.calls)
	}
}

func TestRateLimiterReleasesTheCeilingAsTheWindowSlides(t *testing.T) {
	now := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(2, time.Minute, func() time.Time { return now })

	if !limiter.allow("user") || !limiter.allow("user") {
		t.Fatal("the first two attempts must fit in the window")
	}
	if limiter.allow("user") {
		t.Fatal("the third attempt must be refused")
	}

	now = now.Add(61 * time.Second)
	if !limiter.allow("user") {
		t.Fatal("an attempt after the window slid past must be allowed again")
	}
}

func TestSaveValidatesPersistsAndMasks(t *testing.T) {
	store := &memoryStore{config: DefaultConfig()}
	frozen := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	service := New(context.Background(), store,
		WithTranscriber(&recordingProvider{}),
		WithClock(func() time.Time { return frozen }))

	public, err := service.Save(context.Background(), UpdateInput{
		Enabled:         true,
		Provider:        ProviderOpenAI,
		APIKey:          "sk-proj-abcdwxyz",
		Model:           "whisper-1",
		DefaultLanguage: "ar-EG",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if public.APIKeyMasked != "••••wxyz" {
		t.Fatalf("returned mask = %q", public.APIKeyMasked)
	}
	if store.saved.APIKey != "sk-proj-abcdwxyz" {
		t.Fatalf("stored key = %q, want the plaintext key on disk", store.saved.APIKey)
	}
	if store.saved.UpdatedAt != frozen.UnixMilli() {
		t.Fatalf("UpdatedAt = %d, want %d", store.saved.UpdatedAt, frozen.UnixMilli())
	}
	if !service.ClientConfig().Enabled {
		t.Fatal("saving an active configuration must arm it for the composer")
	}

	if _, err := service.Save(context.Background(), UpdateInput{
		Enabled:     true,
		ClearAPIKey: true,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("clearing the key while enabled = %v, want ErrInvalidConfig", err)
	}
}

// The service must survive a settings document it cannot read: a broken voice
// fallback is not a reason to refuse to boot.
func TestNewDegradesToDefaultsWhenTheStoreFails(t *testing.T) {
	service := New(context.Background(), failingStore{})

	if service.Config().Enabled || service.ClientConfig().Enabled {
		t.Fatal("an unreadable store must leave transcription off")
	}
}

type failingStore struct{}

func (failingStore) Load(context.Context) (Config, error) {
	return Config{}, errors.New("disk on fire")
}

func (failingStore) Save(context.Context, Config) error { return nil }

func TestTestProbeSendsAOneSecondSilentWAV(t *testing.T) {
	provider := &recordingProvider{text: ""}
	frozen := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	ticks := 0
	service := New(context.Background(), &memoryStore{config: activeConfig()},
		WithTranscriber(provider),
		WithClock(func() time.Time {
			ticks++
			return frozen.Add(time.Duration(ticks) * 150 * time.Millisecond)
		}))

	result := service.Test(context.Background())

	if !result.OK || result.Error != "" {
		t.Fatalf("Test = %+v, want a successful round trip", result)
	}
	if result.DurationMS <= 0 {
		t.Fatalf("DurationMS = %d, want the measured round trip", result.DurationMS)
	}
	if provider.request.Filename != "silence.wav" || provider.request.MimeType != "audio/wav" {
		t.Fatalf("probe request = %+v, want a wav sample", provider.request)
	}
	// 44-byte RIFF header plus one second of mono 16 kHz 16-bit silence.
	if len(provider.audio) != 44+32000 {
		t.Fatalf("probe payload = %d bytes, want 32044", len(provider.audio))
	}
	if string(provider.audio[:4]) != "RIFF" || string(provider.audio[8:12]) != "WAVE" {
		t.Fatalf("probe payload is not a WAV: %q", provider.audio[:12])
	}
}

func TestTestProbeReportsAProviderFailureInsteadOfHidingIt(t *testing.T) {
	service := New(context.Background(), &memoryStore{config: activeConfig()},
		WithTranscriber(&recordingProvider{err: errors.New("invalid_api_key")}))

	result := service.Test(context.Background())

	if result.OK || !strings.Contains(result.Error, "invalid_api_key") {
		t.Fatalf("Test = %+v, want the provider failure surfaced", result)
	}
}

func TestTestProbeRefusesBeforeAKeyIsStored(t *testing.T) {
	service := New(context.Background(), &memoryStore{config: DefaultConfig()},
		WithTranscriber(&recordingProvider{}))

	result := service.Test(context.Background())

	if result.OK || result.Error == "" {
		t.Fatalf("Test = %+v, want a refusal explaining the missing key", result)
	}
}

// The OpenAI client is the one place that speaks HTTP, so it is pinned
// against a stand-in endpoint rather than a mock of itself.
func TestOpenAIClientSendsTheDocumentedMultipartRequest(t *testing.T) {
	var (
		gotAuth     string
		gotModel    string
		gotLanguage string
		gotAudio    []byte
		gotFilename string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("parse content type: %v", err)
			return
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			body, _ := io.ReadAll(part)
			switch part.FormName() {
			case "file":
				gotAudio = body
				gotFilename = part.FileName()
			case "model":
				gotModel = string(body)
			case "language":
				gotLanguage = string(body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"صباح الخير"}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(server.URL, server.Client())
	text, err := client.Transcribe(context.Background(), ProviderRequest{
		Audio:    strings.NewReader("opus-bytes"),
		Filename: "dictation.webm",
		Model:    DefaultModel,
		APIKey:   "sk-secret",
		Language: "ar",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if text != "صباح الخير" {
		t.Fatalf("text = %q", text)
	}
	if gotAuth != "Bearer sk-secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotModel != DefaultModel || gotLanguage != "ar" {
		t.Fatalf("model = %q, language = %q", gotModel, gotLanguage)
	}
	if string(gotAudio) != "opus-bytes" || gotFilename != "dictation.webm" {
		t.Fatalf("audio part = %q named %q", gotAudio, gotFilename)
	}
}

func TestOpenAIClientOmitsAnEmptyLanguageHint(t *testing.T) {
	sawLanguage := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			if part.FormName() == "language" {
				sawLanguage = true
			}
			_, _ = io.Copy(io.Discard, part)
		}
		_, _ = w.Write([]byte(`{"text":"hello"}`))
	}))
	defer server.Close()

	if _, err := NewOpenAIClient(server.URL, server.Client()).Transcribe(
		context.Background(),
		ProviderRequest{Audio: strings.NewReader("x"), Model: DefaultModel, APIKey: "sk"},
	); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if sawLanguage {
		t.Fatal("an empty hint must be left out so the provider auto-detects")
	}
}

func TestOpenAIClientQuotesTheProviderErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
	}))
	defer server.Close()

	_, err := NewOpenAIClient(server.URL, server.Client()).Transcribe(
		context.Background(),
		ProviderRequest{Audio: strings.NewReader("x"), Model: DefaultModel, APIKey: "sk-wrong"},
	)

	if err == nil || !strings.Contains(err.Error(), "Incorrect API key provided") {
		t.Fatalf("error = %v, want the provider message", err)
	}
	if strings.Contains(err.Error(), "sk-wrong") {
		t.Fatal("the error must not echo the API key")
	}
}

func TestOpenAIClientRefusesWithoutAKey(t *testing.T) {
	_, err := NewOpenAIClient("", nil).Transcribe(context.Background(), ProviderRequest{
		Audio: strings.NewReader("x"),
		Model: DefaultModel,
	})
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("error = %v, want a missing-key refusal that never leaves the process", err)
	}
}
