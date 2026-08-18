// Package fileglobalsecrets persists the platform secrets vault as a single
// JSON array at DATA_DIR/globalsecrets.json, mode 0600 because the document
// holds tokens, licence keys, and SSH private keys in plaintext — the same
// at-rest posture as the per-project secrets store next to it.
//
// Writes rename a temp file into place, so a crash mid-save leaves either the
// previous document or the new one, never a truncated mixture.
package fileglobalsecrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
)

var _ serviceglobalsecrets.Store = (*Store)(nil)

// FileName is the document's name inside DATA_DIR. Exported so the operations
// docs and any recovery tooling agree on one spelling.
const FileName = "globalsecrets.json"

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

// Path is where this store keeps the vault, for diagnostics.
func (s *Store) Path() string { return s.path }

// Load returns the stored entries. A missing or empty document is an empty
// vault, not an error: a fresh install has simply never saved one.
func (s *Store) Load(ctx context.Context) ([]serviceglobalsecrets.Secret, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read secrets vault: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var list []serviceglobalsecrets.Secret
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse secrets vault: %w", err)
	}
	return list, nil
}

func (s *Store) Save(ctx context.Context, secrets []serviceglobalsecrets.Secret) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if secrets == nil {
		secrets = []serviceglobalsecrets.Secret{}
	}
	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		// The error would carry the offending value, and the offending value
		// is a secret. Report the failure without it.
		return errors.New("encode secrets vault failed")
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".globalsecrets-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp secrets vault: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	// Mode before content: the file must never exist world-readable, not even
	// for the microsecond between create and chmod.
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp secrets vault: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp secrets vault: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp secrets vault: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace secrets vault: %w", err)
	}
	return nil
}
