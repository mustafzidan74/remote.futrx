package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileauth"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileproject"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectaccess"
)

// healthTestCatalog reports every project as running so one sweep populates
// the monitor's cache without LXD.
type healthTestCatalog struct {
	metas []serviceproject.Meta
}

func (c *healthTestCatalog) List(context.Context) ([]serviceproject.Meta, error) {
	return c.metas, nil
}

type healthTestVitals struct{}

func (healthTestVitals) Vitals(
	_ context.Context,
	name string,
) (serviceproject.ContainerInspect, error) {
	return serviceproject.ContainerInspect{
		Name:  name,
		State: serviceproject.ContainerStateRunning,
		Resources: &serviceproject.ResourceInfo{
			MemoryCurrentBytes: 1 << 29,
			MemoryTotalBytes:   1 << 30,
		},
	}, nil
}

func newHealthTestHandler(t *testing.T) (*http.ServeMux, *serviceauth.Service, map[string]string) {
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

	running := func(meta serviceproject.Meta) serviceproject.Meta {
		meta.Status = serviceproject.StatusRunning
		if meta.ContainerName == "" {
			meta.ContainerName = meta.Slug
		}
		return meta
	}
	health := servicehealth.New(servicehealth.Dependencies{
		Projects: &healthTestCatalog{metas: []serviceproject.Meta{
			running(adminProject), running(memberProject),
		}},
		Vitals: healthTestVitals{},
	})
	health.Check(ctx)

	mux := http.NewServeMux()
	NewProjectHealthHandler(projects, health, auth).RegisterRoutes(mux)
	return mux, auth, map[string]string{
		"admin":  string(adminProject.ID),
		"member": string(memberProject.ID),
	}
}

func healthRequest(
	t *testing.T,
	mux *http.ServeMux,
	auth *serviceauth.Service,
	method, email string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/projects/health", nil)
	if email != "" {
		request.AddCookie(&http.Cookie{
			Name:  serviceauth.SessionCookieName,
			Value: auth.SignSession(serviceauth.User{Email: email, Sub: "sub-" + email}),
		})
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func decodeHealth(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Enabled    bool                          `json:"enabled"`
	IntervalMs int64                         `json:"intervalMs"`
	Projects   []servicehealth.ProjectHealth `json:"projects"`
} {
	t.Helper()
	var out struct {
		Enabled    bool                          `json:"enabled"`
		IntervalMs int64                         `json:"intervalMs"`
		Projects   []servicehealth.ProjectHealth `json:"projects"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&out); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	return out
}

func TestProjectHealthRequiresASession(t *testing.T) {
	mux, auth, _ := newHealthTestHandler(t)

	if recorder := healthRequest(t, mux, auth, http.MethodGet, ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status without a session = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestProjectHealthRejectsNonGetMethods(t *testing.T) {
	mux, auth, _ := newHealthTestHandler(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		recorder := healthRequest(t, mux, auth, method, "admin@example.com")
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s = %d, want %d", method, recorder.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestProjectHealthReportsOnlyVisibleProjects(t *testing.T) {
	mux, auth, ids := newHealthTestHandler(t)

	tests := []struct {
		name  string
		email string
		want  []string
	}{
		{name: "an admin sees the whole fleet", email: "admin@example.com", want: []string{ids["admin"], ids["member"]}},
		{name: "a member sees only their project", email: "member@example.com", want: []string{ids["member"]}},
		{name: "an outsider sees nothing", email: "outsider@example.com", want: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := healthRequest(t, mux, auth, http.MethodGet, test.email)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
			}
			body := decodeHealth(t, recorder)
			if !body.Enabled {
				t.Fatal("enabled = false, want the monitor reported as on")
			}
			got := make([]string, 0, len(body.Projects))
			for _, row := range body.Projects {
				got = append(got, row.ProjectID)
				if row.Status != servicehealth.StatusOK {
					t.Fatalf("status for %s = %q, want %q", row.ProjectID, row.Status, servicehealth.StatusOK)
				}
			}
			if len(got) != len(test.want) {
				t.Fatalf("projects = %v, want %v", got, test.want)
			}
			for _, want := range test.want {
				found := false
				for _, id := range got {
					if id == want {
						found = true
					}
				}
				if !found {
					t.Fatalf("projects = %v, want it to contain %q", got, want)
				}
			}
		})
	}
}
