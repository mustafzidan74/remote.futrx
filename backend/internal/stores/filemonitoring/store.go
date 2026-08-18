package filemonitoring

// File-backed storage for the single global monitoring configuration at
// <dataDir>/monitoring.json, mode 0600 because the document holds a heartbeat
// URL — a bearer credential for the external monitoring service. Writes
// rename a temp file into place for atomic replacement.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
)

var _ servicemonitoring.Store = (*Store)(nil)

const fileName = "monitoring.json"

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
func (s *Store) Load(ctx context.Context) (servicemonitoring.Config, error) {
	select {
	case <-ctx.Done():
		return servicemonitoring.Config{}, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return servicemonitoring.DefaultConfig(), nil
		}
		return servicemonitoring.Config{}, fmt.Errorf("read monitoring settings: %w", err)
	}
	if len(raw) == 0 {
		return servicemonitoring.DefaultConfig(), nil
	}

	cfg := servicemonitoring.DefaultConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return servicemonitoring.Config{}, fmt.Errorf("parse monitoring settings: %w", err)
	}
	return cfg.Normalize(), nil
}

func (s *Store) Save(ctx context.Context, cfg servicemonitoring.Config) error {
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
	tmp, err := os.CreateTemp(s.root, ".monitoring-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp monitoring settings: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp monitoring settings: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg.Normalize()); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp monitoring settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp monitoring settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path()); err != nil {
		return fmt.Errorf("replace monitoring settings: %w", err)
	}
	return nil
}

// Probe reports whether DATA_DIR is still a directory this process can write.
// Every file-backed store on the box lives there, so one temp file answers
// for all of them — a full disk, a read-only remount, or a vanished mount all
// surface here before a user discovers them by losing a save.
func (s *Store) Probe(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	info, err := os.Stat(s.root)
	if err != nil {
		return fmt.Errorf("stat data dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data dir %q is not a directory", s.root)
	}
	tmp, err := os.CreateTemp(s.root, ".healthz-*.tmp")
	if err != nil {
		return fmt.Errorf("data dir is not writable: %w", err)
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close probe file: %w", err)
	}
	return os.Remove(name)
}
