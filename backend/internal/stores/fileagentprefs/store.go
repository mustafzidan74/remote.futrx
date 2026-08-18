// Package fileagentprefs persists the platform-wide agent reply preferences as
// a single JSON document at DATA_DIR/agent-preferences.json. It follows the
// same temp-file + rename discipline as the other metadata stores, and reports
// "absent" rather than an error so the service can fall back to its defaults.
package fileagentprefs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	serviceagentprefs "github.com/futrx-com/remote.futrx.com/internal/service/agentprefs"
)

var _ serviceagentprefs.Repository = (*Store)(nil)

// FileName is the document's name inside DATA_DIR. Exported so the operations
// docs and any recovery tooling agree on one spelling.
const FileName = "agent-preferences.json"

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

// Load returns the stored preferences. The second result distinguishes "no
// document yet" from "an admin saved something", which is what lets the
// service tell a fresh install from a deliberately default one.
func (s *Store) Load(ctx context.Context) (serviceagentprefs.Preferences, bool, error) {
	select {
	case <-ctx.Done():
		return serviceagentprefs.Preferences{}, false, ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return serviceagentprefs.Preferences{}, false, nil
	}
	if err != nil {
		return serviceagentprefs.Preferences{}, false, fmt.Errorf("read agent preferences: %w", err)
	}
	if len(data) == 0 {
		return serviceagentprefs.Preferences{}, false, nil
	}

	var prefs serviceagentprefs.Preferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return serviceagentprefs.Preferences{}, false, fmt.Errorf("parse agent preferences: %w", err)
	}
	return prefs, true, nil
}

func (s *Store) Save(ctx context.Context, prefs serviceagentprefs.Preferences) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agent preferences: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "agent-preferences-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp agent preferences: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp agent preferences: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp agent preferences: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp agent preferences: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace agent preferences: %w", err)
	}
	return nil
}
