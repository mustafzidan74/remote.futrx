// Package filemcp persists the MCP registry: the platform document as a
// single JSON array at DATA_DIR/mcpservers.json, and one document per project
// under DATA_DIR/projectmcp/<id>.json.
//
// Both are written mode 0600. Neither ever holds a credential — an entry
// carries ${KEY} placeholders and the vault keys behind them — but an entry
// does describe exactly which external systems a fleet can reach, and that is
// not world-readable information.
//
// Writes rename a temp file into place, so a crash mid-save leaves either the
// previous document or the new one, never a truncated mixture.
package filemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
)

var (
	_ servicemcp.Store        = (*Store)(nil)
	_ servicemcp.ProjectStore = (*ProjectStore)(nil)
)

const (
	// FileName is the platform document's name inside DATA_DIR.
	FileName = "mcpservers.json"
	// ProjectDirName holds one document per project.
	ProjectDirName = "projectmcp"
)

// projectIDPattern guards the path join: a project id reaches this store from
// a URL, and a traversal here would let a caller write anywhere under
// DATA_DIR.
var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// ErrInvalidProjectID rejects an id that cannot safely name a file.
var ErrInvalidProjectID = errors.New("invalid project id")

/* ------------------------------------------------------------------ *
 * Platform registry
 * ------------------------------------------------------------------ */

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

// Path is where this store keeps the registry, for diagnostics.
func (s *Store) Path() string { return s.path }

// Load returns the stored entries. A missing or empty document is an empty
// registry, not an error: a fresh install has simply never saved one.
func (s *Store) Load(ctx context.Context) ([]servicemcp.Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read MCP registry: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var list []servicemcp.Server
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse MCP registry: %w", err)
	}
	return list, nil
}

func (s *Store) Save(ctx context.Context, servers []servicemcp.Server) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if servers == nil {
		servers = []servicemcp.Server{}
	}
	data, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP registry: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return writeAtomic(s.path, append(data, '\n'))
}

/* ------------------------------------------------------------------ *
 * Per-project overrides
 * ------------------------------------------------------------------ */

type ProjectStore struct {
	dir string
	mu  sync.Mutex
}

func NewProjectStore(dataDir string) (*ProjectStore, error) {
	dir := filepath.Join(dataDir, ProjectDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create project MCP dir: %w", err)
	}
	return &ProjectStore{dir: dir}, nil
}

// Dir is where this store keeps its documents, for diagnostics.
func (s *ProjectStore) Dir() string { return s.dir }

// Load returns one project's overrides. A project that never saved any has an
// empty document, which is the same thing as "inherits everything in scope".
func (s *ProjectStore) Load(ctx context.Context, projectID string) (servicemcp.ProjectSettings, error) {
	if err := ctx.Err(); err != nil {
		return servicemcp.ProjectSettings{}, err
	}
	path, err := s.pathFor(projectID)
	if err != nil {
		return servicemcp.ProjectSettings{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return servicemcp.ProjectSettings{}, nil
	}
	if err != nil {
		return servicemcp.ProjectSettings{}, fmt.Errorf("read project MCP settings: %w", err)
	}
	if len(data) == 0 {
		return servicemcp.ProjectSettings{}, nil
	}
	var settings servicemcp.ProjectSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return servicemcp.ProjectSettings{}, fmt.Errorf("parse project MCP settings: %w", err)
	}
	return settings, nil
}

func (s *ProjectStore) Save(
	ctx context.Context,
	projectID string,
	settings servicemcp.ProjectSettings,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.pathFor(projectID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project MCP settings: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create project MCP dir: %w", err)
	}
	return writeAtomic(path, append(data, '\n'))
}

func (s *ProjectStore) pathFor(projectID string) (string, error) {
	if !projectIDPattern.MatchString(projectID) {
		return "", ErrInvalidProjectID
	}
	return filepath.Join(s.dir, projectID+".json"), nil
}

/* ------------------------------------------------------------------ *
 * Shared write
 * ------------------------------------------------------------------ */

// writeAtomic creates the temp file at 0600 before writing a byte, so the
// document never exists world-readable — not even for the microsecond between
// create and chmod.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".mcp-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp MCP document: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp MCP document: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp MCP document: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp MCP document: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace MCP document: %w", err)
	}
	return nil
}
