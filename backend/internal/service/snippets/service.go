package snippets

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Owner identifies whose library is being read. It is derived from the session
// and never from a request path, which is what makes every route in this
// package owner-only by construction.
type Owner string

// OwnerFromSession maps a session to its library key. The subject claim is
// preferred because it survives an address change; an email is the fallback
// for local sign-in, which has no subject.
func OwnerFromSession(email, sub string) (Owner, error) {
	if sub = strings.TrimSpace(sub); sub != "" {
		return Owner("sub:" + sub), nil
	}
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		return Owner("email:" + email), nil
	}
	return "", ErrInvalidOwner
}

// Repository persists one document per owner. A missing document is not an
// error: the service seeds one on the first read that finds none.
type Repository interface {
	Load(ctx context.Context, owner Owner) ([]Snippet, bool, error)
	Save(ctx context.Context, owner Owner, list []Snippet) error
}

// Service is the single owner of every user's snippet library.
type Service struct {
	repo Repository
	now  func() time.Time
}

// Option configures the service at construction.
type Option func(*Service)

// WithClock replaces the wall clock so timestamps are testable.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func New(repo Repository, options ...Option) *Service {
	service := &Service{repo: repo, now: time.Now}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// List returns the owner's library, most used first, seeding the client
// templates on the first read.
func (s *Service) List(ctx context.Context, owner Owner) ([]Snippet, error) {
	list, err := s.ensure(ctx, owner)
	if err != nil {
		return nil, err
	}
	return Sort(list), nil
}

// Create adds one snippet and returns it as stored.
func (s *Service) Create(ctx context.Context, owner Owner, input Input) (Snippet, error) {
	list, err := s.ensure(ctx, owner)
	if err != nil {
		return Snippet{}, err
	}
	if len(list) >= MaxSnippets {
		return Snippet{}, ErrInvalidSnippet
	}
	now := s.now().UnixMilli()
	taken := make(map[string]struct{}, len(list))
	for _, item := range list {
		taken[item.ID] = struct{}{}
	}
	item := Normalize(apply(Snippet{CreatedAt: now}, input))
	item.ID = NewID(item.Title, taken)
	item.UpdatedAt = now
	next := append(append([]Snippet(nil), list...), item)
	if err := s.persist(ctx, owner, next); err != nil {
		return Snippet{}, err
	}
	return item, nil
}

// Update replaces the editable half of one snippet. The counters and the
// creation time are the server's and survive every edit.
func (s *Service) Update(ctx context.Context, owner Owner, id string, input Input) (Snippet, error) {
	list, err := s.ensure(ctx, owner)
	if err != nil {
		return Snippet{}, err
	}
	index := indexOf(list, id)
	if index < 0 {
		return Snippet{}, ErrNotFound
	}
	next := append([]Snippet(nil), list...)
	updated := Normalize(apply(next[index], input))
	updated.ID = next[index].ID
	updated.CreatedAt = next[index].CreatedAt
	updated.Uses = next[index].Uses
	updated.UpdatedAt = s.now().UnixMilli()
	next[index] = updated
	if err := s.persist(ctx, owner, next); err != nil {
		return Snippet{}, err
	}
	return updated, nil
}

// Delete removes one snippet. Deleting the last one leaves an empty document
// rather than no document, so the seed never comes back.
func (s *Service) Delete(ctx context.Context, owner Owner, id string) error {
	list, err := s.ensure(ctx, owner)
	if err != nil {
		return err
	}
	index := indexOf(list, id)
	if index < 0 {
		return ErrNotFound
	}
	next := append(append([]Snippet(nil), list[:index]...), list[index+1:]...)
	return s.persist(ctx, owner, next)
}

// MarkUsed increments the insertion counter, which is what sorts the picker.
// It is deliberately its own verb: an edit and a use mean different things and
// only one of them should move a snippet up the list.
func (s *Service) MarkUsed(ctx context.Context, owner Owner, id string) (Snippet, error) {
	list, err := s.ensure(ctx, owner)
	if err != nil {
		return Snippet{}, err
	}
	index := indexOf(list, id)
	if index < 0 {
		return Snippet{}, ErrNotFound
	}
	next := append([]Snippet(nil), list...)
	next[index].Uses++
	if err := s.persist(ctx, owner, next); err != nil {
		return Snippet{}, err
	}
	return next[index], nil
}

// Import merges an exported document back in. Matching ids are overwritten and
// unknown ones are added, so re-importing a file the user exported is a no-op
// rather than a duplicate library. Replace empties the library first, which is
// the "restore this backup exactly" case.
func (s *Service) Import(ctx context.Context, owner Owner, incoming []Snippet, replace bool) ([]Snippet, error) {
	current, err := s.ensure(ctx, owner)
	if err != nil {
		return nil, err
	}
	if replace {
		current = nil
	}
	now := s.now().UnixMilli()
	merged := append([]Snippet(nil), current...)
	taken := make(map[string]struct{}, len(merged))
	shortcuts := make(map[string]struct{}, len(merged))
	for _, item := range merged {
		taken[item.ID] = struct{}{}
		if item.Shortcut != "" {
			shortcuts[item.Shortcut] = struct{}{}
		}
	}

	for _, item := range incoming {
		item = Normalize(item)
		if item.Title == "" && item.Body == "" && item.Variants == (Variants{}) {
			continue
		}
		if item.CreatedAt <= 0 {
			item.CreatedAt = now
		}
		if item.UpdatedAt <= 0 {
			item.UpdatedAt = now
		}
		if index := indexOf(merged, item.ID); index >= 0 {
			// A re-import keeps the existing counter: the file records what
			// the snippet says, not how often this user reached for it.
			item.Uses = max(item.Uses, merged[index].Uses)
			if item.Shortcut != "" && merged[index].Shortcut != item.Shortcut {
				item.Shortcut = uniqueShortcut(item.Shortcut, shortcuts)
			}
			merged[index] = item
			continue
		}
		if item.ID == "" || Reserved(item.ID) || !idPattern.MatchString(item.ID) {
			item.ID = NewID(item.Title, taken)
		}
		taken[item.ID] = struct{}{}
		item.Shortcut = uniqueShortcut(item.Shortcut, shortcuts)
		if item.Shortcut != "" {
			shortcuts[item.Shortcut] = struct{}{}
		}
		if len(merged) >= MaxSnippets {
			return nil, ErrInvalidSnippet
		}
		merged = append(merged, item)
	}

	if err := s.persist(ctx, owner, merged); err != nil {
		return nil, err
	}
	return Sort(merged), nil
}

// ensure loads the owner's library, seeding it on the first read.
func (s *Service) ensure(ctx context.Context, owner Owner) ([]Snippet, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(string(owner)) == "" {
		return nil, ErrInvalidOwner
	}
	stored, found, err := s.repo.Load(ctx, owner)
	if err != nil {
		return nil, err
	}
	if found {
		return NormalizeList(stored), nil
	}
	seeded := Seed(s.now().UnixMilli())
	if err := s.repo.Save(ctx, owner, seeded); err != nil {
		return nil, err
	}
	return seeded, nil
}

func (s *Service) persist(ctx context.Context, owner Owner, list []Snippet) error {
	normalized := NormalizeList(list)
	if err := Validate(normalized); err != nil {
		return err
	}
	return s.repo.Save(ctx, owner, normalized)
}

func apply(item Snippet, input Input) Snippet {
	item.Title = input.Title
	item.Body = input.Body
	item.Audience = input.Audience
	item.Variants = input.Variants
	item.Tags = input.Tags
	item.Shortcut = input.Shortcut
	return item
}

func indexOf(list []Snippet, id string) int {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return -1
	}
	for index, item := range list {
		if item.ID == id {
			return index
		}
	}
	return -1
}

// uniqueShortcut keeps an imported shortcut from colliding with one the user
// already types. Dropping the collision would be worse than renaming it: the
// import would fail validation and the whole file would be refused.
func uniqueShortcut(shortcut string, taken map[string]struct{}) string {
	if shortcut == "" {
		return ""
	}
	candidate := shortcut
	for index := 2; ; index++ {
		if _, used := taken[candidate]; !used {
			return candidate
		}
		candidate = shortcut + "-" + strconv.Itoa(index)
		if len(candidate) > maxShortcutLength {
			return ""
		}
	}
}
