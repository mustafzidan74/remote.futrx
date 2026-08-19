package mcp

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// Service owns the registry document, the per-project overrides, and the
// materialization of both into a project container.
type Service struct {
	store        Store
	projectStore ProjectStore
	containers   Containers
	secrets      Secrets
	projects     Projects
	audit        audit.Recorder
	now          func() time.Time

	// mu serializes the read-modify-write of the platform document, matching
	// how every other file-backed collection in the platform is guarded.
	mu sync.Mutex
	// projectMu does the same for a project document. One mutex for all
	// projects is enough: the critical section is a file read and write.
	projectMu sync.Mutex
	// syncMu keeps two overlapping runs from pushing conflicting material
	// into the same container.
	syncMu sync.Mutex
}

// Option configures optional collaborators.
type Option func(*Service)

// WithAudit attaches the audit recorder.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithContainers attaches the container port. Without it the registry still
// stores entries; nothing is materialized anywhere.
func WithContainers(containers Containers) Option {
	return func(s *Service) { s.containers = containers }
}

// WithSecrets attaches the vault reader behind ${KEY} substitution. Without
// it, any entry that references a key is skipped rather than half-written.
func WithSecrets(secrets Secrets) Option {
	return func(s *Service) { s.secrets = secrets }
}

// WithProjects attaches the container resolver the Test probe needs.
func WithProjects(projects Projects) Option {
	return func(s *Service) { s.projects = projects }
}

// WithClock overrides time.Now, for tests.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// New builds the registry service. A nil platform store leaves every route
// reporting ErrUnavailable rather than panicking.
func New(store Store, projectStore ProjectStore, options ...Option) *Service {
	service := &Service{
		store:        store,
		projectStore: projectStore,
		audit:        audit.Nop{},
		now:          time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

/* ------------------------------------------------------------------ *
 * Platform registry
 * ------------------------------------------------------------------ */

// List returns every platform entry, in name order.
func (s *Service) List(ctx context.Context) ([]View, error) {
	servers, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(servers))
	for _, server := range servers {
		views = append(views, View{Server: server, Unsupported: UnsupportedNamed(server)})
	}
	return views, nil
}

// Create stores a new entry. It is materialized into each scoped project's
// container on that project's next agent run.
func (s *Service) Create(ctx context.Context, server Server, actor string) (View, error) {
	view, err := s.upsert(ctx, server.Name, server, actor, false)
	s.record(ctx, audit.ActionSettingsMCPCreate, server.Name, audit.Meta{
		"transport": string(server.Transport),
		"scope":     scopeLabel(server.Scope),
	}, err)
	return view, err
}

// Update replaces an existing entry. The name is immutable: renaming one
// would orphan the tool identifier every model already learned.
func (s *Service) Update(ctx context.Context, name string, server Server, actor string) (View, error) {
	view, err := s.upsert(ctx, name, server, actor, true)
	s.record(ctx, audit.ActionSettingsMCPUpdate, name, audit.Meta{
		"transport": string(server.Transport),
		"scope":     scopeLabel(server.Scope),
	}, err)
	return view, err
}

// Delete removes an entry; the next materialization of each affected
// container removes its configuration.
func (s *Service) Delete(ctx context.Context, name string) error {
	err := s.delete(ctx, name)
	s.record(ctx, audit.ActionSettingsMCPDelete, name, nil, err)
	return err
}

func (s *Service) upsert(
	ctx context.Context,
	name string,
	server Server,
	actor string,
	mustExist bool,
) (View, error) {
	if s.store == nil {
		return View{}, ErrUnavailable
	}
	server.Name = name
	candidate, err := Normalize(server, true)
	if err != nil {
		return View{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	servers, err := s.store.Load(ctx)
	if err != nil {
		return View{}, err
	}
	index := indexOf(servers, candidate.Name)
	switch {
	case index < 0 && mustExist:
		return View{}, ErrNotFound
	case index >= 0 && !mustExist:
		return View{}, ErrExists
	}

	candidate.UpdatedAt = s.now().UnixMilli()
	candidate.UpdatedBy = strings.ToLower(strings.TrimSpace(actor))
	if index >= 0 {
		servers[index] = candidate
	} else {
		servers = append(servers, candidate)
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	if err := s.store.Save(ctx, servers); err != nil {
		return View{}, err
	}
	return View{Server: candidate, Unsupported: UnsupportedNamed(candidate)}, nil
}

func (s *Service) delete(ctx context.Context, name string) error {
	if s.store == nil {
		return ErrUnavailable
	}
	if !ValidName(strings.TrimSpace(name)) {
		return ErrInvalidName
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	servers, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	index := indexOf(servers, name)
	if index < 0 {
		return ErrNotFound
	}
	servers = append(servers[:index], servers[index+1:]...)
	return s.store.Save(ctx, servers)
}

/* ------------------------------------------------------------------ *
 * Project view
 * ------------------------------------------------------------------ */

// ProjectSettings returns what one project will have: every entry available
// to it, whether each is on, and when the container last received them.
func (s *Service) ProjectSettings(ctx context.Context, projectID string) (ProjectView, error) {
	if strings.TrimSpace(projectID) == "" {
		return ProjectView{}, ErrNoProject
	}
	servers, err := s.load(ctx)
	if err != nil {
		return ProjectView{}, err
	}
	settings, err := s.loadProject(ctx, projectID)
	if err != nil {
		return ProjectView{}, err
	}
	return ProjectView{
		Available:            Available(servers, settings, projectID),
		MaterializedAt:       settings.MaterializedAt,
		MaterializedNames:    settings.MaterializedNames,
		SupportedProviders:   SupportedProviders(),
		UnsupportedProviders: UnsupportedProviders(),
	}, nil
}

// SaveProjectSettings replaces one project's overrides. Members may call it:
// everything they can add here, an agent in their container could already do
// by hand, and the vault keys an entry may reference are limited to the ones
// the project already receives.
func (s *Service) SaveProjectSettings(
	ctx context.Context,
	projectID string,
	input ProjectInput,
	actor string,
) (ProjectView, error) {
	view, err := s.saveProjectSettings(ctx, projectID, input)
	s.record(ctx, audit.ActionSettingsMCPProject, projectID, audit.Meta{
		"disabled": len(input.Disabled),
		"servers":  len(input.Servers),
	}, err)
	_ = actor
	return view, err
}

func (s *Service) saveProjectSettings(
	ctx context.Context,
	projectID string,
	input ProjectInput,
) (ProjectView, error) {
	if s.projectStore == nil {
		return ProjectView{}, ErrUnavailable
	}
	if strings.TrimSpace(projectID) == "" {
		return ProjectView{}, ErrNoProject
	}

	normalized := make([]Server, 0, len(input.Servers))
	seen := map[string]bool{}
	for _, server := range input.Servers {
		candidate, err := Normalize(server, false)
		if err != nil {
			return ProjectView{}, err
		}
		if seen[candidate.Name] {
			return ProjectView{}, ErrExists
		}
		seen[candidate.Name] = true
		candidate.UpdatedAt = s.now().UnixMilli()
		normalized = append(normalized, candidate)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Name < normalized[j].Name })

	disabled := make([]string, 0, len(input.Disabled))
	disabledSeen := map[string]bool{}
	for _, name := range input.Disabled {
		name = strings.TrimSpace(name)
		if name == "" || disabledSeen[name] || !ValidName(name) {
			continue
		}
		disabledSeen[name] = true
		disabled = append(disabled, name)
	}
	sort.Strings(disabled)

	s.projectMu.Lock()
	stored, err := s.projectStore.Load(ctx, projectID)
	if err != nil {
		s.projectMu.Unlock()
		return ProjectView{}, err
	}
	stored.Disabled = disabled
	stored.Servers = normalized
	if err := s.projectStore.Save(ctx, projectID, stored); err != nil {
		s.projectMu.Unlock()
		return ProjectView{}, err
	}
	s.projectMu.Unlock()

	return s.ProjectSettings(ctx, projectID)
}

/* ------------------------------------------------------------------ *
 * Materialization
 * ------------------------------------------------------------------ */

// EnsureContainer converges one container onto the registry and reports the
// path Claude Code should be handed with --mcp-config, empty when Claude has
// no servers for this project.
//
// It is called on the same hook that refreshes skills before a run, so a
// change made in Settings is live on the very next prompt. It is cheap in the
// steady state: the container's manifest carries a signature over exactly
// what would be written, so an unchanged registry costs one `cat`.
func (s *Service) EnsureContainer(ctx context.Context, projectID, containerName string) (string, error) {
	if s.containers == nil || s.store == nil ||
		strings.TrimSpace(projectID) == "" || strings.TrimSpace(containerName) == "" {
		return "", nil
	}

	material, err := s.materialFor(ctx, projectID)
	if err != nil {
		return "", err
	}

	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	previous, err := s.containers.Manifest(ctx, containerName)
	if err != nil {
		return "", err
	}
	if previous.Version == ManifestVersion && previous.Signature == material.Signature() {
		return previous.ClaudeConfig, nil
	}
	if err := s.containers.Apply(ctx, containerName, material, StaleFiles(previous, material)); err != nil {
		return "", err
	}
	s.recordMaterialization(ctx, projectID, material)
	return material.ClaudeConfigPath, nil
}

// materialFor resolves one project's enabled entries into container material.
func (s *Service) materialFor(ctx context.Context, projectID string) (Material, error) {
	servers, err := s.load(ctx)
	if err != nil {
		return Material{}, err
	}
	settings, err := s.loadProject(ctx, projectID)
	if err != nil {
		return Material{}, err
	}
	entries := Available(servers, settings, projectID)
	values, err := s.secretValues(ctx, projectID, SecretKeys(entries))
	if err != nil {
		return Material{}, err
	}
	return MaterialFor(Resolve(entries, values)), nil
}

// secretValues reads the vault values one project's entries reference. A
// deployment without a vault resolves nothing, which skips every entry that
// needs a secret instead of writing a literal placeholder.
func (s *Service) secretValues(
	ctx context.Context,
	projectID string,
	keys []string,
) (map[string]string, error) {
	if len(keys) == 0 || s.secrets == nil {
		return nil, nil
	}
	return s.secrets.ValuesForProject(ctx, projectID, keys)
}

// recordMaterialization stamps the project document. It is best-effort: the
// container already holds the configuration, and losing the timestamp costs
// one stale line in a settings panel, never a wrong container.
func (s *Service) recordMaterialization(ctx context.Context, projectID string, material Material) {
	if s.projectStore == nil {
		return
	}
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	settings, err := s.projectStore.Load(ctx, projectID)
	if err != nil {
		return
	}
	settings.MaterializedAt = s.now().UnixMilli()
	settings.MaterializedNames = append([]string(nil), material.Names...)
	_ = s.projectStore.Save(ctx, projectID, settings)
}

/* ------------------------------------------------------------------ *
 * Probe
 * ------------------------------------------------------------------ */

// Test runs one server's handshake inside a chosen project's container and
// returns the raw output. Every resolved vault value is masked out of that
// output before it leaves the backend, so a server that echoes its own
// configuration cannot leak a credential through this route.
func (s *Service) Test(ctx context.Context, name, projectID string) (TestResult, error) {
	result, err := s.test(ctx, name, projectID)
	s.record(ctx, audit.ActionSettingsMCPTest, name, audit.Meta{
		"projectId": projectID,
		"ok":        result.OK,
	}, err)
	return result, err
}

func (s *Service) test(ctx context.Context, name, projectID string) (TestResult, error) {
	if s.store == nil {
		return TestResult{}, ErrUnavailable
	}
	if s.containers == nil || s.projects == nil {
		return TestResult{}, ErrProbeFailed
	}
	if strings.TrimSpace(projectID) == "" {
		return TestResult{}, ErrNoProject
	}

	servers, err := s.load(ctx)
	if err != nil {
		return TestResult{}, err
	}
	settings, err := s.loadProject(ctx, projectID)
	if err != nil {
		return TestResult{}, err
	}
	entry, found := findEntry(Available(servers, settings, projectID), name)
	if !found {
		return TestResult{}, ErrNotFound
	}

	values, err := s.secretValues(ctx, projectID, entry.SecretRefs)
	if err != nil {
		return TestResult{}, err
	}
	resolved, ok := resolveServer(entry.Server, values)
	if !ok {
		return TestResult{}, ErrUnresolvedSecret{Server: entry.Name, Key: firstMissing(entry.Server, values)}
	}

	target, err := s.projects.MCPTarget(ctx, projectID)
	if err != nil {
		return TestResult{}, err
	}
	if target.ContainerName == "" || !target.Running {
		return TestResult{}, errors.New("the project's container is not running; start it and try again")
	}

	started := s.now()
	output, probeErr := s.containers.Probe(ctx, target.ContainerName, resolved)
	result := TestResult{
		OK:       probeErr == nil,
		Output:   maskValues(output, values),
		Duration: s.now().Sub(started).Milliseconds(),
	}
	if probeErr != nil && result.Output == "" {
		result.Output = probeErr.Error()
	}
	return result, nil
}

/* ------------------------------------------------------------------ *
 * Helpers
 * ------------------------------------------------------------------ */

func (s *Service) load(ctx context.Context) ([]Server, error) {
	if s.store == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	servers, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return servers, nil
}

func (s *Service) loadProject(ctx context.Context, projectID string) (ProjectSettings, error) {
	if s.projectStore == nil {
		return ProjectSettings{}, nil
	}
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	return s.projectStore.Load(ctx, projectID)
}

func (s *Service) record(ctx context.Context, action, target string, meta audit.Meta, err error) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(
		action,
		audit.Target{Type: audit.TargetMCPServer, ID: target},
		meta,
		err,
	))
}

func indexOf(servers []Server, name string) int {
	for index := range servers {
		if servers[index].Name == name {
			return index
		}
	}
	return -1
}

func findEntry(entries []Entry, name string) (Entry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return Entry{}, false
}

func firstMissing(server Server, values map[string]string) string {
	for _, key := range Placeholders(server) {
		if _, ok := values[key]; !ok {
			return key
		}
	}
	return ""
}

// maskValues replaces every resolved vault value found in probe output. It
// runs longest-first so a value that contains another is not partially
// revealed by masking the shorter one first.
func maskValues(output string, values map[string]string) string {
	if output == "" || len(values) == 0 {
		return output
	}
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if len(value) >= 4 {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		output = strings.ReplaceAll(output, secret, "••••••••")
	}
	return output
}

// scopeLabel is the audit-safe summary of a scope: how wide it is, never
// which project it names.
func scopeLabel(scope Scope) string {
	if scope.All {
		return "all"
	}
	return "projects:" + strconv.Itoa(len(scope.ProjectIDs))
}
