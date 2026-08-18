package transcribe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

// Limits on one uploaded clip. They are enforced in the handler (bytes) and
// here (duration), and reported to the composer through ClientConfig so the
// browser stops recording before it wastes an upload.
const (
	// MaxAudioBytes is the hard ceiling on one upload: 25 MB, which is also
	// the provider's own limit.
	MaxAudioBytes int64 = 25 << 20
	// MaxAudioDuration bounds a single dictation.
	MaxAudioDuration = 5 * time.Minute
	// RequestTimeout bounds the whole round trip to the provider.
	RequestTimeout = 60 * time.Second
)

var (
	// ErrDisabled is returned when server transcription is off or unconfigured.
	ErrDisabled = errors.New("server transcription is not configured")
	// ErrTooLong is returned for a clip past MaxAudioDuration.
	ErrTooLong = errors.New("recording is too long")
	// ErrRateLimited is returned when a user exceeds the per-minute ceiling.
	ErrRateLimited = errors.New("too many transcription requests")
	// ErrEmptyAudio is returned when the request carried no audio at all.
	ErrEmptyAudio = errors.New("no audio was uploaded")
)

// Store persists the single global configuration document.
type Store interface {
	Load(ctx context.Context) (Config, error)
	Save(ctx context.Context, cfg Config) error
}

// Request is one dictation. Audio is read exactly once and never retained.
type Request struct {
	Audio    io.Reader
	Filename string
	MimeType string
	// Language is the BCP-47 tag the user picked in the composer ("ar-EG"),
	// or "auto"/"" to let the provider detect it.
	Language string
	// Duration is what the browser measured. It is advisory — the byte cap is
	// the real defence — but it is what the audit entry records.
	Duration time.Duration
}

// Result is what the composer inserts.
type Result struct {
	Text     string `json:"text"`
	Model    string `json:"model,omitempty"`
	Language string `json:"language,omitempty"`
}

// TestResult reports an admin round-trip probe.
type TestResult struct {
	OK         bool   `json:"ok"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	DurationMS int64  `json:"durationMs"`
	Text       string `json:"text,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Option func(*Service)

// WithTranscriber replaces the provider client. Tests use it to point at an
// httptest server or to assert what the service asked for.
func WithTranscriber(client Transcriber) Option {
	return func(s *Service) {
		if client != nil {
			s.provider = client
		}
	}
}

// WithClock replaces the clock used for UpdatedAt, probe timing, and the rate
// limiter's window.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithRateLimit replaces the per-user ceiling.
func WithRateLimit(limit int, window time.Duration) Option {
	return func(s *Service) {
		s.rateLimit = limit
		s.rateWindow = window
	}
}

// Service owns the configuration cache and the provider client. It holds no
// audio: every clip is streamed from the request straight to the provider.
type Service struct {
	store    Store
	provider Transcriber
	now      func() time.Time

	rateLimit  int
	rateWindow time.Duration
	limiter    *rateLimiter

	mu     sync.RWMutex
	config Config
}

// New loads the stored configuration and returns a ready service. A missing or
// unreadable document degrades to defaults so a transcription problem can
// never keep the server from booting.
func New(ctx context.Context, store Store, options ...Option) *Service {
	service := &Service{
		store:      store,
		now:        time.Now,
		config:     DefaultConfig(),
		rateLimit:  DefaultRateLimit,
		rateWindow: DefaultRateWindow,
	}
	if store != nil {
		loaded, err := store.Load(ctx)
		if err != nil {
			log.Printf("transcribe: reading stored settings failed, voice fallback stays off: %v", err)
		} else {
			service.config = loaded.Normalize()
		}
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	if service.provider == nil {
		service.provider = NewOpenAIClient("", nil)
	}
	service.limiter = newRateLimiter(service.rateLimit, service.rateWindow, func() time.Time {
		return service.now()
	})
	return service
}

// Config returns the live configuration, key included. It must never be
// exposed over HTTP.
func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// PublicConfig returns the admin-facing, key-masked view.
func (s *Service) PublicConfig() PublicConfig {
	return s.Config().Public()
}

// ClientConfig returns the composer-facing view every signed-in user may read.
func (s *Service) ClientConfig() ClientConfig {
	if s == nil {
		return ClientConfig{MaxBytes: MaxAudioBytes, MaxSeconds: int(MaxAudioDuration.Seconds())}
	}
	return s.Config().Client()
}

// Save validates and persists an update, then arms the new configuration.
func (s *Service) Save(ctx context.Context, input UpdateInput) (PublicConfig, error) {
	if s == nil {
		return PublicConfig{}, ErrDisabled
	}
	next := s.Config().Apply(input)
	if err := validate(next); err != nil {
		return PublicConfig{}, err
	}
	next.UpdatedAt = s.now().UnixMilli()

	if s.store != nil {
		if err := s.store.Save(ctx, next); err != nil {
			return PublicConfig{}, fmt.Errorf("save transcription settings: %w", err)
		}
	}
	s.mu.Lock()
	s.config = next
	s.mu.Unlock()
	return next.Public(), nil
}

// Transcribe streams one clip to the provider and returns its text. `user` is
// the rate-limiting key; it is never sent anywhere.
func (s *Service) Transcribe(ctx context.Context, user string, req Request) (Result, error) {
	if s == nil {
		return Result{}, ErrDisabled
	}
	config := s.Config()
	if !config.Active() {
		return Result{}, ErrDisabled
	}
	if req.Audio == nil {
		return Result{}, ErrEmptyAudio
	}
	if req.Duration > MaxAudioDuration {
		return Result{}, fmt.Errorf("%w: %s is over the %s limit",
			ErrTooLong, req.Duration.Round(time.Second), MaxAudioDuration)
	}
	if !s.limiter.allow(strings.ToLower(strings.TrimSpace(user))) {
		return Result{}, ErrRateLimited
	}

	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()

	hint := LanguageHint(req.Language)
	text, err := s.provider.Transcribe(ctx, ProviderRequest{
		Audio:    req.Audio,
		Filename: audioFilename(req.Filename, req.MimeType),
		MimeType: req.MimeType,
		Model:    config.Model,
		APIKey:   config.APIKey,
		Language: hint,
	})
	if err != nil {
		return Result{}, err
	}
	// The composer inserts this at the caret, so it must not arrive with the
	// leading and trailing whitespace some providers pad a transcript with.
	return Result{Text: strings.TrimSpace(text), Model: config.Model, Language: hint}, nil
}

// Test transcribes a one-second silent sample so an operator can prove the key
// and model work without speaking into a microphone. Silence usually comes
// back as empty text; what is being checked is the round trip, not the words.
func (s *Service) Test(ctx context.Context) TestResult {
	config := s.Config()
	result := TestResult{Provider: config.Provider, Model: config.Model}
	if s == nil || !config.Configured() {
		result.Error = "add an API key first"
		return result
	}

	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()

	started := s.now()
	text, err := s.provider.Transcribe(ctx, ProviderRequest{
		Audio:    SilentSampleWAV(time.Second),
		Filename: "silence.wav",
		MimeType: "audio/wav",
		Model:    config.Model,
		APIKey:   config.APIKey,
		Language: LanguageHint(config.DefaultLanguage),
	})
	result.DurationMS = s.now().Sub(started).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.Text = text
	return result
}

// audioFilename gives the provider an extension it can dispatch on. The
// browser sends webm/opus; the admin probe sends wav.
func audioFilename(filename, mimeType string) string {
	if name := strings.TrimSpace(filename); name != "" {
		return name
	}
	switch {
	case strings.Contains(mimeType, "wav"):
		return "audio.wav"
	case strings.Contains(mimeType, "ogg"):
		return "audio.ogg"
	case strings.Contains(mimeType, "mp4"), strings.Contains(mimeType, "mpeg"):
		return "audio.mp4"
	default:
		return "audio.webm"
	}
}

// SilentSampleWAV builds a mono 16 kHz 16-bit PCM WAV of pure silence. It is
// the admin "Test" payload: small, valid, and free of anything private.
func SilentSampleWAV(duration time.Duration) io.Reader {
	const sampleRate = 16000
	const bitsPerSample = 16
	const channels = 1

	if duration <= 0 {
		duration = time.Second
	}
	samples := int(duration.Seconds() * sampleRate)
	dataBytes := samples * channels * bitsPerSample / 8

	buffer := make([]byte, 0, 44+dataBytes)
	buffer = append(buffer, "RIFF"...)
	buffer = binary.LittleEndian.AppendUint32(buffer, uint32(36+dataBytes))
	buffer = append(buffer, "WAVEfmt "...)
	buffer = binary.LittleEndian.AppendUint32(buffer, 16) // PCM chunk size
	buffer = binary.LittleEndian.AppendUint16(buffer, 1)  // PCM format
	buffer = binary.LittleEndian.AppendUint16(buffer, channels)
	buffer = binary.LittleEndian.AppendUint32(buffer, sampleRate)
	buffer = binary.LittleEndian.AppendUint32(buffer, sampleRate*channels*bitsPerSample/8)
	buffer = binary.LittleEndian.AppendUint16(buffer, channels*bitsPerSample/8)
	buffer = binary.LittleEndian.AppendUint16(buffer, bitsPerSample)
	buffer = append(buffer, "data"...)
	buffer = binary.LittleEndian.AppendUint32(buffer, uint32(dataBytes))
	buffer = append(buffer, make([]byte, dataBytes)...)

	return strings.NewReader(string(buffer))
}
