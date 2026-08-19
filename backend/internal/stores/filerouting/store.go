// Package filerouting persists the automatic model routing policy as a single
// JSON document at DATA_DIR/model-routing.json. It follows the same temp-file
// + rename discipline as the other metadata stores, and reports "absent"
// rather than an error so the service can seed the shipped default on first
// run.
package filerouting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	servicerouting "github.com/futrx-com/remote.futrx.com/internal/service/routing"
)

var _ servicerouting.Repository = (*Store)(nil)

// FileName is the policy document's name inside DATA_DIR. Exported so the
// operations docs and any recovery tooling agree on one spelling.
const FileName = "model-routing.json"

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

func (s *Store) Load(ctx context.Context) (servicerouting.Policy, bool, error) {
	select {
	case <-ctx.Done():
		return servicerouting.Policy{}, false, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return servicerouting.Policy{}, false, nil
	}
	if err != nil {
		return servicerouting.Policy{}, false, fmt.Errorf("read model routing policy: %w", err)
	}
	var policy servicerouting.Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return servicerouting.Policy{}, false, fmt.Errorf("parse model routing policy: %w", err)
	}
	return policy, true, nil
}

func (s *Store) Save(ctx context.Context, policy servicerouting.Policy) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal model routing policy: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "model-routing-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp model routing policy: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp model routing policy: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp model routing policy: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp model routing policy: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace model routing policy: %w", err)
	}
	return nil
}
