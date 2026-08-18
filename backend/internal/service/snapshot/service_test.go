package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

const testProjectID = serviceproject.ID("abcd12")

func TestCreatePacksAndPublishesTheRecord(t *testing.T) {
	tests := []struct {
		name           string
		input          CreateInput
		secrets        []serviceproject.Secret
		dump           []byte
		engine         string
		wantDatabase   bool
		wantSecretKeys []string
	}{
		{
			name:  "files only",
			input: CreateInput{Label: "before the plugin upgrade"},
		},
		{
			name:         "with a database dump",
			input:        CreateInput{},
			dump:         []byte("CREATE DATABASE wordpress;"),
			engine:       "mysql",
			wantDatabase: true,
		},
		{
			name:    "secrets stay out unless asked for",
			input:   CreateInput{},
			secrets: []serviceproject.Secret{{Key: "API_KEY", Value: "s3cret"}},
		},
		{
			name:           "secrets included on request",
			input:          CreateInput{IncludeSecrets: true},
			secrets:        []serviceproject.Secret{{Key: "API_KEY", Value: "s3cret"}},
			wantSecretKeys: []string{"API_KEY"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newHarness(t)
			harness.database.dump = test.dump
			harness.database.engine = test.engine
			harness.secrets.records = test.secrets

			record, err := harness.service.Create(context.Background(), testProjectID, test.input, "Ada@Example.com")
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if record.Status != StatusPending {
				t.Fatalf("Create() status = %q, want pending", record.Status)
			}
			if record.CreatedBy != "ada@example.com" {
				t.Fatalf("Create() createdBy = %q, want the normalized email", record.CreatedBy)
			}
			harness.service.Wait()

			stored := harness.mustFind(t, record.ID)
			if stored.Status != StatusReady {
				t.Fatalf("stored status = %q (error %q), want ready", stored.Status, stored.Error)
			}
			if stored.Archive == "" || stored.SizeBytes == 0 {
				t.Fatalf("stored record is missing its archive: %+v", stored)
			}
			if stored.HasDatabase != test.wantDatabase {
				t.Fatalf("hasDatabase = %v, want %v", stored.HasDatabase, test.wantDatabase)
			}

			manifest := harness.archive.manifest(t)
			if manifest.ProjectID != string(testProjectID) || manifest.Template != "wordpress" {
				t.Fatalf("manifest = %+v, want the project's identity", manifest)
			}
			if !slices.Equal(manifest.Directories, []string{WorkspaceEntry, AgentHomeEntry}) {
				t.Fatalf("manifest directories = %v", manifest.Directories)
			}
			gotKeys := make([]string, 0, len(manifest.Secrets))
			for key := range manifest.Secrets {
				gotKeys = append(gotKeys, key)
			}
			slices.Sort(gotKeys)
			if !slices.Equal(gotKeys, test.wantSecretKeys) {
				t.Fatalf("manifest secrets = %v, want %v", gotKeys, test.wantSecretKeys)
			}

			jobs := harness.service.Jobs(testProjectID)
			if len(jobs) != 1 || jobs[0].Kind != JobCapture || jobs[0].Status != StatusReady {
				t.Fatalf("jobs = %+v, want one finished capture", jobs)
			}
		})
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*harness)
		input   CreateInput
		want    error
	}{
		{
			name:  "label bounded",
			input: CreateInput{Label: strings.Repeat("x", MaxLabelLength+1)},
			want:  ErrLabelTooLong,
		},
		{
			name:    "unknown project",
			prepare: func(h *harness) { h.projects.err = serviceproject.ErrNotFound },
			want:    serviceproject.ErrNotFound,
		},
		{
			name:    "no archiver on this host",
			prepare: func(h *harness) { h.archive.unavailable = true },
			want:    ErrUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newHarness(t)
			if test.prepare != nil {
				test.prepare(harness)
			}
			_, err := harness.service.Create(context.Background(), testProjectID, test.input, "ada@example.com")
			if !errors.Is(err, test.want) {
				t.Fatalf("Create() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestOnlyOneOperationRunsPerProject(t *testing.T) {
	harness := newHarness(t)
	release := make(chan struct{})
	harness.archive.beforePack = func() { <-release }

	if _, err := harness.service.Create(context.Background(), testProjectID, CreateInput{}, "ada@example.com"); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if !harness.service.Busy(testProjectID) {
		t.Fatal("Busy() = false while a capture is running")
	}
	if _, err := harness.service.Create(context.Background(), testProjectID, CreateInput{}, "ada@example.com"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Create() error = %v, want ErrBusy", err)
	}
	close(release)
	harness.service.Wait()
	if harness.service.Busy(testProjectID) {
		t.Fatal("Busy() = true after the capture settled")
	}
}

func TestRetentionKeepsTheNewestRecords(t *testing.T) {
	harness := newHarness(t)
	// Seed one more than the retention window, all already settled.
	seeded := make([]Snapshot, 0, RetentionCount+2)
	for index := range RetentionCount + 2 {
		seeded = append(seeded, Snapshot{
			ID:        ID(string(rune('a' + index))),
			Status:    StatusReady,
			CreatedAt: int64(index + 1),
			Archive:   "old-" + string(rune('a'+index)) + ".tar.gz",
		})
	}
	harness.repo.seed(seeded)

	record, err := harness.service.Create(context.Background(), testProjectID, CreateInput{}, "ada@example.com")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	harness.service.Wait()

	kept, err := harness.service.List(context.Background(), testProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != RetentionCount {
		t.Fatalf("kept %d records, want %d", len(kept), RetentionCount)
	}
	if kept[0].ID != record.ID {
		t.Fatalf("newest record = %q, want the one just taken", kept[0].ID)
	}
	// The three oldest archives are gone from disk, not just from the index.
	if got := len(harness.archive.removed); got != 3 {
		t.Fatalf("removed %d archives, want 3: %v", got, harness.archive.removed)
	}
	if !slices.Contains(harness.archive.removed, "old-a.tar.gz") {
		t.Fatalf("oldest archive was not evicted: %v", harness.archive.removed)
	}
}

func TestRetentionNeverEvictsAnUnsettledRecord(t *testing.T) {
	harness := newHarness(t)
	seeded := make([]Snapshot, 0, RetentionCount+1)
	// The oldest record is still being packed by another goroutine.
	seeded = append(seeded, Snapshot{ID: "pending", Status: StatusRunning, CreatedAt: 1})
	for index := range RetentionCount {
		seeded = append(seeded, Snapshot{
			ID: ID("r" + string(rune('a'+index))), Status: StatusReady,
			CreatedAt: int64(index + 2), Archive: "a.tar.gz",
		})
	}
	harness.repo.seed(seeded)

	if _, err := harness.service.Create(context.Background(), testProjectID, CreateInput{}, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	harness.service.Wait()

	kept, _ := harness.service.List(context.Background(), testProjectID)
	if !slices.ContainsFunc(kept, func(s Snapshot) bool { return s.ID == "pending" }) {
		t.Fatal("retention evicted a record whose archive was still being written")
	}
}

func TestPackFailureMarksTheRecordFailed(t *testing.T) {
	harness := newHarness(t)
	harness.archive.packErr = errors.New("no space left on device")

	record, err := harness.service.Create(context.Background(), testProjectID, CreateInput{}, "ada@example.com")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	harness.service.Wait()

	stored := harness.mustFind(t, record.ID)
	if stored.Status != StatusFailed || !strings.Contains(stored.Error, "no space left") {
		t.Fatalf("stored = %+v, want a failed record carrying the cause", stored)
	}
	jobs := harness.service.Jobs(testProjectID)
	if len(jobs) != 1 || jobs[0].Status != StatusFailed {
		t.Fatalf("jobs = %+v, want one failed capture", jobs)
	}
}

func TestRestoreStateMachine(t *testing.T) {
	tests := []struct {
		name      string
		seed      Snapshot
		confirmed bool
		restoreID ID
		packErr   error
		wantErr   error
		wantSteps []string
	}{
		{
			name:      "confirmed restore stops, swaps, remaps, starts, imports",
			seed:      readySnapshot(),
			confirmed: true,
			restoreID: "snap1",
			wantSteps: []string{"stop", "swap", "prepare:workspace", "prepare:agent-home", "start", "import"},
		},
		{
			name:      "unconfirmed restore is refused",
			seed:      readySnapshot(),
			restoreID: "snap1",
			wantErr:   ErrConfirmRequired,
		},
		{
			name:      "a snapshot that is still packing cannot be restored",
			seed:      Snapshot{ID: "snap1", Status: StatusRunning, CreatedAt: 1},
			confirmed: true,
			restoreID: "snap1",
			wantErr:   ErrNotReady,
		},
		{
			name:      "unknown snapshot",
			seed:      readySnapshot(),
			confirmed: true,
			restoreID: "nope",
			wantErr:   ErrNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newHarness(t)
			harness.repo.seed([]Snapshot{test.seed})

			_, err := harness.service.Restore(
				context.Background(), testProjectID, test.restoreID, test.confirmed, "ada@example.com",
			)
			harness.service.Wait()
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Restore() error = %v, want %v", err, test.wantErr)
				}
				if len(harness.steps()) != 0 {
					t.Fatalf("a refused restore still touched the project: %v", harness.steps())
				}
				return
			}
			if err != nil {
				t.Fatalf("Restore() error = %v", err)
			}
			if got := harness.steps(); !slices.Equal(got, test.wantSteps) {
				t.Fatalf("restore steps = %v, want %v", got, test.wantSteps)
			}
			if harness.archive.lastRestore.StashName == "" ||
				!strings.HasPrefix(harness.archive.lastRestore.StashName, ".pre-restore-") {
				t.Fatalf("stash name = %q, want a .pre-restore- directory",
					harness.archive.lastRestore.StashName)
			}
			if filepath.ToSlash(harness.archive.lastRestore.ProjectDir) != "/var/lib/remote/projects/demo" {
				t.Fatalf("restore target = %q", harness.archive.lastRestore.ProjectDir)
			}
		})
	}
}

func TestRestoreFailureBringsTheProjectBackUp(t *testing.T) {
	harness := newHarness(t)
	harness.repo.seed([]Snapshot{readySnapshot()})
	harness.archive.restoreErr = errors.New("archive is truncated")

	if _, err := harness.service.Restore(context.Background(), testProjectID, "snap1", true, "ada@example.com"); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	harness.service.Wait()

	if got := harness.steps(); !slices.Equal(got, []string{"stop", "start"}) {
		t.Fatalf("steps = %v, want the project restarted on the tree that is on disk", got)
	}
	jobs := harness.service.Jobs(testProjectID)
	if len(jobs) != 1 || jobs[0].Status != StatusFailed {
		t.Fatalf("jobs = %+v, want one failed restore", jobs)
	}
}

func TestCaptureTrashUsesTheSuppliedDumpAndSource(t *testing.T) {
	harness := newHarness(t)
	// The container is already gone by the time this runs, so the service must
	// not try to dump it again.
	harness.database.dumpErr = errors.New("instance not found")

	id, err := harness.service.CaptureTrash(
		context.Background(), testProjectID,
		"/var/lib/remote/trash/abcd12", []byte("dump"), "mysql", "ada@example.com",
	)
	if err != nil {
		t.Fatalf("CaptureTrash() error = %v", err)
	}
	harness.service.Wait()

	stored := harness.mustFind(t, ID(id))
	if stored.Kind != KindTrash || stored.Status != StatusReady {
		t.Fatalf("stored = %+v, want a ready trash snapshot", stored)
	}
	if !stored.HasDatabase || stored.DatabaseEngine != "mysql" {
		t.Fatalf("stored database = %v/%q", stored.HasDatabase, stored.DatabaseEngine)
	}
	if harness.archive.lastPack.SourceDir != "/var/lib/remote/trash/abcd12" {
		t.Fatalf("packed from %q, want the trash directory", harness.archive.lastPack.SourceDir)
	}
	if string(harness.archive.lastPack.Database) != "dump" {
		t.Fatalf("packed database = %q", harness.archive.lastPack.Database)
	}
}

func TestRestoreDatabase(t *testing.T) {
	tests := []struct {
		name       string
		seed       Snapshot
		snapshotID string
		wantImport bool
		wantErr    error
	}{
		{
			name:       "imports the archive's dump",
			seed:       Snapshot{ID: "snap1", Status: StatusReady, Archive: "a.tar.gz", HasDatabase: true, DatabaseEngine: "mysql"},
			snapshotID: "snap1",
			wantImport: true,
		},
		{
			name:       "a snapshot without a database is a no-op",
			seed:       Snapshot{ID: "snap1", Status: StatusReady, Archive: "a.tar.gz"},
			snapshotID: "snap1",
		},
		{
			name:       "unknown snapshot",
			seed:       Snapshot{ID: "snap1", Status: StatusReady},
			snapshotID: "missing",
			wantErr:    ErrNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newHarness(t)
			harness.repo.seed([]Snapshot{test.seed})
			harness.archive.storedDatabase = []byte("INSERT INTO wp_posts ...")

			err := harness.service.RestoreDatabase(context.Background(), testProjectID, test.snapshotID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("RestoreDatabase() error = %v, want %v", err, test.wantErr)
			}
			if got := len(harness.database.imported) > 0; got != test.wantImport {
				t.Fatalf("imported = %v, want %v", got, test.wantImport)
			}
		})
	}
}

func TestDeleteAndPurge(t *testing.T) {
	harness := newHarness(t)
	harness.repo.seed([]Snapshot{
		{ID: "snap1", Status: StatusReady, Archive: "one.tar.gz", CreatedAt: 2},
		{ID: "snap2", Status: StatusReady, Archive: "two.tar.gz", CreatedAt: 1},
	})

	if err := harness.service.Delete(context.Background(), testProjectID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() unknown error = %v, want ErrNotFound", err)
	}
	if err := harness.service.Delete(context.Background(), testProjectID, "snap1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	remaining, _ := harness.service.List(context.Background(), testProjectID)
	if len(remaining) != 1 || remaining[0].ID != "snap2" {
		t.Fatalf("remaining = %+v", remaining)
	}
	if !slices.Equal(harness.archive.removed, []string{"one.tar.gz"}) {
		t.Fatalf("removed = %v", harness.archive.removed)
	}

	if err := harness.service.PurgeAll(context.Background(), testProjectID); err != nil {
		t.Fatalf("PurgeAll() error = %v", err)
	}
	if records, _ := harness.service.List(context.Background(), testProjectID); len(records) != 0 {
		t.Fatalf("PurgeAll() left %d records", len(records))
	}
	if !harness.archive.purgedProject {
		t.Fatal("PurgeAll() did not clear the project's archive directory")
	}
}

// --- harness -------------------------------------------------------------

func readySnapshot() Snapshot {
	return Snapshot{
		ID: "snap1", Status: StatusReady, CreatedAt: 1,
		Archive: "snap1.tar.gz", HasDatabase: true, DatabaseEngine: "mysql",
	}
}

type harness struct {
	service  *Service
	repo     *fakeRepository
	archive  *fakeArchive
	projects *fakeProjects
	database *fakeDatabase
	secrets  *fakeSecrets
	preparer *fakePreparer

	mu    sync.Mutex
	trace []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		repo:     &fakeRepository{},
		archive:  &fakeArchive{},
		projects: &fakeProjects{},
		database: &fakeDatabase{},
		secrets:  &fakeSecrets{},
		preparer: &fakePreparer{},
	}
	h.archive.h, h.projects.h, h.database.h, h.preparer.h = h, h, h, h
	h.projects.meta = serviceproject.Meta{
		ID:            testProjectID,
		Name:          "Demo",
		Slug:          "demo",
		Cwd:           "/var/lib/remote/projects/demo/workspace",
		ContainerName: "demo",
		Template:      "wordpress",
	}
	h.service = New(h.repo, h.archive, h.projects,
		WithDatabase(h.database),
		WithSecrets(h.secrets),
		WithPreparer(h.preparer),
		WithClock(func() time.Time { return time.Unix(1_800_000_000, 0).UTC() }),
	)
	return h
}

func (h *harness) step(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.trace = append(h.trace, name)
}

func (h *harness) steps() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.trace)
}

func (h *harness) mustFind(t *testing.T, id ID) Snapshot {
	t.Helper()
	records, err := h.service.List(context.Background(), testProjectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("snapshot %q not found in %+v", id, records)
	return Snapshot{}
}

type fakeRepository struct {
	mu      sync.Mutex
	records []Snapshot
}

func (r *fakeRepository) seed(records []Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = slices.Clone(records)
}

func (r *fakeRepository) List(context.Context, serviceproject.ID) ([]Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.records), nil
}

func (r *fakeRepository) Update(
	_ context.Context,
	_ serviceproject.ID,
	fn func([]Snapshot) ([]Snapshot, error),
) ([]Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, err := fn(slices.Clone(r.records))
	if err != nil {
		return nil, err
	}
	r.records = next
	return slices.Clone(next), nil
}

type fakeArchive struct {
	h              *harness
	unavailable    bool
	packErr        error
	restoreErr     error
	beforePack     func()
	storedDatabase []byte

	mu            sync.Mutex
	lastPack      PackRequest
	lastRestore   RestoreRequest
	removed       []string
	purgedProject bool
}

func (a *fakeArchive) Available() bool { return !a.unavailable }

func (a *fakeArchive) Pack(_ context.Context, req PackRequest) (PackResult, error) {
	if a.beforePack != nil {
		a.beforePack()
	}
	a.mu.Lock()
	a.lastPack = req
	a.mu.Unlock()
	if a.packErr != nil {
		return PackResult{}, a.packErr
	}
	return PackResult{Archive: req.Name + ".tar.zst", Format: "tar.zst", SizeBytes: 4096}, nil
}

func (a *fakeArchive) Restore(_ context.Context, req RestoreRequest) (RestoreResult, error) {
	a.mu.Lock()
	a.lastRestore = req
	a.mu.Unlock()
	if a.restoreErr != nil {
		return RestoreResult{}, a.restoreErr
	}
	a.h.step("swap")
	return RestoreResult{StashPath: "/snapshots/" + req.StashName, Database: []byte("dump")}, nil
}

func (a *fakeArchive) ReadDatabase(context.Context, string, string) ([]byte, error) {
	return a.storedDatabase, nil
}

func (a *fakeArchive) Remove(_ context.Context, _, archive string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.removed = append(a.removed, archive)
	return nil
}

func (a *fakeArchive) RemoveProject(context.Context, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.purgedProject = true
	return nil
}

func (a *fakeArchive) manifest(t *testing.T) Manifest {
	t.Helper()
	a.mu.Lock()
	raw := a.lastPack.Manifest
	a.mu.Unlock()
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("manifest is not valid json: %v", err)
	}
	return manifest
}

type fakeProjects struct {
	h    *harness
	meta serviceproject.Meta
	err  error
}

func (p *fakeProjects) Get(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	return p.meta, p.err
}

func (p *fakeProjects) Start(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	p.h.step("start")
	return p.meta, nil
}

func (p *fakeProjects) Stop(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	p.h.step("stop")
	return p.meta, nil
}

type fakeDatabase struct {
	h       *harness
	dump    []byte
	engine  string
	dumpErr error

	mu       sync.Mutex
	imported [][]byte
}

func (d *fakeDatabase) Dump(context.Context, string) ([]byte, string, error) {
	if d.dumpErr != nil {
		return nil, "", d.dumpErr
	}
	return d.dump, d.engine, nil
}

func (d *fakeDatabase) Import(_ context.Context, _, _ string, dump []byte) error {
	d.h.step("import")
	d.mu.Lock()
	defer d.mu.Unlock()
	d.imported = append(d.imported, dump)
	return nil
}

type fakeSecrets struct {
	records []serviceproject.Secret
}

func (s *fakeSecrets) List(context.Context, serviceproject.ID) ([]serviceproject.Secret, error) {
	return s.records, nil
}

type fakePreparer struct{ h *harness }

func (p *fakePreparer) Prepare(path string) error {
	p.h.step("prepare:" + filepath.Base(path))
	return nil
}
