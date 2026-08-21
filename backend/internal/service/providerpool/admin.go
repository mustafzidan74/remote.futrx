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
