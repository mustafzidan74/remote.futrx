// Package filesnippets persists one snippet library per user under
// DATA_DIR/snippets, using the same temp-file + rename discipline as the other
// JSON stores.
package filesnippets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	servicesnippets "github.com/futrx-com/remote.futrx.com/internal/service/snippets"
)

var _ servicesnippets.Repository = (*Store)(nil)

// maxOwnerSlug bounds the readable half of a filename so a long email cannot
// push the path past what a filesystem accepts.
const maxOwnerSlug = 64

// Store is the file-backed snippet repository.
type Store struct {
	root string
	mu   sync.Mutex
}

// New creates DATA_DIR/snippets and returns a store rooted there.
func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "snippets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create snippets dir: %w", err)
	}
	return &Store{root: dir}, nil
}

// Dir reports where the libraries live, for documentation and backups.
func Dir(dataDir string) string { return filepath.Join(dataDir, "snippets") }

// Load reads one owner's document. The second return value distinguishes "no
// document yet" — which the service answers by seeding — from "an empty
// library", which is a decision the user made.
func (s *Store) Load(ctx context.Context, owner servicesnippets.Owner) ([]servicesnippets.Snippet, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	path, err := s.path(owner)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read snippets: %w", err)
	}
	var list []servicesnippets.Snippet
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, false, fmt.Errorf("parse snippets: %w", err)
	}
	if list == nil {
		list = []servicesnippets.Snippet{}
	}
	return list, true, nil
}

// Save replaces one owner's document atomically.
func (s *Store) Save(ctx context.Context, owner servicesnippets.Owner, list []servicesnippets.Snippet) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	path, err := s.path(owner)
	if err != nil {
		return err
	}
	if list == nil {
		list = []servicesnippets.Snippet{}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snippets: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return fmt.Errorf("create snippets dir: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, "snippets-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp snippets: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp snippets: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp snippets: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp snippets: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace snippets: %w", err)
	}
	return nil
}

// path maps an owner to a filename that is both readable and unambiguous: the
// identity in a filesystem-safe form, plus a short digest of the exact key so
// two owners that flatten to the same slug can never share a document.
func (s *Store) path(owner servicesnippets.Owner) (string, error) {
	value := strings.TrimSpace(string(owner))
	if value == "" {
		return "", servicesnippets.ErrInvalidOwner
	}
	sum := sha256.Sum256([]byte(value))
	name := ownerSlug(value) + "-" + hex.EncodeToString(sum[:4]) + ".json"
	return filepath.Join(s.root, name), nil
}

func ownerSlug(value string) string {
	var out strings.Builder
	previousDash := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			previousDash = false
			continue
		}
		if !previousDash && out.Len() > 0 {
			out.WriteRune('-')
			previousDash = true
		}
		if out.Len() >= maxOwnerSlug {
			break
		}
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" {
		return "owner"
	}
	return slug
}
