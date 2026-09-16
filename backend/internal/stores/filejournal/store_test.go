package filejournal

import (
	"errors"
	"strings"
	"testing"
	"time"

	servicejournal "github.com/futrx-com/remote.futrx.com/internal/service/journal"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func day(year int, month time.Month, dayOfMonth int) int64 {
	return time.Date(year, month, dayOfMonth, 12, 0, 0, 0, time.UTC).UnixMilli()
}

func seed(t *testing.T, store *Store) {
	t.Helper()
	entries := []servicejournal.Entry{
		{ProjectID: "p1", At: day(2026, time.September, 1), Request: "add the barcode scanner", Summary: "scanner added", Files: []string{"src/scan.ts"}, Status: servicejournal.StatusDone},
		{ProjectID: "p1", At: day(2026, time.September, 10), Request: "fix the invoice total", Summary: "rounding corrected", Files: []string{"src/invoice.ts"}, Status: servicejournal.StatusDone},
		{ProjectID: "p1", At: day(2026, time.September, 20), Request: "add WhatsApp alerts", Status: servicejournal.StatusFailed, Error: "no provider configured"},
		{ProjectID: "p2", At: day(2026, time.September, 10), Request: "another project entirely", Status: servicejournal.StatusDone},
	}
	for _, entry := range entries {
		if err := store.Append(entry); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}

func TestQueryReturnsOneProjectNewestFirst(t *testing.T) {
	store := newTestStore(t)
	seed(t, store)

	page, err := store.Query(servicejournal.Query{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("entries = %d, want the project's three runs", len(page.Entries))
	}
	if page.Entries[0].Request != "add WhatsApp alerts" || page.Entries[2].Request != "add the barcode scanner" {
		t.Fatalf("order = %+v", page.Entries)
	}
	if page.NextCursor != "" {
		t.Fatalf("a complete page must not offer a cursor: %q", page.NextCursor)
	}
}

func TestQueryFiltersByDateAndText(t *testing.T) {
	store := newTestStore(t)
	seed(t, store)

	dated, err := store.Query(servicejournal.Query{
		ProjectID: "p1",
		From:      day(2026, time.September, 5),
		To:        day(2026, time.September, 15),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dated.Entries) != 1 || dated.Entries[0].Request != "fix the invoice total" {
		t.Fatalf("date filter = %+v", dated.Entries)
	}

	// Searching covers the request, the summary and the touched files.
	for _, text := range []string{"BARCODE", "scanner added", "src/scan.ts"} {
		found, err := store.Query(servicejournal.Query{ProjectID: "p1", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if len(found.Entries) != 1 || found.Entries[0].Request != "add the barcode scanner" {
			t.Fatalf("search %q = %+v", text, found.Entries)
		}
	}
	empty, err := store.Query(servicejournal.Query{ProjectID: "p1", Text: "nothing here"})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Entries) != 0 {
		t.Fatalf("unmatched search returned %+v", empty.Entries)
	}
}

func TestQueryPagesWithACursor(t *testing.T) {
	store := newTestStore(t)
	seed(t, store)

	first, err := store.Query(servicejournal.Query{ProjectID: "p1", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %+v, cursor %q", first.Entries, first.NextCursor)
	}
	second, err := store.Query(servicejournal.Query{ProjectID: "p1", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 1 || second.Entries[0].Request != "add the barcode scanner" || second.NextCursor != "" {
		t.Fatalf("second page = %+v, cursor %q", second.Entries, second.NextCursor)
	}
	if _, err := store.Query(servicejournal.Query{ProjectID: "p1", Cursor: "../etc"}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("bad cursor error = %v", err)
	}
}

func TestUnknownProjectReadsEmptyAndTraversalIsRefused(t *testing.T) {
	store := newTestStore(t)
	page, err := store.Query(servicejournal.Query{ProjectID: "never-ran"})
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("unknown project = (%+v, %v)", page.Entries, err)
	}
	if err := store.Append(servicejournal.Entry{ProjectID: "../../escape", Request: "x"}); !errors.Is(err, ErrInvalidProjectID) {
		t.Fatalf("traversal append error = %v", err)
	}
	if _, err := store.Query(servicejournal.Query{ProjectID: "../../escape"}); !errors.Is(err, ErrInvalidProjectID) {
		t.Fatalf("traversal query error = %v", err)
	}
}

func TestExportRendersMarkdownNewestFirst(t *testing.T) {
	store := newTestStore(t)
	seed(t, store)

	markdown, err := store.Export("p1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Project change journal",
		"2026-09-20",
		"> add WhatsApp alerts",
		"- `src/invoice.ts`",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("export lacks %q:\n%s", want, markdown)
		}
	}
	if strings.Index(markdown, "2026-09-20") > strings.Index(markdown, "2026-09-01") {
		t.Fatal("export must start with the newest run")
	}
	empty, err := store.Export("never-ran")
	if err != nil || !strings.Contains(empty, "No agent runs recorded yet.") {
		t.Fatalf("empty export = (%q, %v)", empty, err)
	}
}
