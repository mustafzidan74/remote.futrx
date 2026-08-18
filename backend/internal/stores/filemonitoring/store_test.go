package filemonitoring

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
)

func TestLoadReturnsDefaultsBeforeAnythingIsSaved(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cfg, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Enabled || cfg.HeartbeatURL != "" {
		t.Fatalf("config = %+v, want an empty default", cfg)
	}
	if cfg.IntervalMinutes != servicemonitoring.DefaultIntervalMinutes {
		t.Fatalf("interval = %d, want %d", cfg.IntervalMinutes, servicemonitoring.DefaultIntervalMinutes)
	}
}

func TestSaveRoundTripsAndKeepsTheDocumentPrivate(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := servicemonitoring.Config{
		Enabled:         true,
		HeartbeatURL:    "https://hc-ping.com/9f3a1c72-5b6d-4e21-9f0c-2b7ad4e51234",
		IntervalMinutes: 15,
		LastPingAt:      1755500000000,
		LastPingStatus:  servicemonitoring.PingOK,
	}

	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	// The heartbeat URL is a bearer credential; the file must be no more
	// readable than any other secret this platform stores.
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows")
	}
	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o, want 600", mode)
	}
}

func TestProbeAcceptsAWritableDataDirectory(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}

	// The probe must leave nothing behind: /healthz calls it on every hit.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("probe left %d files behind", len(entries))
	}
}

func TestProbeFailsWhenTheDataDirectoryIsGone(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if err := store.Probe(context.Background()); err == nil {
		t.Fatal("Probe accepted a data directory that no longer exists")
	}
}
