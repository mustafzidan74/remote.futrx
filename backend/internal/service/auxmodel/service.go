package auxmodel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	// ErrDisabled reports that the auxiliary model is off, unconfigured, or
	// that this particular job's toggle is off. Every caller answers it by
	// doing exactly what it did before this feature existed.
	ErrDisabled = errors.New("the auxiliary model is not available")
	// ErrBreakerOpen reports that the endpoint failed repeatedly and is being
	// left alone for a while. It is a distinct error only so a caller can log
	// it differently; the fallback is identical.
	ErrBreakerOpen = errors.New("the auxiliary model is temporarily disabled after repeated failures")
	// ErrEmptyInput reports a job asked for with nothing to work on.
	ErrEmptyInput = errors.New("nothing to send to the auxiliary model")
)

const (
	// breakerThreshold is how many consecutive failures open the breaker. An
	// endpoint that is simply slow once must not stop the feature; an
	// endpoint that is down should stop being dialled on every chat title.
	breakerThreshold = 3
	// breakerCooldown is how long the breaker stays open. Five minutes is
	// long enough that a restarting Ollama finishes, and short enough that a
	// fixed endpoint comes back without anyone touching the settings.
	breakerCooldown = 5 * time.Minute
	// maxInputRunes bounds what any job may send. It is a cost ceiling, not a
	// correctness one: every caller already trims its own input, and this is
	// the backstop that keeps a runaway transcript out of a 3B context.
	maxInputRunes = 6000
)

// Store persists the single global auxiliary-model document.
type Store interface {
	Load(ctx context.Context) (Config, error)
	Save(ctx context.Context, cfg Config) error
}

// Service owns the configuration cache, the provider client, and the breaker.
// It holds no per-caller state: every job is a single request/response.
type Service struct {
	store  Store
	client *http.Client
	// completer, when set, replaces the provider client entirely. Tests use
	// it; production leaves it nil and gets the client the provider names.
	completer Completer
	now       func() time.Time

	mu     sync.RWMutex
	config Config

	breakerMu       sync.Mutex
	failures        int
	openedUntil     time.Time
	openLogged      bool
	breakerCooldown time.Duration
}

type Option func(*Service)

// WithHTTPClient replaces the transport. Tests point it at an httptest server.
func WithHTTPClient(client *http.Client) Option {
	return func(s *Service) {
		if client != nil {
			s.client = client
		}
	}
}

// WithCompleter replaces the provider client outright, so a test can assert
// what was asked for without speaking either wire format.
func WithCompleter(completer Completer) Option {
	return func(s *Service) {
		if completer != nil {
			s.completer = completer
		}
	}
}

// WithClock replaces the clock behind UpdatedAt, the breaker window, and the
// latency the Test button reports.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithBreakerCooldown replaces how long the breaker stays open, so a test
// does not have to wait five minutes to prove it closes again.
func WithBreakerCooldown(cooldown time.Duration) Option {
	return func(s *Service) {
		if cooldown > 0 {
			s.breakerCooldown = cooldown
		}
	}
}

// New loads the stored configuration. A missing or unreadable document
// degrades to defaults, so a problem with an optional nice-to-have can never
// keep the server from booting.
func New(ctx context.Context, store Store, options ...Option) *Service {
	service := &Service{
		store:           store,
		client:          &http.Client{},
		now:             time.Now,
		config:          DefaultConfig(),
		breakerCooldown: breakerCooldown,
	}
	if store != nil {
		loaded, err := store.Load(ctx)
		if err != nil {
			log.Printf("auxmodel: reading stored settings failed, the auxiliary model stays off: %v", err)
		} else {
			service.config = loaded.Normalize()
		}
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
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
	if s == nil {
		return DefaultConfig().Public()
	}
	return s.Config().Public()
}

// ClientConfig returns the member-facing view every signed-in user may read.
func (s *Service) ClientConfig() ClientConfig {
	if s == nil {
		return Config{}.Client()
	}
	return s.Config().Client()
}

// Available reports whether one job may be attempted right now. Callers use
// it to decide whether to offer a button; Complete re-checks it anyway,
// because the answer can change between the two.
func (s *Service) Available(job Job) bool {
	if s == nil {
		return false
	}
	config := s.Config()
	return config.Active() && config.Jobs.Enabled(job) && !s.breakerOpen()
}

// Save validates and persists an update, then arms the new configuration. A
// successful save closes the breaker: the operator has just told us something
// changed, and refusing to try for another five minutes would make the Test
// button lie.
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
			return PublicConfig{}, fmt.Errorf("save auxiliary model settings: %w", err)
		}
	}
	s.mu.Lock()
	s.config = next
	s.mu.Unlock()
	s.resetBreaker()
	return next.Public(), nil
}

// Complete runs one job. It is the only way anything in this platform reaches
// the auxiliary model.
//
// Three guards stand in front of the request and each of them answers with an
// error the caller turns into its own fallback: the service or the job is
// switched off, the breaker is open, or there is nothing to send. Past them,
// the request carries a hard timeout and a per-job token cap.
func (s *Service) Complete(
	ctx context.Context,
	job Job,
	systemPrompt string,
	userText string,
) (string, error) {
	if s == nil {
		return "", ErrDisabled
	}
	config := s.Config()
	if !config.Active() {
		return "", ErrDisabled
	}
	if !config.Jobs.Enabled(job) {
		return "", fmt.Errorf("%w: %s are switched off", ErrDisabled, JobLabel(job))
	}
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", ErrEmptyInput
	}
	if s.breakerOpen() {
		return "", ErrBreakerOpen
	}

	// The caller's context may already be cancelled — a settled run hands the
	// observer a dead one — so the timeout hangs off a fresh background
	// context rather than inheriting a corpse. Cancellation of a *live*
	// request still works: ctx is honoured when it is not already done.
	parent := ctx
	if parent == nil || parent.Err() != nil {
		parent = context.Background()
	}
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	requestCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	answer, err := s.completerFor(config).Complete(requestCtx, Completion{
		BaseURL:      config.BaseURL,
		Model:        config.Model,
		APIKey:       config.APIKey,
		SystemPrompt: systemPrompt,
		UserText:     Truncate(userText, maxInputRunes),
		MaxTokens:    tokenCap(config, job),
	})
	if err != nil {
		s.recordFailure(err)
		return "", err
	}
	s.recordSuccess()
	return answer, nil
}

// Test runs a real one-sentence completion so an operator can prove the
// endpoint, the model, and the key all work, and see what the round trip
// costs in wall-clock time.
func (s *Service) Test(ctx context.Context) TestResult {
	config := s.Config()
	result := TestResult{Provider: config.Provider, Model: config.Model, BaseURL: config.BaseURL}
	if s == nil {
		result.Error = "the auxiliary model is unavailable"
		return result
	}
	if !config.Configured() {
		result.Error = "set a base URL and a model first"
		return result
	}

	timeout := time.Duration(config.Normalize().TimeoutSeconds) * time.Second
	requestCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	started := s.now()
	answer, err := s.completerFor(config).Complete(requestCtx, Completion{
		BaseURL:      config.BaseURL,
		Model:        config.Model,
		APIKey:       config.APIKey,
		SystemPrompt: TestSystemPrompt,
		UserText:     TestUserText,
		MaxTokens:    64,
	})
	result.DurationMS = s.now().Sub(started).Milliseconds()
	if err != nil {
		result.Error = collapse(err.Error())
		// A deliberate probe should not leave the breaker half-tripped for
		// the jobs, but it should count: an operator who tests three times
		// against a dead box has proved it is dead.
		s.recordFailure(err)
		return result
	}
	// The probe is the one call that closes the breaker on success, which is
	// what makes "fix the endpoint, press Test" put the feature back to work
	// without waiting out the cooldown.
	s.resetBreaker()
	result.OK = true
	result.Answer = Truncate(answer, 400)
	return result
}

// TestResult reports the admin round-trip probe.
type TestResult struct {
	OK         bool   `json:"ok"`
	Provider   string `json:"provider"`
	BaseURL    string `json:"baseUrl"`
	Model      string `json:"model"`
	DurationMS int64  `json:"durationMs"`
	Answer     string `json:"answer,omitempty"`
	Error      string `json:"error,omitempty"`
}

// TestSystemPrompt and TestUserText are the probe. They ask for one short
// sentence so a slow model still answers inside the configured timeout, and
// so the panel can print the reply verbatim.
const (
	TestSystemPrompt = "You are a terse assistant. Answer in exactly one short sentence."
	TestUserText     = "In one sentence, say that you are working and name the model you are."
)

func (s *Service) completerFor(config Config) Completer {
	if s.completer != nil {
		return s.completer
	}
	return ClientFor(config.Provider, s.client)
}

// tokenCap is the smaller of the operator's ceiling and the job's own. The
// job cap is what keeps a chat title from costing as much as a summary.
func tokenCap(config Config, job Job) int {
	ceiling := maxOutputTokens(job)
	if config.MaxTokens > 0 && config.MaxTokens < ceiling {
		return config.MaxTokens
	}
	return ceiling
}

/* ------------------------------------------------------------------ *
 * Circuit breaker
 * ------------------------------------------------------------------ */

// breakerOpen reports whether the endpoint is being left alone. It also
// closes an expired breaker, so the next caller after the cooldown is the one
// that retries — no timer, no goroutine.
func (s *Service) breakerOpen() bool {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	if s.openedUntil.IsZero() {
		return false
	}
	if s.now().Before(s.openedUntil) {
		return true
	}
	s.failures = 0
	s.openedUntil = time.Time{}
	s.openLogged = false
	return false
}

// recordFailure counts one failed call and opens the breaker at the
// threshold. The open state is logged exactly once: this runs behind chat
// titles and notifications, and an endpoint that is down must not write a log
// line per chat.
func (s *Service) recordFailure(cause error) {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	s.failures++
	if s.failures < breakerThreshold {
		return
	}
	cooldown := s.breakerCooldown
	if cooldown <= 0 {
		cooldown = breakerCooldown
	}
	s.openedUntil = s.now().Add(cooldown)
	if s.openLogged {
		return
	}
	s.openLogged = true
	log.Printf(
		"auxmodel: %d consecutive failures, pausing auxiliary-model jobs for %s (last error: %s)",
		s.failures, cooldown, collapse(cause.Error()),
	)
}

func (s *Service) recordSuccess() {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	s.failures = 0
	s.openedUntil = time.Time{}
	s.openLogged = false
}

func (s *Service) resetBreaker() {
	s.recordSuccess()
}

/* ------------------------------------------------------------------ *
 * Text helpers shared by the callers
 * ------------------------------------------------------------------ */

// Truncate caps a string at a rune count, marking that it was cut. Every job
// trims its own input with this before handing it over.
func Truncate(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}

// OneLine folds a model's answer into a single line and strips the wrappers
// small models like to add: surrounding quotes, a leading "Title:", a
// trailing full stop on a fragment. It is applied to every short job, because
// a 3B model asked for six words will sometimes answer with a paragraph
// introducing its six words.
func OneLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// A reasoning-style preamble in <think> tags is not part of the answer.
	if _, after, found := strings.Cut(text, "</think>"); found {
		text = strings.TrimSpace(after)
	}
	// Keep the first non-empty line: models add "Here is the title:" above or
	// an explanation below, and the answer itself is always one of the lines.
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isPreamble(line) {
			continue
		}
		text = line
		break
	}
	text = strings.Join(strings.Fields(text), " ")
	text = strings.Trim(text, "\"'“”‘’`*")
	for _, prefix := range []string{"Title:", "title:", "Summary:", "summary:", "-", "•"} {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	return strings.TrimSpace(text)
}

// isPreamble spots the "here is your answer" line a chatty small model puts
// above the thing that was asked for.
func isPreamble(line string) bool {
	lower := strings.ToLower(line)
	if !strings.HasSuffix(lower, ":") {
		return false
	}
	return strings.HasPrefix(lower, "here") || strings.HasPrefix(lower, "sure") ||
		strings.HasPrefix(lower, "certainly") || strings.HasPrefix(lower, "okay") ||
		strings.HasPrefix(lower, "ok")
}
