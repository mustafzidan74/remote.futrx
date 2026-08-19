package fileauxmodel

// File-backed storage for the single global auxiliary-model configuration at
// <dataDir>/aux-model.json, mode 0600 because the document can hold an API
// key for a hosted OpenAI-compatible endpoint. Writes rename a temp file into
// place for atomic replacement, exactly like every other settings store here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
)

var _ serviceauxmodel.Store = (*Store)(nil)

const fileName = "aux-model.json"

type Store struct {
	root string
	mu   sync.Mutex
}

func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &Store{root: dataDir}, nil
}

func (s *Store) path() string {
	return filepath.Join(s.root, fileName)
}

// Load returns the stored configuration, or defaults when nothing has been
// saved yet.
func (s *Store) Load(ctx context.Context) (serviceauxmodel.Config, error) {
	select {
	case <-ctx.Done():
		return serviceauxmodel.Config{}, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return serviceauxmodel.DefaultConfig(), nil
		}
		return serviceauxmodel.Config{}, fmt.Errorf("read auxiliary model settings: %w", err)
	}
	if len(raw) == 0 {
		return serviceauxmodel.DefaultConfig(), nil
	}

	cfg := serviceauxmodel.DefaultConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return serviceauxmodel.Config{}, fmt.Errorf("parse auxiliary model settings: %w", err)
	}
	return cfg.Normalize(), nil
}

func (s *Store) Save(ctx context.Context, cfg serviceauxmodel.Config) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, ".aux-model-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp auxiliary model settings: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp auxiliary model settings: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg.Normalize()); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp auxiliary model settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp auxiliary model settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path()); err != nil {
		return fmt.Errorf("replace auxiliary model settings: %w", err)
	}
	return nil
}
