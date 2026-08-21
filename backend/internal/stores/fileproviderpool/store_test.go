package fileproviderpool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	serviceproviderpool "github.com/futrx-com/remote.futrx.com/internal/service/providerpool"
)

func TestLoadOnAFreshServerAnswersWithAnEmptyRegistry(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	registry, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(registry.Providers) != 0 {
		t.Fatalf("providers = %+v, want nothing", registry.Providers)
	}
	// An empty registry that has *not* been seeded is what makes the service
	// install its templates on first boot.
	if registry.Seeded {
		t.Fatal("a fresh document claimed to have been seeded already")
	}
}

func TestSaveRoundTripsIncludingTheKeyAndTheNullableLimits(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	rpd := 1000
	saved := serviceproviderpool.Registry{
		Seeded: true,
		Settings: serviceproviderpool.Settings{
			AutoSwitch:          false,
			PreferredProviderID: "groq",
		},
		Providers: []serviceproviderpool.Provider{{
			ID:      "groq",
			Label:   "Groq",
			Kind:    serviceproviderpool.KindOpenAI,
			BaseURL: "https://api.groq.com/openai/v1",
			APIKey:  "gsk-round-trip-1234",
			Models: []serviceproviderpool.Model{{
				ID:            "llama-3.3-70b-versatile",
				ContextTokens: 131072,
				GoodFor:       []serviceproviderpool.Capability{serviceproviderpool.CapabilityText},
			}},
			// rpd is documented; the other four are not, and the difference
			// has to survive a round trip or the meters start lying.
			Limits:   serviceproviderpool.Limits{RPD: &rpd},
			Priority: 10,
			Enabled:  true,
		}},
	}
	if err := store.Save(context.Background(), saved); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	// A second store over the same directory is the "restart the process"
	// case, which is the one that matters for a settings document.
	reopened, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	loaded, err := reopened.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	provider, found := loaded.Find("groq")
	if !found {
		t.Fatal("the provider did not come back")
	}
	if provider.APIKey != "gsk-round-trip-1234" {
		t.Fatalf("apiKey = %q, want the stored credential back", provider.APIKey)
	}
	if provider.Limits.RPD == nil || *provider.Limits.RPD != 1000 {
		t.Fatalf("rpd = %v, want the documented cap", provider.Limits.RPD)
	}
	if provider.Limits.RPM != nil {
		t.Fatalf("rpm = %v, want \"not documented\" to survive as null rather than becoming a zero cap", provider.Limits.RPM)
	}
	if len(provider.Models) != 1 || provider.Models[0].ContextTokens != 131072 {
		t.Fatalf("models = %+v", provider.Models)
	}
	if !loaded.Settings.AutoSwitch && loaded.Settings.PreferredProviderID != "groq" {
		t.Fatalf("settings = %+v", loaded.Settings)
	}
	if !loaded.Seeded {
		t.Fatal("the seeded flag was lost, so the next boot would reinstall deleted templates")
	}
	// The masked view is what the API returns; the file is what holds the key.
	if masked := serviceproviderpool.MaskSecret(provider.APIKey); masked != "••••1234" {
		t.Fatalf("masked key = %q", masked)
	}
}

func TestTheRegistryIsPrivateBecauseItCanHoldAKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not meaningful on Windows")
	}
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if err := store.Save(context.Background(), serviceproviderpool.Registry{}); err != nil {
		t.Fatalf("Save() = %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, registryFileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("providers.json mode = %o, want 600", mode)
	}
}

func TestAnUnreadableRegistryIsReportedRatherThanGuessedAt(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, registryFileName), []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := store.Load(context.Background()); err == nil {
		t.Fatal("Load() = nil, want the parse failure reported so the caller can log it")
	}
}

/* ------------------------------------------------------------------ *
 * The usage ledger
 * ------------------------------------------------------------------ */

func TestTheLedgerAppendsPerMonthAndReadsBackInOrder(t *testing.T) {
	dir := t.TempDir()
	ledger, err := NewUsageLog(dir)
	if err != nil {
		t.Fatalf("NewUsageLog() = %v", err)
	}
	ctx := context.Background()

	august := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	september := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	records := []serviceproviderpool.UsageRecord{
		{At: august.UnixMilli(), Event: serviceproviderpool.EventRequest, ProviderID: "groq", OK: true, PromptTokens: 10},
		{At: august.Add(time.Hour).UnixMilli(), Event: serviceproviderpool.EventFailover, ProviderID: "groq", NextProviderID: "cerebras"},
		{At: september.UnixMilli(), Event: serviceproviderpool.EventRequest, ProviderID: "cerebras", OK: true, PromptTokens: 20},
	}
	for _, record := range records {
		if err := ledger.Append(ctx, record); err != nil {
			t.Fatalf("Append() = %v", err)
		}
	}

	// Each month is its own file, so a scan of August cannot see September.
	var seen []serviceproviderpool.UsageRecord
	if err := ledger.Scan(ctx, "2026-08", func(record serviceproviderpool.UsageRecord) bool {
		seen = append(seen, record)
		return true
	}); err != nil {
		t.Fatalf("Scan() = %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("scanned %d records for August, want the two written in it", len(seen))
	}
	if seen[0].Event != serviceproviderpool.EventRequest || seen[1].Event != serviceproviderpool.EventFailover {
		t.Fatalf("records came back out of order: %+v", seen)
	}
	if seen[1].NextProviderID != "cerebras" {
		t.Fatalf("the failover lost where it went: %+v", seen[1])
	}

	if _, err := os.Stat(filepath.Join(dir, usageDirName, "usage-2026-09.jsonl")); err != nil {
		t.Fatalf("September's file was not written: %v", err)
	}
}

func TestScanningAMonthThatNeverHappenedIsNotAnError(t *testing.T) {
	ledger, err := NewUsageLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewUsageLog() = %v", err)
	}
	visited := 0
	if err := ledger.Scan(context.Background(), "2019-01", func(serviceproviderpool.UsageRecord) bool {
		visited++
		return true
	}); err != nil {
		t.Fatalf("Scan() = %v, want a month with no file to read as a month in which nothing happened", err)
	}
	if visited != 0 {
		t.Fatalf("visited %d records", visited)
	}
}

func TestATruncatedLineDoesNotAbandonTheRestOfTheMonth(t *testing.T) {
	dir := t.TempDir()
	ledger, err := NewUsageLog(dir)
	if err != nil {
		t.Fatalf("NewUsageLog() = %v", err)
	}
	path := filepath.Join(dir, usageDirName, "usage-2026-08.jsonl")
	body := `{"at":1,"event":"request","providerId":"groq","ok":true}
{"at":2,"event":"reque
{"at":3,"event":"request","providerId":"cerebras","ok":true}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var seen []string
	if err := ledger.Scan(context.Background(), "2026-08", func(record serviceproviderpool.UsageRecord) bool {
		seen = append(seen, record.ProviderID)
		return true
	}); err != nil {
		t.Fatalf("Scan() = %v", err)
	}
	if len(seen) != 2 || seen[0] != "groq" || seen[1] != "cerebras" {
		t.Fatalf("scanned %v, want the readable lines on both sides of the torn one", seen)
	}
}

func TestScanStopsWhenTheVisitorSaysSo(t *testing.T) {
	dir := t.TempDir()
	ledger, err := NewUsageLog(dir)
	if err != nil {
		t.Fatalf("NewUsageLog() = %v", err)
	}
	ctx := context.Background()
	at := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	for index := 0; index < 5; index++ {
		if err := ledger.Append(ctx, serviceproviderpool.UsageRecord{
			At: at.UnixMilli(), Event: serviceproviderpool.EventRequest, ProviderID: "groq",
		}); err != nil {
			t.Fatalf("Append() = %v", err)
		}
	}

	visited := 0
	if err := ledger.Scan(ctx, "2026-08", func(serviceproviderpool.UsageRecord) bool {
		visited++
		return visited < 2
	}); err != nil {
		t.Fatalf("Scan() = %v", err)
	}
	if visited != 2 {
		t.Fatalf("visited %d records after asking to stop at 2", visited)
	}
}

func TestTheLedgerIsPrivateToo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not meaningful on Windows")
	}
	dir := t.TempDir()
	ledger, err := NewUsageLog(dir)
	if err != nil {
		t.Fatalf("NewUsageLog() = %v", err)
	}
	at := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	if err := ledger.Append(context.Background(), serviceproviderpool.UsageRecord{
		At: at.UnixMilli(), Event: serviceproviderpool.EventRequest, ProviderID: "groq",
	}); err != nil {
		t.Fatalf("Append() = %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, usageDirName, "usage-2026-08.jsonl"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("ledger mode = %o, want 600 — it names which providers this server leans on", mode)
	}
}
