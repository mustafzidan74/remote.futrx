package search

import (
	"strings"
	"testing"
)

func highlighted(snippet string) string {
	start := strings.Index(snippet, HighlightStart)
	end := strings.Index(snippet, HighlightEnd)
	if start < 0 || end < start {
		return ""
	}
	return snippet[start+len(HighlightStart) : end]
}

func TestIndexAddAndQuery(t *testing.T) {
	index := NewIndex(0, 0)
	index.SetChat("chat-1", "project-1", "Deploy notes", 100)
	index.SetChat("chat-2", "project-2", "Billing", 200)
	index.Add(Entry{ChatID: "chat-1", Role: RoleUser, At: 110, Text: "how do I restart caddy?"})
	index.Add(Entry{ChatID: "chat-2", Role: RoleUser, At: 210, Text: "invoice numbering is off"})

	tests := []struct {
		name      string
		query     string
		projectID string
		limit     int
		wantChats []string
	}{
		{name: "matches a message body", query: "caddy", limit: 10, wantChats: []string{"chat-1"}},
		{name: "matches a chat title", query: "billing", limit: 10, wantChats: []string{"chat-2"}},
		{
			name:      "project filter excludes other projects",
			query:     "invoice",
			projectID: "project-1",
			limit:     10,
			wantChats: nil,
		},
		{name: "no match", query: "kubernetes", limit: 10, wantChats: nil},
		{name: "empty query matches nothing", query: "   ", limit: 10, wantChats: nil},
		{name: "zero limit matches nothing", query: "caddy", limit: 0, wantChats: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matches := index.Query(test.query, test.projectID, test.limit, nil)
			if len(matches) != len(test.wantChats) {
				t.Fatalf("Query() returned %d matches, want %d", len(matches), len(test.wantChats))
			}
			for position, want := range test.wantChats {
				if matches[position].ChatID != want {
					t.Errorf("match %d chat = %q, want %q", position, matches[position].ChatID, want)
				}
			}
		})
	}
}

func TestQueryAppliesMembershipFilterAndLimit(t *testing.T) {
	index := NewIndex(0, 0)
	for _, chatID := range []string{"mine", "theirs"} {
		index.SetChat(chatID, "project-"+chatID, "shared title", 100)
		index.Add(Entry{ChatID: chatID, Role: RoleUser, At: 110, Text: "shared secret text"})
	}

	matches := index.Query("shared", "", 10, func(chatID string) bool { return chatID == "mine" })
	if len(matches) == 0 {
		t.Fatal("Query() returned nothing for a permitted chat")
	}
	for _, match := range matches {
		if match.ChatID != "mine" {
			t.Fatalf("Query() leaked chat %q past the membership filter", match.ChatID)
		}
	}

	if got := index.Query("shared", "", 1, nil); len(got) != 1 {
		t.Fatalf("Query() honoured limit poorly: %d matches, want 1", len(got))
	}
}

func TestQueryIsArabicAware(t *testing.T) {
	index := NewIndex(0, 0)
	index.SetChat("chat-1", "", "دردشة", 100)
	index.Add(Entry{ChatID: "chat-1", Role: RoleAssistant, At: 110, Text: "أهلاً، الخدمة اشتغلت على السيرفر"})

	for _, query := range []string{"اهلا", "الخدمه", "علي السيرفر"} {
		matches := index.Query(query, "", 5, nil)
		if len(matches) != 1 {
			t.Fatalf("Query(%q) returned %d matches, want 1", query, len(matches))
		}
		if highlighted(matches[0].Snippet) == "" {
			t.Errorf("Query(%q) snippet has no highlighted span: %q", query, matches[0].Snippet)
		}
	}
}

func TestSnippetHighlightsTheMatchInTheOriginalText(t *testing.T) {
	long := strings.Repeat("padding ", 40)
	index := NewIndex(0, 0)
	index.SetChat("chat-1", "", "", 100)
	index.Add(Entry{ChatID: "chat-1", Role: RoleUser, At: 110, Text: long + "NEEDLE " + long})

	matches := index.Query("needle", "", 5, nil)
	if len(matches) != 1 {
		t.Fatalf("Query() returned %d matches, want 1", len(matches))
	}
	snippet := matches[0].Snippet
	if got := highlighted(snippet); got != "NEEDLE" {
		t.Fatalf("highlighted span = %q, want the original casing %q", got, "NEEDLE")
	}
	if !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") {
		t.Errorf("snippet = %q, want both ends elided", snippet)
	}
	if len([]rune(snippet)) > 2*snippetContext+40 {
		t.Errorf("snippet is %d runes, want it bounded near ±%d", len([]rune(snippet)), snippetContext)
	}
}

func TestAssistantDeltasCoalesceIntoOneEntry(t *testing.T) {
	index := NewIndex(0, 0)
	index.SetChat("chat-1", "", "", 100)
	for _, delta := range []string{"the migration ", "ran ", "successfully"} {
		index.Add(Entry{ChatID: "chat-1", Role: RoleAssistant, At: 110, Text: delta})
	}

	// A phrase spanning two deltas is only findable because they merged.
	matches := index.Query("migration ran successfully", "", 5, nil)
	if len(matches) != 1 {
		t.Fatalf("Query() returned %d matches, want 1", len(matches))
	}
	if stats := index.Stats(); stats.Entries != 1 {
		t.Errorf("Entries = %d, want the deltas coalesced into 1", stats.Entries)
	}
}

func TestUserMessageClosesTheAssistantTurn(t *testing.T) {
	index := NewIndex(0, 0)
	index.SetChat("chat-1", "", "", 100)
	index.Add(Entry{ChatID: "chat-1", Role: RoleAssistant, At: 110, Text: "first answer"})
	index.Add(Entry{ChatID: "chat-1", Role: RoleUser, At: 120, Text: "follow up"})
	index.Add(Entry{ChatID: "chat-1", Role: RoleAssistant, At: 130, Text: "second answer"})

	if stats := index.Stats(); stats.Entries != 3 {
		t.Fatalf("Entries = %d, want 3 distinct messages", stats.Entries)
	}
	if got := index.Query("first answer second", "", 5, nil); len(got) != 0 {
		t.Error("two separate assistant turns were merged across a user message")
	}
}

func TestConcurrentChatsDoNotMergeIntoEachOther(t *testing.T) {
	index := NewIndex(0, 0)
	index.SetChat("chat-1", "", "", 100)
	index.SetChat("chat-2", "", "", 100)
	index.Add(Entry{ChatID: "chat-1", Role: RoleAssistant, At: 110, Text: "alpha"})
	index.Add(Entry{ChatID: "chat-2", Role: RoleAssistant, At: 111, Text: "beta"})
	index.Add(Entry{ChatID: "chat-1", Role: RoleAssistant, At: 112, Text: "gamma"})

	matches := index.Query("alpha gamma", "", 5, nil)
	if len(matches) != 1 || matches[0].ChatID != "chat-1" {
		t.Fatalf("interleaved streams did not merge per chat: %+v", matches)
	}
	if got := index.Query("alpha beta", "", 5, nil); len(got) != 0 {
		t.Error("text from two different chats was merged into one entry")
	}
}

func TestIndexEvictsOldestBeyondBounds(t *testing.T) {
	index := NewIndex(2, 0)
	index.SetChat("chat-1", "", "", 100)
	index.Add(Entry{ChatID: "chat-1", Role: RoleUser, At: 1, Text: "oldest"})
	index.Add(Entry{ChatID: "chat-1", Role: RoleUser, At: 2, Text: "middle"})
	index.Add(Entry{ChatID: "chat-1", Role: RoleUser, At: 3, Text: "newest"})

	stats := index.Stats()
	if stats.Entries != 2 || stats.Evicted != 1 {
		t.Fatalf("Stats() = %+v, want 2 entries and 1 eviction", stats)
	}
	if got := index.Query("oldest", "", 5, nil); len(got) != 0 {
		t.Error("an evicted entry is still searchable")
	}
	if got := index.Query("newest", "", 5, nil); len(got) != 1 {
		t.Error("the newest entry was evicted instead of the oldest")
	}
}

func TestRemoveChatDropsEverythingItContributed(t *testing.T) {
	index := NewIndex(0, 0)
	index.SetChat("chat-1", "", "Removable", 100)
	index.SetChat("chat-2", "", "Kept", 100)
	index.Add(Entry{ChatID: "chat-1", Role: RoleUser, At: 110, Text: "vanishing text"})
	index.Add(Entry{ChatID: "chat-2", Role: RoleUser, At: 111, Text: "surviving text"})

	index.RemoveChat("chat-1")

	if got := index.Query("vanishing", "", 5, nil); len(got) != 0 {
		t.Error("a deleted chat's messages are still searchable")
	}
	if got := index.Query("Removable", "", 5, nil); len(got) != 0 {
		t.Error("a deleted chat's title is still searchable")
	}
	if got := index.Query("surviving", "", 5, nil); len(got) != 1 {
		t.Error("removing one chat dropped another chat's messages")
	}
}

func TestIndexedTextCannotForgeHighlightMarkers(t *testing.T) {
	index := NewIndex(0, 0)
	index.SetChat("chat-1", "", "", 100)
	index.Add(Entry{
		ChatID: "chat-1",
		Role:   RoleUser,
		At:     110,
		Text:   "smuggled " + HighlightStart + "fake" + HighlightEnd + " marker",
	})

	matches := index.Query("marker", "", 5, nil)
	if len(matches) != 1 {
		t.Fatalf("Query() returned %d matches, want 1", len(matches))
	}
	if got := highlighted(matches[0].Snippet); got != "marker" {
		t.Fatalf("highlighted span = %q, want only the real match", got)
	}
}
