package filesitewatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
)

const testSiteID = servicesitewatch.ID("aaaaaaaaaaaaaaaaaaaaaaaa")

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store, dir
}

func TestLoadOnAFreshServerIsEmptyRatherThanAnError(t *testing.T) {
	store, _ := newStore(t)
	sites, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(sites) != 0 {
		t.Fatalf("sites = %v, want none", sites)
	}
	records, err := store.LoadHistory(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("history = %v, want none", records)
	}
}

func TestSaveAndLoadRoundTripsTheCatalog(t *testing.T) {
	store, dir := newStore(t)
	want := []servicesitewatch.Site{{
		ID:              testSiteID,
		Label:           "Client shop",
		URL:             "https://shop.example.com/",
		Enabled:         true,
		IntervalMinutes: 5,
		Checks:          servicesitewatch.DefaultChecks(),
		Headers:         map[string]string{"X-Monitor-Token": "secret-value"},
		Method:          servicesitewatch.MethodHEAD,
	}}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].ID != want[0].ID || got[0].URL != want[0].URL {
		t.Fatalf("loaded = %+v, want %+v", got, want)
	}
	if got[0].Headers["X-Monitor-Token"] != "secret-value" {
		t.Fatalf("headers = %v, want the stored token", got[0].Headers)
	}

	// The catalog can hold a shared token, so it must never be world readable.
	assertPrivate(t, filepath.Join(dir, dirName, catalogFile))
}

func TestAppendHistoryTrimsToTheCap(t *testing.T) {
	store, dir := newStore(t)
	// Seed a full window directly: the behaviour under test is what the next
	// appends do to a file that is already at the cap, and writing five
	// hundred records one API call at a time only measures the filesystem.
	path := filepath.Join(dir, dirName, historyDir, string(testSiteID)+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create history dir: %v", err)
	}
	var seed strings.Builder
	for index := range servicesitewatch.MaxHistoryRecords {
		fmt.Fprintf(&seed, "{\"at\":%d,\"st\":\"up\",\"ms\":1}\n", index+1)
	}
	if err := os.WriteFile(path, []byte(seed.String()), 0o600); err != nil {
		t.Fatalf("seed history: %v", err)
	}

	total := servicesitewatch.MaxHistoryRecords
	for range 37 {
		total++
		record := servicesitewatch.Record{
			At:         int64(total),
			Status:     servicesitewatch.StatusUp,
			DurationMs: 1,
		}
		if err := store.AppendHistory(context.Background(), testSiteID, record); err != nil {
			t.Fatalf("AppendHistory: %v", err)
		}
	}
	got, err := store.LoadHistory(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(got) != servicesitewatch.MaxHistoryRecords {
		t.Fatalf("history = %d records, want the cap %d", len(got), servicesitewatch.MaxHistoryRecords)
	}
	// The newest survive; the oldest are the ones dropped.
	if got[len(got)-1].At != int64(total) {
		t.Fatalf("newest record = %d, want %d", got[len(got)-1].At, total)
	}
	if got[0].At != int64(total-servicesitewatch.MaxHistoryRecords+1) {
		t.Fatalf("oldest kept record = %d, want the tail of the window", got[0].At)
	}
	assertPrivate(t, path)
}

func TestLoadHistorySkipsADamagedLine(t *testing.T) {
	store, dir := newStore(t)
	for _, record := range []servicesitewatch.Record{
		{At: 1, Status: servicesitewatch.StatusUp},
		{At: 2, Status: servicesitewatch.StatusDown},
	} {
		if err := store.AppendHistory(context.Background(), testSiteID, record); err != nil {
			t.Fatalf("AppendHistory: %v", err)
		}
	}
	path := filepath.Join(dir, dirName, historyDir, string(testSiteID)+".jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	// A crash mid-append leaves a truncated tail; it must cost one record,
	// not the whole log.
	if err := os.WriteFile(path, append(raw, []byte(`{"at":3,"st":"u`)...), 0o600); err != nil {
		t.Fatalf("write damaged history: %v", err)
	}
	got, err := store.LoadHistory(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("history = %d records, want the two intact ones", len(got))
	}
}

func TestHistoryPathRefusesAnIDItDidNotIssue(t *testing.T) {
	store, _ := newStore(t)
	for _, id := range []servicesitewatch.ID{"", "../../etc/passwd", "AAAAAAAAAAAAAAAAAAAAAAAA", "short"} {
		if _, err := store.LoadHistory(context.Background(), id); !errors.Is(err, servicesitewatch.ErrNotFound) {
			t.Fatalf("LoadHistory(%q) = %v, want ErrNotFound", id, err)
		}
		if err := store.AppendHistory(context.Background(), id, servicesitewatch.Record{}); !errors.Is(err, servicesitewatch.ErrNotFound) {
			t.Fatalf("AppendHistory(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

func TestDeleteHistoryIsIdempotent(t *testing.T) {
	store, _ := newStore(t)
	if err := store.AppendHistory(context.Background(), testSiteID, servicesitewatch.Record{At: 1}); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}
	for range 2 {
		if err := store.DeleteHistory(context.Background(), testSiteID); err != nil {
			t.Fatalf("DeleteHistory: %v", err)
		}
	}
}

// assertPrivate checks a file is not readable by group or other. It is a
// no-op check on Windows, where Go reports a synthetic mode.
func assertPrivate(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if os.PathSeparator == '\\' {
		return
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("%s mode = %o, want no group or other access", path, mode)
	}
}
