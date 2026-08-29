package filelighthouse

// File-backed storage for per-project Lighthouse audit history. One file per
// project at <dataDir>/lighthouse/<projectID>.json, mode 0600, holding the runs
// and an updatedAt unix-ms stamp. Atomic write via temp+rename, one mutex per
// project.
//
// There is no blob directory beside it, unlike screenshots. Lighthouse's own
// report is a few hundred kilobytes of HTML-bearing JSON that this platform
// deliberately does not keep — the parsed summary is a few kilobytes and fits
// in the index.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	servicelighthouse "github.com/futrx-com/remote.futrx.com/internal/service/lighthouse"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

var _ servicelighthouse.Repository = (*Store)(nil)

type indexFile struct {
	State     servicelighthouse.State `json:"state"`
	UpdatedAt int64                   `json:"updatedAt"`
}

type Store struct {
	root string

	mu    sync.Mutex
	locks map[serviceproject.ID]*sync.Mutex
}

func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "lighthouse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lighthouse dir: %w", err)
	}
	_ = os.Chmod(dir, 0o700)
	return &Store{root: dir, locks: map[serviceproject.ID]*sync.Mutex{}}, nil
}

func (s *Store) Load(
	_ context.Context,
	projectID serviceproject.ID,
) (servicelighthouse.State, error) {
	if !serviceproject.ValidID(projectID) {
		return servicelighthouse.State{}, serviceproject.ErrInvalidID
	}
	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()
	return s.loadLocked(projectID)
}

func (s *Store) Update(
	_ context.Context,
	projectID serviceproject.ID,
	fn func(servicelighthouse.State) (servicelighthouse.State, error),
) (servicelighthouse.State, error) {
	if fn == nil {
		return servicelighthouse.State{}, errors.New("lighthouse update function is required")
	}
	if !serviceproject.ValidID(projectID) {
		return servicelighthouse.State{}, serviceproject.ErrInvalidID
	}
	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()

	current, err := s.loadLocked(projectID)
	if err != nil {
		return servicelighthouse.State{}, err
	}
	next, err := fn(current)
	if err != nil {
		return servicelighthouse.State{}, err
	}
	if err := s.saveLocked(projectID, next); err != nil {
		return servicelighthouse.State{}, err
	}
	return next, nil
}

// path is built from an id this store validates itself. ValidID admits only
// lowercase hex, so nothing here ever joins a caller-supplied path segment.
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

func (s *Store) loadLocked(id serviceproject.ID) (servicelighthouse.State, error) {
	raw, err := os.ReadFile(s.path(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return servicelighthouse.State{}, nil
		}
		return servicelighthouse.State{}, err
	}
	if len(raw) == 0 {
		return servicelighthouse.State{}, nil
	}
	var file indexFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return servicelighthouse.State{}, fmt.Errorf("parse lighthouse history for %s: %w", id, err)
	}
	return file.State, nil
}

func (s *Store) saveLocked(id serviceproject.ID, state servicelighthouse.State) error {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fmt.Errorf("create lighthouse dir: %w", err)
	}
	payload, err := json.MarshalIndent(indexFile{
		State:     state,
		UpdatedAt: time.Now().UnixMilli(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lighthouse history: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, ".lighthouse-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path(id))
}
