package agentendpoints

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// probePrompt is the two-word request the Test action sends. Two words is the
// whole point: the question is whether the endpoint answers through the real
// CLI at all, not whether the model is any good.
const probePrompt = "say ready"

// probeOutputLimit bounds what a misbehaving endpoint can push into an API
// response through the Test action.
const probeOutputLimit = 4000

// Service owns the register, resolves a profile into a run's environment, and
// runs the Test probe.
type Service struct {
	store      Store
	secrets    Secrets
	containers Containers
	projects   Projects
	audit      audit.Recorder
	now        func() time.Time

	// mu serializes the read-modify-write of the register, matching how every
	// other file-backed collection in the platform is guarded.
	mu sync.Mutex
}

// Option configures optional collaborators.
type Option func(*Service)

// WithAudit attaches the audit recorder.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithSecrets attaches the vault reader behind `apiKeyRef`. Without it no
// profile can resolve a key, so every run pointed at one fails with a clear
// message rather than reaching a vendor unauthenticated.
func WithSecrets(secrets Secrets) Option {
	return func(s *Service) { s.secrets = secrets }
}

// WithContainers attaches the port the Test probe runs through.
func WithContainers(containers Containers) Option {
	return func(s *Service) { s.containers = containers }
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

// New builds the register service. A nil store leaves every route reporting
// ErrUnavailable rather than panicking.
func New(store Store, options ...Option) *Service {
	service := &Service{store: store, audit: audit.Nop{}, now: time.Now}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

/* ------------------------------------------------------------------ *
 * Register
 * ------------------------------------------------------------------ */

// List returns every profile, in label order, with the resolution state of
// each one's vault key. No value is ever included.
func (s *Service) List(ctx context.Context) ([]View, error) {
	endpoints, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	resolved := s.resolvedKeys(ctx, endpoints)
	views := make([]View, 0, len(endpoints))
	for _, endpoint := range endpoints {
		views = append(views, View{
			Endpoint:    endpoint,
			KeyResolved: endpoint.APIKeyRef != "" && resolved[endpoint.APIKeyRef],
		})
	}
	return views, nil
}

// Choices is the composer's read: the enabled profiles, their labels, and
// their model lists. Any signed-in user may call it — it names which models
// this deployment offers, never how to reach them.
func (s *Service) Choices(ctx context.Context) ([]Choice, error) {
	endpoints, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return Choices(endpoints), nil
}

// Create stores a new profile.
func (s *Service) Create(ctx context.Context, endpoint Endpoint, actor string) (View, error) {
	view, err := s.upsert(ctx, endpoint.ID, endpoint, actor, false)
	s.record(ctx, audit.ActionSettingsAgentEndpointCreate, endpoint.ID, audit.Meta{
		"cli":     string(NormalizeCLI(endpoint.CLI)),
		"enabled": endpoint.Enabled,
	}, err)
	return view, err
}

// Update replaces an existing profile. The id is immutable: it is the handle
// every chat pointed at this endpoint stores, and renaming one would silently
// orphan those chats.
func (s *Service) Update(ctx context.Context, id string, endpoint Endpoint, actor string) (View, error) {
	view, err := s.upsert(ctx, id, endpoint, actor, true)
	s.record(ctx, audit.ActionSettingsAgentEndpointUpdate, id, audit.Meta{
		"cli":     string(NormalizeCLI(endpoint.CLI)),
		"enabled": endpoint.Enabled,
	}, err)
	return view, err
}

// Delete removes a profile. Chats still pointed at it fall back to the
// vendor's own default on their next turn, which is the safe direction to
// fail: a deleted endpoint must never silently keep running.
func (s *Service) Delete(ctx context.Context, id string) error {
	err := s.delete(ctx, id)
	s.record(ctx, audit.ActionSettingsAgentEndpointDelete, id, nil, err)
	return err
}

// SetEnabled flips one profile's switch without restating the rest of it.
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool, actor string) (View, error) {
	view, err := s.setEnabled(ctx, id, enabled, actor)
	s.record(ctx, audit.ActionSettingsAgentEndpointUpdate, id, audit.Meta{
		"enabled": enabled,
	}, err)
	return view, err
}

func (s *Service) upsert(
	ctx context.Context,
	id string,
	endpoint Endpoint,
	actor string,
	mustExist bool,
) (View, error) {
	if s.store == nil {
		return View{}, ErrUnavailable
	}
	endpoint.ID = id
	candidate, err := Normalize(endpoint)
	if err != nil {
		return View{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	endpoints, err := s.loadLocked(ctx)
	if err != nil {
		return View{}, err
	}
	index := indexOf(endpoints, candidate.ID)
	switch {
	case index < 0 && mustExist:
		return View{}, ErrNotFound
	case index >= 0 && !mustExist:
		return View{}, ErrExists
	case index < 0 && len(endpoints) >= MaxEndpoints:
		return View{}, ErrTooLarge
	}

	candidate.UpdatedAt = s.now().UnixMilli()
	candidate.UpdatedBy = strings.ToLower(strings.TrimSpace(actor))
	if index >= 0 {
		// The test record belongs to the endpoint, not to the edit: an
		// operator fixing a typo in the label has not invalidated the last
		// probe.
		candidate.LastTest = endpoints[index].LastTest
		endpoints[index] = candidate
	} else {
		endpoints = append(endpoints, candidate)
	}
	if err := s.saveLocked(ctx, endpoints); err != nil {
		return View{}, err
	}
	return s.view(ctx, candidate), nil
}

func (s *Service) delete(ctx context.Context, id string) error {
	if s.store == nil {
		return ErrUnavailable
	}
	id = strings.ToLower(strings.TrimSpace(id))
	if !ValidID(id) {
		return ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	endpoints, err := s.loadLocked(ctx)
	if err != nil {
		return err
	}
	index := indexOf(endpoints, id)
	if index < 0 {
		return ErrNotFound
	}
	endpoints = append(endpoints[:index], endpoints[index+1:]...)
	return s.saveLocked(ctx, endpoints)
}

func (s *Service) setEnabled(ctx context.Context, id string, enabled bool, actor string) (View, error) {
	if s.store == nil {
		return View{}, ErrUnavailable
	}
	id = strings.ToLower(strings.TrimSpace(id))
	if !ValidID(id) {
		return View{}, ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	endpoints, err := s.loadLocked(ctx)
	if err != nil {
		return View{}, err
	}
	index := indexOf(endpoints, id)
	if index < 0 {
		return View{}, ErrNotFound
	}
	candidate := endpoints[index].Clone()
	candidate.Enabled = enabled
	// Re-validate: enabling tightens the rules — a profile with no vault key
	// may sit disabled forever but must never go live.
	normalized, err := Normalize(candidate)
	if err != nil {
		return View{}, err
	}
	normalized.LastTest = candidate.LastTest
	normalized.UpdatedAt = s.now().UnixMilli()
	normalized.UpdatedBy = strings.ToLower(strings.TrimSpace(actor))
	endpoints[index] = normalized
	if err := s.saveLocked(ctx, endpoints); err != nil {
		return View{}, err
	}
	return s.view(ctx, normalized), nil
}

/* ------------------------------------------------------------------ *
 * The run path
 * ------------------------------------------------------------------ */

// RuntimeFor resolves one profile into the environment and CLI arguments a
// single run needs.
//
// This is the only path on which a vault value is read for an endpoint, and
// the value never leaves the Runtime it is written into: it is not logged,
// not stored, and not returned by any route that reaches a browser.
//
// A disabled profile, a missing profile, or an unresolvable key all fail
// here, before the CLI is launched, so the operator gets a sentence about
// their configuration rather than a vendor's 401 in the transcript.
func (s *Service) RuntimeFor(ctx context.Context, id, model string) (Runtime, error) {
	if s.store == nil {
		return Runtime{}, ErrUnavailable
	}
	endpoints, err := s.load(ctx)
	if err != nil {
		return Runtime{}, err
	}
	endpoint, found := Find(endpoints, id)
	if !found {
		return Runtime{}, ErrNotFound
	}
	if !endpoint.Enabled {
		return Runtime{}, ErrDisabled
	}
	key, err := s.resolveKey(ctx, endpoint)
	if err != nil {
		return Runtime{}, err
	}
	return Render(endpoint, model, key)
}

// resolveKey reads the one vault value this profile names.
func (s *Service) resolveKey(ctx context.Context, endpoint Endpoint) (string, error) {
	unresolved := ErrKeyUnresolved{Endpoint: endpoint.ID, Key: endpoint.APIKeyRef}
	if endpoint.APIKeyRef == "" || s.secrets == nil {
		return "", unresolved
	}
	values, err := s.secrets.PlatformValues(ctx, []string{endpoint.APIKeyRef})
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(values[endpoint.APIKeyRef])
	if value == "" {
		return "", unresolved
	}
	return value, nil
}

// resolvedKeys reports which of the register's key references currently hold
// a value. It answers with names only — the values are discarded here.
func (s *Service) resolvedKeys(ctx context.Context, endpoints []Endpoint) map[string]bool {
	if s.secrets == nil {
		return nil
	}
	keys := make([]string, 0, len(endpoints))
	seen := make(map[string]bool, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.APIKeyRef == "" || seen[endpoint.APIKeyRef] {
			continue
		}
		seen[endpoint.APIKeyRef] = true
		keys = append(keys, endpoint.APIKeyRef)
	}
	if len(keys) == 0 {
		return nil
	}
	values, err := s.secrets.PlatformValues(ctx, keys)
	if err != nil {
		return nil
	}
	resolved := make(map[string]bool, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) != "" {
			resolved[key] = true
		}
	}
	return resolved
}

/* ------------------------------------------------------------------ *
 * Test
 * ------------------------------------------------------------------ */

// Test runs a two-word prompt through the real CLI, configured for this
// endpoint, inside a chosen project's container, and returns the raw result.
//
// The resolved key is masked out of that output before it leaves the backend,
// so an endpoint that echoes its own configuration cannot leak a credential
// through this route.
func (s *Service) Test(ctx context.Context, id, projectID, model string) (TestResult, error) {
	result, err := s.test(ctx, id, projectID, model)
	s.record(ctx, audit.ActionSettingsAgentEndpointTest, id, audit.Meta{
		"projectId": projectID,
		"ok":        result.OK,
	}, err)
	return result, err
}

func (s *Service) test(ctx context.Context, id, projectID, model string) (TestResult, error) {
	if s.store == nil {
		return TestResult{}, ErrUnavailable
	}
	if s.containers == nil || s.projects == nil {
		return TestResult{}, ErrProbeFailed
	}
	if strings.TrimSpace(projectID) == "" {
		return TestResult{}, ErrNoProject
	}

	endpoints, err := s.load(ctx)
	if err != nil {
		return TestResult{}, err
	}
	endpoint, found := Find(endpoints, id)
	if !found {
		return TestResult{}, ErrNotFound
	}
	// A disabled profile is testable on purpose: confirming a template's
	// values is exactly what an operator does *before* switching it on.
	key, err := s.resolveKey(ctx, endpoint)
	if err != nil {
		return TestResult{}, err
	}
	runtime, err := Render(endpoint, model, key)
	if err != nil {
		return TestResult{}, err
	}

	target, err := s.projects.EndpointTarget(ctx, projectID)
	if err != nil {
		return TestResult{}, err
	}
	if target.ContainerName == "" || !target.Running {
		return TestResult{}, errors.New("the project's container is not running; start it and try again")
	}

	started := s.now()
	output, probeErr := s.containers.Probe(ctx, target.ContainerName, Probe{
		CLI:    runtime.CLI,
		Model:  runtime.Model,
		Env:    runtime.Env,
		Args:   runtime.Args,
		Prompt: probePrompt,
	})
	result := TestResult{
		OK:       probeErr == nil,
		Output:   truncate(MaskValue(strings.TrimSpace(output), key), probeOutputLimit),
		Duration: s.now().Sub(started).Milliseconds(),
	}
	if probeErr != nil {
		result.Error = MaskValue(probeErr.Error(), key)
	}
	s.recordTest(ctx, endpoint.ID, TestRecord{
		At:        s.now().UnixMilli(),
		OK:        result.OK,
		ProjectID: projectID,
		Model:     runtime.Model,
		Message:   failureMessage(result),
	})
	return result, nil
}

// failureMessage is the one-line reason kept on the profile. It is the first
// line of already-masked output, bounded, so the admin table can show why the
// last test failed without storing a screenful.
func failureMessage(result TestResult) string {
	if result.OK {
		return ""
	}
	// Prefer the reason over the transcript: the first line the CLI printed
	// is frequently a warning that has nothing to do with the failure.
	line := strings.TrimSpace(result.Error)
	if line == "" {
		line = strings.TrimSpace(result.Output)
	}
	if index := strings.IndexAny(line, "\r\n"); index >= 0 {
		line = strings.TrimSpace(line[:index])
	}
	return truncate(line, 200)
}

// recordTest stamps the profile with the outcome. It is best-effort: losing
// the stamp costs one stale line in a settings table, never a wrong run.
func (s *Service) recordTest(ctx context.Context, id string, record TestRecord) {
	if s.store == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoints, err := s.loadLocked(ctx)
	if err != nil {
		return
	}
	index := indexOf(endpoints, id)
	if index < 0 {
		return
	}
	endpoints[index].LastTest = &record
	_ = s.saveLocked(ctx, endpoints)
}

/* ------------------------------------------------------------------ *
 * Helpers
 * ------------------------------------------------------------------ */

func (s *Service) load(ctx context.Context) ([]Endpoint, error) {
	if s.store == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(ctx)
}

// loadLocked reads and normalizes the register. Normalizing on the way out
// means a document written by an earlier version, or edited by hand, still
// yields profiles the renderers can be trusted with; anything that cannot be
// normalized is dropped rather than allowed to reach a command line.
func (s *Service) loadLocked(ctx context.Context) ([]Endpoint, error) {
	stored, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]Endpoint, 0, len(stored))
	seen := make(map[string]bool, len(stored))
	for _, endpoint := range stored {
		normalized, err := Normalize(endpoint)
		if err != nil {
			continue
		}
		if seen[normalized.ID] {
			continue
		}
		seen[normalized.ID] = true
		normalized.LastTest = endpoint.LastTest
		endpoints = append(endpoints, normalized)
	}
	sortEndpoints(endpoints)
	return endpoints, nil
}

func (s *Service) saveLocked(ctx context.Context, endpoints []Endpoint) error {
	sortEndpoints(endpoints)
	return s.store.Save(ctx, endpoints)
}

// view pairs one profile with the resolution state of its key.
func (s *Service) view(ctx context.Context, endpoint Endpoint) View {
	resolved := s.resolvedKeys(ctx, []Endpoint{endpoint})
	return View{
		Endpoint:    endpoint,
		KeyResolved: endpoint.APIKeyRef != "" && resolved[endpoint.APIKeyRef],
	}
}

func (s *Service) record(ctx context.Context, action, target string, meta audit.Meta, err error) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(
		action,
		audit.Target{Type: audit.TargetAgentEndpoint, ID: target},
		meta,
		err,
	))
}

func sortEndpoints(endpoints []Endpoint) {
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Label != endpoints[j].Label {
			return endpoints[i].Label < endpoints[j].Label
		}
		return endpoints[i].ID < endpoints[j].ID
	})
}

func indexOf(endpoints []Endpoint, id string) int {
	for index := range endpoints {
		if endpoints[index].ID == id {
			return index
		}
	}
	return -1
}

func truncate(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
