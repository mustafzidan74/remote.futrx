package project

// Trash is the two-stage delete. Deleting a project used to destroy the
// container, the host workspace and the metadata in one irreversible step; an
// accidental click cost a WordPress install and its database with no way back.
//
// The replacement keeps everything for a grace period:
//
//	Delete  -> dump the database, destroy the container, move the host
//	           directories under the trash root, record deletedAt, and take an
//	           automatic snapshot of the trashed copy.
//	Restore -> move the directories back, clear deletedAt, re-create the
//	           container through the normal ensure path, re-import the dump.
//	Purge   -> remove the trashed directories, the snapshots, the membership,
//	           the secrets and the metadata. This one is final.
//
// A trashed project keeps its slug reserved so a new project cannot take the
// container name it will be restored under.

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// TrashRetention is how long a trashed project survives before the janitor
// purges it. Overridable per deployment with TRASH_RETENTION_DAYS.
const TrashRetention = 7 * 24 * time.Hour

// trashSweepInterval is how often the janitor looks for expired projects.
const trashSweepInterval = 6 * time.Hour

// SetSnapshots attaches the snapshot service. It is a setter rather than a
// constructor option because the snapshot service is built with this service
// as its project lookup: exactly one of the two can be complete first.
// Composition roots call this before serving any request.
func (s *Service) SetSnapshots(snapshots Snapshots) {
	s.snapshotsMu.Lock()
	defer s.snapshotsMu.Unlock()
	s.snapshots = snapshots
}

// WithStorage attaches the host adapter that moves a project's directories
// between the workspace root and the trash root. Without it a delete still
// records deletedAt and destroys the container, but the files stay where they
// are and Purge cannot reclaim them.
func WithStorage(storage ProjectStorage) Option {
	return func(s *Service) { s.storage = storage }
}

func (s *Service) snapshotService() Snapshots {
	s.snapshotsMu.RLock()
	defer s.snapshotsMu.RUnlock()
	return s.snapshots
}

// Delete moves a project to the trash. It is the handler-facing name for
// Trash, kept so every caller of the old destructive delete now gets the
// recoverable one.
func (s *Service) Delete(ctx context.Context, id ID) error {
	_, err := s.Trash(ctx, id, "")
	return err
}

// Trash performs the soft delete and returns the trashed metadata.
func (s *Service) Trash(ctx context.Context, id ID, actor string) (Meta, error) {
	m, err := s.trash(ctx, id, actor)
	s.record(ctx, audit.ActionProjectTrash, auditProjectTarget(id, m), audit.Meta{
		"slug":       m.Slug,
		"snapshotId": m.TrashSnapshotID,
	}, err)
	return m, err
}

func (s *Service) trash(ctx context.Context, id ID, actor string) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	unlock := s.lockRunState(id)
	defer unlock()
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	if m.Trashed() {
		return m, nil
	}

	// The database lives in the container rootfs, so the dump has to happen
	// while the container is still there. A template without a database
	// answers with an empty engine and that is not an error.
	database, engine := s.dumpDatabase(ctx, m)

	s.clearAgentBrowserState(id)
	s.forgetAgentBrowserActivity(id)
	if s.containerTemplates != nil && m.ContainerName != "" {
		s.containerTemplates.ForgetTemplateState(m.ContainerName)
	}
	if s.containerLifecycle != nil && m.ContainerName != "" {
		if err := s.containerLifecycle.Delete(ctx, m.ContainerName); err != nil {
			log.Printf("projects: delete container %s: %v", m.ContainerName, err)
		}
	}

	trashPath := ""
	if s.storage != nil && m.Cwd != "" {
		moved, err := s.storage.Trash(ctx, id, projectHostDir(m))
		if err != nil {
			return m, err
		}
		trashPath = moved
	}

	snapshotID := ""
	if snapshots := s.snapshotService(); snapshots != nil && trashPath != "" {
		captured, err := snapshots.CaptureTrash(ctx, id, trashPath, database, engine, actor)
		if err != nil {
			// Losing the safety-net snapshot must not strand the project
			// half-deleted: the files are already in the trash and can be
			// restored from there.
			log.Printf("projects: snapshot %s on delete: %v", id, err)
		}
		snapshotID = captured
	}

	deletedAt := time.Now().UnixMilli()
	return s.repo.Update(ctx, id, func(m *Meta) {
		m.DeletedAt = deletedAt
		m.DeletedBy = strings.ToLower(strings.TrimSpace(actor))
		m.TrashSnapshotID = snapshotID
		m.Status = StatusMissing
		m.ErrorMsg = ""
	})
}

// dumpDatabase asks the container for a logical dump. Every failure is
// best-effort: a stopped container, a template with no database, or a broken
// dump tool must not block a delete.
func (s *Service) dumpDatabase(ctx context.Context, m Meta) ([]byte, string) {
	if s.containerDatabase == nil || m.ContainerName == "" {
		return nil, ""
	}
	dump, engine, err := s.containerDatabase.Dump(ctx, m.ContainerName)
	if err != nil {
		log.Printf("projects: dump database in %s: %v", m.ContainerName, err)
		return nil, ""
	}
	return dump, engine
}

// ListTrashed returns the projects in the trash, newest first. Admins see
// every project; members see the ones they belong to.
func (s *Service) ListTrashed(ctx context.Context, callerEmail string, isAdmin bool) ([]Meta, error) {
	all, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	callerEmail = strings.ToLower(strings.TrimSpace(callerEmail))
	out := make([]Meta, 0, len(all))
	for _, m := range all {
		if !m.Trashed() {
			continue
		}
		if !isAdmin {
			if s.access == nil || callerEmail == "" {
				continue
			}
			member, err := s.access.Has(ctx, m.ID, callerEmail)
			if err != nil {
				log.Printf("projects: trash access check %s/%s: %v", m.ID, callerEmail, err)
				continue
			}
			if !member {
				continue
			}
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DeletedAt > out[j].DeletedAt })
	return out, nil
}

// RestoreFromTrash brings a trashed project back: the host directories move
// home, deletedAt is cleared, the container is re-created through the normal
// convergence path, and the database captured on the way out is re-imported.
func (s *Service) RestoreFromTrash(ctx context.Context, id ID) (Meta, error) {
	m, err := s.restoreFromTrash(ctx, id)
	s.record(ctx, audit.ActionProjectRestore, auditProjectTarget(id, m), audit.Meta{
		"slug": m.Slug,
	}, err)
	return m, err
}

func (s *Service) restoreFromTrash(ctx context.Context, id ID) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	unlock := s.lockRunState(id)
	defer unlock()
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	if !m.Trashed() {
		return m, ErrNotTrashed
	}
	snapshots := s.snapshotService()
	if snapshots != nil && snapshots.Busy(id) {
		// The automatic trash snapshot packs straight from the trash
		// directory; moving it away underneath tar would fail the archive.
		return m, ErrSnapshotBusy
	}

	if s.storage != nil && m.Cwd != "" {
		if err := s.storage.Untrash(ctx, id, projectHostDir(m)); err != nil {
			return m, err
		}
	}

	restored, err := s.repo.Update(ctx, id, func(m *Meta) {
		m.DeletedAt = 0
		m.DeletedBy = ""
		m.Status = StatusProvisioning
		m.ErrorMsg = ""
	})
	if err != nil {
		return Meta{}, err
	}

	if s.containerLifecycle != nil {
		if err := s.authorizeStart(ctx, restored, false); err != nil {
			return s.repo.SetStatus(ctx, id, StatusError, err.Error())
		}
		if err := s.containerLifecycle.Ensure(ctx, s.withEffectiveLimits(ctx, restored)); err != nil {
			return s.repo.SetStatus(ctx, id, StatusError, err.Error())
		}
		if syncErr := s.syncContainerEnv(ctx, id, restored.ContainerName); syncErr != nil {
			log.Printf("projects: sync env to %s after restore: %v", restored.ContainerName, syncErr)
		}
	}

	if snapshots != nil && restored.TrashSnapshotID != "" {
		if err := snapshots.RestoreDatabase(ctx, id, restored.TrashSnapshotID); err != nil {
			// The files are back and the container runs; a failed database
			// import is worth reporting but not worth failing the restore,
			// which cannot be retried once deletedAt is cleared.
			log.Printf("projects: re-import database for %s: %v", id, err)
		}
	}
	return s.repo.SetStatus(ctx, id, StatusRunning, "")
}

// Purge permanently removes a trashed project: the trashed directories, every
// snapshot, the membership list, the secrets and the metadata.
func (s *Service) Purge(ctx context.Context, id ID) error {
	m, err := s.purge(ctx, id)
	s.record(ctx, audit.ActionProjectPurge, auditProjectTarget(id, m), audit.Meta{
		"slug": m.Slug,
	}, err)
	return err
}

func (s *Service) purge(ctx context.Context, id ID) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, ErrInvalidID
	}
	unlock := s.lockRunState(id)
	defer unlock()
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	if !m.Trashed() {
		return m, ErrNotTrashed
	}
	return m, s.purgeLocked(ctx, m)
}

// purgeLocked is the irreversible half, shared by the endpoint and the
// janitor. The caller owns the project's run-state lock.
func (s *Service) purgeLocked(ctx context.Context, m Meta) error {
	// A delete that failed part-way may have left the container behind.
	if s.containerLifecycle != nil && m.ContainerName != "" {
		if err := s.containerLifecycle.Delete(ctx, m.ContainerName); err != nil {
			log.Printf("projects: delete container %s on purge: %v", m.ContainerName, err)
		}
	}
	if snapshots := s.snapshotService(); snapshots != nil {
		if err := snapshots.PurgeAll(ctx, m.ID); err != nil {
			log.Printf("projects: purge snapshots %s: %v", m.ID, err)
		}
	}
	if s.storage != nil {
		if err := s.storage.PurgeTrash(ctx, m.ID); err != nil {
			return err
		}
	}
	if s.secrets != nil {
		if err := s.secrets.DeleteAll(ctx, m.ID); err != nil {
			log.Printf("projects: delete secrets %s: %v", m.ID, err)
		}
	}
	if s.access != nil {
		if err := s.access.DeleteAll(ctx, m.ID); err != nil {
			log.Printf("projects: delete access %s: %v", m.ID, err)
		}
	}
	return s.repo.Delete(ctx, m.ID)
}

// PurgeExpiredTrash removes every trashed project older than retention and
// reports how many were purged.
func (s *Service) PurgeExpiredTrash(ctx context.Context, retention time.Duration) (int, error) {
	if retention <= 0 {
		return 0, nil
	}
	all, err := s.repo.List(ctx)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-retention).UnixMilli()
	purged := 0
	var failures []error
	for _, m := range all {
		if !m.Trashed() || m.DeletedAt > cutoff {
			continue
		}
		if snapshots := s.snapshotService(); snapshots != nil && snapshots.Busy(m.ID) {
			continue
		}
		unlock := s.lockRunState(m.ID)
		err := s.purgeLocked(ctx, m)
		unlock()
		if err != nil {
			failures = append(failures, err)
			continue
		}
		s.record(ctx, audit.ActionProjectPurge, auditProjectTarget(m.ID, m), audit.Meta{
			"slug": m.Slug, "reason": "retention",
		}, nil)
		purged++
	}
	return purged, errors.Join(failures...)
}

// StartTrashJanitor sweeps expired trash on an interval until ctx is done.
func (s *Service) StartTrashJanitor(ctx context.Context, retention time.Duration) {
	if retention <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(trashSweepInterval)
		defer ticker.Stop()
		for {
			if purged, err := s.PurgeExpiredTrash(ctx, retention); err != nil {
				log.Printf("projects: purge expired trash: %v", err)
			} else if purged > 0 {
				log.Printf("projects: purged %d expired trashed project(s)", purged)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// projectHostDir is the directory holding a project's durable trees. Cwd is
// ".../<slug>/workspace", so its parent is the project's host directory.
func projectHostDir(m Meta) string {
	if m.Cwd == "" {
		return ""
	}
	return filepath.Dir(filepath.Clean(m.Cwd))
}
