package filegithub

// File-backed storage for the per-project GitHub automation settings. One
// JSON file per project at <dataDir>/github/<projectID>.json, mode 0600,
// written by temp-file + rename with one mutex per project — the same shape
// the portal and share indexes use.
//
// Unlike the portal store this one holds a *plaintext* shared secret rather
// than a digest, and it has to: HMAC verification recomputes the signature,
// which needs the secret itself. That is why the settings live here at 0600
// instead of on the project's meta.json, which is handed to every member.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

var _ servicegithub.Store = (*Store)(nil)

type Store struct {
	root string

	mu    sync.Mutex
	locks map[serviceproject.ID]*sync.Mutex
}

func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "github")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create github dir: %w", err)
	}
	_ = os.Chmod(dir, 0o700)
	return &Store{root: dir, locks: map[serviceproject.ID]*sync.Mutex{}}, nil
}

func (s *Store) path(id serviceproject.ID) string {
	return filepath.Join(s.root, string(id)+".json")
}

func (s *Store) lock(id serviceproject.ID) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.locks[id]; ok {
		return m
	}
	m := &sync.Mutex{}
	s.locks[id] = m
	return m
}

// Get returns the stored settings, or a zero value when the project has never
// had any. The service turns that zero value into its defaults.
func (s *Store) Get(
	ctx context.Context,
	projectID serviceproject.ID,
) (servicegithub.Settings, error) {
	select {
	case <-ctx.Done():
		return servicegithub.Settings{}, ctx.Err()
	default:
	}

	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()

	raw, err := os.ReadFile(s.path(projectID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return servicegithub.Settings{}, nil
		}
		return servicegithub.Settings{}, fmt.Errorf("read github settings for %s: %w", projectID, err)
	}
	if len(raw) == 0 {
		return servicegithub.Settings{}, nil
	}
	var record servicegithub.Settings
	if err := json.Unmarshal(raw, &record); err != nil {
		return servicegithub.Settings{}, fmt.Errorf("parse github settings for %s: %w", projectID, err)
	}
	return record, nil
}

func (s *Store) Save(
	ctx context.Context,
	projectID serviceproject.ID,
	record servicegithub.Settings,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fmt.Errorf("create github dir: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, "."+string(projectID)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp github settings: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp github settings: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp github settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp github settings: %w", err)
	}
	if err := os.Rename(tmpName, s.path(projectID)); err != nil {
		return fmt.Errorf("replace github settings for %s: %w", projectID, err)
	}
	return nil
}

// Delete removes a project's settings, secret and delivery log together. It is
// idempotent: unlinking a project that never had settings succeeds.
func (s *Store) Delete(ctx context.Context, projectID serviceproject.ID) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()

	if err := os.Remove(s.path(projectID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove github settings for %s: %w", projectID, err)
	}
	return nil
}
