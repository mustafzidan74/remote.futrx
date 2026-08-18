package project

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// secretsTestRepo is the project store for the vault-merge tests: enough of a
// repository to resolve one project and no more.
type secretsTestRepo struct {
	mu   sync.Mutex
	meta Meta
}

func (r *secretsTestRepo) List(context.Context) ([]Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []Meta{r.meta}, nil
}

func (r *secretsTestRepo) Create(_ context.Context, meta Meta) (Meta, error) { return meta, nil }

func (r *secretsTestRepo) Get(context.Context, ID) (Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.meta, nil
}

func (r *secretsTestRepo) GetBySlug(ctx context.Context, _ string) (Meta, error) {
	return r.Get(ctx, r.meta.ID)
}

func (r *secretsTestRepo) Update(_ context.Context, _ ID, fn func(*Meta)) (Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.meta)
	return r.meta, nil
}

func (r *secretsTestRepo) SetStatus(_ context.Context, _ ID, status Status, errMsg string) (Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta.Status = status
	r.meta.ErrorMsg = errMsg
	return r.meta, nil
}

func (r *secretsTestRepo) Delete(context.Context, ID) error { return nil }

type secretsTestStore struct {
	mu      sync.Mutex
	secrets []Secret
}

func (s *secretsTestStore) List(context.Context, ID) ([]Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Secret(nil), s.secrets...), nil
}

func (s *secretsTestStore) Set(_ context.Context, _ ID, key, value string) (Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.secrets {
		if s.secrets[index].Key == key {
			s.secrets[index].Value = value
			return s.secrets[index], nil
		}
	}
	secret := Secret{Key: key, Value: value}
	s.secrets = append(s.secrets, secret)
	return secret, nil
}

func (s *secretsTestStore) Delete(_ context.Context, _ ID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.secrets {
		if s.secrets[index].Key == key {
			s.secrets = append(s.secrets[:index], s.secrets[index+1:]...)
			return nil
		}
	}
	return nil
}

func (s *secretsTestStore) DeleteAll(context.Context, ID) error { return nil }

type secretsTestEnvironment struct {
	mu    sync.Mutex
	calls []map[string]string
}

func (e *secretsTestEnvironment) ApplyDiff(_ context.Context, _ string, set map[string]string, _ []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, set)
	return nil
}

type secretsTestVault struct {
	mu        sync.Mutex
	syncCalls []vaultSyncCall
	inherited []InheritedSecret
	syncErr   error
	listErr   error
}

type vaultSyncCall struct {
	projectID string
	container string
	ownKeys   []string
}

func (v *secretsTestVault) SyncContainer(_ context.Context, projectID, container string, ownKeys []string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.syncCalls = append(v.syncCalls, vaultSyncCall{
		projectID: projectID,
		container: container,
		ownKeys:   append([]string(nil), ownKeys...),
	})
	return v.syncErr
}

func (v *secretsTestVault) InheritedForProject(context.Context, string, []string) ([]InheritedSecret, error) {
	if v.listErr != nil {
		return nil, v.listErr
	}
	return v.inherited, nil
}

func newSecretsService(
	store SecretsRepository,
	environment ContainerEnvironment,
	vault GlobalSecrets,
) (*Service, *secretsTestRepo) {
	repo := &secretsTestRepo{meta: Meta{
		ID:            ID("aa11bb22"),
		Name:          "demo",
		ContainerName: "demo",
		Status:        StatusRunning,
	}}
	options := []Option{}
	if vault != nil {
		options = append(options, WithGlobalSecrets(vault))
	}
	service := New(
		repo,
		ContainerDependencies{Environment: environment},
		store,
		nil,
		options...,
	)
	return service, repo
}

func TestSecretsViewReportsWhatTheContainerAlsoInheritsFromTheVault(t *testing.T) {
	store := &secretsTestStore{secrets: []Secret{{Key: "GITHUB_TOKEN", Value: "project"}}}
	vault := &secretsTestVault{inherited: []InheritedSecret{
		{Key: "GITHUB_TOKEN", Kind: "env", Source: "global", Shadowed: true},
		{Key: "NPMRC", Kind: "file", Source: "global", Path: "/root/.npmrc"},
	}}
	service, repo := newSecretsService(store, nil, vault)

	view, err := service.SecretsView(context.Background(), repo.meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Secrets) != 1 || view.Secrets[0].Key != "GITHUB_TOKEN" {
		t.Fatalf("secrets = %+v", view.Secrets)
	}
	if len(view.Inherited) != 2 {
		t.Fatalf("inherited = %+v", view.Inherited)
	}
	if !view.Inherited[0].Shadowed {
		t.Fatal("an overridden vault entry must be reported as shadowed")
	}
}

func TestSecretsViewStillServesTheProjectWhenTheVaultIsAbsentOrBroken(t *testing.T) {
	store := &secretsTestStore{secrets: []Secret{{Key: "A", Value: "1"}}}

	t.Run("no vault attached", func(t *testing.T) {
		service, repo := newSecretsService(store, nil, nil)
		view, err := service.SecretsView(context.Background(), repo.meta.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Secrets) != 1 || len(view.Inherited) != 0 {
			t.Fatalf("view = %+v", view)
		}
	})

	t.Run("vault read fails", func(t *testing.T) {
		vault := &secretsTestVault{listErr: errors.New("vault unavailable")}
		service, repo := newSecretsService(store, nil, vault)
		view, err := service.SecretsView(context.Background(), repo.meta.ID)
		if err != nil {
			t.Fatalf("a vault failure must not break the project's own page: %v", err)
		}
		if len(view.Secrets) != 1 || len(view.Inherited) != 0 {
			t.Fatalf("view = %+v", view)
		}
	})
}

func TestSyncContainerEnvRunsTheVaultBeforeTheProjectSoTheProjectWins(t *testing.T) {
	store := &secretsTestStore{secrets: []Secret{
		{Key: "GITHUB_TOKEN", Value: "project-token"},
		{Key: "OTHER", Value: "x"},
	}}
	environment := &secretsTestEnvironment{}
	vault := &secretsTestVault{}
	service, repo := newSecretsService(store, environment, vault)

	if err := service.syncContainerEnv(context.Background(), repo.meta.ID, "demo"); err != nil {
		t.Fatal(err)
	}

	if len(vault.syncCalls) != 1 {
		t.Fatalf("vault sync calls = %d", len(vault.syncCalls))
	}
	call := vault.syncCalls[0]
	if call.container != "demo" || call.projectID != "aa11bb22" {
		t.Fatalf("vault synced %+v", call)
	}
	if !reflect.DeepEqual(call.ownKeys, []string{"GITHUB_TOKEN", "OTHER"}) {
		t.Fatalf("vault was told own keys %v", call.ownKeys)
	}
	if len(environment.calls) != 1 {
		t.Fatalf("environment calls = %d", len(environment.calls))
	}
	if environment.calls[0]["GITHUB_TOKEN"] != "project-token" {
		t.Fatalf("project env = %v", environment.calls[0])
	}
}

func TestSyncContainerEnvStillConvergesTheVaultForAProjectWithNoSecrets(t *testing.T) {
	environment := &secretsTestEnvironment{}
	vault := &secretsTestVault{}
	service, repo := newSecretsService(&secretsTestStore{}, environment, vault)

	if err := service.syncContainerEnv(context.Background(), repo.meta.ID, "demo"); err != nil {
		t.Fatal(err)
	}
	if len(vault.syncCalls) != 1 {
		t.Fatalf("a project without secrets must still inherit: %d calls", len(vault.syncCalls))
	}
	if len(environment.calls) != 0 {
		t.Fatalf("nothing project-local to push, got %v", environment.calls)
	}
}

func TestSyncContainerEnvIsSkippedWithoutAContainer(t *testing.T) {
	vault := &secretsTestVault{}
	service, repo := newSecretsService(&secretsTestStore{}, &secretsTestEnvironment{}, vault)

	if err := service.syncContainerEnv(context.Background(), repo.meta.ID, ""); err != nil {
		t.Fatal(err)
	}
	if len(vault.syncCalls) != 0 {
		t.Fatalf("a project without a container has nothing to sync: %+v", vault.syncCalls)
	}
}

func TestSettingOrRemovingAProjectSecretReconvergesTheVault(t *testing.T) {
	store := &secretsTestStore{}
	vault := &secretsTestVault{}
	service, repo := newSecretsService(store, &secretsTestEnvironment{}, vault)
	ctx := context.Background()

	if _, err := service.SetSecret(ctx, repo.meta.ID, "GITHUB_TOKEN", "project-token"); err != nil {
		t.Fatal(err)
	}
	if len(vault.syncCalls) != 1 {
		t.Fatalf("setting a secret must reconverge the vault: %d calls", len(vault.syncCalls))
	}
	if !reflect.DeepEqual(vault.syncCalls[0].ownKeys, []string{"GITHUB_TOKEN"}) {
		t.Fatalf("own keys = %v", vault.syncCalls[0].ownKeys)
	}

	if err := service.DeleteSecret(ctx, repo.meta.ID, "GITHUB_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if len(vault.syncCalls) != 2 {
		t.Fatalf("removing a secret must reconverge the vault: %d calls", len(vault.syncCalls))
	}
	if len(vault.syncCalls[1].ownKeys) != 0 {
		t.Fatalf("the removed key must no longer shadow: %v", vault.syncCalls[1].ownKeys)
	}
}

func TestSecretSyncTargetsDescribeTheFleetTheVaultCanPushTo(t *testing.T) {
	store := &secretsTestStore{secrets: []Secret{{Key: "A", Value: "1"}}}
	service, _ := newSecretsService(store, nil, &secretsTestVault{})

	targets, err := service.SecretSyncTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %+v", targets)
	}
	if targets[0].ProjectID != "aa11bb22" || targets[0].ContainerName != "demo" {
		t.Fatalf("target = %+v", targets[0])
	}
	// No lifecycle port is wired, so the stored status is what decides.
	if !targets[0].Running {
		t.Fatalf("a running project must be reported as reachable: %+v", targets[0])
	}
	if !reflect.DeepEqual(targets[0].OwnKeys, []string{"A"}) {
		t.Fatalf("own keys = %v", targets[0].OwnKeys)
	}
}
