package templates

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/shared/output"
)

// provisionTimeout bounds one provisioning run. A WordPress or Laravel stack
// on a small host is minutes of apt and composer work, so the budget is
// generous; it exists to stop a wedged run from leaking a goroutine forever.
const provisionTimeout = 45 * time.Minute

// probeTimeout bounds the cheap marker/seed probes on the hot path (every
// project start pays one of them for a provisioning template).
const probeTimeout = 15 * time.Second

var _ serviceproject.ContainerTemplates = (*Service)(nil)

// Service applies templates to project containers. Provisioning runs in the
// background because it can take many minutes; callers observe progress
// through Status, which the project status payload surfaces.
type Service struct {
	catalog *Catalog
	runtime Runtime
	timeout time.Duration
	// secrets resolves a project's secret template inputs at provisioning
	// time. Optional: without it a template that declares secret inputs still
	// provisions, just with empty values for them.
	secrets serviceproject.SecretsRepository
	// previewHost is the platform's public hostname, used to derive the
	// <slug>--<port>.dev.<host> origin a stack should install itself on.
	previewHost string

	mu     sync.Mutex
	states map[string]State
}

// Option configures a template service. The zero configuration behaves
// exactly as before template inputs existed.
type Option func(*Service)

// WithSecrets lets provisioning read a project's secret inputs (an admin
// password, say) back out of the project secrets store on every run.
func WithSecrets(secrets serviceproject.SecretsRepository) Option {
	return func(s *Service) { s.secrets = secrets }
}

// WithPreviewHost supplies the platform's public hostname so a template can
// install itself on the URL the operator will actually open.
func WithPreviewHost(host string) Option {
	return func(s *Service) { s.previewHost = strings.TrimSpace(host) }
}

// NewService returns a template service backed by a container runtime.
func NewService(catalog *Catalog, runtime Runtime, options ...Option) *Service {
	service := &Service{
		catalog: catalog,
		runtime: runtime,
		timeout: provisionTimeout,
		states:  make(map[string]State),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Catalog exposes the immutable template set.
func (s *Service) Catalog() *Catalog { return s.catalog }

// Has reports whether name is a known template.
func (s *Service) Has(name string) bool { return s.catalog.Has(name) }

// DefaultName returns the template assigned when a caller requests none.
func (s *Service) DefaultName() string { return s.catalog.DefaultName() }

// List returns the catalog annotated with per-host availability of each
// template's dedicated pre-built image.
func (s *Service) List(ctx context.Context) []Descriptor {
	templates := s.catalog.List()
	out := make([]Descriptor, 0, len(templates))
	for _, template := range templates {
		descriptor := Descriptor{
			Name:          template.Name,
			Title:         template.Title,
			Description:   template.Description,
			Icon:          template.Icon,
			DefaultPorts:  template.DefaultPorts,
			Default:       template.Name == DefaultName,
			Provisions:    template.Provisions(),
			PrebuiltImage: template.ImageAlias(),
			Inputs:        template.Inputs,
		}
		if descriptor.PrebuiltImage != "" {
			descriptor.PrebuiltImageAvailable = s.imagePublished(ctx, descriptor.PrebuiltImage)
		}
		out = append(out, descriptor)
	}
	return out
}

// ImageFor returns the dedicated image alias a template's containers should
// launch from, or an empty string when the caller must fall back to the shared
// base image plus a provision script.
func (s *Service) ImageFor(ctx context.Context, name string) string {
	alias := s.catalog.Resolve(name).ImageAlias()
	if alias == "" || !s.imagePublished(ctx, alias) {
		return ""
	}
	return alias
}

func (s *Service) imagePublished(ctx context.Context, alias string) bool {
	if s.runtime == nil || !s.runtime.Available() {
		return false
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	published, err := s.runtime.ImageExists(pctx, alias)
	if err != nil {
		log.Printf("templates: probe image %s: %v", alias, err)
		return false
	}
	return published
}

// Ensure provisions a container for its template unless the marker file says a
// previous run (or a pre-built template image) already did. It returns a
// channel closed once any background work has settled; the lifecycle ignores
// it, tests and future callers can wait on it.
//
// Ensure is called on every convergence of a project container, not only on
// creation: the container rootfs is disposable, so a replaced container must
// be re-provisioned, and an interrupted run must be retried.
func (s *Service) Ensure(ctx context.Context, project serviceproject.Meta) <-chan struct{} {
	done := make(chan struct{})
	containerName := project.ContainerName
	template := s.catalog.Resolve(project.TemplateName())
	if !template.Provisions() || s.runtime == nil || !s.runtime.Available() {
		close(done)
		return done
	}

	observation := s.observe(ctx, containerName, template)
	observation.InFlight = s.inFlight(containerName)
	decision := Decide(template, observation)
	if !decision.Run {
		s.recordObserved(containerName, template, observation)
		close(done)
		return done
	}

	// The environment is resolved on the calling goroutine so a secrets-store
	// failure is logged next to the project that caused it.
	env := s.environment(ctx, project, template)

	s.begin(containerName, template)
	// Provisioning deliberately outlives the request that triggered it: the
	// caller is a container start, which must not block for minutes.
	go func() {
		defer close(done)
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
		defer cancel()
		s.finish(containerName, template, s.provision(runCtx, containerName, template, env))
	}()
	return done
}

// environment renders the TPL_* variables one provisioning run receives:
// every declared input (from project metadata, or its declared default for
// metadata written before the input existed), every secret input (read back
// out of the secrets store), and the project's preview origin.
func (s *Service) environment(
	ctx context.Context,
	project serviceproject.Meta,
	template Template,
) map[string]string {
	return template.Environment(
		Values(project.TemplateInputs),
		s.projectSecrets(ctx, project, template),
		s.PreviewURL(project.Slug, template),
	)
}

// projectSecrets reads back only the secret keys this template declares. A
// read failure provisions with empty values rather than failing the launch:
// the scripts treat an empty secret as "skip the step that needs it".
func (s *Service) projectSecrets(
	ctx context.Context,
	project serviceproject.Meta,
	template Template,
) map[string]string {
	names := template.SecretNames()
	if s.secrets == nil || len(names) == 0 {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	stored, err := s.secrets.List(pctx, project.ID)
	if err != nil {
		log.Printf("templates: read secrets for %s: %v", project.ID, err)
		return nil
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	out := make(map[string]string, len(names))
	for _, secret := range stored {
		if wanted[secret.Key] {
			out[secret.Key] = secret.Value
		}
	}
	return out
}

// PreviewURL is the public origin a template installs itself on:
// https://<slug>--<port>.dev.<public host>. It falls back to the in-container
// address when the deployment has no public hostname (local development), so
// a stack always has an absolute URL to configure itself with.
func (s *Service) PreviewURL(slug string, template Template) string {
	port := template.AdminPort()
	if port == 0 {
		return ""
	}
	if s.previewHost == "" || slug == "" {
		return "http://localhost:" + strconv.Itoa(port)
	}
	return "https://" + slug + "--" + strconv.Itoa(port) + ".dev." + s.previewHost
}

// ResolveTemplateInputs validates a create request's raw inputs against the
// named template. Unknown templates are rejected here too, so a caller cannot
// smuggle inputs past the create-time template check.
func (s *Service) ResolveTemplateInputs(
	name string,
	raw map[string]any,
	inputContext serviceproject.TemplateInputContext,
) (serviceproject.TemplateInputValues, error) {
	template, ok := s.catalog.Get(name)
	if !ok {
		return serviceproject.TemplateInputValues{}, serviceproject.ErrUnknownTemplate
	}
	resolution, err := template.ResolveInputs(raw, Context{
		ProjectName: inputContext.ProjectName,
		UserEmail:   inputContext.UserEmail,
	})
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			// Re-key the failure onto the project package's sentinel so the
			// HTTP layer maps the whole class without importing this one.
			return serviceproject.TemplateInputValues{}, fmt.Errorf(
				"%w: %s", serviceproject.ErrInvalidTemplateInput, inputMessage(err),
			)
		}
		return serviceproject.TemplateInputValues{}, err
	}
	return serviceproject.TemplateInputValues{
		Values:  resolution.Values,
		Secrets: resolution.Secrets,
	}, nil
}

// inputMessage strips this package's sentinel prefix so the operator reads
// "Admin email must be an email address", not the sentinel twice.
func inputMessage(err error) string {
	return strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": ")
}

// provision writes the seeds and runs the provision program. Seeding happens
// first so the agent instructions are in place even if the install fails.
func (s *Service) provision(
	ctx context.Context,
	containerName string,
	template Template,
	env map[string]string,
) error {
	for _, seed := range template.allSeeds() {
		if err := s.seed(ctx, containerName, seed); err != nil {
			return err
		}
	}
	program := ProvisionProgram(template)
	if program == "" {
		return nil
	}
	if out, err := s.runtime.RunScript(ctx, containerName, program, env); err != nil {
		return provisionError{
			template: template.Name,
			cause:    err,
			output:   output.TruncateTail(out, 2000),
		}
	}
	return nil
}

// seed writes one file, never overwriting an existing one: /workspace is
// durable and may already hold the user's own version of that path.
func (s *Service) seed(ctx context.Context, containerName string, seed Seed) error {
	exists, err := s.runtime.FileExists(ctx, containerName, seed.Target)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := s.runtime.EnsureDirectory(ctx, containerName, path.Dir(seed.Target)); err != nil {
		return err
	}
	return s.runtime.PushFile(ctx, containerName, seed.Content, seed.Target, seed.Mode)
}

func (s *Service) observe(ctx context.Context, containerName string, template Template) Observation {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	marker, err := s.runtime.FileExists(pctx, containerName, MarkerPath)
	if err != nil {
		log.Printf("templates: probe marker in %s: %v", containerName, err)
	}
	failure, err := s.runtime.FileExists(pctx, containerName, FailurePath)
	if err != nil {
		log.Printf("templates: probe failure marker in %s: %v", containerName, err)
	}
	observation := Observation{MarkerPresent: marker, FailurePresent: failure}
	// A template that installs into /workspace names a file there. Only when
	// the rootfs already claims success is the extra probe worth paying for.
	if template.WorkspaceMarker != "" && marker {
		present, err := s.runtime.FileExists(pctx, containerName, template.WorkspaceMarker)
		if err != nil {
			// Unknown means "do not re-run": a probe failure must not
			// reinstall a stack over a working workspace.
			log.Printf("templates: probe workspace marker in %s: %v", containerName, err)
			present = true
		}
		observation.WorkspaceMissing = !present
	}
	return observation
}

// Status reports one container's template provisioning state. In-memory state
// from a run this process started wins; otherwise the container's marker files
// are the source of truth, so the answer survives a backend restart.
func (s *Service) Status(ctx context.Context, containerName, name string) State {
	template := s.catalog.Resolve(name)
	base := State{Template: template.Name, Title: template.Title, LogPath: LogPath}
	if !template.Provisions() {
		base.Status = StatusNone
		base.LogPath = ""
		return base
	}
	if state, ok := s.state(containerName); ok && state.Template == template.Name {
		return state
	}
	if s.runtime == nil || !s.runtime.Available() {
		base.Status = StatusPending
		return base
	}
	base.Status = ObservedStatus(template, s.observe(ctx, containerName, template))
	return base
}

// TemplateStatus adapts Status to the project status payload and, once the
// stack is installed, points the operator at its admin sign-in.
func (s *Service) TemplateStatus(
	ctx context.Context,
	project serviceproject.Meta,
) serviceproject.TemplateStatus {
	state := s.Status(ctx, project.ContainerName, project.TemplateName())
	status := serviceproject.TemplateStatus{
		Name:       state.Template,
		Title:      state.Title,
		Status:     string(state.Status),
		Error:      state.Error,
		LogPath:    state.LogPath,
		StartedAt:  state.StartedAt,
		FinishedAt: state.FinishedAt,
	}
	if state.Status == StatusDone {
		status.Admin = s.adminAccess(project)
	}
	return status
}

// adminAccess describes where to sign in to what the template installed. It
// carries the secret's NAME, never its value: revealing the password stays a
// separate, audited read of the project secrets endpoint.
func (s *Service) adminAccess(project serviceproject.Meta) *serviceproject.TemplateAdmin {
	template := s.catalog.Resolve(project.TemplateName())
	if template.AdminAccess == nil {
		return nil
	}
	origin := s.PreviewURL(project.Slug, template)
	if origin == "" {
		return nil
	}
	return &serviceproject.TemplateAdmin{
		Label:          template.AdminAccess.Label,
		URL:            origin + template.AdminAccess.Path,
		User:           project.TemplateInputs[template.AdminAccess.UserInput],
		PasswordSecret: template.AdminAccess.PasswordSecret,
	}
}

func (s *Service) state(containerName string) (State, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[containerName]
	return state, ok
}

func (s *Service) inFlight(containerName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.states[containerName].Status == StatusRunning
}

func (s *Service) begin(containerName string, template Template) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[containerName] = State{
		Template:  template.Name,
		Title:     template.Title,
		Status:    StatusRunning,
		LogPath:   LogPath,
		StartedAt: time.Now().UnixMilli(),
	}
}

func (s *Service) finish(containerName string, template Template, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[containerName]
	state.Template = template.Name
	state.Title = template.Title
	state.LogPath = LogPath
	state.FinishedAt = time.Now().UnixMilli()
	if err != nil {
		state.Status = StatusFailed
		state.Error = err.Error()
		log.Printf("templates: provision %s in %s: %v", template.Name, containerName, err)
	} else {
		state.Status = StatusDone
		state.Error = ""
	}
	s.states[containerName] = state
}

func (s *Service) recordObserved(containerName string, template Template, observation Observation) {
	status := ObservedStatus(template, observation)
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.states[containerName]
	if ok && existing.Template == template.Name && existing.Status == status {
		return
	}
	s.states[containerName] = State{
		Template: template.Name,
		Title:    template.Title,
		Status:   status,
		LogPath:  LogPath,
	}
}

// ForgetTemplateState drops cached state for a container that no longer
// exists, so a slug reused by a later project starts from a clean answer.
func (s *Service) ForgetTemplateState(containerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, containerName)
}

type provisionError struct {
	template string
	cause    error
	output   string
}

func (e provisionError) Error() string {
	message := "provision template " + e.template + ": " + e.cause.Error()
	if e.output != "" {
		message += "; output: " + e.output
	}
	return message
}

func (e provisionError) Unwrap() error { return e.cause }
