package hostarchive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

var _ serviceproject.ProjectStorage = (*TrashStorage)(nil)

// TrashStorage parks a deleted project's host directory under the trash root
// and brings it back on request. A move is a rename when the workspace root
// and the trash root share a filesystem, which they do on every supported
// layout; the copy fallback exists so an operator who mounted one of them
// elsewhere still gets a working delete.
type TrashStorage struct {
	root string
}

// NewTrashStorage returns storage rooted at dir.
func NewTrashStorage(root string) *TrashStorage {
	return &TrashStorage{root: root}
}

func (s *TrashStorage) entry(id serviceproject.ID) string {
	return filepath.Join(s.root, string(id))
}

// Trash moves projectDir under the trash root and returns its new path.
func (s *TrashStorage) Trash(
	_ context.Context,
	id serviceproject.ID,
	projectDir string,
) (string, error) {
	if projectDir == "" {
		return "", errors.New("project has no host directory")
	}
	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return "", fmt.Errorf("create trash root: %w", err)
	}
	_ = os.Chmod(s.root, dirMode)

	target := s.entry(id)
	// A previous purge that failed part-way would otherwise silently swallow
	// this delete. Keep the leftovers, out of the way.
	if _, err := os.Lstat(target); err == nil {
		stale := target + ".stale-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.Rename(target, stale); err != nil {
			return "", fmt.Errorf("set aside a previous trash entry for %s: %w", id, err)
		}
	}
	if _, err := os.Lstat(projectDir); errors.Is(err, fs.ErrNotExist) {
		// Nothing on disk to move (a project whose container never launched).
		// The trash entry still exists so restore and purge behave uniformly.
		if err := os.MkdirAll(target, dirMode); err != nil {
			return "", err
		}
		return target, nil
	}
	if err := moveTree(projectDir, target); err != nil {
		return "", fmt.Errorf("move %s to the trash: %w", projectDir, err)
	}
	return target, nil
}

// Untrash moves the trashed copy back to projectDir.
func (s *TrashStorage) Untrash(
	_ context.Context,
	id serviceproject.ID,
	projectDir string,
) error {
	if projectDir == "" {
		return errors.New("project has no host directory")
	}
	source := s.entry(id)
	if _, err := os.Lstat(source); errors.Is(err, fs.ErrNotExist) {
		// Already gone: the container ensure path recreates the directories.
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(projectDir), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(projectDir); err == nil {
		return fmt.Errorf(
			"cannot restore %s: %s already exists on the host", id, projectDir,
		)
	}
	return moveTree(source, projectDir)
}

// PurgeTrash permanently removes the trashed copy.
func (s *TrashStorage) PurgeTrash(_ context.Context, id serviceproject.ID) error {
	return os.RemoveAll(s.entry(id))
}

// moveTree relocates a directory tree. Rename first; when the two paths sit on
// different filesystems, fall back to a copy that preserves symlinks and modes
// (a workspace holds skill links that must survive).
func moveTree(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return err
	}
	// Rename onto an existing empty directory succeeds on Linux and fails on
	// Windows; removing it first makes both behave the same. A non-empty
	// target keeps its contents and the rename below reports the conflict.
	_ = os.Remove(target)
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	if err := copyTree(source, target); err != nil {
		return err
	}
	return os.RemoveAll(source)
}

// copyTree duplicates a tree, preserving directory and file modes and
// recreating symlinks as symlinks rather than following them.
func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(destination, info.Mode().Perm())
		case entry.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, destination)
		case !info.Mode().IsRegular():
			// Sockets and devices carry no durable state worth copying.
			return nil
		default:
			return copyFile(path, destination, info.Mode().Perm())
		}
	})
}

func copyFile(source, target string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
