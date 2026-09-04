package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/hostarchive"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileauth"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileproject"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectaccess"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filesnapshot"
)

const (
	snapshotAdmin     = "admin@example.com"
	snapshotMember    = "member@example.com"
	snapshotOutsider  = "stranger@example.com"
	snapshotHostname  = "remote.example.test"
	snapshotRetention = 7 * 24 * time.Hour
)

func TestSnapshotRoutesRequireMembership(t *testing.T) {
	fixture := newSnapshotFixture(t)
	base := "/api/projects/" + string(fixture.project.ID) + "/snapshots"

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		email      string
		wantStatus int
	}{
		{name: "outsider cannot list", method: http.MethodGet, path: base, email: snapshotOutsider, wantStatus: http.StatusForbidden},
		{name: "outsider cannot snapshot", method: http.MethodPost, path: base, email: snapshotOutsider, wantStatus: http.StatusForbidden},
		{name: "outsider cannot delete", method: http.MethodDelete, path: base + "/abc", email: snapshotOutsider, wantStatus: http.StatusForbidden},
		{name: "anonymous is rejected", method: http.MethodGet, path: base, wantStatus: http.StatusUnauthorized},
		{name: "member can list", method: http.MethodGet, path: base, email: snapshotMember, wantStatus: http.StatusOK},
		{name: "admin can list", method: http.MethodGet, path: base, email: snapshotAdmin, wantStatus: http.StatusOK},
		{
			name: "member can snapshot with a label", method: http.MethodPost, path: base,
			body: `{"label":"before the upgrade"}`, email: snapshotMember, wantStatus: http.StatusAccepted,
		},
		{
			name: "an over-long label is rejected", method: http.MethodPost, path: base,
			body:  `{"label":"` + strings.Repeat("x", servicesnapshot.MaxLabelLength+1) + `"}`,
			email: snapshotMember, wantStatus: http.StatusBadRequest,
		},
		{
			name: "unknown snapshot", method: http.MethodDelete, path: base + "/deadbeef",
			email: snapshotAdmin, wantStatus: http.StatusNotFound,
		},
		{name: "unsupported method", method: http.MethodPut, path: base, email: snapshotAdmin, wantStatus: http.StatusMethodNotAllowed},
		{name: "unknown sub-resource", method: http.MethodGet, path: base + "/abc/history", email: snapshotAdmin, wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture.request(t, test.method, test.path, test.body, test.email, test.wantStatus)
			fixture.snapshots.Wait()
		})
	}
}

func TestSnapshotRestoreNeedsConfirmationFromMembers(t *testing.T) {
	fixture := newSnapshotFixture(t)
	base := "/api/projects/" + string(fixture.project.ID) + "/snapshots"

	record := fixture.takeSnapshot(t)
	restore := base + "/" + string(record.ID) + "/restore"

	// A member is told to acknowledge that the current files are replaced.
	fixture.request(t, http.MethodPost, restore, "", snapshotMember, http.StatusBadRequest)
	fixture.request(t, http.MethodPost, restore, `{"confirm":true}`, snapshotMember, http.StatusAccepted)
	fixture.snapshots.Wait()

	// An admin does not need the flag.
	fixture.request(t, http.MethodPost, restore, "", snapshotAdmin, http.StatusAccepted)
	fixture.snapshots.Wait()

	// An outsider never gets that far.
	fixture.request(t, http.MethodPost, restore, `{"confirm":true}`, snapshotOutsider, http.StatusForbidden)
}

func TestSnapshotRoutesReportUnavailableWithoutAnArchiver(t *testing.T) {
	fixture := newSnapshotFixture(t)
	fixture.handler.snapshots = nil
	fixture.request(
		t, http.MethodGet, "/api/projects/"+string(fixture.project.ID)+"/snapshots",
		"", snapshotAdmin, http.StatusServiceUnavailable,
	)
}

func TestTrashRoutes(t *testing.T) {
	fixture := newSnapshotFixture(t)
	item := "/api/projects/" + string(fixture.project.ID)

	// Deleting is admin-only, exactly as it was before the trash existed.
	fixture.request(t, http.MethodDelete, item, "", snapshotMember, http.StatusForbidden)
	// Purging a project that is not in the trash is a conflict, not a 500.
	fixture.request(t, http.MethodDelete, item+"/purge", "", snapshotAdmin, http.StatusConflict)
	// Restoring one that is not in the trash likewise.
	fixture.request(t, http.MethodPost, item+"/restore", "", snapshotAdmin, http.StatusConflict)

	fixture.request(t, http.MethodDelete, item, "", snapshotAdmin, http.StatusOK)
	fixture.snapshots.Wait()

	// It is gone from the live listing and present in the trash.
	if live := fixture.listProjects(t, snapshotAdmin); len(live) != 0 {
		t.Fatalf("live projects after delete = %+v, want none", live)
	}
	for _, email := range []string{snapshotAdmin, snapshotMember} {
		trashed := fixture.listTrash(t, email)
		if len(trashed) != 1 || trashed[0].ID != fixture.project.ID {
			t.Fatalf("trash for %s = %+v", email, trashed)
		}
		if trashed[0].ExpiresAt != trashed[0].DeletedAt+snapshotRetention.Milliseconds() {
			t.Fatalf("expiresAt = %d, want deletedAt plus the retention window", trashed[0].ExpiresAt)
		}
	}
	if trashed := fixture.listTrash(t, snapshotOutsider); len(trashed) != 0 {
		t.Fatalf("an outsider saw %d trashed projects", len(trashed))
	}

	// A new project cannot take the trashed project's container name.
	fixture.request(
		t, http.MethodPost, "/api/projects", `{"name":"Snapshot Project"}`,
		snapshotAdmin, http.StatusConflict,
	)

	// A member may undo their own accidental delete.
	fixture.request(t, http.MethodPost, item+"/restore", "", snapshotMember, http.StatusOK)
	if live := fixture.listProjects(t, snapshotAdmin); len(live) != 1 {
		t.Fatalf("live projects after restore = %+v, want the project back", live)
	}

	// Purging is admin-only and permanent.
	fixture.request(t, http.MethodDelete, item, "", snapshotAdmin, http.StatusOK)
	fixture.snapshots.Wait()
	fixture.request(t, http.MethodDelete, item+"/purge", "", snapshotMember, http.StatusForbidden)
	fixture.request(t, http.MethodDelete, item+"/purge", "", snapshotAdmin, http.StatusOK)
	if trashed := fixture.listTrash(t, snapshotAdmin); len(trashed) != 0 {
		t.Fatalf("trash after purge = %+v", trashed)
	}
}

func TestTrashListingRejectsNonGet(t *testing.T) {
	fixture := newSnapshotFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/projects/trash", nil)
	req.AddCookie(fixture.cookie(t, snapshotAdmin))
	rec := httptest.NewRecorder()
	fixture.handler.HandleTrash(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/projects/trash = %d, want 405", rec.Code)
	}
}

// --- fixture -------------------------------------------------------------

type snapshotFixture struct {
	handler   *ProjectHandler
	mux       *http.ServeMux
	auth      *serviceauth.Service
	projects  *serviceproject.Service
	snapshots *servicesnapshot.Service
	project   serviceproject.Meta
}

func newSnapshotFixture(t *testing.T) *snapshotFixture {
	t.Helper()
	dataDir := t.TempDir()
	auth, err := serviceauth.New(
		context.Background(),
		fileauth.New(t.TempDir()),
		auditTestDirectory{admins: map[string]bool{
			snapshotAdmin:    true,
			snapshotMember:   false,
			snapshotOutsider: false,
		}},
		func(string, string, string) serviceauth.OAuthProvider { return auditTestOAuth{} },
		"https://remote.example.com",
		[]byte("test-session-key"),
		twoFactorStoreForTest(t),
		sessionRegistryStoreForTest(t),
		serviceauth.DefaultOptions(),
	)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}

	workspaceRoot := t.TempDir()
	repo, err := fileproject.NewWithWorkspaceRoot(dataDir, workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	access, err := fileprojectaccess.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	archiver := hostarchive.NewArchiver(t.TempDir())
	if !archiver.Available() {
		t.Skip("tar is not on PATH")
	}
	projects := serviceproject.New(
		repo, serviceproject.ContainerDependencies{}, nil, access,
		serviceproject.WithStorage(hostarchive.NewTrashStorage(t.TempDir())),
	)
	project, err := projects.Create(
		context.Background(), serviceproject.CreateInput{Name: "Snapshot Project"}, snapshotMember,
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshotStore, err := filesnapshot.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := servicesnapshot.New(snapshotStore, archiver, projects)
	projects.SetSnapshots(snapshots)

	handler := NewProjectHandler(projects, nil, auth, snapshotHostname).
		WithSnapshots(snapshots, snapshotRetention)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return &snapshotFixture{
		handler: handler, mux: mux, auth: auth,
		projects: projects, snapshots: snapshots, project: project,
	}
}

func (f *snapshotFixture) cookie(t *testing.T, email string) *http.Cookie {
	t.Helper()
	return &http.Cookie{
		Name:  serviceauth.SessionCookieName,
		Value: issueTestSession(t, f.auth, serviceauth.User{Email: email, Sub: "sub-" + email}),
	}
}

func (f *snapshotFixture) request(
	t *testing.T,
	method, path, body, email string,
	wantStatus int,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = snapshotHostname
	if email != "" {
		req.AddCookie(f.cookie(t, email))
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s as %q = %d, want %d; body=%s",
			method, path, email, rec.Code, wantStatus, rec.Body.String())
	}
	return rec
}

// takeSnapshot creates one snapshot and waits for its archive to exist, which
// is what makes it restorable.
func (f *snapshotFixture) takeSnapshot(t *testing.T) servicesnapshot.Snapshot {
	t.Helper()
	rec := f.request(
		t, http.MethodPost, "/api/projects/"+string(f.project.ID)+"/snapshots",
		`{"label":"baseline"}`, snapshotMember, http.StatusAccepted,
	)
	var record servicesnapshot.Snapshot
	if err := json.NewDecoder(rec.Body).Decode(&record); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	f.snapshots.Wait()

	stored, err := f.snapshots.List(context.Background(), f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Status != servicesnapshot.StatusReady {
		t.Fatalf("stored snapshots = %+v, want one ready record", stored)
	}
	return stored[0]
}

func (f *snapshotFixture) listProjects(t *testing.T, email string) []serviceproject.Meta {
	t.Helper()
	rec := f.request(t, http.MethodGet, "/api/projects", "", email, http.StatusOK)
	var out []serviceproject.Meta
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	return out
}

func (f *snapshotFixture) listTrash(t *testing.T, email string) []trashedProjectResponse {
	t.Helper()
	rec := f.request(t, http.MethodGet, "/api/projects/trash", "", email, http.StatusOK)
	var out []trashedProjectResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode trash: %v", err)
	}
	return out
}
