// Package hostarchive packs and unpacks project snapshot archives on the
// host, and parks a deleted project's directories in the trash.
//
// It shells out to the `tar` binary rather than streaming through
// archive/tar: a project workspace is an arbitrary tree with symlinks, hard
// links, sparse files and unix modes that the system archiver already handles
// correctly, and tar plus zstd is dramatically faster than anything in-process
// on a small host. Everything that touches a path below the archive root or
// the trash root lives here, so the snapshot and project services stay free of
// filesystem detail.
package hostarchive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
)

const (
	// FormatZstd is preferred: a workspace of source code and a SQL dump
	// compress well and zstd is several times faster than gzip.
	FormatZstd = "tar.zst"
	// FormatGzip is the fallback for a host without the zstd binary. GNU tar
	// implements --zstd by executing it, so its absence is disqualifying.
	FormatGzip = "tar.gz"

	// dirMode is root-only: an archive is a verbatim copy of a project's
	// files, and may hold its secrets when the operator asked for them.
	dirMode  = 0o700
	fileMode = 0o600
)

var _ servicesnapshot.Archive = (*Archiver)(nil)

// Archiver writes and reads snapshot archives under one root directory.
type Archiver struct {
	root string
	tar  string
	zstd bool
}

// NewArchiver returns an archiver rooted at dir. Tool discovery happens once:
// a host either has tar (and maybe zstd) or it does not.
func NewArchiver(root string) *Archiver {
	archiver := &Archiver{root: root}
	if path, err := exec.LookPath("tar"); err == nil {
		archiver.tar = path
	}
	if _, err := exec.LookPath("zstd"); err == nil {
		archiver.zstd = true
	}
	return archiver
}

// Available reports whether this host can pack an archive at all.
func (a *Archiver) Available() bool { return a != nil && a.tar != "" }

// Format is the archive format this host produces.
func (a *Archiver) Format() string {
	if a.zstd {
		return FormatZstd
	}
	return FormatGzip
}

// projectDir is where one project's archives live.
func (a *Archiver) projectDir(projectID string) string {
	return filepath.Join(a.root, projectID)
}

// Pack writes one archive: the requested entries of SourceDir, plus meta.json
// and (when supplied) db.sql staged next to it. The archive is written to a
// temporary name and renamed on success, so a listing never shows a partial
// file.
func (a *Archiver) Pack(
	ctx context.Context,
	req servicesnapshot.PackRequest,
) (servicesnapshot.PackResult, error) {
	if !a.Available() {
		return servicesnapshot.PackResult{}, errors.New("tar was not found on PATH - snapshots are unavailable")
	}
	if strings.TrimSpace(req.Name) == "" {
		return servicesnapshot.PackResult{}, errors.New("archive name is required")
	}

	dir := a.projectDir(req.ProjectID)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return servicesnapshot.PackResult{}, fmt.Errorf("create snapshot dir: %w", err)
	}
	_ = os.Chmod(dir, dirMode)

	staging := filepath.Join(dir, ".staging-"+req.Name)
	if err := os.MkdirAll(staging, dirMode); err != nil {
		return servicesnapshot.PackResult{}, fmt.Errorf("stage snapshot: %w", err)
	}
	defer os.RemoveAll(staging)

	staged := make([]string, 0, 2)
	if len(req.Manifest) > 0 {
		if err := os.WriteFile(
			filepath.Join(staging, servicesnapshot.ManifestName), req.Manifest, fileMode,
		); err != nil {
			return servicesnapshot.PackResult{}, fmt.Errorf("write manifest: %w", err)
		}
		staged = append(staged, servicesnapshot.ManifestName)
	}
	if len(req.Database) > 0 {
		if err := os.WriteFile(
			filepath.Join(staging, servicesnapshot.DatabaseName), req.Database, fileMode,
		); err != nil {
			return servicesnapshot.PackResult{}, fmt.Errorf("write database dump: %w", err)
		}
		staged = append(staged, servicesnapshot.DatabaseName)
	}

	format := a.Format()
	archive := req.Name + "." + format
	final := filepath.Join(dir, archive)
	tmp := final + ".partial"
	_ = os.Remove(tmp)

	args := PackArgs(
		filepath.Base(tmp), a.zstd,
		req.SourceDir, presentEntries(req.SourceDir, req.Entries),
		staging, staged,
	)
	if out, err := a.run(ctx, dir, args...); err != nil {
		_ = os.Remove(tmp)
		return servicesnapshot.PackResult{}, fmt.Errorf("pack snapshot: %w; output: %s", err, out)
	}
	if err := os.Chmod(tmp, fileMode); err != nil {
		_ = os.Remove(tmp)
		return servicesnapshot.PackResult{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return servicesnapshot.PackResult{}, err
	}
	info, err := os.Stat(final)
	if err != nil {
		return servicesnapshot.PackResult{}, err
	}
	return servicesnapshot.PackResult{
		Archive:   archive,
		Format:    format,
		SizeBytes: info.Size(),
	}, nil
}

// PackArgs builds the tar invocation. It is exported and pure so the argument
// order — which is what makes the two -C groups land in one archive — is
// testable without a filesystem.
//
// archiveName is relative and every tar call runs with the archive directory
// as its working directory. GNU tar reads "host:path" out of the -f argument,
// so an absolute path is only safe when it cannot look like one.
func PackArgs(
	archiveName string,
	zstd bool,
	sourceDir string,
	entries []string,
	stagingDir string,
	stagedFiles []string,
) []string {
	args := []string{"-c"}
	if zstd {
		args = append(args, "--zstd")
	} else {
		args = append(args, "-z")
	}
	args = append(args, "-f", archiveName)
	if sourceDir != "" && len(entries) > 0 {
		args = append(args, "-C", sourceDir)
		args = append(args, entries...)
	}
	if stagingDir != "" && len(stagedFiles) > 0 {
		args = append(args, "-C", stagingDir)
		args = append(args, stagedFiles...)
	}
	return args
}

// Restore swaps the project's durable directories for the archive's. The
// archive is expanded into a staging directory first, so a truncated or
// unreadable archive cannot leave the project with half a workspace.
func (a *Archiver) Restore(
	ctx context.Context,
	req servicesnapshot.RestoreRequest,
) (servicesnapshot.RestoreResult, error) {
	if !a.Available() {
		return servicesnapshot.RestoreResult{}, errors.New("tar was not found on PATH - snapshots are unavailable")
	}
	dir := a.projectDir(req.ProjectID)
	if _, err := os.Stat(filepath.Join(dir, req.Archive)); err != nil {
		return servicesnapshot.RestoreResult{}, err
	}

	staging := filepath.Join(dir, ".restore-"+req.Archive)
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, dirMode); err != nil {
		return servicesnapshot.RestoreResult{}, fmt.Errorf("stage restore: %w", err)
	}
	defer os.RemoveAll(staging)

	// Extraction runs from the staging directory itself rather than passing
	// -C: GNU tar resolves a -f that follows a -C against the new directory,
	// and the archive is one level up from staging.
	if out, err := a.run(ctx, staging, "-x", "-f", "../"+req.Archive); err != nil {
		return servicesnapshot.RestoreResult{}, fmt.Errorf("expand snapshot: %w; output: %s", err, out)
	}

	stash := filepath.Join(dir, req.StashName)
	if err := os.MkdirAll(stash, dirMode); err != nil {
		return servicesnapshot.RestoreResult{}, fmt.Errorf("create pre-restore stash: %w", err)
	}
	if err := os.MkdirAll(req.ProjectDir, 0o755); err != nil {
		return servicesnapshot.RestoreResult{}, err
	}
	for _, entry := range req.Entries {
		live := filepath.Join(req.ProjectDir, entry)
		if _, err := os.Lstat(live); err == nil {
			if err := moveTree(live, filepath.Join(stash, entry)); err != nil {
				return servicesnapshot.RestoreResult{}, fmt.Errorf("stash current %s: %w", entry, err)
			}
		}
		staged := filepath.Join(staging, entry)
		if _, err := os.Lstat(staged); err != nil {
			// The archive did not carry this directory. The stash keeps the
			// replaced copy either way.
			continue
		}
		if err := moveTree(staged, live); err != nil {
			return servicesnapshot.RestoreResult{}, fmt.Errorf("install restored %s: %w", entry, err)
		}
	}

	result := servicesnapshot.RestoreResult{StashPath: stash}
	dump, err := os.ReadFile(filepath.Join(staging, servicesnapshot.DatabaseName))
	if err == nil {
		result.Database = dump
	} else if !errors.Is(err, fs.ErrNotExist) {
		return result, err
	}
	return result, nil
}

// ReadDatabase streams one member out of an archive without expanding it.
// An archive without a database answers with no bytes and no error.
func (a *Archiver) ReadDatabase(ctx context.Context, projectID, archive string) ([]byte, error) {
	if !a.Available() {
		return nil, nil
	}
	dir := a.projectDir(projectID)
	if _, err := os.Stat(filepath.Join(dir, archive)); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, a.tar, "-x", "-O", "-f", archive, servicesnapshot.DatabaseName)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if notFoundInArchive(stderr.String()) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s from %s: %w; output: %s",
			servicesnapshot.DatabaseName, archive, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Remove deletes one archive.
func (a *Archiver) Remove(_ context.Context, projectID, archive string) error {
	if archive == "" {
		return nil
	}
	err := os.Remove(filepath.Join(a.projectDir(projectID), archive))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// RemoveProject deletes every archive of one project, plus any stash left
// behind by a restore.
func (a *Archiver) RemoveProject(_ context.Context, projectID string) error {
	if projectID == "" {
		return nil
	}
	return os.RemoveAll(a.projectDir(projectID))
}

// run invokes tar from dir. Every call sets a working directory so the -f
// argument can stay relative (see PackArgs).
func (a *Archiver) run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, a.tar, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// presentEntries drops the entries that do not exist. A project whose agent
// homes were never created must still be archivable.
func presentEntries(sourceDir string, entries []string) []string {
	if sourceDir == "" {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, err := os.Lstat(filepath.Join(sourceDir, entry)); err == nil {
			out = append(out, entry)
		}
	}
	return out
}

// notFoundInArchive recognizes both archivers' way of saying a member is
// absent, which for db.sql means "this template has no database".
func notFoundInArchive(output string) bool {
	lowered := strings.ToLower(output)
	return strings.Contains(lowered, "not found in archive") ||
		strings.Contains(lowered, "not found in the archive")
}
