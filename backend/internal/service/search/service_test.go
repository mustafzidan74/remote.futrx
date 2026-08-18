package search

import (
	"context"
	"errors"
	"strings"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

type stubChats struct {
	metas  []servicechat.Meta
	events map[servicechat.ID][]servicechat.Event
}

func (s stubChats) List(context.Context) ([]servicechat.Meta, error) {
	return append([]servicechat.Meta(nil), s.metas...), nil
}

func (s stubChats) ReadEvents(_ context.Context, id servicechat.ID) ([]servicechat.Event, error) {
	return s.events[id], nil
}

// stubAccess answers with only the chats the caller may see, exactly like the
// real chat access service.
type stubAccess struct {
	byEmail map[string][]servicechat.Meta
	all     []servicechat.Meta
}

func (s stubAccess) List(_ context.Context, email string, isAdmin bool) ([]servicechat.Meta, error) {
	if isAdmin {
		return s.all, nil
	}
	return s.byEmail[email], nil
}

type stubProjectDirectory []serviceproject.Meta

func (s stubProjectDirectory) ListVisible(context.Context, string, bool) ([]serviceproject.Meta, error) {
	return s, nil
}

func newTestFixture() (stubChats, []servicechat.Meta) {
	metas := []servicechat.Meta{
		{ID: "chat-a", Title: "Caddy rollout", ProjectID: "project-1", LastMessageAt: 300},
		{ID: "chat-b", Title: "Invoices", ProjectID: "project-2", LastMessageAt: 200},
	}
	chats := stubChats{
		metas: metas,
		events: map[servicechat.ID][]servicechat.Event{
			"chat-a": {
				{Type: "user", T: 310, Seq: 1, Text: "restart caddy on the box"},
				{Type: "tool_use_start", T: 311, Seq: 2, Name: "Bash"},
				{Type: "assistant_text", T: 312, Seq: 3, Text: "done, caddy is "},
				{Type: "assistant_text", T: 313, Seq: 4, Text: "running again"},
			},
			"chat-b": {
				{Type: "user", T: 210, Seq: 1, Text: "the invoice total is wrong"},
			},
		},
	}
	return chats, metas
}

func buildService(t *testing.T, chats stubChats, options ...Option) *Service {
	t.Helper()
	service := New(chats, options...)
	service.Start(context.Background())
	<-service.Ready()
	return service
}

func TestSearchMembershipFilter(t *testing.T) {
	chats, metas := newTestFixture()
	access := stubAccess{
		all:     metas,
		byEmail: map[string][]servicechat.Meta{"member@example.com": {metas[0]}},
	}
	service := buildService(t, chats, WithAccess(access))

	tests := []struct {
		name      string
		query     Query
		wantChats []string
	}{
		{
			// The title "Invoices" matches too, and titles are reported first
			// because a chat named for the query is usually the target.
			name:      "an admin sees every chat",
			query:     Query{Text: "invoice", Email: "admin@example.com", IsAdmin: true},
			wantChats: []string{"chat-b", "chat-b"},
		},
		{
			name:      "a member only sees their own chats",
			query:     Query{Text: "invoice", Email: "member@example.com"},
			wantChats: nil,
		},
		{
			name:      "a member still searches the chats they belong to",
			query:     Query{Text: "caddy", Email: "member@example.com"},
			wantChats: []string{"chat-a", "chat-a", "chat-a"},
		},
		{
			name:      "a stranger sees nothing",
			query:     Query{Text: "caddy", Email: "nobody@example.com"},
			wantChats: nil,
		},
		{
			name:      "the project filter narrows the result",
			query:     Query{Text: "caddy", ProjectID: "project-2", Email: "admin@example.com", IsAdmin: true},
			wantChats: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := service.Search(context.Background(), test.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != len(test.wantChats) {
				t.Fatalf("Search() returned %d results %+v, want %d", len(results), results, len(test.wantChats))
			}
			for position, want := range test.wantChats {
				if results[position].ChatID != want {
					t.Errorf("result %d chat = %q, want %q", position, results[position].ChatID, want)
				}
			}
		})
	}
}

func TestSearchWithoutAccessServiceReturnsNothing(t *testing.T) {
	chats, _ := newTestFixture()
	service := buildService(t, chats)

	results, err := service.Search(context.Background(), Query{Text: "caddy", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("Search() returned %d results without a membership source, want 0", len(results))
	}
}

func TestSearchRejectsShortQueries(t *testing.T) {
	chats, metas := newTestFixture()
	service := buildService(t, chats, WithAccess(stubAccess{all: metas}))

	for _, query := range []string{"", " ", "c"} {
		if _, err := service.Search(context.Background(), Query{Text: query, IsAdmin: true}); !errors.Is(err, ErrQueryTooShort) {
			t.Errorf("Search(%q) error = %v, want ErrQueryTooShort", query, err)
		}
	}
}

func TestBuildSkipsToolPayloadsAndCoalescesAssistantText(t *testing.T) {
	chats, metas := newTestFixture()
	service := buildService(t, chats, WithAccess(stubAccess{all: metas}))

	results, err := service.Search(
		context.Background(),
		Query{Text: "caddy is running again", IsAdmin: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, want the coalesced assistant turn", len(results))
	}
	if results[0].Role != RoleAssistant {
		t.Errorf("role = %q, want %q", results[0].Role, RoleAssistant)
	}
	if got := service.Stats().Entries; got != 3 {
		t.Errorf("indexed entries = %d, want 3 (two users, one merged assistant turn)", got)
	}
}

func TestResultsCarryChatAndProjectNames(t *testing.T) {
	chats, metas := newTestFixture()
	service := buildService(
		t, chats,
		WithAccess(stubAccess{all: metas}),
		WithProjects(stubProjectDirectory{{ID: "project-1", Name: "Platform"}}),
	)

	results, err := service.Search(context.Background(), Query{Text: "restart caddy", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, want 1", len(results))
	}
	got := results[0]
	if got.ChatTitle != "Caddy rollout" || got.ProjectName != "Platform" {
		t.Fatalf("result = %+v, want the chat and project names attached", got)
	}
	if !strings.Contains(got.Snippet, HighlightStart) {
		t.Errorf("snippet = %q, want a highlighted span", got.Snippet)
	}
	if got.At != 310 {
		t.Errorf("at = %d, want the event timestamp 310", got.At)
	}
}

func TestIncrementalUpdatesAreSearchableImmediately(t *testing.T) {
	chats, metas := newTestFixture()
	service := buildService(t, chats, WithAccess(stubAccess{all: metas}))

	service.IndexEvent("chat-b", servicechat.Event{
		Type: "user", T: 400, Seq: 2, Text: "please regenerate the receipt",
	})
	results, err := service.Search(context.Background(), Query{Text: "receipt", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ChatID != "chat-b" {
		t.Fatalf("Search() = %+v, want the freshly appended event", results)
	}

	service.RemoveChat("chat-b")
	results, err = service.Search(context.Background(), Query{Text: "receipt", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("Search() = %+v after RemoveChat, want nothing", results)
	}
}
