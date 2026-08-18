package playbooks

import (
	"context"
	"sync"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// Repository persists the library document. A missing document is not an
// error: the service seeds one on the first start that finds none, and never
// rewrites it afterwards.
type Repository interface {
	Load(ctx context.Context) ([]Playbook, bool, error)
	Save(ctx context.Context, list []Playbook) error
}

// Service is the single owner of the playbook library.
type Service struct {
	repo  Repository
	audit serviceaudit.Recorder

	mu     sync.RWMutex
	cached []Playbook
	loaded bool
}

// Option configures the service at construction.
type Option func(*Service)

// WithAudit records admin edits of the library.
func WithAudit(recorder serviceaudit.Recorder) Option {
	return func(s *Service) { s.audit = serviceaudit.RecorderOrNop(recorder) }
}

func New(repo Repository, options ...Option) *Service {
	service := &Service{repo: repo, audit: serviceaudit.Nop{}}
	for _, option := range options {
		option(service)
	}
	return service
}

// Ensure seeds the library on first run. It is idempotent: an existing
// document — including one an admin has deliberately emptied — is left alone.
// Returns the number of seeded entries, which is zero on every start after the
// first.
func (s *Service) Ensure(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	stored, found, err := s.repo.Load(ctx)
	if err != nil {
		return 0, err
	}
	if found {
		s.cache(Normalize(stored))
		return 0, nil
	}
	seeded := Seed()
	if err := s.repo.Save(ctx, seeded); err != nil {
		return 0, err
	}
	s.cache(seeded)
	return len(seeded), nil
}

// List returns the library, loading it lazily when Ensure has not run.
func (s *Service) List(ctx context.Context) ([]Playbook, error) {
	if s == nil {
		return []Playbook{}, nil
	}
	s.mu.RLock()
	loaded, cached := s.loaded, s.cached
	s.mu.RUnlock()
	if loaded {
		return append([]Playbook(nil), cached...), nil
	}
	if s.repo == nil {
		return []Playbook{}, nil
	}
	stored, found, err := s.repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		stored = Seed()
	}
	list := Normalize(stored)
	s.cache(list)
	return append([]Playbook(nil), list...), nil
}

// Replace stores the whole library. Partial updates are deliberately not
// supported: the admin page edits one document and submits it as a whole, so
// deletes and reordering need no separate verbs.
func (s *Service) Replace(ctx context.Context, list []Playbook, actor string) ([]Playbook, error) {
	if s == nil || s.repo == nil {
		return nil, ErrInvalidPlaybooks
	}
	normalized := Normalize(list)
	if err := Validate(normalized); err != nil {
		return nil, err
	}
	err := s.repo.Save(ctx, normalized)
	entry := serviceaudit.Result(
		serviceaudit.ActionSettingsPlaybooks,
		serviceaudit.Target{Type: serviceaudit.TargetServer, ID: "playbooks", Name: "Playbooks"},
		serviceaudit.Meta{"count": len(normalized)},
		err,
	)
	if actor != "" {
		entry.Actor = serviceaudit.Actor{Email: serviceaudit.NormalizeActorEmail(actor), IsAdmin: true}
	}
	s.audit.Record(ctx, entry)
	if err != nil {
		return nil, err
	}
	s.cache(normalized)
	return append([]Playbook(nil), normalized...), nil
}

func (s *Service) cache(list []Playbook) {
	s.mu.Lock()
	s.cached = list
	s.loaded = true
	s.mu.Unlock()
}
