package providerpool

import (
	"context"
	"fmt"
	"strings"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// Editing the registry.
//
// The credential follows the same write-only semantics as every other stored
// secret in this platform: the client only ever saw a mask, so an empty
// APIKey on an update means "keep what is stored" and ClearAPIKey is the
// explicit removal path.

// ProviderInput is the admin create/update body.
type ProviderInput struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	BaseURL   string `json:"baseUrl"`
	APIKeyRef string `json:"apiKeyRef"`
	APIKey    string `json:"apiKey"`
	// ClearAPIKey removes the stored inline key. Clearing a vault reference
	// is done by sending an empty apiKeyRef, which has no secrecy problem
	// because a key *name* was never hidden.
	ClearAPIKey bool    `json:"clearApiKey"`
	Models      []Model `json:"models"`
	Limits      Limits  `json:"limits"`
	Priority    int     `json:"priority"`
	Enabled     bool    `json:"enabled"`
	Notes       string  `json:"notes"`
}

// SettingsInput is the admin body for the pool's global policy.
type SettingsInput struct {
	AutoSwitch          bool   `json:"autoSwitch"`
	PreferredProviderID string `json:"preferredProviderId"`
}

// Save creates or updates one provider.
func (s *Service) Save(ctx context.Context, input ProviderInput, actor string) (PoolView, error) {
	if s == nil {
		return PoolView{}, ErrNoProvider
	}
	id := strings.ToLower(strings.TrimSpace(input.ID))
	if id == "" {
		return PoolView{}, invalid("an id is required")
	}

	registry := s.Registry().Clone()
	index := -1
	for position, provider := range registry.Providers {
		if provider.ID == id {
			index = position
			break
		}
	}

	next := Provider{ID: id}
	action := audit.ActionSettingsProviderCreate
	if index >= 0 {
		next = registry.Providers[index]
		action = audit.ActionSettingsProviderUpdate
	}

	previousLimits := next.Limits
	next.Label = strings.TrimSpace(input.Label)
	next.Kind = NormalizeKind(input.Kind)
	next.BaseURL = strings.TrimSpace(input.BaseURL)
	next.APIKeyRef = strings.TrimSpace(input.APIKeyRef)
	next.Models = input.Models
	next.Limits = input.Limits.Normalize()
	next.Priority = input.Priority
	next.Enabled = input.Enabled
	next.Notes = strings.TrimSpace(input.Notes)
	switch {
	case input.ClearAPIKey:
		next.APIKey = ""
	case strings.TrimSpace(input.APIKey) != "":
		next.APIKey = strings.TrimSpace(input.APIKey)
	}
	// An operator who edited the limits has checked them against the vendor,
	// which is exactly what the seed warning was asking for — so the warning
	// goes away.
	if index >= 0 && !sameLimits(previousLimits, next.Limits) {
		next.LimitsNote = ""
	}
	next.UpdatedAt = s.now().UnixMilli()
	next = next.Normalize()

	if err := validate(next); err != nil {
		return PoolView{}, err
	}
	if index >= 0 {
		registry.Providers[index] = next
	} else {
		registry.Providers = append(registry.Providers, next)
	}

	if err := s.persist(ctx, registry); err != nil {
		s.record(ctx, action, id, nil, err)
		return PoolView{}, err
	}
	s.record(ctx, action, id, audit.Meta{
		"label":     next.Label,
		"kind":      string(next.Kind),
		"enabled":   next.Enabled,
		"keySource": keySourceOf(next),
	}, nil)
	return s.View(), nil
}

// Delete removes one provider and forgets its counters, so an id that is
// created again later starts from zero rather than inheriting a stranger's
// consumption.
func (s *Service) Delete(ctx context.Context, id, actor string) (PoolView, error) {
	if s == nil {
		return PoolView{}, ErrNoProvider
	}
	id = strings.ToLower(strings.TrimSpace(id))
	registry := s.Registry().Clone()
	kept := make([]Provider, 0, len(registry.Providers))
	found := false
	for _, provider := range registry.Providers {
		if provider.ID == id {
			found = true
			continue
		}
		kept = append(kept, provider)
	}
	if !found {
		return PoolView{}, fmt.Errorf("%w: %s", ErrUnknownProvider, id)
	}
	registry.Providers = kept
	// A pool set to a provider that no longer exists would decline every
	// request with a confusing error, so the pin goes with it.
	if registry.Settings.PreferredProviderID == id {
		registry.Settings.PreferredProviderID = ""
	}

	if err := s.persist(ctx, registry); err != nil {
		s.record(ctx, audit.ActionSettingsProviderDelete, id, nil, err)
		return PoolView{}, err
	}
	s.tracker.forget(id)
	s.record(ctx, audit.ActionSettingsProviderDelete, id, nil, nil)
	return s.View(), nil
}

// Reorder rewrites the priority column from an explicit id order. The panel's
// up/down buttons send the whole list rather than a swap, so a reorder is one
// idempotent write instead of a sequence that can half-apply.
//
// Ids the caller did not mention keep their relative order after the ones it
// did, so a list that raced with a create does not silently drop the new row
// to an arbitrary place.
func (s *Service) Reorder(ctx context.Context, ids []string, actor string) (PoolView, error) {
	if s == nil {
		return PoolView{}, ErrNoProvider
	}
	registry := s.Registry().Clone()
	position := make(map[string]int, len(ids))
	for index, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if _, duplicate := position[id]; duplicate {
			continue
		}
		position[id] = index
	}
	step := 10
	tail := len(position)
	for index := range registry.Providers {
		id := registry.Providers[index].ID
		rank, named := position[id]
		if !named {
			rank = tail
			tail++
		}
		registry.Providers[index].Priority = rank * step
		registry.Providers[index].UpdatedAt = s.now().UnixMilli()
	}

	if err := s.persist(ctx, registry); err != nil {
		s.record(ctx, audit.ActionSettingsProviderPool, "", nil, err)
		return PoolView{}, err
	}
	s.record(ctx, audit.ActionSettingsProviderPool, "", audit.Meta{"reordered": len(position)}, nil)
	return s.View(), nil
}

// SaveSettings stores the pool's global policy.
func (s *Service) SaveSettings(ctx context.Context, input SettingsInput, actor string) (PoolView, error) {
	if s == nil {
		return PoolView{}, ErrNoProvider
	}
	registry := s.Registry().Clone()
	preferred := strings.ToLower(strings.TrimSpace(input.PreferredProviderID))
	if preferred != "" {
		if _, found := registry.Find(preferred); !found {
			return PoolView{}, invalid("no provider called %q", preferred)
		}
	}
	if !input.AutoSwitch && preferred == "" {
		return PoolView{}, invalid("choose a preferred provider, or switch auto-switching on")
	}
	registry.Settings = Settings{
		AutoSwitch:          input.AutoSwitch,
		PreferredProviderID: preferred,
		UpdatedAt:           s.now().UnixMilli(),
	}

	if err := s.persist(ctx, registry); err != nil {
		s.record(ctx, audit.ActionSettingsProviderPool, "", nil, err)
		return PoolView{}, err
	}
	s.record(ctx, audit.ActionSettingsProviderPool, "", audit.Meta{
		"autoSwitch": input.AutoSwitch,
		"preferred":  preferred,
	}, nil)
	return s.View(), nil
}

// persist normalizes, writes, and arms the new registry. The in-memory copy
// is only replaced once the write succeeded, so a failed save leaves the pool
// running on what is actually on disk.
func (s *Service) persist(ctx context.Context, registry Registry) error {
	next := registry.Normalize()
	if s.store != nil {
		if err := s.store.Save(ctx, next); err != nil {
			return fmt.Errorf("save the provider registry: %w", err)
		}
	}
	s.mu.Lock()
	s.registry = next
	s.mu.Unlock()
	return nil
}

func keySourceOf(provider Provider) string {
	switch {
	case provider.APIKeyRef != "":
		return "vault"
	case provider.APIKey != "":
		return "inline"
	default:
		return ""
	}
}

// sameLimits compares two nullable limit sets.
func sameLimits(a, b Limits) bool {
	same := func(x, y *int) bool {
		if x == nil || y == nil {
			return x == nil && y == nil
		}
		return *x == *y
	}
	return same(a.RPM, b.RPM) && same(a.RPD, b.RPD) &&
		same(a.TPM, b.TPM) && same(a.TPD, b.TPD) &&
		same(a.MonthlyTokens, b.MonthlyTokens)
}

// Discover asks one provider what it serves and reports the gap against what
// this registry has configured.
//
// It reports rather than rewrites. A provider listing a model does not mean
// the operator's key may call it, and quietly replacing a curated list with a
// discovery response would overrule a choice the platform cannot see the
// reasons for. AdoptModels is the deliberate second step.
func (s *Service) Discover(ctx context.Context, id string) Discovery {
	result := Discovery{ProviderID: strings.ToLower(strings.TrimSpace(id))}
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

	key, err := s.credential(ctx, provider)
	if err != nil || strings.TrimSpace(key) == "" {
		// Most providers refuse an unauthenticated /models, so an empty key is
		// reported as the cause rather than left to surface as a 401.
		result.Error = "add an API key first, so the provider will answer /models"
		return result
	}

	available, err := s.lister.ListModels(ctx, provider.BaseURL, key)
	if err != nil {
		result.Error = collapse(err.Error())
		s.record(ctx, audit.ActionSettingsProviderTest, provider.ID, audit.Meta{"action": "discover"}, err)
		return result
	}

	result.Available = available
	result.Missing, result.Unlisted = compare(configuredModelIDs(provider), available)
	s.record(ctx, audit.ActionSettingsProviderTest, provider.ID, audit.Meta{
		"action":    "discover",
		"available": len(available),
		"missing":   len(result.Missing),
	}, nil)
	return result
}

// configuredModelIDs lists the ids this registry offers for one provider.
func configuredModelIDs(provider Provider) []string {
	ids := make([]string, 0, len(provider.Models))
	for _, model := range provider.Models {
		ids = append(ids, model.ID)
	}
	return ids
}

// AdoptModels prunes a provider's model list. It cannot extend it.
//
// The rule is the important part: an id that is not already configured is
// ignored. Adoption exists to drop models the provider has retired, and every
// other change to the list is a deliberate edit.
//
// That restriction is not tidiness. A discovery response is the provider's
// whole catalog, and on a gateway like OpenRouter that is hundreds of paid
// models next to the handful of free ones an operator chose. Letting adoption
// write from it turns one click into a configuration that quietly spends money
// on an account the platform does not own — the operator here keeps paid credit
// on OpenRouter for work done elsewhere. Pruning cannot do that; extending can.
//
// Capability tags survive for any id that is staying: a model an operator
// marked as good for code should not lose that because the list was refreshed.
func (s *Service) AdoptModels(ctx context.Context, id string, ids []string, actor string) (PoolView, error) {
	if s == nil {
		return PoolView{}, ErrNoProvider
	}
	registry := s.Registry()
	provider, found := registry.Find(strings.ToLower(strings.TrimSpace(id)))
	if !found {
		return PoolView{}, ErrUnknownProvider
	}

	previous := make(map[string]Model, len(provider.Models))
	for _, model := range provider.Models {
		previous[model.ID] = model
	}

	adopted := make([]Model, 0, len(ids))
	seen := map[string]bool{}
	var refused []string
	for _, raw := range ids {
		modelID := strings.TrimSpace(raw)
		if modelID == "" || seen[modelID] {
			continue
		}
		seen[modelID] = true
		kept, ok := previous[modelID]
		if !ok {
			// Not configured, so not this action's business.
			refused = append(refused, modelID)
			continue
		}
		adopted = append(adopted, kept)
	}
	if len(adopted) == 0 {
		return PoolView{}, fmt.Errorf("%w: a provider needs at least one model", ErrInvalidProvider)
	}

	view, err := s.Save(ctx, ProviderInput{
		ID:        provider.ID,
		Label:     provider.Label,
		Kind:      string(provider.Kind),
		BaseURL:   provider.BaseURL,
		APIKeyRef: provider.APIKeyRef,
		Models:    adopted,
		Limits:    provider.Limits,
		Priority:  provider.Priority,
		Enabled:   provider.Enabled,
		Notes:     provider.Notes,
	}, actor)
	s.record(ctx, audit.ActionSettingsProviderUpdate, provider.ID, audit.Meta{
		"action":  "adopt-models",
		"models":  len(adopted),
		"refused": len(refused),
	}, err)
	return view, err
}
