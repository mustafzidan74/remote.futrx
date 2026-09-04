package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicedashboard "github.com/futrx-com/remote.futrx.com/internal/service/dashboard"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileauth"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileproject"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectaccess"
)

// newDashboardHandlerWithCaller is the test seam for the cases that are about
// the route rather than about who is asking.
func newDashboardHandlerWithCaller(
	dashboard DashboardService,
	caller CallerResolver,
) *DashboardHandler {
	return &DashboardHandler{dashboard: dashboard, caller: caller}
}

// newDashboardTestMux wires the real project service, the real access store
// and the real aggregator behind the route, so the membership assertions
// exercise the same filter GET /api/projects uses rather than a stub of it.
func newDashboardTestMux(t *testing.T) (*http.ServeMux, *serviceauth.Service, map[string]string) {
	t.Helper()

	auth, err := serviceauth.New(
		context.Background(),
		fileauth.New(t.TempDir()),
		auditTestDirectory{admins: map[string]bool{
			"admin@example.com":    true,
			"member@example.com":   false,
			"outsider@example.com": false,
		}},
		func(string, string, string) serviceauth.OAuthProvider { return auditTestOAuth{} },
		"https://remote.example.com",
		[]byte("test-session-key"),
		twoFactorStoreForTest(t),
		sessionRegistryStoreForTest(t),
		serviceauth.DefaultOptions(),
	)
	if err != nil {
		t.Fatalf("New auth service: %v", err)
	}

	repo, err := fileproject.NewWithWorkspaceRoot(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	access, err := fileprojectaccess.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	projects := serviceproject.New(repo, serviceproject.ContainerDependencies{}, nil, access)

	ctx := context.Background()
	adminProject, err := projects.Create(
		ctx, serviceproject.CreateInput{Name: "Admin Only"}, "admin@example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	memberProject, err := projects.Create(
		ctx, serviceproject.CreateInput{Name: "Member Project"}, "member@example.com",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Every other source is deliberately absent: this test is about who the
	// dashboard is allowed to describe, not about what it says.
	dashboard := servicedashboard.New(servicedashboard.Dependencies{
		Projects:       projects,
		TrashRetention: 7 * 24 * time.Hour,
	})

	mux := http.NewServeMux()
	NewDashboardHandler(dashboard, auth).RegisterRoutes(mux)
	return mux, auth, map[string]string{
		"admin":  string(adminProject.ID),
		"member": string(memberProject.ID),
	}
}

func dashboardRequest(
	t *testing.T,
	mux *http.ServeMux,
	auth *serviceauth.Service,
	method, email string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/dashboard", nil)
	if email != "" {
		request.AddCookie(&http.Cookie{
			Name:  serviceauth.SessionCookieName,
			Value: issueTestSession(t, auth, serviceauth.User{Email: email, Sub: "sub-" + email}),
		})
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func decodeDashboard(t *testing.T, recorder *httptest.ResponseRecorder) servicedashboard.Snapshot {
	t.Helper()
	var snapshot servicedashboard.Snapshot
	if err := json.NewDecoder(recorder.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	return snapshot
}

func TestDashboardRequiresASession(t *testing.T) {
	mux, auth, _ := newDashboardTestMux(t)

	recorder := dashboardRequest(t, mux, auth, http.MethodGet, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status without a session = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDashboardRejectsNonGetMethods(t *testing.T) {
	mux, auth, _ := newDashboardTestMux(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		recorder := dashboardRequest(t, mux, auth, method, "admin@example.com")
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s = %d, want %d", method, recorder.Code, http.StatusMethodNotAllowed)
		}
	}
}

// The home screen is the one page that names everything at once, so it is the
// page where a leak would be widest.
func TestDashboardDescribesOnlyVisibleProjects(t *testing.T) {
	mux, auth, ids := newDashboardTestMux(t)

	tests := []struct {
		name  string
		email string
		want  []string
	}{
		{
			name:  "an admin sees the whole fleet",
			email: "admin@example.com",
			want:  []string{ids["admin"], ids["member"]},
		},
		{
			name:  "a member sees only their project",
			email: "member@example.com",
			want:  []string{ids["member"]},
		},
		{
			name:  "an outsider sees nothing",
			email: "outsider@example.com",
			want:  nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := dashboardRequest(t, mux, auth, http.MethodGet, test.email)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
			}
			snapshot := decodeDashboard(t, recorder)

			got := make(map[string]bool, len(snapshot.Projects))
			for _, project := range snapshot.Projects {
				got[project.ID] = true
			}
			if len(got) != len(test.want) {
				t.Fatalf("projects = %v, want %v", got, test.want)
			}
			for _, want := range test.want {
				if !got[want] {
					t.Fatalf("projects = %v, want it to contain %q", got, want)
				}
			}
			if snapshot.KPIs.TotalProjects != len(test.want) {
				t.Fatalf("kpis.totalProjects = %d, want %d — the tile must count the same fleet the cards show",
					snapshot.KPIs.TotalProjects, len(test.want))
			}
		})
	}
}

func TestDashboardReportsUnavailableWithoutAnAggregator(t *testing.T) {
	mux := http.NewServeMux()
	newDashboardHandlerWithCaller(nil, stubCaller{email: "admin@example.com", isAdmin: true}).
		RegisterRoutes(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

// /api/dashboard must not be swallowed by any broader /api/... prefix pattern
// registered alongside it.
func TestDashboardRouteResolvesToItsOwnHandler(t *testing.T) {
	mux, _, _ := newDashboardTestMux(t)
	NewProjectHandler(nil, nil, nil, "example.com").RegisterRoutes(mux)

	_, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if pattern != "/api/dashboard" {
		t.Fatalf("/api/dashboard resolved to pattern %q", pattern)
	}
}
