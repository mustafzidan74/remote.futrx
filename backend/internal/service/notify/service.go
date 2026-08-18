package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrInvalidConfig is returned for configurations the operator can fix.
var ErrInvalidConfig = errors.New("invalid notification settings")

// testTimeout bounds the synchronous admin "send test" probe.
const testTimeout = 30 * time.Second

// Store persists the single global configuration document.
type Store interface {
	Load(ctx context.Context) (Config, error)
	Save(ctx context.Context, cfg Config) error
}

// Service owns the configuration cache and the notifier. Handlers talk to it;
// the run and schedule pipelines publish through Publish, which never blocks.
type Service struct {
	store    Store
	notifier *Notifier
	baseURL  string
	now      func() time.Time

	digestSource   DigestSource
	digestInterval time.Duration
	digestOnce     sync.Once
	digestStopped  chan struct{}
	digestStopOnce sync.Once

	mu     sync.RWMutex
	config Config
}

type Option func(*Service)

// WithNotifier replaces the notifier. Tests use it to install fake sinks.
func WithNotifier(notifier *Notifier) Option {
	return func(s *Service) {
		if notifier != nil {
			s.notifier = notifier
		}
	}
}

// WithDigestSource attaches the usage ledger the weekly digest aggregates.
// Without it the digest loop never starts and "send digest now" reports that
// the ledger is unavailable.
func WithDigestSource(source DigestSource) Option {
	return func(s *Service) {
		if source != nil {
			s.digestSource = source
		}
	}
}

// WithDigestInterval replaces how often the digest loop wakes up. Tests use it
// to avoid waiting for the production tick.
func WithDigestInterval(interval time.Duration) Option {
	return func(s *Service) {
		if interval > 0 {
			s.digestInterval = interval
		}
	}
}

// WithClock replaces the clock used for UpdatedAt and event timestamps.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// New loads the stored configuration and starts the delivery worker. A missing
// or unreadable document degrades to defaults so notification problems can
// never keep the server from booting.
func New(ctx context.Context, store Store, baseURL string, options ...Option) *Service {
	service := &Service{
		store:          store,
		baseURL:        strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		now:            time.Now,
		config:         DefaultConfig(),
		digestInterval: defaultDigestInterval,
		digestStopped:  make(chan struct{}),
	}
	if store != nil {
		loaded, err := store.Load(ctx)
		if err != nil {
			log.Printf("notify: reading stored settings failed, notifications stay off: %v", err)
		} else {
			service.config = loaded.Normalize()
		}
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	if service.notifier == nil {
		service.notifier = NewNotifier(service.Config)
	} else {
		service.notifier.config = service.Config
	}
	service.notifier.Start(ctx)
	service.StartDigest(ctx)
	return service
}

// Stop shuts the delivery worker down.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.digestStopOnce.Do(func() { close(s.digestStopped) })
	s.notifier.Stop()
}

// Config returns the live configuration, secrets included. It is the notifier's
// config source and must not be exposed over HTTP.
func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// PublicConfig returns the admin-facing, secret-masked view.
func (s *Service) PublicConfig() PublicConfig {
	return s.Config().Public()
}

// Save validates and persists an update, then arms the new configuration.
func (s *Service) Save(ctx context.Context, input UpdateInput) (PublicConfig, error) {
	if s == nil {
		return PublicConfig{}, errors.New("notifications are unavailable")
	}
	next := s.Config().Apply(input)
	if err := validate(next); err != nil {
		return PublicConfig{}, err
	}
	next.UpdatedAt = s.now().UnixMilli()

	if s.store != nil {
		if err := s.store.Save(ctx, next); err != nil {
			return PublicConfig{}, fmt.Errorf("save notification settings: %w", err)
		}
	}
	s.mu.Lock()
	s.config = next
	s.mu.Unlock()
	return next.Public(), nil
}

// Test delivers a synthetic event to every configured sink and reports each
// sink's outcome so an operator can debug credentials without waiting for a
// real agent run.
func (s *Service) Test(ctx context.Context) []SinkResult {
	if s == nil {
		return nil
	}
	// Bound the whole probe: retries across two sinks could otherwise hold an
	// admin request open for more than a minute.
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	return s.notifier.SendTest(ctx, Event{
		Event:   KindTest,
		Status:  StatusFinished,
		Summary: "Notifications from Remote are working. You will be pinged when an agent run finishes, fails, or needs you.",
		URL:     ChatURL(s.baseURL, ""),
		At:      s.now().UnixMilli(),
	})
}

// Publish queues an event for delivery. It fills in the timestamp and the deep
// link, and returns immediately.
func (s *Service) Publish(event Event) {
	if s == nil {
		return
	}
	if event.At == 0 {
		event.At = s.now().UnixMilli()
	}
	if event.URL == "" {
		event.URL = ChatURL(s.baseURL, event.ChatID)
	}
	s.notifier.Publish(event)
}

func validate(cfg Config) error {
	cfg = cfg.Normalize()
	if cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID == "" {
		return fmt.Errorf("%w: a Telegram chat ID is required alongside the bot token", ErrInvalidConfig)
	}
	if cfg.Telegram.ChatID != "" && cfg.Telegram.BotToken != "" && strings.ContainsAny(cfg.Telegram.ChatID, " \t") {
		return fmt.Errorf("%w: the Telegram chat ID must not contain spaces", ErrInvalidConfig)
	}
	if cfg.Webhook.URL != "" {
		parsed, err := url.Parse(cfg.Webhook.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%w: the webhook URL must be an absolute http(s) URL", ErrInvalidConfig)
		}
	}
	if cfg.Webhook.Secret != "" && cfg.Webhook.URL == "" {
		return fmt.Errorf("%w: a webhook URL is required alongside the shared secret", ErrInvalidConfig)
	}
	if err := validateWhatsApp(cfg.WhatsApp); err != nil {
		return err
	}
	if cfg.Enabled && !cfg.TelegramConfigured() && !cfg.WebhookConfigured() && !cfg.WhatsAppConfigured() {
		return fmt.Errorf(
			"%w: configure Telegram, WhatsApp, or a webhook before enabling notifications",
			ErrInvalidConfig,
		)
	}
	if cfg.Digest.Enabled && !cfg.Enabled {
		return fmt.Errorf("%w: turn notifications on before scheduling the weekly digest", ErrInvalidConfig)
	}
	return nil
}

// validateWhatsApp rejects the half-filled combinations that would otherwise
// fail silently at delivery time, once per event, in the server log.
func validateWhatsApp(cfg WhatsAppConfig) error {
	cfg = cfg.normalize()
	if cfg.Cloud.AccessToken != "" && (cfg.Cloud.PhoneNumberID == "" || cfg.Cloud.Recipient == "") {
		return fmt.Errorf(
			"%w: the WhatsApp Cloud API needs a phone number ID and a recipient alongside the access token",
			ErrInvalidConfig,
		)
	}
	if cfg.Cloud.TemplateName != "" && !validTemplateName(cfg.Cloud.TemplateName) {
		return fmt.Errorf(
			"%w: a WhatsApp template name may only contain lowercase letters, digits, and underscores",
			ErrInvalidConfig,
		)
	}
	if cfg.CallMeBot.APIKey != "" && cfg.CallMeBot.Phone == "" {
		return fmt.Errorf("%w: CallMeBot needs your WhatsApp number alongside the API key", ErrInvalidConfig)
	}
	switch cfg.Provider {
	case WhatsAppProviderCloud:
		if !cfg.Cloud.configured() {
			return fmt.Errorf(
				"%w: fill in the phone number ID, access token, and recipient for the WhatsApp Cloud API",
				ErrInvalidConfig,
			)
		}
	case WhatsAppProviderCallMeBot:
		if !cfg.CallMeBot.configured() {
			return fmt.Errorf("%w: fill in your WhatsApp number and API key for CallMeBot", ErrInvalidConfig)
		}
	}
	return nil
}

// validTemplateName mirrors Meta's own naming rule for message templates.
func validTemplateName(name string) bool {
	if len(name) > maxTemplateNameLength {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}

// defaultDigestInterval is how often the weekly-digest loop wakes up. The
// schedule has hour granularity, so a coarse tick is plenty and keeps an idle
// server quiet.
const defaultDigestInterval = 5 * time.Minute

// maxTemplateNameLength bounds the WhatsApp template name an operator may
// store, matching Meta's own limit.
const maxTemplateNameLength = 512

// StartDigest launches the weekly-digest loop. It is idempotent, and a nil
// digest source (no usage ledger on this deployment) makes it a no-op.
func (s *Service) StartDigest(ctx context.Context) {
	if s == nil || s.digestSource == nil {
		return
	}
	s.digestOnce.Do(func() {
		go s.runDigestLoop(ctx)
	})
}

func (s *Service) runDigestLoop(ctx context.Context) {
	ticker := time.NewTicker(s.digestInterval)
	defer ticker.Stop()
	// Run one pass immediately so a server that was down over the scheduled
	// hour still reports as soon as it is back.
	s.digestTick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.digestStopped:
			return
		case <-ticker.C:
			s.digestTick(ctx)
		}
	}
}

// digestTick sends at most one digest. The occurrence it fires for is written
// back to the store before anything else can tick, so a restart, a second
// worker pass, or a slow sink cannot produce two reports for one week.
func (s *Service) digestTick(ctx context.Context) {
	cfg := s.Config()
	occurrence, due := cfg.Digest.DueAt(s.now())
	if !due {
		// A schedule that has never sent is armed here rather than fired, so
		// enabling the digest does not immediately deliver last week's report.
		if cfg.Digest.Enabled && cfg.Digest.LastSentAt <= 0 {
			s.markDigestSent(ctx, occurrence.UnixMilli())
		}
		return
	}
	if !s.markDigestSent(ctx, occurrence.UnixMilli()) {
		return
	}
	event, err := s.digestEvent(ctx, occurrence)
	if err != nil {
		log.Printf("notify: weekly digest aggregation failed: %v", err)
		return
	}
	s.Publish(event)
}

// markDigestSent claims an occurrence. It reports false when another pass
// already claimed this one, which is what keeps the digest idempotent.
func (s *Service) markDigestSent(ctx context.Context, occurrenceMilli int64) bool {
	s.mu.Lock()
	if s.config.Digest.LastSentAt >= occurrenceMilli {
		s.mu.Unlock()
		return false
	}
	next := s.config
	next.Digest.LastSentAt = occurrenceMilli
	s.config = next
	s.mu.Unlock()

	if s.store == nil {
		return true
	}
	if err := s.store.Save(ctx, next); err != nil {
		log.Printf("notify: recording the weekly digest delivery failed: %v", err)
	}
	return true
}

// digestEvent aggregates the seven days before occurrence into one event.
func (s *Service) digestEvent(ctx context.Context, occurrence time.Time) (Event, error) {
	if s.digestSource == nil {
		return Event{}, errors.New("the usage ledger is unavailable")
	}
	from, to := DigestWindow(occurrence)
	digest, err := s.digestSource.WeeklyDigest(ctx, from, to)
	if err != nil {
		return Event{}, err
	}
	digest.From, digest.To = from, to
	return Event{
		Event:     KindDigest,
		Status:    StatusFinished,
		Summary:   DigestSummary(digest, s.Config().Digest.Location()) + "\nOpen Settings → Usage in Remote for the full breakdown.",
		URL:       UsageURL(s.baseURL),
		At:        occurrence.UnixMilli(),
		DedupeKey: fmt.Sprintf("digest:%d", occurrence.UnixMilli()),
	}, nil
}

// SendDigestNow builds the current week's digest and delivers it synchronously
// to every configured sink, reporting each outcome. It is the debugging twin
// of Test: it bypasses the schedule and never moves LastSentAt, so using it
// cannot make the operator miss the real report.
func (s *Service) SendDigestNow(ctx context.Context) ([]SinkResult, error) {
	if s == nil {
		return nil, errors.New("notifications are unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	event, err := s.digestEvent(ctx, s.now())
	if err != nil {
		return nil, err
	}
	return s.notifier.SendTest(ctx, event), nil
}
