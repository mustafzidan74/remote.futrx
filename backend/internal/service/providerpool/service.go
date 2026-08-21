package providerpool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// Need is what a caller wants from the pool. Everything on it is a hint
// except ProviderID, which is a pin.
type Need struct {
	// Job is the ledger label — "chatTitle", "bulk", "translate". It also
	// picks the default capability when Want is empty.
	Job string
	// Want narrows which models are eligible. Empty derives it from Job.
	Want Capability
	// MinContext skips models whose declared context window is too small. A
	// model that declares no context window is never skipped: an unknown is
	// not a "no".
	MinContext int
	// PreferModel pins a model id when the chosen provider has it, and is
	// ignored when it does not.
	PreferModel string
	// ProviderID pins one provider for this call, whatever the pool's mode.
	ProviderID string
}

// Capability resolves what the need is asking for.
func (n Need) Capability() Capability {
	switch n.Want {
	case CapabilityText, CapabilityCode, CapabilityBulk:
		return n.Want
	}
	switch strings.ToLower(strings.TrimSpace(n.Job)) {
	case "bulk":
		return CapabilityBulk
	case "code", "commitmessage":
		return CapabilityCode
	default:
		return CapabilityText
	}
}

// Request is one completion through the pool.
type Request struct {
	Need
	System    string
	Prompt    string
	MaxTokens int
}

// Result is one answer, plus which provider actually produced it.
type Result struct {
	Text             string `json:"text"`
	ProviderID       string `json:"providerId"`
	ProviderLabel    string `json:"providerLabel"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	LatencyMS        int64  `json:"latencyMs"`
	// Failovers is how many providers refused before this one answered. It is
	// zero on the happy path and is reported so the bulk lane's caller can
	// see that the pool is straining without reading the ledger.
	Failovers int `json:"failovers"`
}

// TestResult reports the admin round-trip probe: a real one-sentence
// completion against one provider.
type TestResult struct {
	OK         bool   `json:"ok"`
	ProviderID string `json:"providerId"`
	Label      string `json:"label"`
	Model      string `json:"model"`
	DurationMS int64  `json:"durationMs"`
	Answer     string `json:"answer,omitempty"`
	Error      string `json:"error,omitempty"`
}

// TestSystemPrompt and TestUserText are the probe. They ask for one short
// sentence so a slow model still answers inside the timeout, and so the panel
// can print the reply verbatim.
const (
	TestSystemPrompt = "You are a terse assistant. Answer in exactly one short sentence."
	TestUserText     = "In one sentence, say that you are working and name the model you are."
)

// Defaults for one call.
const (
	// DefaultTimeout bounds a single provider attempt. A failover has to fit
	// several of these inside whatever the caller is willing to wait, so it
	// is deliberately tighter than the auxiliary model's own timeout.
	DefaultTimeout = 25 * time.Second
	// DefaultMaxTokens is the answer cap when a caller names none.
	DefaultMaxTokens = 1024
	// MaxFailovers bounds one Complete. Walking eight dead providers before
	// giving up would blow every caller's patience; three attempts is enough
	// for "the first one is out of quota" without becoming a stall.
	MaxFailovers = 3
	// answerLimit bounds the text handed back to a caller.
	answerLimit = 32 << 10
)

// Service owns the registry, the counters, and the failover loop.
type Service struct {
	store     Store
	usageLog  UsageLog
	secrets   SecretResolver
	completer Completer
	audit     audit.Recorder
	now       func() time.Time
	timeout   time.Duration
	tracker   *tracker

	mu       sync.RWMutex
	registry Registry
}

type Option func(*Service)

// WithHTTPClient replaces the transport behind the production completer.
func WithHTTPClient(client *http.Client) Option {
	return func(s *Service) {
		if client != nil {
			s.completer = NewHTTPCompleter(client)
		}
	}
}

// WithCompleter replaces the provider client outright, so a test can assert
// what was asked for without speaking any vendor's wire format.
func WithCompleter(completer Completer) Option {
	return func(s *Service) {
		if completer != nil {
			s.completer = completer
		}
	}
}

// WithUsageLog attaches the append-only ledger.
func WithUsageLog(log UsageLog) Option {
	return func(s *Service) {
		if log != nil {
			s.usageLog = log
		}
	}
}

// WithSecrets attaches the Secrets-vault reader behind apiKeyRef.
func WithSecrets(secrets SecretResolver) Option {
	return func(s *Service) {
		if secrets != nil {
			s.secrets = secrets
		}
	}
}

// WithAudit attaches the audit recorder.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) {
		if recorder != nil {
			s.audit = recorder
		}
	}
}

// WithClock replaces time.Now behind the windows, the cooldowns and the
// latency the Test button reports.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithTimeout replaces the per-attempt ceiling.
func WithTimeout(timeout time.Duration) Option {
	return func(s *Service) {
		if timeout > 0 {
			s.timeout = timeout
		}
	}
}

// New loads the registry, installs the shipped seeds the first time, and
// rebuilds this month's counters from the ledger.
//
// Every failure here degrades rather than propagates: a pool that cannot read
// its own document is an empty pool, and an empty pool simply declines every
// request, which every caller already handles.
func New(ctx context.Context, store Store, options ...Option) *Service {
	service := &Service{
		store:     store,
		completer: NewHTTPCompleter(nil),
		audit:     audit.Nop{},
		now:       time.Now,
		timeout:   DefaultTimeout,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	service.tracker = newTracker(service.now)

	registry := Registry{}
	if store != nil {
		loaded, err := store.Load(ctx)
		if err != nil {
			log.Printf("providerpool: reading the registry failed, the pool stays empty: %v", err)
		} else {
			registry = loaded
		}
	}
	seeded, changed := SeedInto(registry)
	service.registry = seeded.Normalize()
	if changed && store != nil {
		if err := store.Save(ctx, service.registry); err != nil {
			log.Printf("providerpool: installing the seed templates failed: %v", err)
		}
	}
	service.replay(ctx)
	return service
}

// replay rebuilds the day and month counters from this month's ledger, so a
// restart does not reset every meter to zero and hand the operator a
// comfortable lie about how much free tier is left.
func (s *Service) replay(ctx context.Context) {
	if s.usageLog == nil {
		return
	}
	month := MonthKey(s.now())
	err := s.usageLog.Scan(ctx, month, func(record UsageRecord) bool {
		s.tracker.replay(record)
		return true
	})
	if err != nil {
		log.Printf("providerpool: rebuilding usage counters for %s failed, the meters start from zero: %v", month, err)
	}
}

/* ------------------------------------------------------------------ *
 * Reading the registry
 * ------------------------------------------------------------------ */

// Registry returns the live document, credentials included. It must never be
// exposed over HTTP.
func (s *Service) Registry() Registry {
	if s == nil {
		return Registry{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.registry
}

// ProviderView is one row of the settings table.
type ProviderView struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Kind          Kind   `json:"kind"`
	BaseURL       string `json:"baseUrl"`
	APIKeyRef     string `json:"apiKeyRef,omitempty"`
	APIKeyMasked  string `json:"apiKeyMasked,omitempty"`
	KeyConfigured bool   `json:"keyConfigured"`
	// KeySource is "vault", "inline", or empty. It is what lets the panel say
	// where to go and change a credential.
	KeySource  string  `json:"keySource,omitempty"`
	Models     []Model `json:"models"`
	Limits     Limits  `json:"limits"`
	LimitsNote string  `json:"limitsNote,omitempty"`
	Priority   int     `json:"priority"`
	Enabled    bool    `json:"enabled"`
	Notes      string  `json:"notes,omitempty"`
	Seed       bool    `json:"seed,omitempty"`
	UpdatedAt  int64   `json:"updatedAt,omitempty"`
	Status     Status  `json:"status"`
	Usage      Usage   `json:"usage"`
}

// PoolView is the whole admin-facing panel payload.
type PoolView struct {
	Providers    []ProviderView `json:"providers"`
	Settings     Settings       `json:"settings"`
	Kinds        []Kind         `json:"kinds"`
	Capabilities []Capability   `json:"capabilities"`
	// Available reports whether anything at all could take a request.
	Available bool `json:"available"`
	// Month is the ledger month the meters are counted against, so the panel
	// can say "month to date" and mean something specific.
	Month string `json:"month"`
	// SeedLimitsNote is echoed so the panel's warning text and this package
	// cannot drift apart.
	SeedLimitsNote string `json:"seedLimitsNote"`
}

// View renders the admin panel payload. No credential crosses this boundary:
// an inline key becomes a mask and a vault reference stays a key *name*.
func (s *Service) View() PoolView {
	if s == nil {
		return PoolView{
			Providers:      []ProviderView{},
			Kinds:          Kinds(),
			Capabilities:   Capabilities(),
			SeedLimitsNote: SeedLimitsNote,
		}
	}
	registry := s.Registry()
	now := s.now()
	views := make([]ProviderView, 0, len(registry.Providers))
	available := false
	for _, provider := range registry.Providers {
		view := s.viewOf(provider, now)
		if view.Status == StatusReady {
			available = true
		}
		views = append(views, view)
	}
	return PoolView{
		Providers:      views,
		Settings:       registry.Settings,
		Kinds:          Kinds(),
		Capabilities:   Capabilities(),
		Available:      available,
		Month:          MonthKey(now),
		SeedLimitsNote: SeedLimitsNote,
	}
}

func (s *Service) viewOf(provider Provider, now time.Time) ProviderView {
	entry := s.tracker.snapshot(provider.ID)
	usage := usageFor(provider, entry, now)
	cooling := !entry.cooldownUntil.IsZero() && now.Before(entry.cooldownUntil)
	keySource := ""
	switch {
	case provider.APIKeyRef != "":
		keySource = "vault"
	case provider.APIKey != "":
		keySource = "inline"
	}
	return ProviderView{
		ID:            provider.ID,
		Label:         provider.Label,
		Kind:          provider.Kind,
		BaseURL:       provider.BaseURL,
		APIKeyRef:     provider.APIKeyRef,
		APIKeyMasked:  MaskSecret(provider.APIKey),
		KeyConfigured: provider.HasKey(),
		KeySource:     keySource,
		Models:        provider.Models,
		Limits:        provider.Limits,
		LimitsNote:    provider.LimitsNote,
		Priority:      provider.Priority,
		Enabled:       provider.Enabled,
		Notes:         provider.Notes,
		Seed:          provider.Seed,
		UpdatedAt:     provider.UpdatedAt,
		Status:        statusOf(provider, usage, cooling),
		Usage:         usage,
	}
}

// QuotaRow is one line of the member-facing "Free quota" card. It carries a
// label and three meters and nothing else: no endpoint, no key state, no
// error text. A member does not need to know which box answers.
type QuotaRow struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Status        Status `json:"status"`
	RequestsToday Meter  `json:"requestsToday"`
	TokensToday   Meter  `json:"tokensToday"`
	TokensMonth   Meter  `json:"tokensMonth"`
}

// QuotaView is what the Home dashboard card reads.
type QuotaView struct {
	// Available is false when no provider is connected at all, which the card
	// renders as "not set up" rather than as an empty list.
	Available bool       `json:"available"`
	Providers []QuotaRow `json:"providers"`
	Month     string     `json:"month"`
}

// QuotaCardSize is how many providers the dashboard card shows.
const QuotaCardSize = 4

// Quota renders the compact card: the providers closest to their documented
// caps first, because the one about to run out is the one worth a glance.
func (s *Service) Quota() QuotaView {
	if s == nil {
		return QuotaView{Providers: []QuotaRow{}}
	}
	view := s.View()
	rows := make([]QuotaRow, 0, QuotaCardSize)
	for _, provider := range topByConsumption(view.Providers, QuotaCardSize) {
		rows = append(rows, QuotaRow{
			ID:            provider.ID,
			Label:         provider.Label,
			Status:        provider.Status,
			RequestsToday: provider.Usage.RequestsToday,
			TokensToday:   provider.Usage.TokensToday,
			TokensMonth:   provider.Usage.TokensMonth,
		})
	}
	// "Available" means somebody has connected something, not that a request
	// would succeed right now — a pool whose only provider is cooling down is
	// still a pool that is set up.
	available := false
	for _, provider := range view.Providers {
		if provider.Enabled && provider.KeyConfigured {
			available = true
			break
		}
	}
	return QuotaView{Available: available, Providers: rows, Month: view.Month}
}

// Available reports whether the pool could take a request right now. Callers
// use it to decide whether to offer something; Complete re-checks anyway,
// because the answer changes between the two.
func (s *Service) Available() bool {
	if s == nil {
		return false
	}
	return len(s.candidates(Need{})) > 0
}

/* ------------------------------------------------------------------ *
 * Picking
 * ------------------------------------------------------------------ */

// candidate pairs a provider with the model chosen for one need.
type candidate struct {
	provider Provider
	model    Model
}

// candidates returns everything that could take this need, in the order the
// failover loop should try them.
//
// Two modes:
//
//   - Pinned. A caller-supplied ProviderID, or — with auto-switch off — the
//     operator's preferred provider. Exactly one candidate, and no failover:
//     "do not switch" has to mean it.
//   - Auto. Priority order, skipping anything that cannot answer.
//
// A pinned provider is skipped only for reasons that make the call certain to
// fail — disabled, keyless, no suitable model, already cooling down. It is
// *not* skipped for being over a locally counted limit: our count is an
// estimate, and an operator who pinned a provider deliberately should not be
// overruled by our arithmetic.
func (s *Service) candidates(need Need) []candidate {
	registry := s.Registry()
	now := s.now()

	pinned := strings.ToLower(strings.TrimSpace(need.ProviderID))
	if pinned == "" && !registry.Settings.AutoSwitch {
		pinned = registry.Settings.PreferredProviderID
		if pinned == "" {
			// Manual mode with nothing chosen declines everything. That is
			// the honest reading of "do not switch, and I have not said what
			// to use".
			return nil
		}
	}

	if pinned != "" {
		provider, found := registry.Find(pinned)
		if !found || !provider.Enabled || !provider.HasKey() {
			return nil
		}
		if cooling, _ := s.tracker.cooling(provider.ID); cooling {
			return nil
		}
		model, ok := provider.PickModel(need)
		if !ok {
			return nil
		}
		return []candidate{{provider: provider, model: model}}
	}

	ordered := make([]Provider, len(registry.Providers))
	copy(ordered, registry.Providers)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })

	out := make([]candidate, 0, len(ordered))
	for _, provider := range ordered {
		if !provider.Enabled || !provider.HasKey() {
			continue
		}
		if cooling, _ := s.tracker.cooling(provider.ID); cooling {
			continue
		}
		if exhausted(usageFor(provider, s.tracker.snapshot(provider.ID), now)) {
			continue
		}
		model, ok := provider.PickModel(need)
		if !ok {
			continue
		}
		out = append(out, candidate{provider: provider, model: model})
	}
	return out
}

// Pick answers "who would take this right now" without spending anything. It
// is the read half of the pool: the settings panel and any caller that wants
// to know before it commits.
func (s *Service) Pick(ctx context.Context, need Need) (Provider, Model, error) {
	if s == nil {
		return Provider{}, Model{}, ErrNoProvider
	}
	if err := ctx.Err(); err != nil {
		return Provider{}, Model{}, err
	}
	candidates := s.candidates(need)
	if len(candidates) == 0 {
		return Provider{}, Model{}, ErrNoProvider
	}
	return candidates[0].provider, candidates[0].model, nil
}

/* ------------------------------------------------------------------ *
 * Completing
 * ------------------------------------------------------------------ */

// Complete runs one request through the pool, moving to the next provider
// when one refuses. The failover is invisible to the caller: it gets an
// answer and the id of whoever produced it, or one error saying nobody could.
func (s *Service) Complete(ctx context.Context, request Request) (Result, error) {
	if s == nil {
		return Result{}, ErrNoProvider
	}
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Prompt == "" {
		return Result{}, ErrEmptyPrompt
	}
	if request.MaxTokens <= 0 {
		request.MaxTokens = DefaultMaxTokens
	}

	candidates := s.candidates(request.Need)
	if len(candidates) == 0 {
		return Result{}, ErrNoProvider
	}
	if len(candidates) > MaxFailovers {
		candidates = candidates[:MaxFailovers]
	}

	// The caller's context may already be cancelled — a settled run hands an
	// observer a dead one — so the timeout hangs off a fresh background
	// context rather than a corpse. Cancellation of a *live* request still
	// works: ctx is honoured when it is not already done.
	parent := ctx
	if parent == nil || parent.Err() != nil {
		parent = context.Background()
	}

	var lastErr error
	for index, next := range candidates {
		result, err := s.attempt(parent, next, request)
		if err == nil {
			result.Failovers = index
			return result, nil
		}
		lastErr = err
		// Every failure moves on, retryable or not. A 429 is the quota case
		// this feature exists for; a bad key or a model that does not exist
		// would fail on a retry too, but that is *this provider's* problem
		// rather than the request's, and the next provider may well have
		// neither. attempt() has already cooled the loser down, so a
		// misconfigured row stops being tried on every call.
		s.recordFailover(parent, next, candidates, index, err)
	}
	return Result{}, fmt.Errorf("%w: %v", ErrNoProvider, lastErr)
}

// attempt runs one provider and books the result either way.
func (s *Service) attempt(parent context.Context, next candidate, request Request) (Result, error) {
	key, err := s.credential(parent, next.provider)
	if err != nil {
		return Result{}, err
	}

	requestCtx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()

	started := s.now()
	call, err := s.completer.Complete(requestCtx, Call{
		Kind:         next.provider.Kind,
		BaseURL:      next.provider.BaseURL,
		Model:        next.model.ID,
		APIKey:       key,
		SystemPrompt: request.System,
		UserText:     request.Prompt,
		MaxTokens:    request.MaxTokens,
	})
	latency := s.now().Sub(started).Milliseconds()

	if err != nil {
		s.bookFailure(parent, next, request, err, latency)
		return Result{}, err
	}

	s.tracker.observe(next.provider.ID, ParseRateLimitHeaders(call.Header, s.now()))
	s.tracker.record(next.provider.ID, call.PromptTokens+call.CompletionTokens, true, "")
	s.tracker.revive(next.provider.ID)
	s.append(parent, UsageRecord{
		At:               s.now().UnixMilli(),
		Event:            EventRequest,
		ProviderID:       next.provider.ID,
		Model:            next.model.ID,
		Job:              request.Job,
		OK:               true,
		Status:           call.Status,
		PromptTokens:     call.PromptTokens,
		CompletionTokens: call.CompletionTokens,
		LatencyMS:        latency,
		TokenSource:      call.TokenSource,
	})
	return Result{
		Text:             truncate(call.Text, answerLimit),
		ProviderID:       next.provider.ID,
		ProviderLabel:    next.provider.Label,
		Model:            next.model.ID,
		PromptTokens:     call.PromptTokens,
		CompletionTokens: call.CompletionTokens,
		LatencyMS:        latency,
	}, nil
}

// bookFailure records a refusal: the counters, the cooldown, and the ledger.
func (s *Service) bookFailure(
	ctx context.Context,
	next candidate,
	request Request,
	cause error,
	latency int64,
) {
	var callErr *CallError
	status := 0
	var header http.Header
	if errors.As(cause, &callErr) {
		status = callErr.Status
		header = callErr.Header
	}
	// A 429 still carries useful headers — often the only place a vendor says
	// when the window reopens.
	s.tracker.observe(next.provider.ID, ParseRateLimitHeaders(header, s.now()))
	s.tracker.record(next.provider.ID, 0, false, collapse(cause.Error()))

	retryAfter := time.Duration(0)
	if wait, ok := RetryAfter(header, s.now()); ok {
		retryAfter = wait
	}
	slept := s.tracker.cool(next.provider.ID, retryAfter)

	s.append(ctx, UsageRecord{
		At:         s.now().UnixMilli(),
		Event:      EventRequest,
		ProviderID: next.provider.ID,
		Model:      next.model.ID,
		Job:        request.Job,
		OK:         false,
		Status:     status,
		LatencyMS:  latency,
		Error:      collapse(cause.Error()),
	})
	s.append(ctx, UsageRecord{
		At:         s.now().UnixMilli(),
		Event:      EventCooldown,
		ProviderID: next.provider.ID,
		Status:     status,
		CooldownMS: slept.Milliseconds(),
		Error:      collapse(cause.Error()),
	})
}

// recordFailover writes the "and then it tried somebody else" line, which is
// what makes the ledger answer "why did this month's tokens land on Cerebras
// when Groq is first".
func (s *Service) recordFailover(
	ctx context.Context,
	from candidate,
	candidates []candidate,
	index int,
	cause error,
) {
	nextID := ""
	if index+1 < len(candidates) {
		nextID = candidates[index+1].provider.ID
	}
	s.append(ctx, UsageRecord{
		At:             s.now().UnixMilli(),
		Event:          EventFailover,
		ProviderID:     from.provider.ID,
		Model:          from.model.ID,
		NextProviderID: nextID,
		Error:          collapse(cause.Error()),
	})
}

// credential resolves a provider's key: the vault reference first, the inline
// value second.
func (s *Service) credential(ctx context.Context, provider Provider) (string, error) {
	if provider.APIKeyRef != "" {
		if s.secrets == nil {
			return "", fmt.Errorf("%s references the vault key %s but the Secrets vault is unavailable",
				provider.Label, provider.APIKeyRef)
		}
		value, found, err := s.secrets.Value(ctx, provider.APIKeyRef)
		if err != nil {
			return "", fmt.Errorf("read the vault key %s for %s: %w", provider.APIKeyRef, provider.Label, err)
		}
		if !found || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("the vault has no value for %s, which %s needs",
				provider.APIKeyRef, provider.Label)
		}
		return value, nil
	}
	if provider.APIKey == "" {
		return "", fmt.Errorf("%s has no API key", provider.Label)
	}
	return provider.APIKey, nil
}

// append writes one ledger line. A ledger failure is logged and dropped: this
// is bookkeeping behind an optional feature and must never fail a caller's
// request.
func (s *Service) append(ctx context.Context, record UsageRecord) {
	if s.usageLog == nil {
		return
	}
	// The caller's context may be finished by the time the bookkeeping runs,
	// and a lost ledger line is worse than a slightly detached write.
	writeCtx := ctx
	if writeCtx == nil || writeCtx.Err() != nil {
		writeCtx = context.Background()
	}
	if err := s.usageLog.Append(writeCtx, record); err != nil {
		log.Printf("providerpool: recording usage for %s failed: %v", record.ProviderID, err)
	}
}

/* ------------------------------------------------------------------ *
 * The admin probe
 * ------------------------------------------------------------------ */

// Test runs a real one-sentence completion against one provider so an
// operator can prove the endpoint, the model and the key all work, and see
// what the round trip costs in wall-clock time.
//
// It bypasses the pool's own skipping rules on purpose: the whole point of
// pressing Test on a cooling-down provider is to find out whether it is well
// again, and a success clears the cooldown.
func (s *Service) Test(ctx context.Context, id string) TestResult {
	result := TestResult{ProviderID: strings.ToLower(strings.TrimSpace(id))}
	if s == nil {
		result.Error = "the provider pool is unavailable"
		return result
	}
	provider, found := s.Registry().Find(result.ProviderID)
	if !found {
		result.Error = "no such provider"
		return result
	}
	result.Label = provider.Label
	if !provider.HasKey() {
		result.Error = "add an API key, or a Secrets-vault key name, first"
		return result
	}
	model, ok := provider.PickModel(Need{})
	if !ok {
		result.Error = "this provider lists no usable model"
		return result
	}
	result.Model = model.ID

	key, err := s.credential(ctx, provider)
	if err != nil {
		result.Error = collapse(err.Error())
		return result
	}

	requestCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	started := s.now()
	call, err := s.completer.Complete(requestCtx, Call{
		Kind:         provider.Kind,
		BaseURL:      provider.BaseURL,
		Model:        model.ID,
		APIKey:       key,
		SystemPrompt: TestSystemPrompt,
		UserText:     TestUserText,
		MaxTokens:    64,
	})
	result.DurationMS = s.now().Sub(started).Milliseconds()

	if err != nil {
		result.Error = collapse(err.Error())
		// A deliberate probe against a dead box should still count: an
		// operator who tests three times has proved it is dead.
		s.bookFailure(ctx, candidate{provider: provider, model: model}, Request{Need: Need{Job: "test"}}, err, result.DurationMS)
		s.record(ctx, audit.ActionSettingsProviderTest, provider.ID, audit.Meta{"model": model.ID}, err)
		return result
	}

	s.tracker.observe(provider.ID, ParseRateLimitHeaders(call.Header, s.now()))
	s.tracker.record(provider.ID, call.PromptTokens+call.CompletionTokens, true, "")
	// The probe is the one call that clears a cooldown outright, which is what
	// makes "fix the key, press Test" put a provider back to work without
	// waiting out the back-off.
	s.tracker.revive(provider.ID)
	s.append(ctx, UsageRecord{
		At:               s.now().UnixMilli(),
		Event:            EventRequest,
		ProviderID:       provider.ID,
		Model:            model.ID,
		Job:              "test",
		OK:               true,
		Status:           call.Status,
		PromptTokens:     call.PromptTokens,
		CompletionTokens: call.CompletionTokens,
		LatencyMS:        result.DurationMS,
		TokenSource:      call.TokenSource,
	})
	s.record(ctx, audit.ActionSettingsProviderTest, provider.ID, audit.Meta{"model": model.ID}, nil)

	result.OK = true
	result.Answer = truncate(call.Text, 400)
	return result
}

func (s *Service) record(ctx context.Context, action, id string, meta audit.Meta, err error) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(
		action,
		audit.Target{Type: audit.TargetProvider, ID: id},
		meta,
		err,
	))
}

// truncate caps a string at a rune count, marking that it was cut.
func truncate(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
