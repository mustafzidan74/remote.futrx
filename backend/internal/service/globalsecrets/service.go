package globalsecrets

import (
	"context"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// Service owns the vault document and everything derived from it. Reads
// return masked views; only the sync path ever sees a value.
type Service struct {
	store       Store
	environment ContainerEnvironment
	material    ContainerMaterializer
	prober      SSHProber
	audit       audit.Recorder
	now         func() time.Time

	// mu serializes the read-modify-write of the single document, matching
	// how every other file-backed collection in the platform is guarded.
	mu sync.Mutex

	// projects is attached after construction (see SetProjects): the fleet
	// listing needs this service, so the two cannot both be built with the
	// other already in hand.
	projectsMu sync.RWMutex
	projects   Projects

	// resyncMu keeps two overlapping vault edits from pushing conflicting
	// material into the same container.
	resyncMu sync.Mutex
	// resyncSync makes the post-change fan-out run inline, which is what
	// tests assert against instead of racing a goroutine.
	resyncSync bool
}

// Option configures optional collaborators.
type Option func(*Service)

// WithAudit attaches the audit recorder. Values never reach it.
func WithAudit(recorder audit.Recorder) Option {
	return func(s *Service) { s.audit = audit.RecorderOrNop(recorder) }
}

// WithContainers attaches the two container ports. Without them the vault
// still stores entries; nothing is pushed anywhere.
func WithContainers(environment ContainerEnvironment, material ContainerMaterializer) Option {
	return func(s *Service) {
		s.environment = environment
		s.material = material
	}
}

// WithSSHProber attaches the host-side connectivity probe.
func WithSSHProber(prober SSHProber) Option {
	return func(s *Service) { s.prober = prober }
}

// WithClock overrides time.Now, for tests.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithSynchronousResync makes the post-change container fan-out block until
// it finishes. Production leaves it asynchronous so an admin's save is not
// held hostage by a slow container.
func WithSynchronousResync() Option {
	return func(s *Service) { s.resyncSync = true }
}

func New(store Store, options ...Option) *Service {
	service := &Service{
		store: store,
		audit: audit.Nop{},
		now:   time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// SetProjects attaches the fleet listing after construction. The vault and
// the project service each need the other, so the vault is built first — from
// ports alone — and told about projects before anything can serve a request.
func (s *Service) SetProjects(projects Projects) {
	s.projectsMu.Lock()
	defer s.projectsMu.Unlock()
	s.projects = projects
}

func (s *Service) projectPort() Projects {
	s.projectsMu.RLock()
	defer s.projectsMu.RUnlock()
	return s.projects
}

// Input is one create-or-update request. A blank Value keeps whatever is
// stored; Clear removes it. The distinction is what lets an admin edit a
// description or a scope without re-pasting a key.
type Input struct {
	Key         string
	Kind        Kind
	Value       string
	Path        string
	SSH         *SSHTarget
	Scope       Scope
	Description string
	Clear       bool
}

// List returns every entry, masked, in key order.
func (s *Service) List(ctx context.Context) ([]View, error) {
	secrets, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return s.views(ctx, secrets), nil
}

// Create stores a new entry and pushes it to every container in scope.
func (s *Service) Create(ctx context.Context, input Input, actor string) (View, error) {
	view, err := s.upsert(ctx, input, actor, false)
	s.record(ctx, audit.ActionSettingsSecretCreate, input.Key, audit.Meta{
		"kind":  string(input.Kind),
		"scope": scopeLabel(input.Scope),
	}, err)
	return view, err
}

// Update replaces an existing entry. The key is immutable: renaming one would
// orphan whatever the old key materialized.
func (s *Service) Update(ctx context.Context, key string, input Input, actor string) (View, error) {
	input.Key = key
	view, err := s.upsert(ctx, input, actor, true)
	s.record(ctx, audit.ActionSettingsSecretUpdate, key, audit.Meta{
		"kind":    string(input.Kind),
		"scope":   scopeLabel(input.Scope),
		"cleared": input.Clear,
	}, err)
	return view, err
}

// Delete removes an entry; the next sync of each affected container removes
// its material. The acting principal comes from the context, as it does for
// every other audited call, so there is nothing to stamp on a removed entry.
func (s *Service) Delete(ctx context.Context, key string) error {
	err := s.delete(ctx, key)
	s.record(ctx, audit.ActionSettingsSecretDelete, key, nil, err)
	return err
}

func (s *Service) delete(ctx context.Context, key string) error {
	if s.store == nil {
		return ErrUnavailable
	}
	if !ValidKey(key) {
		return ErrInvalidKey
	}

	s.mu.Lock()
	secrets, err := s.store.Load(ctx)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	index := indexOf(secrets, key)
	if index < 0 {
		s.mu.Unlock()
		return ErrNotFound
	}
	removed := secrets[index]
	secrets = append(secrets[:index], secrets[index+1:]...)
	if err := s.store.Save(ctx, secrets); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	s.resync(ctx, removed.Scope)
	return nil
}

func (s *Service) upsert(ctx context.Context, input Input, actor string, mustExist bool) (View, error) {
	if s.store == nil {
		return View{}, ErrUnavailable
	}
	candidate, err := normalizeInput(input)
	if err != nil {
		return View{}, err
	}

	s.mu.Lock()
	secrets, err := s.store.Load(ctx)
	if err != nil {
		s.mu.Unlock()
		return View{}, err
	}
	index := indexOf(secrets, candidate.Key)
	switch {
	case index < 0 && mustExist:
		s.mu.Unlock()
		return View{}, ErrNotFound
	case index >= 0 && !mustExist:
		s.mu.Unlock()
		return View{}, ErrExists
	}

	previousScope := Scope{}
	if index >= 0 {
		previousScope = secrets[index].Scope
		candidate = carryStoredValue(secrets[index], candidate, input.Clear)
	}
	candidate.UpdatedAt = s.now().UnixMilli()
	candidate.UpdatedBy = actorLabel(actor)

	if index >= 0 {
		secrets[index] = candidate
	} else {
		secrets = append(secrets, candidate)
	}
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].Key < secrets[j].Key })
	if err := s.store.Save(ctx, secrets); err != nil {
		s.mu.Unlock()
		return View{}, err
	}
	s.mu.Unlock()

	// Both scopes are resynced: narrowing a scope has to strip material from
	// the projects that just lost the entry.
	s.resync(ctx, unionScope(previousScope, candidate.Scope))
	return s.view(candidate, nil), nil
}

// TestSSH probes one `ssh` entry from the host and reports the outcome. The
// private key never appears in the result or in any error.
func (s *Service) TestSSH(ctx context.Context, key string) (TestResult, error) {
	result, err := s.testSSH(ctx, key)
	s.record(ctx, audit.ActionSettingsSecretTest, key, audit.Meta{
		"ok":        result.OK,
		"latencyMs": result.LatencyMS,
	}, err)
	return result, err
}

func (s *Service) testSSH(ctx context.Context, key string) (TestResult, error) {
	if s.store == nil {
		return TestResult{}, ErrUnavailable
	}
	if s.prober == nil {
		return TestResult{}, ErrProbeUnavailable
	}
	secrets, err := s.load(ctx)
	if err != nil {
		return TestResult{}, err
	}
	index := indexOf(secrets, key)
	if index < 0 {
		return TestResult{}, ErrNotFound
	}
	secret := secrets[index]
	if secret.Kind != KindSSH || secret.SSH == nil {
		return TestResult{}, ErrWrongKind
	}
	if secret.SSH.PrivateKey == "" {
		return TestResult{}, ErrNoValue
	}
	return s.prober.Probe(ctx, *secret.SSH)
}

// EnvForProject returns the environment the vault publishes to one project's
// container, with the project's own keys already winning.
func (s *Service) EnvForProject(
	ctx context.Context,
	projectID string,
	ownKeys []string,
) (map[string]string, error) {
	secrets, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return EnvFor(secrets, projectID, ownKeys), nil
}

// InheritedForProject lists what the vault contributes to one project.
func (s *Service) InheritedForProject(
	ctx context.Context,
	projectID string,
	ownKeys []string,
) ([]Inherited, error) {
	secrets, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	return InheritedFor(secrets, projectID, ownKeys), nil
}

// SyncContainer converges one container onto the vault: files and SSH
// material through the materializer, environment through the environment
// port, and whatever the previous manifest owned but this pass does not is
// removed. Safe to call for a project that inherits nothing — that call is
// what cleans a container up after the last entry leaves its scope.
func (s *Service) SyncContainer(ctx context.Context, projectID, containerName string, ownKeys []string) error {
	if containerName == "" || (s.material == nil && s.environment == nil) {
		return nil
	}
	secrets, err := s.load(ctx)
	if err != nil {
		return err
	}
	material := MaterialFor(secrets, projectID, ownKeys)

	var previous Manifest
	if s.material != nil {
		previous, err = s.material.Manifest(ctx, containerName)
		if err != nil {
			return err
		}
		if err := s.material.Apply(ctx, containerName, material, StaleFiles(previous, material)); err != nil {
			return err
		}
	}
	if s.environment == nil {
		return nil
	}
	return s.environment.ApplyDiff(
		ctx,
		containerName,
		EnvFor(secrets, projectID, ownKeys),
		StaleEnvKeys(previous, material),
	)
}

// resync pushes the vault to every running container the change could touch.
// It is best-effort by design: a stopped or wedged container converges on its
// next start, and a failure here must not fail the admin's edit.
func (s *Service) resync(ctx context.Context, scope Scope) {
	projects := s.projectPort()
	if projects == nil || (s.material == nil && s.environment == nil) {
		return
	}
	// The HTTP request context dies with the response, so the fan-out keeps
	// the values (audit actor, deadlines aside) but not the cancellation.
	background := context.WithoutCancel(ctx)
	run := func() {
		s.resyncMu.Lock()
		defer s.resyncMu.Unlock()

		targets, err := projects.SecretTargets(background)
		if err != nil {
			log.Printf("secrets vault: list projects for resync: %v", err)
			return
		}
		synced, failed := 0, 0
		for _, target := range targets {
			if !target.Running || target.ContainerName == "" || !scope.Includes(target.ProjectID) {
				continue
			}
			if err := s.SyncContainer(
				background,
				target.ProjectID,
				target.ContainerName,
				target.OwnKeys,
			); err != nil {
				failed++
				log.Printf("secrets vault: sync %s: %v", target.ContainerName, err)
				continue
			}
			synced++
		}
		log.Printf("secrets vault: resynced %d running container(s), %d failed", synced, failed)
	}
	if s.resyncSync {
		run()
		return
	}
	go run()
}

func (s *Service) load(ctx context.Context) ([]Secret, error) {
	if s.store == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	secrets, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].Key < secrets[j].Key })
	return secrets, nil
}

func (s *Service) views(ctx context.Context, secrets []Secret) []View {
	shadowed := s.shadowMap(ctx, secrets)
	list := make([]View, 0, len(secrets))
	for _, secret := range secrets {
		list = append(list, s.view(secret, shadowed[secret.Key]))
	}
	return list
}

func (s *Service) view(secret Secret, shadowedIn []string) View {
	value := secret.secretValue()
	view := View{
		Key:         secret.Key,
		Kind:        secret.Kind,
		Path:        secret.Path,
		Scope:       secret.Scope,
		Description: secret.Description,
		UpdatedAt:   secret.UpdatedAt,
		UpdatedBy:   secret.UpdatedBy,
		Masked:      Mask(value),
		HasValue:    value != "",
		ShadowedIn:  shadowedIn,
	}
	if secret.Kind == KindSSH && secret.SSH != nil {
		host, user, port := EnvNamesForSSH(secret.SSH.Name)
		view.SSH = &SSHTargetView{
			Name:           secret.SSH.Name,
			Host:           secret.SSH.Host,
			Port:           secret.SSH.EffectivePort(),
			User:           secret.SSH.User,
			KnownHostsLine: secret.SSH.KnownHostsLine,
		}
		view.EnvVars = []string{host, user, port}
	}
	return view
}

// shadowMap answers "which projects override this env key with one of their
// own?" so the admin table can warn before an operator debugs a value that
// never arrives.
func (s *Service) shadowMap(ctx context.Context, secrets []Secret) map[string][]string {
	projects := s.projectPort()
	if projects == nil {
		return nil
	}
	hasEnv := false
	for _, secret := range secrets {
		if secret.Kind == KindEnv {
			hasEnv = true
			break
		}
	}
	if !hasEnv {
		return nil
	}
	targets, err := projects.SecretTargets(ctx)
	if err != nil {
		log.Printf("secrets vault: resolve shadowing: %v", err)
		return nil
	}
	shadowed := make(map[string][]string)
	for _, target := range targets {
		own := keySet(target.OwnKeys)
		for _, secret := range secrets {
			if secret.Kind != KindEnv || !secret.Scope.Includes(target.ProjectID) {
				continue
			}
			if own[secret.Key] {
				shadowed[secret.Key] = append(shadowed[secret.Key], target.ProjectID)
			}
		}
	}
	for key := range shadowed {
		sort.Strings(shadowed[key])
	}
	return shadowed
}

func (s *Service) record(ctx context.Context, action, key string, meta audit.Meta, err error) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, audit.Result(
		action,
		audit.Target{Type: audit.TargetSecret, ID: key},
		meta,
		err,
	))
}

func indexOf(secrets []Secret, key string) int {
	for index := range secrets {
		if secrets[index].Key == key {
			return index
		}
	}
	return -1
}

// carryStoredValue implements the write-only contract: a submitted blank
// value means "leave the stored one alone", and only an explicit clear wipes
// it.
func carryStoredValue(stored, candidate Secret, clear bool) Secret {
	if clear {
		return candidate
	}
	if candidate.Kind == KindSSH {
		if candidate.SSH != nil && candidate.SSH.PrivateKey == "" &&
			stored.Kind == KindSSH && stored.SSH != nil {
			candidate.SSH.PrivateKey = stored.SSH.PrivateKey
		}
		return candidate
	}
	if candidate.Value == "" && stored.Kind == candidate.Kind {
		candidate.Value = stored.Value
	}
	return candidate
}

// unionScope merges the scope an entry had with the scope it now has, so a
// narrowed scope still reaches the projects that must forget the entry.
func unionScope(previous, next Scope) Scope {
	if previous.All || next.All {
		return Scope{All: true}
	}
	merged := append(append([]string(nil), previous.ProjectIDs...), next.ProjectIDs...)
	return Scope{ProjectIDs: merged}.Normalize()
}

// scopeLabel is the audit-safe summary of a scope: how wide it is, never
// which secret it belongs to.
func scopeLabel(scope Scope) string {
	if scope.All {
		return "all"
	}
	return "projects:" + strconv.Itoa(len(scope.ProjectIDs))
}

func actorLabel(actor string) string {
	return strings.ToLower(strings.TrimSpace(actor))
}

func normalizeInput(input Input) (Secret, error) {
	key := strings.TrimSpace(input.Key)
	if !ValidKey(key) {
		return Secret{}, ErrInvalidKey
	}
	if !ValidKind(input.Kind) {
		return Secret{}, ErrInvalidKind
	}
	if len(input.Value) > maxValueBytes {
		return Secret{}, ErrValueTooLarge
	}

	secret := Secret{
		Key:         key,
		Kind:        input.Kind,
		Scope:       input.Scope.Normalize(),
		Description: normalizeDescription(input.Description),
	}
	if !secret.Scope.All && len(secret.Scope.ProjectIDs) == 0 {
		return Secret{}, ErrInvalidScope
	}
	switch input.Kind {
	case KindEnv:
		if strings.ContainsAny(input.Value, "\r\n\x00") {
			return Secret{}, ErrMultilineEnvValue
		}
		secret.Value = input.Value
	case KindFile:
		path, err := NormalizeFilePath(input.Path)
		if err != nil {
			return Secret{}, err
		}
		secret.Path = path
		secret.Value = input.Value
	case KindSSH:
		if input.SSH == nil {
			return Secret{}, ErrInvalidSSHTarget
		}
		target, err := normalizeSSHTarget(*input.SSH)
		if err != nil {
			return Secret{}, err
		}
		secret.SSH = &target
	}
	return secret, nil
}
