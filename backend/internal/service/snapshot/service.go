package snapshot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// maxJobsPerProject bounds the in-memory job history the status endpoint
// serves. Only the newest matter; the records themselves are the durable
// history.
const maxJobsPerProject = 10

var _ serviceproject.Snapshots = (*Service)(nil)

// Service is the policy layer over snapshots: it decides what goes into an
// archive, runs the slow parts in the background, enforces retention, and
// sequences a restore against the container lifecycle.
type Service struct {
	repo     Repository
	archive  Archive
	projects Projects
	database Database
	secrets  Secrets
	preparer Preparer
	audit    audit.Recorder
	now      func() time.Time

	mu   sync.Mutex
	jobs map[serviceproject.ID][]Job
	busy map[serviceproject.ID]int
	// wait counts every background goroutine so tests can join them.
	wait sync.WaitGroup
}

// Option customizes a Service at construction.
type Option func(*Service)

// WithAudit attaches the audit recorder.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithClock replaces the wall clock so timestamps are testable.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithDatabase attaches the in-container dump/import capability. Without it a
// snapshot carries files only.
func WithDatabase(database Database) Option {
	return func(s *Service) { s.database = database }
}

// WithSecrets enables the opt-in secret manifest.
func WithSecrets(secrets Secrets) Option {
	return func(s *Service) { s.secrets = secrets }
}

// WithPreparer attaches the idmap remapper applied to a restored tree.
func WithPreparer(preparer Preparer) Option {
	return func(s *Service) { s.preparer = preparer }
}

func New(repo Repository, archive Archive, projects Projects, options ...Option) *Service {
	service := &Service{
		repo:     repo,
		archive:  archive,
		projects: projects,
		audit:    audit.Nop{},
		now:      time.Now,
		jobs:     make(map[serviceproject.ID][]Job),
		busy:     make(map[serviceproject.ID]int),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Available reports whether snapshots can be taken on this host.
func (s *Service) Available() bool {
	return s != nil && s.repo != nil && s.archive != nil && s.archive.Available()
}

// List returns one project's snapshots, newest first.
func (s *Service) List(ctx context.Context, projectID serviceproject.ID) ([]Snapshot, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return nil, serviceproject.ErrInvalidID
	}
	records, err := s.repo.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	sortNewestFirst(records)
	return records, nil
}

// Jobs returns the recent background operations for one project, newest
// first.
func (s *Service) Jobs(projectID serviceproject.ID) []Job {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := s.jobs[projectID]
	out := make([]Job, len(jobs))
	copy(out, jobs)
	return out
}

// Busy reports whether a capture or restore is running for one project. The
// project service consults it before moving a trashed project's files back:
// the automatic trash snapshot packs from the trash directory, and pulling
// that directory out from under tar would fail the archive.
func (s *Service) Busy(projectID serviceproject.ID) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy[projectID] > 0
}

// Create records a snapshot and packs it in the background. The record is
// returned immediately with status "pending" so the caller can poll it.
func (s *Service) Create(
	ctx context.Context,
	projectID serviceproject.ID,
	in CreateInput,
	actor string,
) (Snapshot, error) {
	record, err := s.create(ctx, projectID, in, actor)
	s.record(ctx, audit.ActionSnapshotCreate, snapshotTarget(projectID, record), audit.Meta{
		"snapshotId":     string(record.ID),
		"label":          record.Label,
		"includeSecrets": in.IncludeSecrets,
	}, err)
	return record, err
}

func (s *Service) create(
	ctx context.Context,
	projectID serviceproject.ID,
	in CreateInput,
	actor string,
) (Snapshot, error) {
	if !s.Available() {
		return Snapshot{}, ErrUnavailable
	}
	if !serviceproject.ValidID(projectID) {
		return Snapshot{}, serviceproject.ErrInvalidID
	}
	label := strings.TrimSpace(in.Label)
	if len(label) > MaxLabelLength {
		return Snapshot{}, ErrLabelTooLong
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return Snapshot{}, err
	}
	if !s.acquire(projectID) {
		return Snapshot{}, ErrBusy
	}

	record := Snapshot{
		ID:              newID(),
		Label:           label,
		Kind:            KindManual,
		Status:          StatusPending,
		CreatedBy:       strings.ToLower(strings.TrimSpace(actor)),
		CreatedAt:       s.now().UnixMilli(),
		IncludesSecrets: in.IncludeSecrets,
		Slug:            meta.Slug,
		Template:        meta.TemplateName(),
	}
	if err := s.append(ctx, projectID, record); err != nil {
		s.release(projectID)
		return Snapshot{}, err
	}

	job := s.beginJob(projectID, JobCapture, record.ID)
	// Packing deliberately outlives the request that triggered it: tar of a
	// multi-gigabyte workspace is minutes of work.
	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		defer s.release(projectID)
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), captureTimeout)
		defer cancel()
		err := s.pack(runCtx, meta, record, packOptions{
			sourceDir:      projectHostDir(meta),
			includeSecrets: in.IncludeSecrets,
			dumpDatabase:   true,
		})
		s.finishJob(projectID, job, err)
	}()
	return record, nil
}

// CaptureTrash is the project service's safety net: it records the automatic
// snapshot taken when a project is moved to the trash. The database dump is
// supplied by the caller because it can only be taken while the container
// still exists, and sourceDir is the trash directory, because the project's
// files have already been moved there by the time this runs.
func (s *Service) CaptureTrash(
	ctx context.Context,
	projectID serviceproject.ID,
	sourceDir string,
	database []byte,
	engine, actor string,
) (string, error) {
	if !s.Available() {
		return "", ErrUnavailable
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return "", err
	}
	if !s.acquire(projectID) {
		return "", ErrBusy
	}
	record := Snapshot{
		ID:             newID(),
		Label:          "Before delete",
		Kind:           KindTrash,
		Status:         StatusPending,
		CreatedBy:      strings.ToLower(strings.TrimSpace(actor)),
		CreatedAt:      s.now().UnixMilli(),
		Slug:           meta.Slug,
		Template:       meta.TemplateName(),
		HasDatabase:    len(database) > 0,
		DatabaseEngine: engine,
	}
	if err := s.append(ctx, projectID, record); err != nil {
		s.release(projectID)
		return "", err
	}

	job := s.beginJob(projectID, JobCapture, record.ID)
	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		defer s.release(projectID)
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), captureTimeout)
		defer cancel()
		err := s.pack(runCtx, meta, record, packOptions{
			sourceDir: sourceDir,
			database:  database,
			engine:    engine,
		})
		s.finishJob(projectID, job, err)
	}()
	return string(record.ID), nil
}

type packOptions struct {
	sourceDir      string
	includeSecrets bool
	// dumpDatabase asks for a fresh dump from the running container. The trash
	// path instead supplies database/engine directly.
	dumpDatabase bool
	database     []byte
	engine       string
}

// pack runs one capture to completion and writes the outcome onto the record.
func (s *Service) pack(
	ctx context.Context,
	meta serviceproject.Meta,
	record Snapshot,
	options packOptions,
) error {
	if err := s.setStatus(ctx, meta.ID, record.ID, func(r *Snapshot) {
		r.Status = StatusRunning
	}); err != nil {
		return err
	}

	database, engine := options.database, options.engine
	if options.dumpDatabase && s.database != nil && meta.ContainerName != "" {
		dumpCtx, cancel := context.WithTimeout(ctx, databaseTimeout)
		dumped, dumpedEngine, err := s.database.Dump(dumpCtx, meta.ContainerName)
		cancel()
		if err != nil {
			// A container that is stopped or holds no dump tool is not a
			// failed snapshot: the files are still worth archiving.
			log.Printf("snapshots: dump database for %s: %v", meta.ContainerName, err)
		} else {
			database, engine = dumped, dumpedEngine
		}
	}

	manifest, err := s.manifest(ctx, meta, record, options, engine, len(database) > 0)
	if err != nil {
		return s.fail(ctx, meta.ID, record.ID, err)
	}

	result, err := s.archive.Pack(ctx, PackRequest{
		ProjectID: string(meta.ID),
		Name:      archiveName(time.UnixMilli(record.CreatedAt).UTC(), record.ID),
		SourceDir: options.sourceDir,
		Entries:   []string{WorkspaceEntry, AgentHomeEntry},
		Manifest:  manifest,
		Database:  database,
	})
	if err != nil {
		return s.fail(ctx, meta.ID, record.ID, err)
	}

	finishedAt := s.now().UnixMilli()
	if err := s.setStatus(ctx, meta.ID, record.ID, func(r *Snapshot) {
		r.Status = StatusReady
		r.Error = ""
		r.FinishedAt = finishedAt
		r.Archive = result.Archive
		r.Format = result.Format
		r.SizeBytes = result.SizeBytes
		r.HasDatabase = len(database) > 0
		r.DatabaseEngine = engine
	}); err != nil {
		return err
	}
	s.enforceRetention(ctx, meta.ID)
	return nil
}

// manifest renders meta.json. Secret values are included only when the caller
// asked for them.
func (s *Service) manifest(
	ctx context.Context,
	meta serviceproject.Meta,
	record Snapshot,
	options packOptions,
	engine string,
	hasDatabase bool,
) ([]byte, error) {
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		SnapshotID:    string(record.ID),
		ProjectID:     string(meta.ID),
		ProjectName:   meta.Name,
		Slug:          meta.Slug,
		ContainerName: meta.ContainerName,
		Template:      meta.TemplateName(),
		Label:         record.Label,
		Kind:          string(record.Kind),
		CreatedBy:     record.CreatedBy,
		CreatedAt:     record.CreatedAt,
		Directories:   []string{WorkspaceEntry, AgentHomeEntry},
	}
	if hasDatabase {
		manifest.Database = engine
	}
	if options.includeSecrets && s.secrets != nil {
		secrets, err := s.secrets.List(ctx, meta.ID)
		if err != nil {
			return nil, fmt.Errorf("read project secrets: %w", err)
		}
		if len(secrets) > 0 {
			manifest.Secrets = make(map[string]string, len(secrets))
			for _, secret := range secrets {
				manifest.Secrets[secret.Key] = secret.Value
			}
		}
	}
	return json.MarshalIndent(manifest, "", "  ")
}

// Restore replaces the project's durable directories with a snapshot's. The
// swap runs in the background because it stops the container, expands an
// archive, and starts it again.
func (s *Service) Restore(
	ctx context.Context,
	projectID serviceproject.ID,
	snapshotID ID,
	confirmed bool,
	actor string,
) (Job, error) {
	job, err := s.restore(ctx, projectID, snapshotID, confirmed)
	s.record(ctx, audit.ActionSnapshotRestore, audit.Target{
		Type: audit.TargetSnapshot, ID: string(snapshotID),
	}, audit.Meta{"projectId": string(projectID), "actor": actor}, err)
	return job, err
}

func (s *Service) restore(
	ctx context.Context,
	projectID serviceproject.ID,
	snapshotID ID,
	confirmed bool,
) (Job, error) {
	if !s.Available() {
		return Job{}, ErrUnavailable
	}
	if !confirmed {
		return Job{}, ErrConfirmRequired
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return Job{}, err
	}
	record, err := s.find(ctx, projectID, snapshotID)
	if err != nil {
		return Job{}, err
	}
	if record.Status != StatusReady || record.Archive == "" {
		return Job{}, ErrNotReady
	}
	if !s.acquire(projectID) {
		return Job{}, ErrBusy
	}

	job := s.beginJob(projectID, JobRestore, record.ID)
	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		defer s.release(projectID)
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), restoreTimeout)
		defer cancel()
		s.finishJob(projectID, job, s.runRestore(runCtx, meta, record))
	}()
	return job, nil
}

// runRestore is the restore state machine: stop, swap, remap, start, import.
// The container is stopped first so nothing writes into /workspace while the
// directory underneath it is being replaced.
func (s *Service) runRestore(ctx context.Context, meta serviceproject.Meta, record Snapshot) error {
	if _, err := s.projects.Stop(ctx, meta.ID); err != nil {
		log.Printf("snapshots: stop %s before restore: %v", meta.ID, err)
	}

	result, err := s.archive.Restore(ctx, RestoreRequest{
		ProjectID:  string(meta.ID),
		Archive:    record.Archive,
		ProjectDir: projectHostDir(meta),
		StashName:  stashName(s.now().UTC()),
		Entries:    []string{WorkspaceEntry, AgentHomeEntry},
	})
	if err != nil {
		// Bring the project back up on the tree that is on disk, whichever
		// one that is, rather than leaving it stopped.
		if _, startErr := s.projects.Start(ctx, meta.ID); startErr != nil {
			log.Printf("snapshots: restart %s after failed restore: %v", meta.ID, startErr)
		}
		return err
	}

	if s.preparer != nil {
		for _, entry := range []string{WorkspaceEntry, AgentHomeEntry} {
			path := filepath.Join(projectHostDir(meta), entry)
			if err := s.preparer.Prepare(path); err != nil {
				return fmt.Errorf("remap restored %s into the container idmap: %w", entry, err)
			}
		}
	}

	if _, err := s.projects.Start(ctx, meta.ID); err != nil {
		return fmt.Errorf("start container after restore: %w", err)
	}

	if len(result.Database) > 0 && s.database != nil && meta.ContainerName != "" {
		importCtx, cancel := context.WithTimeout(ctx, databaseTimeout)
		defer cancel()
		if err := s.database.Import(
			importCtx, meta.ContainerName, record.DatabaseEngine, result.Database,
		); err != nil {
			return fmt.Errorf("import database: %w", err)
		}
	}
	return nil
}

// RestoreDatabase re-imports one snapshot's db.sql into a project's running
// container. It is the last step of un-trashing a project: the files came back
// with the trash directory, but the database lived in the container rootfs
// that was destroyed.
func (s *Service) RestoreDatabase(
	ctx context.Context,
	projectID serviceproject.ID,
	snapshotID string,
) error {
	if !s.Available() || s.database == nil {
		return nil
	}
	record, err := s.find(ctx, projectID, ID(snapshotID))
	if err != nil {
		return err
	}
	if !record.HasDatabase || record.Archive == "" {
		return nil
	}
	meta, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return err
	}
	if meta.ContainerName == "" {
		return nil
	}
	dump, err := s.archive.ReadDatabase(ctx, string(projectID), record.Archive)
	if err != nil {
		return err
	}
	if len(dump) == 0 {
		return nil
	}
	importCtx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	return s.database.Import(importCtx, meta.ContainerName, record.DatabaseEngine, dump)
}

// Delete removes one snapshot record and its archive.
func (s *Service) Delete(
	ctx context.Context,
	projectID serviceproject.ID,
	snapshotID ID,
) error {
	err := s.deleteSnapshot(ctx, projectID, snapshotID)
	s.record(ctx, audit.ActionSnapshotDelete, audit.Target{
		Type: audit.TargetSnapshot, ID: string(snapshotID),
	}, audit.Meta{"projectId": string(projectID)}, err)
	return err
}

func (s *Service) deleteSnapshot(
	ctx context.Context,
	projectID serviceproject.ID,
	snapshotID ID,
) error {
	if !s.Available() {
		return ErrUnavailable
	}
	var removed Snapshot
	if _, err := s.repo.Update(ctx, projectID, func(records []Snapshot) ([]Snapshot, error) {
		next := make([]Snapshot, 0, len(records))
		for _, r := range records {
			if r.ID == snapshotID {
				removed = r
				continue
			}
			next = append(next, r)
		}
		if removed.ID == "" {
			return nil, ErrNotFound
		}
		return next, nil
	}); err != nil {
		return err
	}
	if removed.Archive != "" {
		if err := s.archive.Remove(ctx, string(projectID), removed.Archive); err != nil {
			log.Printf("snapshots: remove archive %s: %v", removed.Archive, err)
		}
	}
	return nil
}

// PurgeAll drops every snapshot of a project. It runs when a trashed project
// is purged, permanently.
func (s *Service) PurgeAll(ctx context.Context, projectID serviceproject.ID) error {
	if s == nil || s.repo == nil {
		return nil
	}
	if _, err := s.repo.Update(ctx, projectID, func([]Snapshot) ([]Snapshot, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if s.archive == nil {
		return nil
	}
	return s.archive.RemoveProject(ctx, string(projectID))
}

// enforceRetention keeps the newest RetentionCount records and deletes the
// archives of the rest. Records that have not settled are never evicted:
// their archive may still be being written.
func (s *Service) enforceRetention(ctx context.Context, projectID serviceproject.ID) {
	var evicted []Snapshot
	if _, err := s.repo.Update(ctx, projectID, func(records []Snapshot) ([]Snapshot, error) {
		sortNewestFirst(records)
		kept := make([]Snapshot, 0, len(records))
		for index, record := range records {
			if index < RetentionCount || !record.Terminal() {
				kept = append(kept, record)
				continue
			}
			evicted = append(evicted, record)
		}
		return kept, nil
	}); err != nil {
		log.Printf("snapshots: enforce retention for %s: %v", projectID, err)
		return
	}
	for _, record := range evicted {
		if record.Archive == "" {
			continue
		}
		if err := s.archive.Remove(ctx, string(projectID), record.Archive); err != nil {
			log.Printf("snapshots: evict archive %s: %v", record.Archive, err)
		}
	}
}

// Wait blocks until every background capture and restore has settled. Tests
// use it; production code never needs to.
func (s *Service) Wait() { s.wait.Wait() }

func (s *Service) find(
	ctx context.Context,
	projectID serviceproject.ID,
	snapshotID ID,
) (Snapshot, error) {
	records, err := s.repo.List(ctx, projectID)
	if err != nil {
		return Snapshot{}, err
	}
	for _, record := range records {
		if record.ID == snapshotID {
			return record, nil
		}
	}
	return Snapshot{}, ErrNotFound
}

func (s *Service) append(ctx context.Context, projectID serviceproject.ID, record Snapshot) error {
	_, err := s.repo.Update(ctx, projectID, func(records []Snapshot) ([]Snapshot, error) {
		return append(records, record), nil
	})
	return err
}

func (s *Service) setStatus(
	ctx context.Context,
	projectID serviceproject.ID,
	snapshotID ID,
	mutate func(*Snapshot),
) error {
	_, err := s.repo.Update(ctx, projectID, func(records []Snapshot) ([]Snapshot, error) {
		for index := range records {
			if records[index].ID == snapshotID {
				mutate(&records[index])
				return records, nil
			}
		}
		return nil, ErrNotFound
	})
	return err
}

func (s *Service) fail(
	ctx context.Context,
	projectID serviceproject.ID,
	snapshotID ID,
	cause error,
) error {
	finishedAt := s.now().UnixMilli()
	if err := s.setStatus(ctx, projectID, snapshotID, func(r *Snapshot) {
		r.Status = StatusFailed
		r.Error = cause.Error()
		r.FinishedAt = finishedAt
	}); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// acquire takes the per-project operation slot. One capture or restore at a
// time keeps a restore from racing the tar of the same directories.
func (s *Service) acquire(projectID serviceproject.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[projectID] > 0 {
		return false
	}
	s.busy[projectID] = 1
	return true
}

func (s *Service) release(projectID serviceproject.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.busy, projectID)
}

func (s *Service) beginJob(projectID serviceproject.ID, kind JobKind, snapshotID ID) Job {
	job := Job{
		ID:         string(newID()),
		ProjectID:  string(projectID),
		Kind:       kind,
		SnapshotID: snapshotID,
		Status:     StatusRunning,
		StartedAt:  s.now().UnixMilli(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := append([]Job{job}, s.jobs[projectID]...)
	if len(jobs) > maxJobsPerProject {
		jobs = jobs[:maxJobsPerProject]
	}
	s.jobs[projectID] = jobs
	return job
}

func (s *Service) finishJob(projectID serviceproject.ID, job Job, cause error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := s.jobs[projectID]
	for index := range jobs {
		if jobs[index].ID != job.ID {
			continue
		}
		jobs[index].FinishedAt = s.now().UnixMilli()
		if cause != nil {
			jobs[index].Status = StatusFailed
			jobs[index].Error = cause.Error()
			return
		}
		jobs[index].Status = StatusReady
		return
	}
}

func (s *Service) record(ctx context.Context, action string, target audit.Target, meta audit.Meta, err error) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(action, target, meta, err))
}

func snapshotTarget(projectID serviceproject.ID, record Snapshot) audit.Target {
	return audit.Target{
		Type: audit.TargetSnapshot,
		ID:   string(record.ID),
		Name: string(projectID),
	}
}

// projectHostDir is the directory holding a project's durable trees. Cwd is
// ".../<slug>/workspace", so its parent is the project's host directory.
func projectHostDir(meta serviceproject.Meta) string {
	if meta.Cwd == "" {
		return ""
	}
	return filepath.Dir(filepath.Clean(meta.Cwd))
}

// archiveName is the archive's base name: a sortable UTC timestamp plus the
// snapshot id, so two snapshots taken in the same second never collide.
func archiveName(at time.Time, id ID) string {
	return at.Format("20060102T150405Z") + "-" + string(id)
}

// stashName is the directory a replaced tree is moved to during a restore.
func stashName(at time.Time) string {
	return ".pre-restore-" + at.Format("20060102T150405Z")
}

func sortNewestFirst(records []Snapshot) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].CreatedAt == records[j].CreatedAt {
			return records[i].ID > records[j].ID
		}
		return records[i].CreatedAt > records[j].CreatedAt
	})
}

func newID() ID {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	return ID(hex.EncodeToString(raw[:]))
}
