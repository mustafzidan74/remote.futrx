package globalsecrets

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// memoryStore is the in-process stand-in for the file-backed vault. It copies
// on read and write so a test cannot accidentally mutate stored state through
// a slice it was handed.
type memoryStore struct {
	mu      sync.Mutex
	secrets []Secret
	loadErr error
	saveErr error
}

func (s *memoryStore) Load(context.Context) ([]Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return append([]Secret(nil), s.secrets...), nil
}

func (s *memoryStore) Save(_ context.Context, secrets []Secret) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.secrets = append([]Secret(nil), secrets...)
	return nil
}

type fakeEnvironment struct {
	mu    sync.Mutex
	calls []envCall
	err   error
}

type envCall struct {
	container string
	set       map[string]string
	unset     []string
}

func (f *fakeEnvironment) ApplyDiff(_ context.Context, container string, set map[string]string, unset []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, envCall{container: container, set: set, unset: unset})
	return f.err
}

type fakeMaterializer struct {
	mu        sync.Mutex
	manifests map[string]Manifest
	applied   []applyCall
	applyErr  error
}

type applyCall struct {
	container string
	material  Material
	stale     []string
}

func (f *fakeMaterializer) Manifest(_ context.Context, container string) (Manifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.manifests[container], nil
}

func (f *fakeMaterializer) Apply(_ context.Context, container string, material Material, stale []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = append(f.applied, applyCall{container: container, material: material, stale: stale})
	if f.manifests == nil {
		f.manifests = map[string]Manifest{}
	}
	f.manifests[container] = ManifestFor(material)
	return nil
}

type fakeProjects struct {
	targets []SecretTarget
	err     error
}

func (f fakeProjects) SecretTargets(context.Context) ([]SecretTarget, error) {
	return f.targets, f.err
}

type fakeProber struct {
	result TestResult
	err    error
	seen   []SSHTarget
}

func (f *fakeProber) Probe(_ context.Context, target SSHTarget) (TestResult, error) {
	f.seen = append(f.seen, target)
	return f.result, f.err
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1700000000, 0).UTC() }
}

func TestServiceRoundTripsThroughTheStoreAndMasksOnRead(t *testing.T) {
	store := &memoryStore{}
	service := New(store, WithClock(fixedClock()))
	ctx := context.Background()

	created, err := service.Create(ctx, Input{
		Key:         "GITHUB_TOKEN",
		Kind:        KindEnv,
		Value:       "ghp_supersecret1234",
		Scope:       Scope{All: true},
		Description: "  gh CLI auth  ",
	}, "Admin@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	if created.Masked != "••••••••1234" || !created.HasValue {
		t.Fatalf("create returned %+v", created)
	}
	if created.UpdatedBy != "admin@example.com" {
		t.Fatalf("actor = %q", created.UpdatedBy)
	}
	if created.Description != "gh CLI auth" {
		t.Fatalf("description = %q", created.Description)
	}
	if created.UpdatedAt != time.Unix(1700000000, 0).UnixMilli() {
		t.Fatalf("updatedAt = %d", created.UpdatedAt)
	}

	if store.secrets[0].Value != "ghp_supersecret1234" {
		t.Fatal("the plaintext value must reach the store")
	}

	list, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if strings.Contains(list[0].Masked, "supersecret") {
		t.Fatalf("a read leaked the value: %q", list[0].Masked)
	}
}

func TestServiceKeepsStoredValueUnlessClearedOrReplaced(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		input Input
		want  string
	}{
		{
			name:  "a blank value keeps the stored one",
			input: Input{Kind: KindEnv, Scope: Scope{All: true}, Description: "renamed"},
			want:  "ghp_original",
		},
		{
			name:  "a submitted value replaces it",
			input: Input{Kind: KindEnv, Scope: Scope{All: true}, Value: "ghp_rotated"},
			want:  "ghp_rotated",
		},
		{
			name:  "an explicit clear removes it",
			input: Input{Kind: KindEnv, Scope: Scope{All: true}, Clear: true},
			want:  "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryStore{secrets: []Secret{{
				Key:   "GITHUB_TOKEN",
				Kind:  KindEnv,
				Value: "ghp_original",
				Scope: Scope{All: true},
			}}}
			service := New(store, WithClock(fixedClock()))
			view, err := service.Update(ctx, "GITHUB_TOKEN", test.input, "admin@example.com")
			if err != nil {
				t.Fatal(err)
			}
			if store.secrets[0].Value != test.want {
				t.Fatalf("stored value = %q, want %q", store.secrets[0].Value, test.want)
			}
			if view.HasValue != (test.want != "") {
				t.Fatalf("hasValue = %t for stored %q", view.HasValue, test.want)
			}
		})
	}
}

func TestServiceKeepsAnSSHPrivateKeyAcrossAMetadataEdit(t *testing.T) {
	store := &memoryStore{secrets: []Secret{{
		Key:   "HESTIA",
		Kind:  KindSSH,
		Scope: Scope{All: true},
		SSH: &SSHTarget{
			Name: "hestia", Host: "old.example.com", User: "root", Port: 22,
			PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----",
		},
	}}}
	service := New(store, WithClock(fixedClock()))

	if _, err := service.Update(context.Background(), "HESTIA", Input{
		Kind:  KindSSH,
		Scope: Scope{All: true},
		SSH:   &SSHTarget{Name: "hestia", Host: "new.example.com", User: "root", Port: 22},
	}, "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	stored := store.secrets[0]
	if stored.SSH.Host != "new.example.com" {
		t.Fatalf("host = %q", stored.SSH.Host)
	}
	if stored.SSH.PrivateKey != "-----BEGIN OPENSSH PRIVATE KEY-----" {
		t.Fatal("editing the host must not discard the stored key")
	}
}

func TestServiceRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		input Input
		want  error
	}{
		{
			name:  "empty key",
			input: Input{Kind: KindEnv, Scope: Scope{All: true}},
			want:  ErrInvalidKey,
		},
		{
			name:  "key that is not an env name",
			input: Input{Key: "my-key", Kind: KindEnv, Scope: Scope{All: true}},
			want:  ErrInvalidKey,
		},
		{
			name:  "unknown kind",
			input: Input{Key: "K", Kind: Kind("blob"), Scope: Scope{All: true}},
			want:  ErrInvalidKind,
		},
		{
			name:  "scope selecting nothing",
			input: Input{Key: "K", Kind: KindEnv, Scope: Scope{}},
			want:  ErrInvalidScope,
		},
		{
			name:  "multi-line environment value",
			input: Input{Key: "K", Kind: KindEnv, Scope: Scope{All: true}, Value: "a\nb"},
			want:  ErrMultilineEnvValue,
		},
		{
			name:  "file path outside the allowed roots",
			input: Input{Key: "K", Kind: KindFile, Scope: Scope{All: true}, Path: "/etc/passwd"},
			want:  ErrInvalidPath,
		},
		{
			name:  "file path escaping through ..",
			input: Input{Key: "K", Kind: KindFile, Scope: Scope{All: true}, Path: "/root/../etc/shadow"},
			want:  ErrInvalidPath,
		},
		{
			name:  "ssh entry without a target",
			input: Input{Key: "K", Kind: KindSSH, Scope: Scope{All: true}},
			want:  ErrInvalidSSHTarget,
		},
		{
			name: "ssh target with an unusable name",
			input: Input{Key: "K", Kind: KindSSH, Scope: Scope{All: true}, SSH: &SSHTarget{
				Name: "-bad name", Host: "h", User: "u",
			}},
			want: ErrInvalidSSHTarget,
		},
		{
			name: "ssh target with a port out of range",
			input: Input{Key: "K", Kind: KindSSH, Scope: Scope{All: true}, SSH: &SSHTarget{
				Name: "hestia", Host: "h", User: "u", Port: 70000,
			}},
			want: ErrInvalidSSHTarget,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := New(&memoryStore{}, WithClock(fixedClock()))
			_, err := service.Create(ctx, test.input, "admin@example.com")
			if !errors.Is(err, test.want) {
				t.Fatalf("Create() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestServiceRefusesToCreateOverAnExistingKeyOrUpdateAMissingOne(t *testing.T) {
	store := &memoryStore{secrets: []Secret{{Key: "TOKEN", Kind: KindEnv, Scope: Scope{All: true}}}}
	service := New(store, WithClock(fixedClock()))
	ctx := context.Background()

	_, err := service.Create(ctx, Input{Key: "TOKEN", Kind: KindEnv, Scope: Scope{All: true}}, "a@b.c")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("Create() error = %v, want ErrExists", err)
	}
	_, err = service.Update(ctx, "MISSING", Input{Kind: KindEnv, Scope: Scope{All: true}}, "a@b.c")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update() error = %v, want ErrNotFound", err)
	}
	if err := service.Delete(ctx, "MISSING"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestSyncContainerRemovesExactlyWhatThePreviousManifestOwned(t *testing.T) {
	store := &memoryStore{secrets: []Secret{{
		Key: "NPMRC", Kind: KindFile, Path: "/root/.npmrc",
		Value: "token", Scope: Scope{All: true},
	}}}
	environment := &fakeEnvironment{}
	materializer := &fakeMaterializer{manifests: map[string]Manifest{
		"proj": {
			Version: ManifestVersion,
			EnvKeys: []string{"OLD_TOKEN"},
			Files:   []string{"/root/.npmrc", "/root/.composer/auth.json"},
		},
	}}
	service := New(store, WithContainers(environment, materializer), WithClock(fixedClock()))

	if err := service.SyncContainer(context.Background(), "p1", "proj", nil); err != nil {
		t.Fatal(err)
	}
	if len(materializer.applied) != 1 {
		t.Fatalf("apply calls = %d", len(materializer.applied))
	}
	applied := materializer.applied[0]
	if !reflect.DeepEqual(applied.stale, []string{"/root/.composer/auth.json"}) {
		t.Fatalf("stale files = %v", applied.stale)
	}
	if len(applied.material.Files) != 1 || applied.material.Files[0].Path != "/root/.npmrc" {
		t.Fatalf("material = %+v", applied.material)
	}
	if len(environment.calls) != 1 {
		t.Fatalf("environment calls = %d", len(environment.calls))
	}
	if !reflect.DeepEqual(environment.calls[0].unset, []string{"OLD_TOKEN"}) {
		t.Fatalf("unset = %v", environment.calls[0].unset)
	}
}

func TestSyncContainerStripsEverythingWhenAProjectFallsOutOfScope(t *testing.T) {
	store := &memoryStore{}
	environment := &fakeEnvironment{}
	materializer := &fakeMaterializer{manifests: map[string]Manifest{
		"proj": {
			Version: ManifestVersion,
			EnvKeys: []string{"GITHUB_TOKEN", "SSH_TARGET_HESTIA_HOST"},
			Files:   []string{"/root/.ssh/hestia_key"},
			SSH:     []string{"hestia"},
		},
	}}
	service := New(store, WithContainers(environment, materializer), WithClock(fixedClock()))

	if err := service.SyncContainer(context.Background(), "p1", "proj", nil); err != nil {
		t.Fatal(err)
	}
	applied := materializer.applied[0]
	if !applied.material.Empty() {
		t.Fatalf("nothing should be materialized, got %+v", applied.material)
	}
	if !reflect.DeepEqual(applied.stale, []string{"/root/.ssh/hestia_key"}) {
		t.Fatalf("stale = %v", applied.stale)
	}
	want := []string{"GITHUB_TOKEN", "SSH_TARGET_HESTIA_HOST"}
	if !reflect.DeepEqual(environment.calls[0].unset, want) {
		t.Fatalf("unset = %v, want %v", environment.calls[0].unset, want)
	}
}

func TestSyncContainerIsANoOpForAnAlreadyConvergedContainer(t *testing.T) {
	store := &memoryStore{secrets: []Secret{{
		Key: "NPMRC", Kind: KindFile, Path: "/root/.npmrc", Value: "t", Scope: Scope{All: true},
	}}}
	materializer := &fakeMaterializer{}
	service := New(store, WithContainers(&fakeEnvironment{}, materializer), WithClock(fixedClock()))
	ctx := context.Background()

	if err := service.SyncContainer(ctx, "p1", "proj", nil); err != nil {
		t.Fatal(err)
	}
	if err := service.SyncContainer(ctx, "p1", "proj", nil); err != nil {
		t.Fatal(err)
	}
	if len(materializer.applied) != 2 {
		t.Fatalf("apply calls = %d", len(materializer.applied))
	}
	if len(materializer.applied[1].stale) != 0 {
		t.Fatalf("a converged container has nothing stale, got %v", materializer.applied[1].stale)
	}
	if !reflect.DeepEqual(materializer.applied[0].material, materializer.applied[1].material) {
		t.Fatal("regeneration is not stable across syncs")
	}
}

func TestChangingAVaultEntryResyncsOnlyRunningContainersInScope(t *testing.T) {
	store := &memoryStore{}
	materializer := &fakeMaterializer{}
	service := New(
		store,
		WithContainers(&fakeEnvironment{}, materializer),
		WithClock(fixedClock()),
		WithSynchronousResync(),
	)
	service.SetProjects(fakeProjects{targets: []SecretTarget{
		{ProjectID: "p1", ContainerName: "c1", Running: true},
		{ProjectID: "p2", ContainerName: "c2", Running: true},
		{ProjectID: "p3", ContainerName: "c3", Running: false},
		{ProjectID: "p4", ContainerName: "", Running: true},
	}})

	if _, err := service.Create(context.Background(), Input{
		Key:   "ACF_LICENCE",
		Kind:  KindEnv,
		Value: "k",
		Scope: Scope{ProjectIDs: []string{"p1", "p3", "p4"}},
	}, "admin@example.com"); err != nil {
		t.Fatal(err)
	}

	if len(materializer.applied) != 1 {
		t.Fatalf("expected only the running, in-scope container to sync, got %+v", materializer.applied)
	}
	if materializer.applied[0].container != "c1" {
		t.Fatalf("synced %q", materializer.applied[0].container)
	}
}

func TestNarrowingAScopeStillReachesTheProjectThatLostTheEntry(t *testing.T) {
	store := &memoryStore{secrets: []Secret{{
		Key: "ACF_LICENCE", Kind: KindEnv, Value: "k",
		Scope: Scope{ProjectIDs: []string{"p1", "p2"}},
	}}}
	materializer := &fakeMaterializer{}
	service := New(
		store,
		WithContainers(&fakeEnvironment{}, materializer),
		WithClock(fixedClock()),
		WithSynchronousResync(),
	)
	service.SetProjects(fakeProjects{targets: []SecretTarget{
		{ProjectID: "p1", ContainerName: "c1", Running: true},
		{ProjectID: "p2", ContainerName: "c2", Running: true},
	}})

	if _, err := service.Update(context.Background(), "ACF_LICENCE", Input{
		Kind:  KindEnv,
		Scope: Scope{ProjectIDs: []string{"p1"}},
	}, "admin@example.com"); err != nil {
		t.Fatal(err)
	}

	synced := map[string]bool{}
	for _, call := range materializer.applied {
		synced[call.container] = true
	}
	if !synced["c1"] || !synced["c2"] {
		t.Fatalf("both the kept and the dropped project must resync, got %v", synced)
	}
}

func TestListReportsWhichProjectsShadowAnEnvironmentEntry(t *testing.T) {
	store := &memoryStore{secrets: []Secret{{
		Key: "GITHUB_TOKEN", Kind: KindEnv, Value: "ghp_x", Scope: Scope{All: true},
	}}}
	service := New(store, WithClock(fixedClock()))
	service.SetProjects(fakeProjects{targets: []SecretTarget{
		{ProjectID: "p1", ContainerName: "c1", Running: true, OwnKeys: []string{"GITHUB_TOKEN"}},
		{ProjectID: "p2", ContainerName: "c2", Running: true},
	}})

	list, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list[0].ShadowedIn, []string{"p1"}) {
		t.Fatalf("shadowedIn = %v", list[0].ShadowedIn)
	}
}

func TestTestSSHProbesTheStoredTargetAndRefusesOtherKinds(t *testing.T) {
	prober := &fakeProber{result: TestResult{OK: true, Output: "ok", LatencyMS: 42}}
	store := &memoryStore{secrets: []Secret{
		{Key: "HESTIA", Kind: KindSSH, Scope: Scope{All: true}, SSH: &SSHTarget{
			Name: "hestia", Host: "h", User: "u", Port: 2222, PrivateKey: "key",
		}},
		{Key: "NO_KEY", Kind: KindSSH, Scope: Scope{All: true}, SSH: &SSHTarget{
			Name: "nokey", Host: "h", User: "u",
		}},
		{Key: "GITHUB_TOKEN", Kind: KindEnv, Value: "x", Scope: Scope{All: true}},
	}}
	service := New(store, WithSSHProber(prober), WithClock(fixedClock()))
	ctx := context.Background()

	result, err := service.TestSSH(ctx, "HESTIA")
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.LatencyMS != 42 {
		t.Fatalf("result = %+v", result)
	}
	if len(prober.seen) != 1 || prober.seen[0].Port != 2222 {
		t.Fatalf("probed %+v", prober.seen)
	}
	if _, err := service.TestSSH(ctx, "GITHUB_TOKEN"); !errors.Is(err, ErrWrongKind) {
		t.Fatalf("TestSSH(env) error = %v, want ErrWrongKind", err)
	}
	if _, err := service.TestSSH(ctx, "NO_KEY"); !errors.Is(err, ErrNoValue) {
		t.Fatalf("TestSSH(no key) error = %v, want ErrNoValue", err)
	}
	if _, err := service.TestSSH(ctx, "MISSING"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TestSSH(missing) error = %v, want ErrNotFound", err)
	}

	noProber := New(store, WithClock(fixedClock()))
	if _, err := noProber.TestSSH(ctx, "HESTIA"); !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("TestSSH without a prober = %v, want ErrProbeUnavailable", err)
	}
}

func TestServiceWithoutAStoreReportsUnavailable(t *testing.T) {
	service := New(nil)
	ctx := context.Background()
	if _, err := service.List(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("List() error = %v", err)
	}
	if _, err := service.Create(ctx, Input{Key: "K", Kind: KindEnv, Scope: Scope{All: true}}, "a@b.c"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Create() error = %v", err)
	}
	if err := service.Delete(ctx, "K"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Delete() error = %v", err)
	}
}
