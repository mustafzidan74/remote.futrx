package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicejournal "github.com/futrx-com/remote.futrx.com/internal/service/journal"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// journalTestChats is the slice of the chat repository this driver reads.
type journalTestChats struct {
	servicechat.Repository
	meta   servicechat.Meta
	events []servicechat.Event
	err    error
}

func (c journalTestChats) Get(context.Context, servicechat.ID) (servicechat.Meta, error) {
	return c.meta, c.err
}

func (c journalTestChats) ReadEvents(context.Context, servicechat.ID) ([]servicechat.Event, error) {
	return c.events, nil
}

type journalTestStore struct{ entries []servicejournal.Entry }

func (s *journalTestStore) Append(entry servicejournal.Entry) error {
	s.entries = append(s.entries, entry)
	return nil
}

func (s *journalTestStore) Query(servicejournal.Query) (servicejournal.Page, error) {
	return servicejournal.Page{}, nil
}

func (s *journalTestStore) Export(string) (string, error) { return "", nil }

func toolEvent(name string, input map[string]any) servicechat.Event {
	encoded, _ := json.Marshal(input)
	return servicechat.Event{Type: "tool_use_start", Name: name, Input: encoded}
}

func runDriver(t *testing.T, chats journalTestChats, outcome prompt.RunOutcome) *journalTestStore {
	t.Helper()
	store := &journalTestStore{}
	driver := newJournalDriver(servicejournal.New(store), chats)
	// Run the work inline so the test does not race the goroutine.
	driver.background = func(work func()) { work() }
	driver.RunSettled(context.Background(), outcome)
	return store
}

func TestSettledRunBecomesAJournalEntry(t *testing.T) {
	chats := journalTestChats{
		meta: servicechat.Meta{ID: "c1", ProjectID: "p1", Title: "Landing page", Provider: "claude", Model: "opus"},
		events: []servicechat.Event{
			{Type: "user", T: 1000, Text: "old request"},
			{Type: "assistant_text", T: 1100, Text: "old answer"},
			{Type: "user", T: 2000, Text: "  add a barcode scanner  "},
			toolEvent("Read", map[string]any{"file_path": "/workspace/src/app.ts"}),
			toolEvent("Write", map[string]any{"file_path": "/workspace/src/scan.ts"}),
			toolEvent("Edit", map[string]any{"file_path": "/workspace/src/scan.ts"}),
			toolEvent("Bash", map[string]any{"command": "npm test\nnpm run build"}),
			{Type: "assistant_text", T: 2600, Text: "done"},
		},
	}
	store := runDriver(t, chats, prompt.RunOutcome{ChatID: "c1", Output: " scanner added "})

	if len(store.entries) != 1 {
		t.Fatalf("entries = %+v", store.entries)
	}
	entry := store.entries[0]
	if entry.ProjectID != "p1" || entry.ChatID != "c1" || entry.ChatTitle != "Landing page" {
		t.Fatalf("entry identity = %+v", entry)
	}
	if entry.Request != "add a barcode scanner" {
		t.Fatalf("request = %q, want the last turn's own prompt", entry.Request)
	}
	if entry.Summary != "scanner added" || entry.Status != servicejournal.StatusDone {
		t.Fatalf("outcome = %+v", entry)
	}
	// Written files only, workspace-relative, each one once.
	if len(entry.Files) != 1 || entry.Files[0] != "src/scan.ts" {
		t.Fatalf("files = %v", entry.Files)
	}
	if len(entry.Commands) != 1 || entry.Commands[0] != "npm test …" {
		t.Fatalf("commands = %v", entry.Commands)
	}
	if entry.At != 2000 || entry.DurationMs != 600 {
		t.Fatalf("timing = at %d, duration %d", entry.At, entry.DurationMs)
	}
}

func TestFailedRunIsRecordedWithItsError(t *testing.T) {
	chats := journalTestChats{
		meta:   servicechat.Meta{ID: "c1", ProjectID: "p1"},
		events: []servicechat.Event{{Type: "user", T: 5, Text: "deploy it"}},
	}
	store := runDriver(t, chats, prompt.RunOutcome{ChatID: "c1", Err: errors.New("agent run failed")})
	if len(store.entries) != 1 {
		t.Fatalf("entries = %+v", store.entries)
	}
	if store.entries[0].Status != servicejournal.StatusFailed || store.entries[0].Error != "agent run failed" {
		t.Fatalf("failed entry = %+v", store.entries[0])
	}
}

func TestChatOutsideAProjectIsNotJournalled(t *testing.T) {
	chats := journalTestChats{
		meta:   servicechat.Meta{ID: "c1"},
		events: []servicechat.Event{{Type: "user", T: 1, Text: "hello"}},
	}
	if store := runDriver(t, chats, prompt.RunOutcome{ChatID: "c1", Output: "hi"}); len(store.entries) != 0 {
		t.Fatalf("entries = %+v", store.entries)
	}
}
