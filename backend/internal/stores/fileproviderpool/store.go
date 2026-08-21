// Package fileproviderpool persists the free-tier provider pool.
//
// Two files with opposite write patterns, so two types:
//
//	DATA_DIR/providers.json                          (0600) the registry
//	DATA_DIR/providerpool/                           (0700)
//	DATA_DIR/providerpool/usage-2026-08.jsonl        (0600) one line per event
//
// The registry is 0600 because an entry may carry an inline API key. The
// ledger is 0600 too: it names which providers this server uses and how hard,
// which is operator business rather than world-readable.
package fileproviderpool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	serviceproviderpool "github.com/futrx-com/remote.futrx.com/internal/service/providerpool"
)

var (
	_ serviceproviderpool.Store    = (*Store)(nil)
	_ serviceproviderpool.UsageLog = (*UsageLog)(nil)
)

const (
	registryFileName = "providers.json"
	usageDirName     = "providerpool"
	dirMode          = 0o700
	fileMode         = 0o600
	// scanBufferMax bounds one JSONL line. Records are small, but a corrupt
	// file must not be able to exhaust memory.
	scanBufferMax = 1 << 20
)

/* ------------------------------------------------------------------ *
 * The registry document
 * ------------------------------------------------------------------ */

// Store is the file-backed registry at DATA_DIR/providers.json.
type Store struct {
	root string
	mu   sync.Mutex
}

func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, dirMode); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &Store{root: dataDir}, nil
}

func (s *Store) path() string { return filepath.Join(s.root, registryFileName) }

// Load returns the stored registry, or an empty one when nothing has been
// saved yet. An empty registry is what makes the service install its seeds.
func (s *Store) Load(ctx context.Context) (serviceproviderpool.Registry, error) {
	if err := ctx.Err(); err != nil {
		return serviceproviderpool.Registry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return serviceproviderpool.Registry{}, nil
		}
		return serviceproviderpool.Registry{}, fmt.Errorf("read the provider registry: %w", err)
	}
	if len(raw) == 0 {
		return serviceproviderpool.Registry{}, nil
	}

	var registry serviceproviderpool.Registry
	if err := json.Unmarshal(raw, &registry); err != nil {
		return serviceproviderpool.Registry{}, fmt.Errorf("parse the provider registry: %w", err)
	}
	return registry.Normalize(), nil
}

// Save replaces the document atomically: temp file, chmod, rename.
func (s *Store) Save(ctx context.Context, registry serviceproviderpool.Registry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, ".providers-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp provider registry: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp provider registry: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(registry.Normalize()); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp provider registry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp provider registry: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path()); err != nil {
		return fmt.Errorf("replace the provider registry: %w", err)
	}
	return nil
}

/* ------------------------------------------------------------------ *
 * The usage ledger
 * ------------------------------------------------------------------ */

// UsageLog is the append-only monthly ledger under DATA_DIR/providerpool.
type UsageLog struct {
	dir string
	mu  sync.Mutex
}

func NewUsageLog(dataDir string) (*UsageLog, error) {
	dir := filepath.Join(dataDir, usageDirName)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("create provider pool usage directory: %w", err)
	}
	return &UsageLog{dir: dir}, nil
}

func (l *UsageLog) monthPath(month string) string {
	return filepath.Join(l.dir, "usage-"+month+".jsonl")
}

// Append writes one line, opening the month's file if this is the first
// record of it.
func (l *UsageLog) Append(ctx context.Context, record serviceproviderpool.UsageRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, dirMode); err != nil {
		return fmt.Errorf("create provider pool usage directory: %w", err)
	}
	month := serviceproviderpool.MonthKey(recordTime(record))
	file, err := os.OpenFile(l.monthPath(month), os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(line)
	return err
}

// Scan visits one month's records oldest first. A month with no file is not
// an error — it is a month in which nothing happened.
func (l *UsageLog) Scan(
	ctx context.Context,
	month string,
	visit func(serviceproviderpool.UsageRecord) bool,
) error {
	if visit == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	file, err := os.Open(l.monthPath(month))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 8192), scanBufferMax)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var record serviceproviderpool.UsageRecord
		if err := json.Unmarshal(line, &record); err != nil {
			// One unreadable line is a truncated write, not a reason to
			// abandon the month's history.
			continue
		}
		if !visit(record) {
			return nil
		}
	}
	return scanner.Err()
}

// recordTime is the instant a record belongs to. A record with no timestamp
// lands in the current month rather than in 1970, which is the only reading
// that keeps a clock-less write out of a file nobody scans.
func recordTime(record serviceproviderpool.UsageRecord) time.Time {
	if record.At <= 0 {
		return time.Now()
	}
	return time.UnixMilli(record.At)
}
