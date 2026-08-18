package fileportal

// File-backed storage for per-project client portals. One JSON file per
// project at <dataDir>/portals/<projectID>.json, mode 0600, written by
// temp-file + rename with one mutex per project.
//
// Only the SHA-256 digest of the portal token is stored, so this file cannot
// be turned back into a working client link.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	serviceportal "github.com/futrx-com/remote.futrx.com/internal/service/portal"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

var _ serviceportal.Repository = (*Store)(nil)

type Store struct {
	root string

	mu    sync.Mutex
	locks map[serviceproject.ID]*sync.Mutex
}

func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "portals")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create portals dir: %w", err)
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

// Get returns the stored record, or a zero record when the project has never
// had a portal. The service turns that zero value into its defaults.
func (s *Store) Get(
	ctx context.Context,
	projectID serviceproject.ID,
) (serviceportal.Portal, error) {
	select {
	case <-ctx.Done():
		return serviceportal.Portal{}, ctx.Err()
	default:
	}

	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()

	raw, err := os.ReadFile(s.path(projectID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return serviceportal.Portal{}, nil
		}
		return serviceportal.Portal{}, fmt.Errorf("read portal for %s: %w", projectID, err)
	}
	if len(raw) == 0 {
		return serviceportal.Portal{}, nil
	}
	var record serviceportal.Portal
	if err := json.Unmarshal(raw, &record); err != nil {
		return serviceportal.Portal{}, fmt.Errorf("parse portal for %s: %w", projectID, err)
	}
	return record, nil
}

func (s *Store) Save(
	ctx context.Context,
	projectID serviceproject.ID,
	record serviceportal.Portal,
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
		return fmt.Errorf("create portals dir: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, "."+string(projectID)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp portal: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp portal: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp portal: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp portal: %w", err)
	}
	if err := os.Rename(tmpName, s.path(projectID)); err != nil {
		return fmt.Errorf("replace portal for %s: %w", projectID, err)
	}
	return nil
}
