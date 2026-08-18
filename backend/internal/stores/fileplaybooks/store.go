// Package fileplaybooks persists the playbook library as a single JSON array
// at DATA_DIR/playbooks.json. It follows the same temp-file + rename
// discipline as the other metadata stores, and reports "absent" rather than an
// error so the service can seed the library on first run.
package fileplaybooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	serviceplaybooks "github.com/futrx-com/remote.futrx.com/internal/service/playbooks"
)

var _ serviceplaybooks.Repository = (*Store)(nil)

// FileName is the library document's name inside DATA_DIR. Exported so the
// operations docs and any recovery tooling agree on one spelling.
const FileName = "playbooks.json"

type Store struct {
	path string
	mu   sync.Mutex
}

func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &Store{path: filepath.Join(dataDir, FileName)}, nil
}

// Load returns the stored library. The second result distinguishes "no
// document yet" (seed it) from "an operator emptied it on purpose" (leave it).
func (s *Store) Load(ctx context.Context) ([]serviceplaybooks.Playbook, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read playbooks: %w", err)
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	var list []serviceplaybooks.Playbook
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, false, fmt.Errorf("parse playbooks: %w", err)
	}
	if list == nil {
		list = []serviceplaybooks.Playbook{}
	}
	return list, true, nil
}

func (s *Store) Save(ctx context.Context, list []serviceplaybooks.Playbook) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if list == nil {
		list = []serviceplaybooks.Playbook{}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal playbooks: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "playbooks-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp playbooks: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp playbooks: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp playbooks: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp playbooks: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace playbooks: %w", err)
	}
	return nil
}
