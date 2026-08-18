package templates

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// fakeSecrets is the project secrets store as far as provisioning is
// concerned: a read-only lookup of one project's stored values.
type fakeSecrets struct {
	values map[string]string
	err    error
	reads  int
}

func (f *fakeSecrets) List(
	context.Context, serviceproject.ID,
) ([]serviceproject.Secret, error) {
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]serviceproject.Secret, 0, len(f.values))
	for key, value := range f.values {
		out = append(out, serviceproject.Secret{Key: key, Value: value})
	}
	return out, nil
}

func (f *fakeSecrets) Set(
	context.Context, serviceproject.ID, string, string,
) (serviceproject.Secret, error) {
	return serviceproject.Secret{}, errors.New("not used")
}

func (f *fakeSecrets) Delete(context.Context, serviceproject.ID, string) error { return nil }

func (f *fakeSecrets) DeleteAll(context.Context, serviceproject.ID) error { return nil }

// inputCatalog is a catalog whose one provisioning template collects inputs
// and installs into /workspace, like the shipped WordPress template.
func inputCatalog(t *testing.T) *Catalog {
	t.Helper()
	catalog, err := LoadFS(fstest.MapFS{
		"blank/template.json": &fstest.MapFile{
			Data: []byte(`{"name":"blank","title":"Blank","description":"d","icon":"blank"}`),
		},
		"stack/template.json": &fstest.MapFile{Data: []byte(`{
			"name":"stack","title":"Stack","description":"d","icon":"stack",
			"provisionScript":"provision.sh","defaultPorts":[8080],
			"workspaceMarker":"/workspace/site/config.php",
			"adminAccess":{"label":"Stack admin","port":8080,"path":"/admin",
				"userInput":"adminUser","passwordSecret":"WP_ADMIN_PASSWORD"},
			"inputs":[
				{"key":"siteTitle","label":"Site title","type":"text","required":true,
					"defaultFrom":"projectName"},
				{"key":"adminUser","label":"Admin user","type":"text","default":"admin"},
				{"key":"adminPassword","label":"Admin password","type":"password",
					"secret":true,"secretName":"WP_ADMIN_PASSWORD","generate":true},
				{"key":"language","label":"Language","type":"select","default":"ar",
					"options":[{"value":"ar","label":"AR"},{"value":"en_US","label":"EN"}]}
			]}`)},
		"stack/provision.sh": &fstest.MapFile{Data: []byte("echo install\n")},
	})
	if err != nil {
		t.Fatalf("LoadFS() = %v", err)
	}
	return catalog
}

func inputProject() serviceproject.Meta {
	return serviceproject.Meta{
		ID:            "abcd1234",
		Name:          "My Shop",
		Slug:          "my-shop",
		ContainerName: "my-shop",
		Template:      "stack",
		TemplateInputs: map[string]string{
			"siteTitle": "My Shop",
			"adminUser": "admin",
			"language":  "ar",
		},
	}
}

func TestEnsurePassesInputsAndSecretsAsEnvironment(t *testing.T) {
	runtime := newFakeRuntime()
	secrets := &fakeSecrets{values: map[string]string{
		"WP_ADMIN_PASSWORD": "s3cr3t",
		"UNRELATED":         "leave me alone",
	}}
	service := NewService(
		inputCatalog(t), runtime,
		WithSecrets(secrets),
		WithPreviewHost("remote.example.com"),
	)

	<-service.Ensure(context.Background(), inputProject())

	want := map[string]string{
		"TPL_SITE_TITLE":     "My Shop",
		"TPL_ADMIN_USER":     "admin",
		"TPL_ADMIN_PASSWORD": "s3cr3t",
		"TPL_LANGUAGE":       "ar",
		"TPL_PREVIEW_URL":    "https://my-shop--8080.dev.remote.example.com",
	}
	if len(runtime.env) != len(want) {
		t.Fatalf("env = %v, want %d entries", runtime.env, len(want))
	}
	for key, value := range want {
		if runtime.env[key] != value {
			t.Fatalf("env[%q] = %q, want %q", key, runtime.env[key], value)
		}
	}
	if secrets.reads != 1 {
		t.Fatalf("secrets read %d times, want exactly one per run", secrets.reads)
	}
}

func TestEnsureProvisionsWithoutSecretsWhenTheStoreFails(t *testing.T) {
	// A secrets outage must not block a container launch: the scripts treat
	// an empty secret as "skip the step that needs it".
	runtime := newFakeRuntime()
	service := NewService(
		inputCatalog(t), runtime,
		WithSecrets(&fakeSecrets{err: errors.New("disk gone")}),
	)

	<-service.Ensure(context.Background(), inputProject())

	if runtime.env["TPL_ADMIN_PASSWORD"] != "" {
		t.Fatalf("TPL_ADMIN_PASSWORD = %q, want empty", runtime.env["TPL_ADMIN_PASSWORD"])
	}
	if runtime.env["TPL_SITE_TITLE"] != "My Shop" {
		t.Fatalf("the non-secret inputs should still be passed: %v", runtime.env)
	}
}

func TestPreviewURLFallsBackToTheContainerAddress(t *testing.T) {
	tests := []struct {
		name string
		host string
		slug string
		want string
	}{
		{name: "public host", host: "remote.example.com", slug: "my-shop",
			want: "https://my-shop--8080.dev.remote.example.com"},
		{name: "no public host", slug: "my-shop", want: "http://localhost:8080"},
		{name: "no slug", host: "remote.example.com", want: "http://localhost:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(inputCatalog(t), newFakeRuntime(), WithPreviewHost(tt.host))
			template, _ := service.Catalog().Get("stack")
			if got := service.PreviewURL(tt.slug, template); got != tt.want {
				t.Fatalf("PreviewURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureReRunsWhenTheWorkspaceMarkerIsMissing(t *testing.T) {
	// A container launched from a pre-built template image carries the rootfs
	// marker, but its durable workspace is mounted over whatever the image
	// baked. The stack must be installed again into the real workspace.
	runtime := newFakeRuntime()
	runtime.setFile(MarkerPath, true)
	service := NewService(inputCatalog(t), runtime)

	<-service.Ensure(context.Background(), inputProject())

	ran := false
	for _, call := range runtime.recorded() {
		if strings.HasPrefix(call, "run ") {
			ran = true
		}
	}
	if !ran {
		t.Fatalf("provisioning was skipped over an empty workspace: %v", runtime.recorded())
	}
}

func TestEnsureSkipsWhenBothMarkersArePresent(t *testing.T) {
	runtime := newFakeRuntime()
	runtime.setFile(MarkerPath, true)
	runtime.setFile("/workspace/site/config.php", true)
	service := NewService(inputCatalog(t), runtime)

	<-service.Ensure(context.Background(), inputProject())

	for _, call := range runtime.recorded() {
		if strings.HasPrefix(call, "run ") {
			t.Fatalf("provisioning re-ran over a complete workspace: %v", runtime.recorded())
		}
	}
	status := service.TemplateStatus(context.Background(), inputProject())
	if status.Status != string(StatusDone) {
		t.Fatalf("Status = %q, want %q", status.Status, StatusDone)
	}
}

func TestTemplateStatusPointsAtTheAdminSignIn(t *testing.T) {
	runtime := newFakeRuntime()
	service := NewService(
		inputCatalog(t), runtime,
		WithSecrets(&fakeSecrets{values: map[string]string{"WP_ADMIN_PASSWORD": "s3cr3t"}}),
		WithPreviewHost("remote.example.com"),
	)
	ctx := context.Background()
	project := inputProject()
	<-service.Ensure(ctx, project)

	status := service.TemplateStatus(ctx, project)
	if status.Admin == nil {
		t.Fatal("TemplateStatus() carried no admin access")
	}
	if status.Admin.URL != "https://my-shop--8080.dev.remote.example.com/admin" {
		t.Fatalf("Admin.URL = %q", status.Admin.URL)
	}
	if status.Admin.User != "admin" || status.Admin.Label != "Stack admin" {
		t.Fatalf("Admin = %+v", status.Admin)
	}
	if status.Admin.PasswordSecret != "WP_ADMIN_PASSWORD" {
		t.Fatalf("Admin.PasswordSecret = %q", status.Admin.PasswordSecret)
	}
	// The status payload names the secret; it must never carry its value.
	if strings.Contains(status.Admin.URL+status.Admin.User+status.Admin.PasswordSecret, "s3cr3t") {
		t.Fatal("the admin password leaked into the status payload")
	}
}

func TestTemplateStatusOmitsAdminUntilProvisioningIsDone(t *testing.T) {
	runtime := newFakeRuntime()
	service := NewService(inputCatalog(t), runtime, WithPreviewHost("remote.example.com"))

	status := service.TemplateStatus(context.Background(), inputProject())

	if status.Status != string(StatusPending) {
		t.Fatalf("Status = %q, want %q", status.Status, StatusPending)
	}
	if status.Admin != nil {
		t.Fatalf("Admin = %+v, want nil while the site is still being built", status.Admin)
	}
}

func TestResolveTemplateInputsAdaptsToTheProjectPort(t *testing.T) {
	service := NewService(inputCatalog(t), newFakeRuntime())
	inputContext := serviceproject.TemplateInputContext{
		ProjectName: "My Shop", UserEmail: "owner@example.com",
	}

	resolved, err := service.ResolveTemplateInputs("stack", map[string]any{"language": "en_US"}, inputContext)
	if err != nil {
		t.Fatalf("ResolveTemplateInputs() = %v", err)
	}
	if resolved.Values["siteTitle"] != "My Shop" || resolved.Values["language"] != "en_US" {
		t.Fatalf("Values = %v", resolved.Values)
	}
	if len(resolved.Secrets["WP_ADMIN_PASSWORD"]) != generatedPasswordLength {
		t.Fatalf("Secrets = %v, want a generated admin password", resolved.Secrets)
	}

	_, err = service.ResolveTemplateInputs("stack", map[string]any{"nope": "x"}, inputContext)
	if !errors.Is(err, serviceproject.ErrInvalidTemplateInput) {
		t.Fatalf("error = %v, want ErrInvalidTemplateInput", err)
	}
	if strings.Contains(err.Error(), "invalid template input: invalid template input") {
		t.Fatalf("error = %q, want the sentinel stated once", err)
	}

	if _, err := service.ResolveTemplateInputs("gone", nil, inputContext); !errors.Is(
		err, serviceproject.ErrUnknownTemplate,
	) {
		t.Fatalf("error = %v, want ErrUnknownTemplate", err)
	}
}

func TestListPublishesTheInputDeclaration(t *testing.T) {
	service := NewService(inputCatalog(t), newFakeRuntime())

	for _, descriptor := range service.List(context.Background()) {
		switch descriptor.Name {
		case "blank":
			if len(descriptor.Inputs) != 0 {
				t.Fatalf("blank published inputs: %+v", descriptor.Inputs)
			}
		case "stack":
			if len(descriptor.Inputs) != 4 {
				t.Fatalf("stack published %d inputs, want 4", len(descriptor.Inputs))
			}
			if descriptor.Inputs[0].Key != "siteTitle" {
				t.Fatalf("inputs are not in declaration order: %+v", descriptor.Inputs)
			}
		}
	}
}
