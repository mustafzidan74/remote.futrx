package filesitewatch

// File-backed storage for the client-site watcher.
//
//	<dataDir>/sitewatch/sites.json          the catalog, replaced atomically
//	<dataDir>/sitewatch/history/<id>.jsonl  one append-only log per site
//
// The catalog can carry a shared token in a custom request header, so the
// whole tree is mode 0700/0600. History files are trimmed on every append,
// which keeps the log bounded without a background sweeper and without ever
// loading an unbounded file.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
)

var _ servicesitewatch.Store = (*Store)(nil)

const (
	dirName      = "sitewatch"
	historyDir   = "history"
	catalogFile  = "sites.json"
	scannerLimit = 1 << 20
)

type Store struct {
	root string
	mu   sync.Mutex
}

// New creates the sitewatch tree under dataDir.
func New(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, dirName)
	if err := os.MkdirAll(filepath.Join(root, historyDir), 0o700); err != nil {
		return nil, fmt.Errorf("create client site directory: %w", err)
	}
	return &Store{root: root}, nil
}

func (s *Store) catalogPath() string {
	return filepath.Join(s.root, catalogFile)
}

// document is the on-disk shape. The sites live under a key rather than at the
// top level so a later field (a global pause switch, a default interval) can
// be added without rewriting every file.
type document struct {
	Sites []servicesitewatch.Site `json:"sites"`
}

// Load returns the stored catalog, or an empty one when nothing is saved yet.
func (s *Store) Load(ctx context.Context) ([]servicesitewatch.Site, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.catalogPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []servicesitewatch.Site{}, nil
		}
		return nil, fmt.Errorf("read client sites: %w", err)
	}
	if len(raw) == 0 {
		return []servicesitewatch.Site{}, nil
	}
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse client sites: %w", err)
	}
	if doc.Sites == nil {
		doc.Sites = []servicesitewatch.Site{}
	}
	return doc.Sites, nil
}

func (s *Store) Save(ctx context.Context, sites []servicesitewatch.Site) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if sites == nil {
		sites = []servicesitewatch.Site{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fmt.Errorf("create client site directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, ".sites-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp client sites: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp client sites: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document{Sites: sites}); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp client sites: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp client sites: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.catalogPath()); err != nil {
		return fmt.Errorf("replace client sites: %w", err)
	}
	return nil
}

func (s *Store) LoadHistory(ctx context.Context, id servicesitewatch.ID) ([]servicesitewatch.Record, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	path, err := s.historyPath(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return readRecords(path)
}

// AppendHistory adds one check and trims the file back to the newest
// MaxHistoryRecords lines. Read-modify-write on a five-hundred-line file is
// cheaper than the bookkeeping a rotating appender would need, and it leaves
// the file bounded at every instant rather than between sweeps.
func (s *Store) AppendHistory(
	ctx context.Context,
	id servicesitewatch.ID,
	record servicesitewatch.Record,
) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	path, err := s.historyPath(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := readRecords(path)
	if err != nil {
		return err
	}
	existing = append(existing, record)
	if len(existing) > servicesitewatch.MaxHistoryRecords {
		existing = existing[len(existing)-servicesitewatch.MaxHistoryRecords:]
	}
	return writeRecords(path, existing)
}

func (s *Store) DeleteHistory(ctx context.Context, id servicesitewatch.ID) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	path, err := s.historyPath(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete client site history: %w", err)
	}
	return nil
}

// historyPath rejects anything that is not a well-formed site id, so a caller
// can never steer this store outside its own directory.
func (s *Store) historyPath(id servicesitewatch.ID) (string, error) {
	if !servicesitewatch.ValidID(id) {
		return "", servicesitewatch.ErrNotFound
	}
	dir := filepath.Join(s.root, historyDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create client site history directory: %w", err)
	}
	return filepath.Join(dir, string(id)+".jsonl"), nil
}

func readRecords(path string) ([]servicesitewatch.Record, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []servicesitewatch.Record{}, nil
		}
		return nil, fmt.Errorf("read client site history: %w", err)
	}
	defer file.Close()

	records := make([]servicesitewatch.Record, 0, servicesitewatch.MaxHistoryRecords)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 8<<10), scannerLimit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record servicesitewatch.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			// A truncated tail (a crash mid-append) must not make the whole
			// history unreadable: skip the damaged line and keep going.
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read client site history: %w", err)
	}
	return records, nil
}

func writeRecords(path string, records []servicesitewatch.Record) error {
	var buffer strings.Builder
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("encode client site check: %w", err)
		}
		buffer.Write(encoded)
		buffer.WriteByte('\n')
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(buffer.String()), 0o600); err != nil {
		return fmt.Errorf("write client site history: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace client site history: %w", err)
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
