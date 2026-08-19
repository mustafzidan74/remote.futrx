package routing

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// Repository persists the policy document. "Missing" is not an error: the
// service seeds the shipped default on the first read that finds none.
type Repository interface {
	Load(ctx context.Context) (Policy, bool, error)
	Save(ctx context.Context, policy Policy) error
}

// ProviderDirectory reports which agent providers have a live host
// credential. Routing never sends a run to one that does not.
type ProviderDirectory interface {
	Connected() []string
}

// Option configures the service.
type Option func(*Service)

// WithAudit records every policy edit.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithProviders installs the connected-provider probe. Without it routing
// checks the model catalog only, and a rule pointing at a disconnected
// provider fails at run time the way an unrouted chat already would.
func WithProviders(directory ProviderDirectory) Option {
	return func(s *Service) { s.providers = directory }
}

// Service is the single owner of the automatic model routing policy. The
// document is small and read on every run, so it is cached in memory and the
// file is only touched on a miss or an edit.
type Service struct {
	repo      Repository
	providers ProviderDirectory
	audit     audit.Recorder

	mu     sync.RWMutex
	policy Policy
	loaded bool
}

func New(repo Repository, options ...Option) *Service {
	service := &Service{repo: repo, audit: audit.Nop{}}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Policy returns the stored policy, seeding the shipped default on first use.
func (s *Service) Policy(ctx context.Context) (Policy, error) {
	if s == nil || s.repo == nil {
		return Policy{}, ErrUnavailable
	}
	s.mu.RLock()
	if s.loaded {
		policy := s.policy
		s.mu.RUnlock()
		return policy, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s.policy, nil
	}
	stored, found, err := s.repo.Load(ctx)
	if err != nil {
		return Policy{}, err
	}
	if !found {
		stored = DefaultPolicy()
		stored.UpdatedAt = time.Now().UnixMilli()
		if err := s.repo.Save(ctx, stored); err != nil {
			return Policy{}, err
		}
	}
	normalized, err := stored.Normalize()
	if err != nil {
		// A hand-edited file that no longer normalizes must not take routing
		// down with it. Fall back to the shipped default, which is off.
		log.Printf("routing: stored policy is invalid (%v); falling back to the shipped default", err)
		normalized = DefaultPolicy()
	}
	s.policy = normalized
	s.loaded = true
	return s.policy, nil
}

// View is the admin read: the policy plus what this deployment can route to.
func (s *Service) View(ctx context.Context) (View, error) {
	policy, err := s.Policy(ctx)
	if err != nil {
		return View{}, err
	}
	return View{Policy: policy, Providers: s.connected(), Catalog: Catalog()}, nil
}

// Update replaces the policy after normalizing it.
func (s *Service) Update(ctx context.Context, policy Policy, actor string) (View, error) {
	if s == nil || s.repo == nil {
		return View{}, ErrUnavailable
	}
	normalized, err := policy.Normalize()
	if err != nil {
		s.record(ctx, actor, policy, err)
		return View{}, err
	}
	normalized.UpdatedAt = time.Now().UnixMilli()
	normalized.UpdatedBy = strings.TrimSpace(actor)
	if err := s.repo.Save(ctx, normalized); err != nil {
		s.record(ctx, actor, normalized, err)
		return View{}, err
	}

	s.mu.Lock()
	s.policy = normalized
	s.loaded = true
	s.mu.Unlock()

	s.record(ctx, actor, normalized, nil)
	return View{Policy: normalized, Providers: s.connected(), Catalog: Catalog()}, nil
}

// Route answers the prompt service's question. It never fails a run: a policy
// the store cannot produce leaves the chat's own model in force.
func (s *Service) Route(ctx context.Context, input Input) Decision {
	if s == nil {
		return Decision{
			Provider: input.Provider,
			Model:    input.Model,
			Reason:   "automatic routing is not configured",
		}
	}
	policy, err := s.Policy(ctx)
	if err != nil {
		log.Printf("routing: read policy: %v", err)
		return Decision{
			Provider:        input.Provider,
			Model:           input.Model,
			ReasoningEffort: input.ReasoningEffort,
			Reason:          "automatic routing is unavailable",
		}
	}
	return Decide(policy, input, s.availability())
}

// Test explains one pasted prompt without running it. It is what the "Test
// this policy" box in Settings calls.
func (s *Service) Test(ctx context.Context, in TestInput) (Decision, error) {
	policy, err := s.Policy(ctx)
	if err != nil {
		return Decision{}, err
	}
	return Decide(policy, Input{
		Pinned:      in.Pinned,
		Provider:    in.Provider,
		Model:       in.Model,
		Prompt:      in.Prompt,
		Mode:        in.Mode,
		Synthetic:   strings.TrimSpace(in.Synthetic),
		ProjectID:   strings.TrimSpace(in.ProjectID),
		ProjectSlug: strings.TrimSpace(in.ProjectSlug),
		Skills:      in.Skills,
	}, s.availability()), nil
}

// Defaults reports the three reference models the savings report prices
// against. The second return is false when routing has never been configured.
func (s *Service) Defaults(ctx context.Context) (Policy, bool) {
	policy, err := s.Policy(ctx)
	if err != nil {
		return Policy{}, false
	}
	return policy, true
}

func (s *Service) availability() Availability {
	connected := s.connected()
	if connected == nil {
		return Availability{}
	}
	set := make(map[string]bool, len(connected))
	for _, provider := range connected {
		set[provider] = true
	}
	return Availability{Providers: set, Known: true}
}

// connected returns the connected providers, or nil when the deployment
// cannot be asked (no directory wired).
func (s *Service) connected() []string {
	if s == nil || s.providers == nil {
		return nil
	}
	raw := s.providers.Connected()
	out := make([]string, 0, len(raw))
	for _, provider := range raw {
		normalized := strings.ToLower(strings.TrimSpace(provider))
		if normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func (s *Service) record(ctx context.Context, actor string, policy Policy, err error) {
	if s.audit == nil {
		return
	}
	entry := audit.Result(
		audit.ActionSettingsModelRouting,
		audit.Target{Type: audit.TargetServer, ID: "model-routing"},
		audit.Meta{
			"enabled":        policy.Enabled,
			"rules":          len(policy.Rules),
			"autoHeuristics": policy.AutoHeuristics,
			"default":        policy.Default.Provider + "/" + policy.Default.Model,
		},
		err,
	)
	if actor = strings.TrimSpace(actor); actor != "" {
		entry.Actor = audit.Actor{Email: audit.NormalizeActorEmail(actor), IsAdmin: true}
	}
	s.audit.Record(ctx, entry)
}
