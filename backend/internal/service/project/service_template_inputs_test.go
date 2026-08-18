package project

import (
	"context"
	"errors"
	"testing"
)

// memorySecrets is the project secrets store, in memory. Create writes the
// template's secret inputs through it before the container is launched.
type memorySecrets struct {
	values  map[string]string
	written []string
}

func newMemorySecrets() *memorySecrets {
	return &memorySecrets{values: map[string]string{}}
}

func (m *memorySecrets) List(context.Context, ID) ([]Secret, error) {
	out := make([]Secret, 0, len(m.values))
	for key, value := range m.values {
		out = append(out, Secret{Key: key, Value: value})
	}
	return out, nil
}

func (m *memorySecrets) Set(_ context.Context, _ ID, key, value string) (Secret, error) {
	m.values[key] = value
	m.written = append(m.written, key)
	return Secret{Key: key, Value: value}, nil
}

func (m *memorySecrets) Delete(_ context.Context, _ ID, key string) error {
	delete(m.values, key)
	return nil
}

func (m *memorySecrets) DeleteAll(context.Context, ID) error {
	m.values = map[string]string{}
	return nil
}

func TestCreateSplitsTemplateInputsBetweenMetadataAndSecrets(t *testing.T) {
	repo := &startTestRepository{}
	secrets := newMemorySecrets()
	catalog := newTemplateCatalogStub("blank", "wordpress")
	catalog.inputs = TemplateInputValues{
		Values:  map[string]string{"siteTitle": "My Shop", "language": "ar"},
		Secrets: map[string]string{"WP_ADMIN_PASSWORD": "generated-one"},
	}
	service := New(repo, ContainerDependencies{Templates: catalog}, secrets, nil)

	meta, err := service.Create(context.Background(), CreateInput{
		Name:           "My Shop",
		Template:       "wordpress",
		TemplateInputs: map[string]any{"language": "ar"},
	}, "Owner@Example.com")
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	if meta.TemplateInputs["siteTitle"] != "My Shop" || meta.TemplateInputs["language"] != "ar" {
		t.Fatalf("TemplateInputs = %v", meta.TemplateInputs)
	}
	if _, leaked := meta.TemplateInputs["WP_ADMIN_PASSWORD"]; leaked {
		t.Fatal("a secret input was persisted in project metadata")
	}
	if repo.meta.TemplateInputs["siteTitle"] != "My Shop" {
		t.Fatalf("the repository stored %v", repo.meta.TemplateInputs)
	}
	if secrets.values["WP_ADMIN_PASSWORD"] != "generated-one" {
		t.Fatalf("secrets = %v, want the admin password stored", secrets.values)
	}
	// The creating user's email is what an adminEmail input defaults to, so
	// the normalized address has to reach the catalog.
	want := "wordpress My Shop owner@example.com"
	if len(catalog.resolved) != 1 || catalog.resolved[0] != want {
		t.Fatalf("resolved = %q, want [%q]", catalog.resolved, want)
	}
}

func TestCreateRejectsInvalidTemplateInputsBeforeWritingAnything(t *testing.T) {
	repo := &startTestRepository{}
	secrets := newMemorySecrets()
	catalog := newTemplateCatalogStub("blank", "wordpress")
	catalog.resolveErr = ErrInvalidTemplateInput
	service := New(repo, ContainerDependencies{Templates: catalog}, secrets, nil)

	_, err := service.Create(context.Background(), CreateInput{
		Name:           "My Shop",
		Template:       "wordpress",
		TemplateInputs: map[string]any{"nope": "x"},
	}, "owner@example.com")

	if !errors.Is(err, ErrInvalidTemplateInput) {
		t.Fatalf("Create() error = %v, want ErrInvalidTemplateInput", err)
	}
	if repo.meta.Name != "" {
		t.Fatalf("a rejected create still persisted %#v", repo.meta)
	}
	if len(secrets.written) != 0 {
		t.Fatalf("a rejected create still wrote secrets: %q", secrets.written)
	}
}

func TestCreateWithoutACatalogRejectsAnyTemplateInput(t *testing.T) {
	service := New(&startTestRepository{}, ContainerDependencies{}, newMemorySecrets(), nil)

	_, err := service.Create(context.Background(), CreateInput{
		Name:           "P",
		TemplateInputs: map[string]any{"siteTitle": "x"},
	}, "")

	if !errors.Is(err, ErrInvalidTemplateInput) {
		t.Fatalf("Create() error = %v, want ErrInvalidTemplateInput", err)
	}
}

func TestCreateReportsAnUnusableSecretsStore(t *testing.T) {
	// A generated admin password that cannot be stored is unrecoverable: the
	// project would provision with a credential nobody can read back.
	repo := &startTestRepository{}
	catalog := newTemplateCatalogStub("blank", "wordpress")
	catalog.inputs = TemplateInputValues{
		Secrets: map[string]string{"WP_ADMIN_PASSWORD": "generated-one"},
	}
	service := New(repo, ContainerDependencies{Templates: catalog}, nil, nil)

	meta, err := service.Create(context.Background(), CreateInput{
		Name:           "My Shop",
		Template:       "wordpress",
		TemplateInputs: map[string]any{"adminPassword": ""},
	}, "owner@example.com")
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if meta.Status != StatusError || meta.ErrorMsg == "" {
		t.Fatalf("Create() = %#v, want the failure recorded on the project", meta)
	}
}
