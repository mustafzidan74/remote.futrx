package mcp

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

/* ------------------------------------------------------------------ *
 * Doubles
 * ------------------------------------------------------------------ */

type memoryStore struct {
	mu      sync.Mutex
	servers []Server
	loadErr error
}

func (s *memoryStore) Load(context.Context) ([]Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return append([]Server(nil), s.servers...), nil
}

func (s *memoryStore) Save(_ context.Context, servers []Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.servers = append([]Server(nil), servers...)
	return nil
}

type memoryProjectStore struct {
	mu   sync.Mutex
	docs map[string]ProjectSettings
}

func newProjectStore() *memoryProjectStore {
	return &memoryProjectStore{docs: map[string]ProjectSettings{}}
}

func (s *memoryProjectStore) Load(_ context.Context, projectID string) (ProjectSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.docs[projectID], nil
}

func (s *memoryProjectStore) Save(_ context.Context, projectID string, settings ProjectSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[projectID] = settings
	return nil
}

type recordingContainers struct {
	manifest Manifest
	applies  []Material
	stale    [][]string
	probeOut string
	probeErr error
	probed   []Server
}

func (c *recordingContainers) Manifest(context.Context, string) (Manifest, error) {
	return c.manifest, nil
}

func (c *recordingContainers) Apply(_ context.Context, _ string, material Material, stale []string) error {
	c.applies = append(c.applies, material)
	c.stale = append(c.stale, stale)
	c.manifest = ManifestFor(material)
	return nil
}

func (c *recordingContainers) Probe(_ context.Context, _ string, server Server) (string, error) {
	c.probed = append(c.probed, server)
	return c.probeOut, c.probeErr
}

type staticSecrets struct {
	values map[string]string
	asked  [][]string
}

func (s *staticSecrets) ValuesForProject(
	_ context.Context,
	_ string,
	keys []string,
) (map[string]string, error) {
	s.asked = append(s.asked, append([]string(nil), keys...))
	out := map[string]string{}
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

type staticProjects struct {
	target Target
	err    error
}

func (p staticProjects) MCPTarget(context.Context, string) (Target, error) {
	return p.target, p.err
}

func fixedClock() func() time.Time {
	now := time.UnixMilli(1_700_000_000_000)
	return func() time.Time {
		now = now.Add(time.Second)
		return now
	}
}

/* ------------------------------------------------------------------ *
 * Registry CRUD
 * ------------------------------------------------------------------ */

func TestCreateUpdateDeleteRoundTrip(t *testing.T) {
	store := &memoryStore{}
	service := New(store, newProjectStore(), WithClock(fixedClock()))
	ctx := context.Background()

	entry := Server{
		Name: "fetch", Transport: TransportStdio, Command: "uvx",
		Args: []string{"mcp-server-fetch"}, Scope: Scope{All: true},
	}
	view, err := service.Create(ctx, entry, "Admin@Example.com")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if view.UpdatedBy != "admin@example.com" || view.UpdatedAt == 0 {
		t.Fatalf("stamp = %q / %d", view.UpdatedBy, view.UpdatedAt)
	}

	if _, err := service.Create(ctx, entry, "admin@example.com"); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create() error = %v, want ErrExists", err)
	}

	entry.Description = "http fetching"
	if _, err := service.Update(ctx, "fetch", entry, "admin@example.com"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	list, err := service.List(ctx)
	if err != nil || len(list) != 1 || list[0].Description != "http fetching" {
		t.Fatalf("List() = %#v, err %v", list, err)
	}

	if _, err := service.Update(ctx, "missing", entry, "admin@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update(missing) error = %v", err)
	}
	if err := service.Delete(ctx, "fetch"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := service.Delete(ctx, "fetch"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete() error = %v", err)
	}
}

func TestRegistryIsUnavailableWithoutAStore(t *testing.T) {
	service := New(nil, nil)
	if _, err := service.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("List() error = %v, want ErrUnavailable", err)
	}
	if _, err := service.Create(context.Background(), Server{}, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Create() error = %v, want ErrUnavailable", err)
	}
}

/* ------------------------------------------------------------------ *
 * Project settings
 * ------------------------------------------------------------------ */

func TestProjectSettingsMergesPlatformScopeAndOverrides(t *testing.T) {
	store := &memoryStore{servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx", Scope: Scope{All: true}},
		{Name: "jira", Transport: TransportHTTP, URL: "https://j.example.com", Scope: Scope{ProjectIDs: []string{"other"}}},
	}}
	service := New(store, newProjectStore(), WithClock(fixedClock()))
	ctx := context.Background()

	view, err := service.SaveProjectSettings(ctx, "p1", ProjectInput{
		Disabled: []string{"fetch"},
		Servers: []Server{
			{Name: "shop", Transport: TransportHTTP, URL: "https://shop.example.com/mcp"},
		},
	}, "member@example.com")
	if err != nil {
		t.Fatalf("SaveProjectSettings() error = %v", err)
	}

	names := make([]string, 0, len(view.Available))
	for _, entry := range view.Available {
		names = append(names, entry.Name)
	}
	if !reflect.DeepEqual(names, []string{"fetch", "shop"}) {
		t.Fatalf("available = %v", names)
	}
	if view.Available[0].Enabled {
		t.Fatalf("fetch should be disabled for this project")
	}
	if !reflect.DeepEqual(view.SupportedProviders, SupportedProviders()) {
		t.Fatalf("supported = %v", view.SupportedProviders)
	}
	if !reflect.DeepEqual(view.UnsupportedProviders, UnsupportedProviders()) {
		t.Fatalf("unsupported = %v", view.UnsupportedProviders)
	}
}

func TestSaveProjectSettingsRejectsAnInvalidEntry(t *testing.T) {
	service := New(&memoryStore{}, newProjectStore(), WithClock(fixedClock()))
	_, err := service.SaveProjectSettings(context.Background(), "p1", ProjectInput{
		Servers: []Server{{Name: "bad name", Transport: TransportStdio, Command: "npx"}},
	}, "member@example.com")
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("error = %v, want ErrInvalidName", err)
	}
}

/* ------------------------------------------------------------------ *
 * Materialization
 * ------------------------------------------------------------------ */

func TestEnsureContainerWritesBothConfigsAndStampsTheProject(t *testing.T) {
	store := &memoryStore{servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx", Scope: Scope{All: true}},
	}}
	projects := newProjectStore()
	containers := &recordingContainers{}
	service := New(store, projects,
		WithContainers(containers),
		WithClock(fixedClock()),
	)

	configPath, err := service.EnsureContainer(context.Background(), "p1", "proj-p1")
	if err != nil {
		t.Fatalf("EnsureContainer() error = %v", err)
	}
	if configPath != ClaudeConfigPath {
		t.Fatalf("claude config path = %q", configPath)
	}
	if len(containers.applies) != 1 {
		t.Fatalf("applies = %d", len(containers.applies))
	}
	if !strings.Contains(containers.applies[0].CodexRegion, `[mcp_servers."fetch"]`) {
		t.Fatalf("codex region = %s", containers.applies[0].CodexRegion)
	}
	settings, _ := projects.Load(context.Background(), "p1")
	if settings.MaterializedAt == 0 || !reflect.DeepEqual(settings.MaterializedNames, []string{"fetch"}) {
		t.Fatalf("project stamp = %#v", settings)
	}
}

func TestEnsureContainerSkipsAnUnchangedContainer(t *testing.T) {
	store := &memoryStore{servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx", Scope: Scope{All: true}},
	}}
	containers := &recordingContainers{}
	service := New(store, newProjectStore(), WithContainers(containers), WithClock(fixedClock()))
	ctx := context.Background()

	if _, err := service.EnsureContainer(ctx, "p1", "proj-p1"); err != nil {
		t.Fatalf("first EnsureContainer() error = %v", err)
	}
	path, err := service.EnsureContainer(ctx, "p1", "proj-p1")
	if err != nil {
		t.Fatalf("second EnsureContainer() error = %v", err)
	}
	if len(containers.applies) != 1 {
		t.Fatalf("an unchanged registry was re-applied %d times", len(containers.applies))
	}
	if path != ClaudeConfigPath {
		t.Fatalf("the skipped pass lost the config path: %q", path)
	}
}

func TestEnsureContainerPrunesTheConfigOfADeletedEntry(t *testing.T) {
	store := &memoryStore{servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx", Scope: Scope{All: true}},
	}}
	containers := &recordingContainers{}
	service := New(store, newProjectStore(), WithContainers(containers), WithClock(fixedClock()))
	ctx := context.Background()

	if _, err := service.EnsureContainer(ctx, "p1", "proj-p1"); err != nil {
		t.Fatalf("EnsureContainer() error = %v", err)
	}
	if err := service.Delete(ctx, "fetch"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	path, err := service.EnsureContainer(ctx, "p1", "proj-p1")
	if err != nil {
		t.Fatalf("EnsureContainer() after delete error = %v", err)
	}
	if path != "" {
		t.Fatalf("claude was still handed a config path: %q", path)
	}
	if len(containers.applies) != 2 {
		t.Fatalf("applies = %d, want 2", len(containers.applies))
	}
	if !reflect.DeepEqual(containers.stale[1], []string{ClaudeConfigPath}) {
		t.Fatalf("stale = %v, want the claude config", containers.stale[1])
	}
	if containers.applies[1].CodexRegion != "" {
		t.Fatalf("the codex region survived a deletion: %q", containers.applies[1].CodexRegion)
	}
}

func TestEnsureContainerSubstitutesVaultValuesAndAsksOnlyForWhatItNeeds(t *testing.T) {
	store := &memoryStore{servers: []Server{
		{
			Name: "pg", Transport: TransportStdio, Command: "npx",
			Env:        map[string]string{"PGPASSWORD": "${PG_PASSWORD}"},
			SecretRefs: []string{"PG_PASSWORD"},
			Scope:      Scope{All: true},
		},
		{
			Name: "unused", Transport: TransportStdio, Command: "npx",
			Env:        map[string]string{"X": "${OTHER_KEY}"},
			SecretRefs: []string{"OTHER_KEY"},
			Scope:      Scope{ProjectIDs: []string{"somewhere-else"}},
		},
	}}
	containers := &recordingContainers{}
	secrets := &staticSecrets{values: map[string]string{"PG_PASSWORD": "s3cr3t", "OTHER_KEY": "nope"}}
	service := New(store, newProjectStore(),
		WithContainers(containers),
		WithSecrets(secrets),
		WithClock(fixedClock()),
	)

	if _, err := service.EnsureContainer(context.Background(), "p1", "proj-p1"); err != nil {
		t.Fatalf("EnsureContainer() error = %v", err)
	}
	if !reflect.DeepEqual(secrets.asked, [][]string{{"PG_PASSWORD"}}) {
		t.Fatalf("asked the vault for %v", secrets.asked)
	}
	material := containers.applies[0]
	if !strings.Contains(material.CodexRegion, `"PGPASSWORD" = "s3cr3t"`) {
		t.Fatalf("codex region did not receive the value: %s", material.CodexRegion)
	}
	if strings.Contains(material.CodexRegion, "${PG_PASSWORD}") {
		t.Fatalf("a literal placeholder survived: %s", material.CodexRegion)
	}
	if strings.Contains(material.CodexRegion, "unused") {
		t.Fatalf("an out-of-scope entry was materialized: %s", material.CodexRegion)
	}
}

func TestEnsureContainerSkipsAnEntryWhoseSecretTheProjectCannotRead(t *testing.T) {
	store := &memoryStore{servers: []Server{
		{
			Name: "jira", Transport: TransportHTTP, URL: "https://j.example.com/mcp",
			Headers:    map[string]string{"Authorization": "Bearer ${JIRA_TOKEN}"},
			SecretRefs: []string{"JIRA_TOKEN"},
			Scope:      Scope{All: true},
		},
	}}
	containers := &recordingContainers{}
	service := New(store, newProjectStore(),
		WithContainers(containers),
		WithSecrets(&staticSecrets{values: map[string]string{}}),
		WithClock(fixedClock()),
	)

	if _, err := service.EnsureContainer(context.Background(), "p1", "proj-p1"); err != nil {
		t.Fatalf("EnsureContainer() error = %v", err)
	}
	material := containers.applies[0]
	if !reflect.DeepEqual(material.Skipped, []string{"jira"}) {
		t.Fatalf("skipped = %v", material.Skipped)
	}
	if !material.Empty() {
		t.Fatalf("an unresolvable entry was still written: %#v", material)
	}
}

func TestEnsureContainerIsANoOpWithoutAContainerPort(t *testing.T) {
	service := New(&memoryStore{}, newProjectStore(), WithClock(fixedClock()))
	path, err := service.EnsureContainer(context.Background(), "p1", "proj-p1")
	if err != nil || path != "" {
		t.Fatalf("EnsureContainer() = %q, %v", path, err)
	}
}

/* ------------------------------------------------------------------ *
 * Probe
 * ------------------------------------------------------------------ */

func TestTestMasksResolvedValuesOutOfProbeOutput(t *testing.T) {
	store := &memoryStore{servers: []Server{{
		Name: "pg", Transport: TransportStdio, Command: "npx",
		Env:        map[string]string{"PGPASSWORD": "${PG_PASSWORD}"},
		SecretRefs: []string{"PG_PASSWORD"},
		Scope:      Scope{All: true},
	}}}
	containers := &recordingContainers{
		probeOut: `connecting with PGPASSWORD=super-secret-value ... ok`,
	}
	service := New(store, newProjectStore(),
		WithContainers(containers),
		WithSecrets(&staticSecrets{values: map[string]string{"PG_PASSWORD": "super-secret-value"}}),
		WithProjects(staticProjects{target: Target{ContainerName: "proj-p1", Running: true}}),
		WithClock(fixedClock()),
	)

	result, err := service.Test(context.Background(), "pg", "p1")
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(result.Output, "super-secret-value") {
		t.Fatalf("the probe leaked a value: %s", result.Output)
	}
	if !strings.Contains(result.Output, "••••••••") {
		t.Fatalf("expected a mask, got %s", result.Output)
	}
	// The probe itself still receives the real value; only the response is
	// masked.
	if got := containers.probed[0].Env["PGPASSWORD"]; got != "super-secret-value" {
		t.Fatalf("the probe was given %q", got)
	}
}

func TestTestRefusesAStoppedContainer(t *testing.T) {
	store := &memoryStore{servers: []Server{
		{Name: "fetch", Transport: TransportStdio, Command: "uvx", Scope: Scope{All: true}},
	}}
	service := New(store, newProjectStore(),
		WithContainers(&recordingContainers{}),
		WithProjects(staticProjects{target: Target{ContainerName: "proj-p1", Running: false}}),
		WithClock(fixedClock()),
	)
	if _, err := service.Test(context.Background(), "fetch", "p1"); err == nil {
		t.Fatalf("Test() against a stopped container should fail")
	}
}

func TestTestRequiresAProjectAndAKnownServer(t *testing.T) {
	service := New(&memoryStore{}, newProjectStore(),
		WithContainers(&recordingContainers{}),
		WithProjects(staticProjects{target: Target{ContainerName: "proj-p1", Running: true}}),
		WithClock(fixedClock()),
	)
	if _, err := service.Test(context.Background(), "fetch", ""); !errors.Is(err, ErrNoProject) {
		t.Fatalf("error = %v, want ErrNoProject", err)
	}
	if _, err := service.Test(context.Background(), "fetch", "p1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
