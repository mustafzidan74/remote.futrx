package filevisualdiff

// File-backed storage for per-project visual comparison. One directory per
// project at <dataDir>/visual/<projectID>/, mode 0700, holding the PNGs plus
// an index.json with the baseline, the comparisons, and an updatedAt unix-ms
// stamp. Atomic write via temp+rename, one mutex per project.
//
// The images sit beside the index rather than inside it: a JSON document
// holding base64 page captures would be rewritten in full after every page of
// every run, and a twelve-page comparison writes thirteen times.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicevisualdiff "github.com/futrx-com/remote.futrx.com/internal/service/visualdiff"
)

var (
	_ servicevisualdiff.Repository = (*Store)(nil)
	_ servicevisualdiff.Blobs      = (*Store)(nil)
)

// indexName is the per-project record file. It can never collide with an
// image: image names are "<hex>-<kind>-<hex>.png".
const indexName = "index.json"

// safeFileName is the only shape a blob name may have. The service generates
// them, but this store is the last thing between a stored string and an
// os.Remove, so it re-checks rather than trusting the caller.
var safeFileName = regexp.MustCompile(`^[0-9a-f]{8,32}-(base|after|diff)-[0-9a-f]{16}\.png$`)

type indexFile struct {
	State     servicevisualdiff.State `json:"state"`
	UpdatedAt int64                   `json:"updatedAt"`
}

type Store struct {
	root string

	mu    sync.Mutex
	locks map[serviceproject.ID]*sync.Mutex
}

func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "visual")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create visual diff dir: %w", err)
	}
	_ = os.Chmod(dir, 0o700)
	return &Store{root: dir, locks: map[serviceproject.ID]*sync.Mutex{}}, nil
}

func (s *Store) Load(
	_ context.Context,
	projectID serviceproject.ID,
) (servicevisualdiff.State, error) {
	if !serviceproject.ValidID(projectID) {
		return servicevisualdiff.State{}, serviceproject.ErrInvalidID
	}
	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()
	return s.loadLocked(projectID)
}

func (s *Store) Update(
	_ context.Context,
	projectID serviceproject.ID,
	fn func(servicevisualdiff.State) (servicevisualdiff.State, error),
) (servicevisualdiff.State, error) {
	if fn == nil {
		return servicevisualdiff.State{}, errors.New("visual diff update function is required")
	}
	if !serviceproject.ValidID(projectID) {
		return servicevisualdiff.State{}, serviceproject.ErrInvalidID
	}
	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()

	current, err := s.loadLocked(projectID)
	if err != nil {
		return servicevisualdiff.State{}, err
	}
	next, err := fn(current)
	if err != nil {
		return servicevisualdiff.State{}, err
	}
	if err := s.saveLocked(projectID, next); err != nil {
		return servicevisualdiff.State{}, err
	}
	return next, nil
}

func (s *Store) Write(projectID serviceproject.ID, file string, data []byte) error {
	path, err := s.blobPath(projectID, file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create visual diff dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".visual-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s *Store) Read(projectID serviceproject.ID, file string) ([]byte, error) {
	path, err := s.blobPath(projectID, file)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *Store) Remove(projectID serviceproject.ID, file string) error {
	path, err := s.blobPath(projectID, file)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// blobPath resolves one image file, refusing anything that is not the
// generated shape. Nothing here ever joins caller-supplied path segments.
func (s *Store) blobPath(projectID serviceproject.ID, file string) (string, error) {
	if !serviceproject.ValidID(projectID) {
		return "", serviceproject.ErrInvalidID
	}
	if !safeFileName.MatchString(file) {
		return "", fmt.Errorf("invalid visual diff file name %q", file)
	}
	return filepath.Join(s.projectDir(projectID), file), nil
}

func (s *Store) projectDir(id serviceproject.ID) string {
	return filepath.Join(s.root, string(id))
}

func (s *Store) indexPath(id serviceproject.ID) string {
	return filepath.Join(s.projectDir(id), indexName)
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

func (s *Store) loadLocked(id serviceproject.ID) (servicevisualdiff.State, error) {
	raw, err := os.ReadFile(s.indexPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return servicevisualdiff.State{}, nil
		}
		return servicevisualdiff.State{}, err
	}
	if len(raw) == 0 {
		return servicevisualdiff.State{}, nil
	}
	var file indexFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return servicevisualdiff.State{}, fmt.Errorf("parse visual diff state for %s: %w", id, err)
	}
	return file.State, nil
}

func (s *Store) saveLocked(id serviceproject.ID, state servicevisualdiff.State) error {
	dir := s.projectDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create visual diff dir: %w", err)
	}
	payload, err := json.MarshalIndent(indexFile{
		State:     state,
		UpdatedAt: time.Now().UnixMilli(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode visual diff state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".index-*.tmp")
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
	return os.Rename(tmpName, s.indexPath(id))
}
