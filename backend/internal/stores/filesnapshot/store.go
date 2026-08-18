package filesnapshot

// File-backed index of a project's snapshots: one JSON file per project at
// <dataDir>/snapshots/<projectID>.json, mode 0600, holding the records plus an
// updatedAt unix-ms stamp. Atomic write via temp+rename, one mutex per
// project.
//
// The archives themselves are far too large for DATA_DIR and live under
// ArchiveRoot on the host, next to the workspaces they were taken from. This
// file is only the index: which snapshots exist, how far each one got, and
// which archive file backs it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
)

// ArchiveRoot is where snapshot archives are written. It is root-only (0700):
// an archive holds a verbatim copy of a project's files and, when the operator
// asked for it, its secrets.
const ArchiveRoot = "/var/lib/remote/snapshots"

// TrashRoot is where a deleted project's directories are parked until they are
// restored or purged. Root-only for the same reason.
const TrashRoot = "/var/lib/remote/trash"

var _ servicesnapshot.Repository = (*Store)(nil)

type snapshotsFile struct {
	Snapshots []servicesnapshot.Snapshot `json:"snapshots"`
	UpdatedAt int64                      `json:"updatedAt"`
}

type Store struct {
	root string

	mu    sync.Mutex
	locks map[serviceproject.ID]*sync.Mutex
}

func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "snapshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create snapshots dir: %w", err)
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

func (s *Store) List(
	_ context.Context,
	projectID serviceproject.ID,
) ([]servicesnapshot.Snapshot, error) {
	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()
	return s.loadLocked(projectID)
}

func (s *Store) Update(
	_ context.Context,
	projectID serviceproject.ID,
	fn func([]servicesnapshot.Snapshot) ([]servicesnapshot.Snapshot, error),
) ([]servicesnapshot.Snapshot, error) {
	if fn == nil {
		return nil, errors.New("snapshot update function is required")
	}
	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()

	current, err := s.loadLocked(projectID)
	if err != nil {
		return nil, err
	}
	next, err := fn(current)
	if err != nil {
		return nil, err
	}
	if err := s.saveLocked(projectID, next); err != nil {
		return nil, err
	}
	return next, nil
}

func (s *Store) loadLocked(id serviceproject.ID) ([]servicesnapshot.Snapshot, error) {
	raw, err := os.ReadFile(s.path(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var file snapshotsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse snapshots for %s: %w", id, err)
	}
	return file.Snapshots, nil
}

func (s *Store) saveLocked(id serviceproject.ID, records []servicesnapshot.Snapshot) error {
	if len(records) == 0 {
		if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	out := snapshotsFile{Snapshots: records, UpdatedAt: time.Now().UnixMilli()}

	tmp, err := os.CreateTemp(s.root, "."+string(id)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path(id))
}
