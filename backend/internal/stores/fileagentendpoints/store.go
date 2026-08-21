// Package fileagentendpoints persists the third-party agent endpoint
// register as a single JSON array at DATA_DIR/agent-endpoints.json.
//
// The document is written mode 0600. It never holds an API key — a profile
// carries the *name* of a Secrets-vault entry and nothing else — but it does
// describe exactly which outside inference providers this fleet is wired to,
// and that is not world-readable information.
//
// A fresh install is seeded with the vendor templates, every one of them
// disabled and without a key reference. Seeding happens once: the file's
// existence is the marker, so an operator who deletes every template keeps an
// empty register instead of having them grow back on the next boot.
//
// Writes rename a temp file into place, so a crash mid-save leaves either the
// previous document or the new one, never a truncated mixture.
package fileagentendpoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	serviceendpoints "github.com/futrx-com/remote.futrx.com/internal/service/agentendpoints"
)

var _ serviceendpoints.Store = (*Store)(nil)

// FileName is the document's name inside DATA_DIR.
const FileName = "agent-endpoints.json"

type Store struct {
	path string
	mu   sync.Mutex
}

// New opens (and on first run seeds) the register at dataDir/agent-endpoints.json.
func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	store := &Store{path: filepath.Join(dataDir, FileName)}
	if err := store.seed(); err != nil {
		return nil, err
	}
	return store, nil
}

// Path is where this store keeps the register, for diagnostics.
func (s *Store) Path() string { return s.path }

// Load returns the stored profiles. A missing document is an empty register
// rather than an error: an operator may legitimately have deleted them all.
func (s *Store) Load(ctx context.Context) ([]serviceendpoints.Endpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agent endpoints: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var list []serviceendpoints.Endpoint
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse agent endpoints: %w", err)
	}
	return list, nil
}

func (s *Store) Save(ctx context.Context, endpoints []serviceendpoints.Endpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(endpoints)
}

// seed writes the disabled vendor templates the first time this store is
// opened. An existing file — including an empty register the operator emptied
// on purpose — is left exactly as it is.
func (s *Store) seed() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat agent endpoints: %w", err)
	}
	return s.writeLocked(serviceendpoints.Seed())
}

func (s *Store) writeLocked(endpoints []serviceendpoints.Endpoint) error {
	if endpoints == nil {
		endpoints = []serviceendpoints.Endpoint{}
	}
	data, err := json.MarshalIndent(endpoints, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agent endpoints: %w", err)
	}
	return writeAtomic(s.path, append(data, '\n'))
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".agent-endpoints-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp agent endpoints document: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp agent endpoints document: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp agent endpoints document: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp agent endpoints document: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace agent endpoints document: %w", err)
	}
	return nil
}
