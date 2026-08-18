package filetranscribe

// File-backed storage for the single global transcription configuration at
// <dataDir>/transcription.json, mode 0600 because the document holds a
// provider API key. Writes rename a temp file into place for atomic
// replacement, matching every other settings document under DATA_DIR.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
)

var _ servicetranscribe.Store = (*Store)(nil)

const fileName = "transcription.json"

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
func (s *Store) Load(ctx context.Context) (servicetranscribe.Config, error) {
	select {
	case <-ctx.Done():
		return servicetranscribe.Config{}, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return servicetranscribe.DefaultConfig(), nil
		}
		return servicetranscribe.Config{}, fmt.Errorf("read transcription settings: %w", err)
	}
	if len(raw) == 0 {
		return servicetranscribe.DefaultConfig(), nil
	}

	cfg := servicetranscribe.DefaultConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return servicetranscribe.Config{}, fmt.Errorf("parse transcription settings: %w", err)
	}
	return cfg.Normalize(), nil
}

func (s *Store) Save(ctx context.Context, cfg servicetranscribe.Config) error {
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
	tmp, err := os.CreateTemp(s.root, ".transcription-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp transcription settings: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp transcription settings: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg.Normalize()); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp transcription settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp transcription settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path()); err != nil {
		return fmt.Errorf("replace transcription settings: %w", err)
	}
	return nil
}
