package search

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// MinQueryLength is the shortest query the service answers. One character
// matches most of the corpus and is never what anybody meant.
const MinQueryLength = 2

// DefaultLimit and MaxLimit bound how many hits one request returns.
const (
	DefaultLimit = 30
	MaxLimit     = 100
)

// ErrQueryTooShort reports a query below MinQueryLength.
var ErrQueryTooShort = errors.New("search query is too short")

// Service answers full-text queries over the chat corpus.
//
// It owns an in-memory index that is built once in the background at startup
// and then maintained incrementally: every appended chat event is offered to
// it as it is persisted, so a message is searchable as soon as it is on disk.
// Nothing is written to disk — a restart simply rebuilds.
type Service struct {
	index    *Index
	chats    ChatSource
	access   ChatDirectory
	projects ProjectDirectory
	ready    chan struct{}
}

func New(chats ChatSource, options ...Option) *Service {
	service := &Service{
		index: NewIndex(DefaultMaxEntries, DefaultMaxBytes),
		chats: chats,
		ready: make(chan struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

type Option func(*Service)

// WithAccess installs the membership filter. Without it a query returns
// nothing, because the service refuses to answer without knowing who may see
// what — failing closed is the only safe default for a cross-chat reader.
func WithAccess(access ChatDirectory) Option {
	return func(s *Service) { s.access = access }
}

// WithProjects lets results carry project names as well as ids.
func WithProjects(projects ProjectDirectory) Option {
	return func(s *Service) { s.projects = projects }
}

// WithBounds overrides the index's memory bounds.
func WithBounds(maxEntries, maxBytes int) Option {
	return func(s *Service) { s.index = NewIndex(maxEntries, maxBytes) }
}

// Start builds the index in the background and returns immediately. A slow
// build must never delay the first HTTP request; queries issued before it
// finishes simply see whatever has been indexed so far plus every live event.
func (s *Service) Start(ctx context.Context) {
	if s == nil || s.chats == nil {
		if s != nil {
			s.markReady()
		}
		return
	}
	go func() {
		started := time.Now()
		chats, events, err := s.build(ctx)
		s.markReady()
		if err != nil {
			log.Printf("search: index build failed after %s: %v", time.Since(started).Round(time.Millisecond), err)
			return
		}
		stats := s.index.Stats()
		log.Printf(
			"search: indexed %d messages from %d chats in %s (%d KiB, %d evicted)",
			events, chats, time.Since(started).Round(time.Millisecond),
			stats.Bytes/1024, stats.Evicted,
		)
	}()
}

// Ready blocks until the initial build has settled. Only tests need it; the
// serving path is deliberately non-blocking.
func (s *Service) Ready() <-chan struct{} {
	return s.ready
}

func (s *Service) markReady() {
	select {
	case <-s.ready:
	default:
		close(s.ready)
	}
}

func (s *Service) build(ctx context.Context) (chats int, events int, err error) {
	metas, err := s.chats.List(ctx)
	if err != nil {
		return 0, 0, err
	}
	// Oldest chat first, so the eviction window keeps the newest work.
	for index := len(metas) - 1; index >= 0; index-- {
		meta := metas[index]
		if err := ctx.Err(); err != nil {
			return chats, events, err
		}
		chats++
		s.IndexChat(meta)
		persisted, err := s.chats.ReadEvents(ctx, meta.ID)
		if err != nil {
			// One unreadable chat log must not abort the whole build.
			log.Printf("search: skipping chat %s: %v", meta.ID, err)
			continue
		}
		for _, event := range persisted {
			if s.indexEvent(meta.ID, event) {
				events++
			}
		}
	}
	return chats, events, nil
}

// IndexChat records a chat's title. It is called on create, on rename, and
// once per chat during the initial build.
func (s *Service) IndexChat(meta servicechat.Meta) {
	if s == nil {
		return
	}
	at := meta.LastMessageAt
	if at == 0 {
		at = meta.CreatedAt
	}
	s.index.SetChat(string(meta.ID), string(meta.ProjectID), meta.Title, at)
}

// IndexEvent offers one persisted chat event to the index. Events that carry
// no searchable prose are ignored. The chat's project is not a parameter: the
// index already knows it from IndexChat, which keeps the streaming path free
// of a metadata read per delta.
func (s *Service) IndexEvent(chatID servicechat.ID, event servicechat.Event) {
	if s == nil {
		return
	}
	s.indexEvent(chatID, event)
}

// RemoveChat drops a deleted chat from the index.
func (s *Service) RemoveChat(chatID servicechat.ID) {
	if s == nil {
		return
	}
	s.index.RemoveChat(string(chatID))
}

// Stats exposes the index size for diagnostics.
func (s *Service) Stats() Stats {
	if s == nil {
		return Stats{}
	}
	return s.index.Stats()
}

// indexEvent maps a chat event onto an index entry. It reports whether
// anything was indexed.
func (s *Service) indexEvent(chatID servicechat.ID, event servicechat.Event) bool {
	role, ok := roleForEvent(event)
	if !ok {
		return false
	}
	text := strings.TrimSpace(event.Text)
	if text == "" {
		return false
	}
	s.index.Add(Entry{
		ChatID: string(chatID),
		Role:   role,
		At:     event.T,
		Seq:    event.Seq,
		Text:   text,
	})
	return true
}

// roleForEvent selects the event types worth indexing: what the human asked
// and what the agent said back. Tool calls, reasoning, usage and lifecycle
// events are skipped — they are either noise or a restatement of the two.
func roleForEvent(event servicechat.Event) (string, bool) {
	switch event.Type {
	case "user":
		return RoleUser, true
	case "assistant_text":
		return RoleAssistant, true
	default:
		return "", false
	}
}

// Query is one search request.
type Query struct {
	Text      string
	ProjectID string
	Limit     int
	Email     string
	IsAdmin   bool
}

// Result is one hit, ready to serialize.
type Result struct {
	ChatID      string `json:"chatId"`
	ChatTitle   string `json:"chatTitle"`
	ProjectID   string `json:"projectId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	Role        string `json:"role"`
	At          int64  `json:"at"`
	Snippet     string `json:"snippet"`
}

// Search runs one query, filtered to the chats the caller can see.
//
// Membership comes from the same chat access service the sidebar listing uses,
// so a chat that is invisible there is invisible here by construction rather
// than by a second, drifting copy of the rule.
func (s *Service) Search(ctx context.Context, query Query) ([]Result, error) {
	if s == nil {
		return nil, nil
	}
	text := strings.TrimSpace(query.Text)
	if len([]rune(text)) < MinQueryLength {
		return nil, ErrQueryTooShort
	}
	limit := query.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	if s.access == nil {
		return []Result{}, nil
	}

	visible, err := s.access.List(ctx, query.Email, query.IsAdmin)
	if err != nil {
		return nil, err
	}
	titles := make(map[string]string, len(visible))
	for _, meta := range visible {
		titles[string(meta.ID)] = meta.Title
	}

	matches := s.index.Query(text, strings.TrimSpace(query.ProjectID), limit, func(chatID string) bool {
		_, ok := titles[chatID]
		return ok
	})
	if len(matches) == 0 {
		return []Result{}, nil
	}

	names := s.projectNames(ctx, query)
	results := make([]Result, 0, len(matches))
	for _, match := range matches {
		results = append(results, Result{
			ChatID:      match.ChatID,
			ChatTitle:   titles[match.ChatID],
			ProjectID:   match.ProjectID,
			ProjectName: names[match.ProjectID],
			Role:        match.Role,
			At:          match.At,
			Snippet:     match.Snippet,
		})
	}
	return results, nil
}

func (s *Service) projectNames(ctx context.Context, query Query) map[string]string {
	names := map[string]string{}
	if s.projects == nil {
		return names
	}
	metas, err := s.projects.ListVisible(ctx, query.Email, query.IsAdmin)
	if err != nil {
		return names
	}
	for _, meta := range metas {
		names[string(meta.ID)] = meta.Name
	}
	return names
}
