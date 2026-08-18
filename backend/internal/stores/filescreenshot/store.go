package filescreenshot

// File-backed storage for per-project preview screenshots. One directory per
// project at <dataDir>/screenshots/<projectID>/, mode 0700, holding the PNGs
// plus an index.json with the records and an updatedAt unix-ms stamp. Atomic
// write via temp+rename, one mutex per project.
//
// Only the SHA-256 digest of a public link's token is stored, so a copy of
// DATA_DIR cannot be turned back into a working login-less link.

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
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
)

var (
	_ servicescreenshot.Repository = (*Store)(nil)
	_ servicescreenshot.Blobs      = (*Store)(nil)
)

// indexName is the per-project record file. It can never collide with a
// capture: capture file names are "<digits>-<hex>.png".
const indexName = "index.json"

// safeFileName is the only shape a blob name may have. The service generates
// them, but this store is the last thing between a stored string and an
// os.Remove, so it re-checks rather than trusting the caller.
var safeFileName = regexp.MustCompile(`^[0-9]+-[0-9a-f]{8,32}\.png$`)

type indexFile struct {
	Screenshots []servicescreenshot.Screenshot `json:"screenshots"`
	UpdatedAt   int64                          `json:"updatedAt"`
}

type Store struct {
	root string

	mu    sync.Mutex
	locks map[serviceproject.ID]*sync.Mutex
}

func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "screenshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create screenshots dir: %w", err)
	}
	_ = os.Chmod(dir, 0o700)
	return &Store{root: dir, locks: map[serviceproject.ID]*sync.Mutex{}}, nil
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

func (s *Store) List(
	_ context.Context,
	projectID serviceproject.ID,
) ([]servicescreenshot.Screenshot, error) {
	if !serviceproject.ValidID(projectID) {
		return nil, serviceproject.ErrInvalidID
	}
	mu := s.lock(projectID)
	mu.Lock()
	defer mu.Unlock()
	return s.loadLocked(projectID)
}

func (s *Store) Update(
	_ context.Context,
	projectID serviceproject.ID,
	fn func([]servicescreenshot.Screenshot) ([]servicescreenshot.Screenshot, error),
) ([]servicescreenshot.Screenshot, error) {
	if fn == nil {
		return nil, errors.New("screenshot update function is required")
	}
	if !serviceproject.ValidID(projectID) {
		return nil, serviceproject.ErrInvalidID
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

// Write stores one PNG. The bytes land beside the index rather than inside it:
// a JSON document holding base64 images would be rewritten in full on every
// capture and read in full on every list.
func (s *Store) Write(projectID serviceproject.ID, file string, data []byte) error {
	path, err := s.blobPath(projectID, file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create screenshot dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".shot-*.tmp")
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

// blobPath resolves one capture file, refusing anything that is not the
// generated shape. Nothing here ever joins caller-supplied path segments.
func (s *Store) blobPath(projectID serviceproject.ID, file string) (string, error) {
	if !serviceproject.ValidID(projectID) {
		return "", serviceproject.ErrInvalidID
	}
	if !safeFileName.MatchString(file) {
		return "", fmt.Errorf("invalid screenshot file name %q", file)
	}
	return filepath.Join(s.projectDir(projectID), file), nil
}

func (s *Store) loadLocked(id serviceproject.ID) ([]servicescreenshot.Screenshot, error) {
	raw, err := os.ReadFile(s.indexPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var file indexFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse screenshots for %s: %w", id, err)
	}
	return file.Screenshots, nil
}

func (s *Store) saveLocked(id serviceproject.ID, records []servicescreenshot.Screenshot) error {
	dir := s.projectDir(id)
	if len(records) == 0 {
		if err := os.Remove(s.indexPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// Best effort: an empty project directory is tidied away, a
		// non-empty one (a blob whose unlink failed) is left alone.
		_ = os.Remove(dir)
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create screenshot dir: %w", err)
	}
	out := indexFile{Screenshots: records, UpdatedAt: time.Now().UnixMilli()}

	tmp, err := os.CreateTemp(dir, "."+indexName+"-*.tmp")
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
	return os.Rename(tmpName, s.indexPath(id))
}
