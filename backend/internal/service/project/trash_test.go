package project

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestTrashRemovesTheProjectFromEveryLiveListing(t *testing.T) {
	harness := newTrashHarness()

	trashed, err := harness.service.Trash(context.Background(), "aaaa11", "Ada@Example.com")
	if err != nil {
		t.Fatalf("Trash() error = %v", err)
	}
	if !trashed.Trashed() || trashed.DeletedBy != "ada@example.com" {
		t.Fatalf("Trash() meta = %+v, want a trashed project with a normalized actor", trashed)
	}
	if trashed.Status != StatusMissing {
		t.Fatalf("Trash() status = %q, want missing", trashed.Status)
	}

	// The database dump has to be taken before the container is destroyed.
	if !slices.Equal(harness.trace(), []string{"dump", "container-delete", "storage-trash", "snapshot"}) {
		t.Fatalf("trash order = %v", harness.trace())
	}
	if string(harness.snapshots.database) != "DUMP" || harness.snapshots.engine != "mysql" {
		t.Fatalf("snapshot database = %q/%q", harness.snapshots.database, harness.snapshots.engine)
	}
	if harness.snapshots.sourceDir != "/trash/aaaa11" {
		t.Fatalf("snapshot source = %q, want the trash directory", harness.snapshots.sourceDir)
	}
	if trashed.TrashSnapshotID != "snap-1" {
		t.Fatalf("trashSnapshotId = %q", trashed.TrashSnapshotID)
	}

	live, err := harness.service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].ID != "bbbb22" {
		t.Fatalf("List() = %+v, want only the live project", live)
	}
	visible, _ := harness.service.ListVisible(context.Background(), "ada@example.com", true)
	if len(visible) != 1 {
		t.Fatalf("ListVisible() = %+v, want only the live project", visible)
	}
	// The slug must stop resolving: no preview certificate, no share link.
	if _, err := harness.service.GetBySlug(context.Background(), "demo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBySlug() error = %v, want ErrNotFound", err)
	}
	// The project itself is still readable by id, which is what the Trash
	// page and the restore endpoint need.
	if _, err := harness.service.Get(context.Background(), "aaaa11"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestLifecycleOperationsRefuseATrashedProject(t *testing.T) {
	tests := []struct {
		name      string
		operation func(context.Context, *Service) error
	}{
		{"start", func(ctx context.Context, s *Service) error { _, err := s.Start(ctx, "aaaa11"); return err }},
		{"stop", func(ctx context.Context, s *Service) error { _, err := s.Stop(ctx, "aaaa11"); return err }},
		{"restart", func(ctx context.Context, s *Service) error { _, err := s.Restart(ctx, "aaaa11"); return err }},
		{"upgrade", func(ctx context.Context, s *Service) error { _, err := s.Upgrade(ctx, "aaaa11", false); return err }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newTrashHarness()
			if _, err := harness.service.Trash(context.Background(), "aaaa11", ""); err != nil {
				t.Fatal(err)
			}
			if err := test.operation(context.Background(), harness.service); !errors.Is(err, ErrTrashed) {
				t.Fatalf("%s on a trashed project = %v, want ErrTrashed", test.name, err)
			}
		})
	}
}

func TestTrashIsIdempotent(t *testing.T) {
	harness := newTrashHarness()
	if _, err := harness.service.Trash(context.Background(), "aaaa11", ""); err != nil {
		t.Fatal(err)
	}
	before := len(harness.trace())
	if _, err := harness.service.Trash(context.Background(), "aaaa11", ""); err != nil {
		t.Fatalf("second Trash() error = %v", err)
	}
	if got := len(harness.trace()); got != before {
		t.Fatalf("second Trash() ran %d extra steps", got-before)
	}
}

func TestRestoreFromTrash(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*trashHarness)
		wantErr   error
		wantSteps []string
	}{
		{
			name:      "moves the files home, recreates the container and re-imports the database",
			wantSteps: []string{"storage-untrash", "container-ensure", "snapshot-restore-db"},
		},
		{
			name:    "a live project has nothing to restore",
			prepare: func(h *trashHarness) { h.skipTrash = true },
			wantErr: ErrNotTrashed,
		},
		{
			name:    "refused while the trash snapshot is still being packed",
			prepare: func(h *trashHarness) { h.snapshots.busy = true },
			wantErr: ErrSnapshotBusy,
		},
		{
			name:    "a storage failure leaves the project in the trash",
			prepare: func(h *trashHarness) { h.storage.untrashErr = errors.New("target exists") },
			wantErr: errors.New("target exists"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newTrashHarness()
			if test.prepare != nil {
				test.prepare(harness)
			}
			if !harness.skipTrash {
				if _, err := harness.service.Trash(context.Background(), "aaaa11", ""); err != nil {
					t.Fatal(err)
				}
			}
			harness.resetTrace()

			restored, err := harness.service.RestoreFromTrash(context.Background(), "aaaa11")
			if test.wantErr != nil {
				if err == nil || (!errors.Is(err, test.wantErr) && err.Error() != test.wantErr.Error()) {
					t.Fatalf("RestoreFromTrash() error = %v, want %v", err, test.wantErr)
				}
				stored, _ := harness.service.Get(context.Background(), "aaaa11")
				if !harness.skipTrash && !stored.Trashed() {
					t.Fatal("a failed restore un-trashed the project anyway")
				}
				return
			}
			if err != nil {
				t.Fatalf("RestoreFromTrash() error = %v", err)
			}
			if restored.Trashed() || restored.Status != StatusRunning {
				t.Fatalf("restored = %+v, want a live running project", restored)
			}
			if got := harness.trace(); !slices.Equal(got, test.wantSteps) {
				t.Fatalf("restore steps = %v, want %v", got, test.wantSteps)
			}
			if harness.snapshots.restoredSnapshotID != "snap-1" {
				t.Fatalf("re-imported snapshot = %q", harness.snapshots.restoredSnapshotID)
			}
			live, _ := harness.service.List(context.Background())
			if len(live) != 2 {
				t.Fatalf("List() = %+v, want the project back", live)
			}
		})
	}
}

func TestPurge(t *testing.T) {
	harness := newTrashHarness()
	if err := harness.service.Purge(context.Background(), "aaaa11"); !errors.Is(err, ErrNotTrashed) {
		t.Fatalf("Purge() on a live project = %v, want ErrNotTrashed", err)
	}
	if _, err := harness.service.Trash(context.Background(), "aaaa11", ""); err != nil {
		t.Fatal(err)
	}
	harness.resetTrace()

	if err := harness.service.Purge(context.Background(), "aaaa11"); err != nil {
		t.Fatalf("Purge() error = %v", err)
	}
	want := []string{"container-delete", "snapshot-purge", "storage-purge", "secrets-delete", "access-delete"}
	if got := harness.trace(); !slices.Equal(got, want) {
		t.Fatalf("purge steps = %v, want %v", got, want)
	}
	if _, err := harness.service.Get(context.Background(), "aaaa11"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after purge = %v, want ErrNotFound", err)
	}
}

func TestPurgeExpiredTrash(t *testing.T) {
	tests := []struct {
		name       string
		deletedAgo time.Duration
		retention  time.Duration
		busy       bool
		wantPurged int
	}{
		{name: "inside the window is kept", deletedAgo: 2 * time.Hour, retention: TrashRetention},
		{name: "past the window is purged", deletedAgo: 8 * 24 * time.Hour, retention: TrashRetention, wantPurged: 1},
		{name: "retention disabled keeps everything", deletedAgo: 90 * 24 * time.Hour, retention: 0},
		{
			name:       "a project whose snapshot is still packing is left for the next sweep",
			deletedAgo: 8 * 24 * time.Hour, retention: TrashRetention, busy: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newTrashHarness()
			harness.snapshots.busy = test.busy
			if _, err := harness.service.Trash(context.Background(), "aaaa11", ""); err != nil {
				t.Fatal(err)
			}
			harness.repo.mutate("aaaa11", func(m *Meta) {
				m.DeletedAt = time.Now().Add(-test.deletedAgo).UnixMilli()
			})

			purged, err := harness.service.PurgeExpiredTrash(context.Background(), test.retention)
			if err != nil {
				t.Fatalf("PurgeExpiredTrash() error = %v", err)
			}
			if purged != test.wantPurged {
				t.Fatalf("purged = %d, want %d", purged, test.wantPurged)
			}
			trashedLeft, _ := harness.service.ListTrashed(context.Background(), "", true)
			if len(trashedLeft) != 1-test.wantPurged {
				t.Fatalf("trash holds %d projects after the sweep", len(trashedLeft))
			}
		})
	}
}

func TestListTrashedRespectsMembership(t *testing.T) {
	harness := newTrashHarness()
	harness.access.members["aaaa11"] = []string{"ada@example.com"}
	if _, err := harness.service.Trash(context.Background(), "aaaa11", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.Trash(context.Background(), "bbbb22", ""); err != nil {
		t.Fatal(err)
	}
	// Pin the delete instants so the newest-first order is not decided by how
	// fast the two calls ran.
	harness.repo.mutate("aaaa11", func(m *Meta) { m.DeletedAt = 1_000 })
	harness.repo.mutate("bbbb22", func(m *Meta) { m.DeletedAt = 2_000 })

	tests := []struct {
		name    string
		email   string
		isAdmin bool
		wantIDs []ID
	}{
		{name: "admin sees every trashed project", isAdmin: true, wantIDs: []ID{"bbbb22", "aaaa11"}},
		{name: "member sees only their own", email: "ada@example.com", wantIDs: []ID{"aaaa11"}},
		{name: "a stranger sees nothing", email: "eve@example.com"},
		{name: "an anonymous caller sees nothing"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := harness.service.ListTrashed(context.Background(), test.email, test.isAdmin)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]ID, 0, len(got))
			for _, meta := range got {
				ids = append(ids, meta.ID)
			}
			if !slices.Equal(ids, test.wantIDs) {
				t.Fatalf("ListTrashed() = %v, want %v", ids, test.wantIDs)
			}
		})
	}
}

func TestTrashExpiresAt(t *testing.T) {
	tests := []struct {
		name      string
		meta      Meta
		retention time.Duration
		want      int64
	}{
		{name: "live project has no expiry", meta: Meta{}, retention: TrashRetention},
		{name: "retention disabled has no expiry", meta: Meta{DeletedAt: 1000}},
		{name: "trashed project expires after the window", meta: Meta{DeletedAt: 1000}, retention: 2 * time.Hour, want: 1000 + 7_200_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.meta.TrashExpiresAt(test.retention); got != test.want {
				t.Fatalf("TrashExpiresAt() = %d, want %d", got, test.want)
			}
		})
	}
}

// --- harness -------------------------------------------------------------

type trashHarness struct {
	service   *Service
	repo      *trashRepository
	storage   *trashStorage
	snapshots *trashSnapshots
	access    *trashAccess
	secrets   *trashSecrets
	skipTrash bool

	mu     sync.Mutex
	trail  []string
	sealed bool
}

func newTrashHarness() *trashHarness {
	h := &trashHarness{
		repo: &trashRepository{metas: map[ID]Meta{
			"aaaa11": {
				ID: "aaaa11", Name: "Demo", Slug: "demo",
				Cwd: "/var/lib/remote/projects/demo/workspace", ContainerName: "demo",
				Status: StatusRunning, Template: "wordpress",
			},
			"bbbb22": {
				ID: "bbbb22", Name: "Other", Slug: "other",
				Cwd: "/var/lib/remote/projects/other/workspace", ContainerName: "other",
				Status: StatusRunning,
			},
		}},
		storage:   &trashStorage{},
		snapshots: &trashSnapshots{},
		access:    &trashAccess{members: map[ID][]string{}},
		secrets:   &trashSecrets{},
	}
	lifecycle := &trashLifecycle{h: h}
	h.storage.h, h.snapshots.h, h.access.h, h.secrets.h = h, h, h, h
	h.service = New(
		h.repo,
		ContainerDependencies{Lifecycle: lifecycle, Database: &trashDatabase{h: h}},
		h.secrets,
		h.access,
		WithStorage(h.storage),
	)
	h.service.SetSnapshots(h.snapshots)
	return h
}

func (h *trashHarness) step(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.trail = append(h.trail, name)
}

func (h *trashHarness) trace() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.trail)
}

func (h *trashHarness) resetTrace() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.trail = nil
}

type trashRepository struct {
	mu    sync.Mutex
	metas map[ID]Meta
}

func (r *trashRepository) mutate(id ID, fn func(*Meta)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta := r.metas[id]
	fn(&meta)
	r.metas[id] = meta
}

func (r *trashRepository) List(context.Context) ([]Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Meta, 0, len(r.metas))
	for _, meta := range r.metas {
		out = append(out, meta)
	}
	slices.SortFunc(out, func(a, b Meta) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	return out, nil
}

func (r *trashRepository) Create(_ context.Context, meta Meta) (Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metas[meta.ID] = meta
	return meta, nil
}

func (r *trashRepository) Get(_ context.Context, id ID) (Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta, ok := r.metas[id]
	if !ok {
		return Meta{}, ErrNotFound
	}
	return meta, nil
}

func (r *trashRepository) GetBySlug(_ context.Context, slug string) (Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, meta := range r.metas {
		if meta.Slug == slug {
			return meta, nil
		}
	}
	return Meta{}, ErrNotFound
}

func (r *trashRepository) Update(_ context.Context, id ID, fn func(*Meta)) (Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta, ok := r.metas[id]
	if !ok {
		return Meta{}, ErrNotFound
	}
	fn(&meta)
	r.metas[id] = meta
	return meta, nil
}

func (r *trashRepository) SetStatus(ctx context.Context, id ID, status Status, errMsg string) (Meta, error) {
	return r.Update(ctx, id, func(m *Meta) {
		m.Status = status
		m.ErrorMsg = errMsg
	})
}

func (r *trashRepository) Delete(_ context.Context, id ID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.metas, id)
	return nil
}

type trashLifecycle struct{ h *trashHarness }

func (*trashLifecycle) Available() bool { return true }
func (l *trashLifecycle) Ensure(context.Context, Meta) error {
	l.h.step("container-ensure")
	return nil
}
func (*trashLifecycle) Busy(context.Context, string) (bool, error) { return false, nil }
func (*trashLifecycle) Start(context.Context, string) error        { return nil }
func (*trashLifecycle) Stop(context.Context, string) error         { return nil }
func (*trashLifecycle) Restart(context.Context, string) error      { return nil }
func (l *trashLifecycle) Delete(context.Context, string) error {
	l.h.step("container-delete")
	return nil
}
func (*trashLifecycle) State(context.Context, string) (ContainerState, error) {
	return ContainerStateRunning, nil
}
func (*trashLifecycle) EnsureResources(context.Context, string) error { return nil }
func (*trashLifecycle) SetResourceLimits(context.Context, string, ContainerLimits) error {
	return nil
}

type trashDatabase struct{ h *trashHarness }

func (d *trashDatabase) Dump(context.Context, string) ([]byte, string, error) {
	d.h.step("dump")
	return []byte("DUMP"), "mysql", nil
}

type trashStorage struct {
	h          *trashHarness
	untrashErr error
}

func (s *trashStorage) Trash(_ context.Context, id ID, _ string) (string, error) {
	s.h.step("storage-trash")
	return "/trash/" + string(id), nil
}

func (s *trashStorage) Untrash(context.Context, ID, string) error {
	s.h.step("storage-untrash")
	return s.untrashErr
}

func (s *trashStorage) PurgeTrash(context.Context, ID) error {
	s.h.step("storage-purge")
	return nil
}

type trashSnapshots struct {
	h                  *trashHarness
	busy               bool
	sourceDir          string
	database           []byte
	engine             string
	restoredSnapshotID string
}

func (s *trashSnapshots) CaptureTrash(
	_ context.Context, _ ID, sourceDir string, database []byte, engine, _ string,
) (string, error) {
	s.h.step("snapshot")
	s.sourceDir, s.database, s.engine = sourceDir, database, engine
	return "snap-1", nil
}

func (s *trashSnapshots) RestoreDatabase(_ context.Context, _ ID, snapshotID string) error {
	s.h.step("snapshot-restore-db")
	s.restoredSnapshotID = snapshotID
	return nil
}

func (s *trashSnapshots) PurgeAll(context.Context, ID) error {
	s.h.step("snapshot-purge")
	return nil
}

func (s *trashSnapshots) Busy(ID) bool { return s.busy }

type trashAccess struct {
	h       *trashHarness
	members map[ID][]string
}

func (a *trashAccess) List(_ context.Context, id ID) ([]string, error) { return a.members[id], nil }
func (a *trashAccess) Add(context.Context, ID, string) error           { return nil }
func (a *trashAccess) Remove(context.Context, ID, string) error        { return nil }
func (a *trashAccess) Set(context.Context, ID, []string) error         { return nil }
func (a *trashAccess) Has(_ context.Context, id ID, email string) (bool, error) {
	return slices.Contains(a.members[id], email), nil
}
func (a *trashAccess) DeleteAll(context.Context, ID) error {
	a.h.step("access-delete")
	return nil
}

type trashSecrets struct{ h *trashHarness }

func (*trashSecrets) List(context.Context, ID) ([]Secret, error)              { return nil, nil }
func (*trashSecrets) Set(context.Context, ID, string, string) (Secret, error) { return Secret{}, nil }
func (*trashSecrets) Delete(context.Context, ID, string) error                { return nil }
func (s *trashSecrets) DeleteAll(context.Context, ID) error {
	s.h.step("secrets-delete")
	return nil
}
