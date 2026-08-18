package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
	"github.com/futrx-com/remote.futrx.com/internal/integration/googleoauth"
	agentauth "github.com/futrx-com/remote.futrx.com/internal/service/agent/auth"
	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceplaybooks "github.com/futrx-com/remote.futrx.com/internal/service/playbooks"
	serviceportal "github.com/futrx-com/remote.futrx.com/internal/service/portal"
	servicepostrun "github.com/futrx-com/remote.futrx.com/internal/service/postrun"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
	serviceresources "github.com/futrx-com/remote.futrx.com/internal/service/resources"
	"github.com/futrx-com/remote.futrx.com/internal/service/runhub"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	"github.com/futrx-com/remote.futrx.com/internal/service/schedulecapability"
	serviceserverinfo "github.com/futrx-com/remote.futrx.com/internal/service/serverinfo"
	serviceshare "github.com/futrx-com/remote.futrx.com/internal/service/share"
	serviceskills "github.com/futrx-com/remote.futrx.com/internal/service/skills"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	servicetmux "github.com/futrx-com/remote.futrx.com/internal/service/tmux"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
	serviceuser "github.com/futrx-com/remote.futrx.com/internal/service/user"
	serviceusersettings "github.com/futrx-com/remote.futrx.com/internal/service/usersettings"
	"github.com/futrx-com/remote.futrx.com/internal/service/workspacehub"
)

type AuthStore interface {
	serviceauth.Store
}

type TmuxClient interface {
	servicetmux.SessionClient
}

type Dependencies struct {
	Chats             servicechat.Repository
	Projects          serviceproject.Repository
	ProjectSecrets    serviceproject.SecretsRepository
	ProjectAccess     serviceproject.AccessRepository
	ProjectShares     serviceshare.Repository
	Snapshots         servicesnapshot.Repository
	SnapshotArchive   servicesnapshot.Archive
	ProjectStorage    serviceproject.ProjectStorage
	WorkspacePreparer servicesnapshot.Preparer
	Database          servicesnapshot.Database
	Schedules         serviceschedule.Repository
	Auth              AuthStore
	Users             serviceuser.Repository
	UserSettings      serviceusersettings.Repository
	Notifications     servicenotify.Store
	Playbooks         serviceplaybooks.Repository
	GlobalSkills      serviceskills.GlobalRepository
	Usage             serviceusage.Repository
	ResourceSettings  serviceresources.Repository
	ResourceFleet     serviceresources.Fleet
	HostCollector     serviceserverinfo.Collector
	ProjectPortals    serviceportal.Repository
	// GitHistory backs the client portal's changelog. It is the same service
	// the project page uses; the composition root builds it once and hands it
	// to both.
	GitHistory     serviceportal.History
	Audit          serviceaudit.Store
	AuditRetention int
	// TrashRetention is how long a soft-deleted project survives before the
	// janitor purges it. Zero disables the sweep.
	TrashRetention    time.Duration
	AuthBaseURL       string
	ProjectContainers serviceproject.ContainerDependencies
	// HealthVitals is the cheap per-container usage probe the health monitor
	// polls. It is separate from ProjectContainers.Inspector because a full
	// inspection shells into the guest a dozen times and must never run on a
	// timer. Nil disables the monitor.
	HealthVitals    servicehealth.ContainerVitals
	HealthInterval  time.Duration
	AgentContainers provisioning.ContainerDependencies
	TmuxClient      TmuxClient
	ValidTmuxName   func(string) bool
	ScheduleLimits  ScheduleLimits
}

// ScheduleLimits mirrors the deployment's scheduled-task guardrails without
// coupling the service layer to the config package. Zero values disable a
// limit.
type ScheduleLimits struct {
	MinInterval        time.Duration
	MaxConcurrentRuns  int
	MaxTasksPerProject int
}

type Services struct {
	Chats         *servicechat.Service
	ChatAccess    *servicechat.AccessService
	Projects      *serviceproject.Service
	Shares        *serviceshare.Service
	Portals       *serviceportal.Service
	Prompt        *prompt.Service
	Schedules     *serviceschedule.Service
	ScheduleCaps  *schedulecapability.Registry
	AgentAuth     *agentauth.Registry
	Runs          *runhub.Hub
	Workspace     *workspacehub.Hub
	Auth          *serviceauth.Service
	Users         *serviceuser.Service
	UserSettings  *serviceusersettings.Service
	Notifications *servicenotify.Service
	Playbooks     *serviceplaybooks.Service
	Skills        *serviceskills.Catalog
	Tmux          *servicetmux.Service
	Access        *serviceauth.AccessVerifier
	GlobalSkills  *serviceskills.GlobalService
	Usage         *serviceusage.Service
	Resources     *serviceresources.Service
	Audit         *serviceaudit.Service
	Health        *servicehealth.Service
	Snapshots     *servicesnapshot.Service
	PostRun       *servicepostrun.Driver
}

func New(ctx context.Context, deps Dependencies) (Services, error) {
	if err := deps.AgentContainers.Validate(); err != nil {
		return Services{}, fmt.Errorf("agent container dependencies: %w", err)
	}
	if deps.Schedules == nil {
		return Services{}, errors.New("scheduled task repository is required")
	}

	// The audit recorder is built first so every other service can take it.
	auditRetention := deps.AuditRetention
	if auditRetention == 0 {
		auditRetention = serviceaudit.DefaultRetentionMonths
	}
	auditLog := serviceaudit.New(deps.Audit, serviceaudit.WithRetentionMonths(auditRetention))
	auditLog.StartJanitor(ctx, 24*time.Hour)

	workspace := workspacehub.New()
	var runs *runhub.Hub
	chats := notifyingChatRepository{
		Repository: deps.Chats,
		workspace:  workspace,
		running: func(id servicechat.ID) bool {
			return runs != nil && runs.IsRunning(id)
		},
	}
	projects := notifyingProjectRepository{Repository: deps.Projects, workspace: workspace}
	definitions := agentDefinitions()
	profiles := profilesFromDefinitions(definitions)

	// The fleet resource policy is loaded (or derived from host capacity on
	// first run) before any project can launch, so the very first container
	// of a fresh install already lands inside a host-aware envelope.
	resourceService := serviceresources.New(
		deps.ResourceSettings,
		hostFactsAdapter{collector: deps.HostCollector},
		deps.ResourceFleet,
	)
	if deps.ResourceSettings != nil {
		if err := resourceService.Ensure(ctx); err != nil {
			log.Printf("resources: converge fleet defaults: %v", err)
		}
		policy := resourcePolicyAdapter{resources: resourceService}
		deps.ProjectContainers.Policy = policy
		deps.ProjectContainers.Admission = policy
	}

	projectService := serviceproject.New(
		projects,
		deps.ProjectContainers,
		deps.ProjectSecrets,
		deps.ProjectAccess,
		serviceproject.WithAudit(auditLog),
		serviceproject.WithStorage(deps.ProjectStorage),
	)
	projectService.StartAgentBrowserReaper(ctx, 20*time.Minute)

	// Snapshots and projects each need the other: a delete takes a snapshot,
	// and a snapshot resolves the project it belongs to. The project service
	// is built first and told about snapshots afterwards, before anything
	// serves a request.
	var snapshotService *servicesnapshot.Service
	if deps.Snapshots != nil && deps.SnapshotArchive != nil {
		snapshotOptions := []servicesnapshot.Option{
			servicesnapshot.WithAudit(auditLog),
			servicesnapshot.WithDatabase(deps.Database),
			servicesnapshot.WithPreparer(deps.WorkspacePreparer),
		}
		if deps.ProjectSecrets != nil {
			snapshotOptions = append(snapshotOptions, servicesnapshot.WithSecrets(deps.ProjectSecrets))
		}
		snapshotService = servicesnapshot.New(
			deps.Snapshots, deps.SnapshotArchive, projectService, snapshotOptions...,
		)
		projectService.SetSnapshots(snapshotService)
	}
	projectService.StartTrashJanitor(ctx, deps.TrashRetention)
	runs = runhub.New(chats)
	runs.SetRunningSubscriber(func(id servicechat.ID, _ bool) {
		chats.publishChat(context.Background(), id)
	})

	var tmuxResolver servicechat.TmuxResolver
	if deps.TmuxClient != nil {
		tmuxResolver = chatTmuxResolver{client: deps.TmuxClient, validName: deps.ValidTmuxName}
	}

	globalSkillService := serviceskills.NewGlobalService(deps.GlobalSkills, projectService)
	chatOptions := []servicechat.Option{servicechat.WithAudit(auditLog)}
	if globalSkillService != nil {
		chatOptions = append(
			chatOptions,
			servicechat.WithDefaultSkills(globalSkillDefaults{global: globalSkillService}),
		)
	}
	chatService := servicechat.New(
		chats,
		chatProjectResolver{projects: projectService},
		tmuxResolver,
		runs,
		chatOptions...,
	)
	chatAccessService := servicechat.NewAccessService(chatService, projectService)
	agents := agent.NewRegistry()
	agentAuth := agentauth.NewRegistry()
	for index, definition := range definitions {
		provider := definition.provider(projectService, deps.AgentContainers)
		if string(provider.ID()) != profiles[index].ID {
			return Services{}, fmt.Errorf(
				"agent registration mismatch: provider %q has profile %q",
				provider.ID(), profiles[index].ID,
			)
		}
		if err := agents.Register(provider); err != nil {
			return Services{}, err
		}
		authBinding := definition.authBinding()
		if authBinding.ID() != provider.ID() {
			return Services{}, fmt.Errorf(
				"agent auth registration mismatch: binding %q has provider %q",
				authBinding.ID(), provider.ID(),
			)
		}
		if err := agentAuth.Register(authBinding); err != nil {
			return Services{}, err
		}
	}
	userService := serviceuser.New(deps.Users, serviceuser.WithAudit(auditLog))
	authService, err := newAuth(ctx, deps.Auth, userService, deps.AuthBaseURL, auditLog)
	if err != nil {
		return Services{}, err
	}
	scheduleCaps := schedulecapability.New(deps.AuthBaseURL)
	// The usage ledger is built before the notification service so the weekly
	// cost digest has a source to aggregate; without a ledger the digest loop
	// simply never starts.
	var usageService *serviceusage.Service
	notifyOptions := []servicenotify.Option{}
	if deps.Usage != nil {
		usageService = serviceusage.New(deps.Usage, projectService, chats)
		notifyOptions = append(
			notifyOptions,
			servicenotify.WithDigestSource(usageDigestSource{usage: usageService}),
		)
	}
	notifications := servicenotify.New(ctx, deps.Notifications, deps.AuthBaseURL, notifyOptions...)
	runNotifications := &notifyObserver{
		notifications: notifications,
		chats:         chats,
		projects:      projectService,
		baseURL:       deps.AuthBaseURL,
	}
	// The post-run driver both observes settled runs and starts follow-up runs
	// through the same prompt service, and it has to keep out of chats the
	// scheduler owns. Neither collaborator exists yet, so it is built against
	// indirect handles that the two assignments below fill in — the same
	// late-binding shape the run hub already uses for its chat repository.
	var promptService *prompt.Service
	var scheduleService *serviceschedule.Service
	postRunDriver := servicepostrun.New(servicepostrun.Dependencies{
		Chats:     chats,
		Runs:      runs,
		Starter:   postRunStarter{prompts: &promptService},
		Schedules: postRunSchedules{schedules: &scheduleService},
		Notifier:  runNotifications,
	})
	promptOptions := []prompt.Option{
		prompt.WithScheduleToolIssuer(scheduleCaps),
		prompt.WithRunObserver(runNotifications),
		prompt.WithRunObserver(postRunDriver),
		prompt.WithAudit(auditLog),
	}
	if usageService != nil {
		promptOptions = append(promptOptions, prompt.WithUsageRecorder(usageService))
	}
	promptService = prompt.New(
		chats,
		deps.TmuxClient,
		projectService,
		runs,
		agents,
		promptOptions...,
	)
	scheduleService = serviceschedule.New(
		deps.Schedules,
		chatService,
		projectService,
		authService,
		scheduledPromptExecutor{prompts: promptService},
		serviceschedule.WithMinInterval(deps.ScheduleLimits.MinInterval),
		serviceschedule.WithMaxConcurrentRuns(deps.ScheduleLimits.MaxConcurrentRuns),
		serviceschedule.WithMaxTasksPerProject(deps.ScheduleLimits.MaxTasksPerProject),
		serviceschedule.WithRunObserver(runNotifications),
		serviceschedule.WithAudit(auditLog),
	)
	if err := scheduleService.Start(ctx); err != nil {
		return Services{}, fmt.Errorf("start scheduled tasks: %w", err)
	}
	// The playbook library is seeded on the first start that finds no
	// document, so a fresh install already offers the composer's one-click
	// prompts. A seeding failure is logged, never fatal: the library is a
	// convenience, not a precondition for running agents.
	playbookService := serviceplaybooks.New(deps.Playbooks, serviceplaybooks.WithAudit(auditLog))
	if deps.Playbooks != nil {
		if seeded, err := playbookService.Ensure(ctx); err != nil {
			log.Printf("playbooks: seed warning: %v", err)
		} else if seeded > 0 {
			log.Printf("playbooks: seeded %d built-in playbooks", seeded)
		}
	}

	userSettingsService := serviceusersettings.New(deps.UserSettings)
	skillService := serviceskills.New()
	skillCatalog := serviceskills.NewCatalog(skillService, projectService, authService).
		WithGlobalLibrary(globalSkillService)
	var accessVerifier *serviceauth.AccessVerifier
	if authService != nil {
		accessVerifier = serviceauth.NewAccessVerifier(authService, projectService)
	}
	var shareService *serviceshare.Service
	if deps.ProjectShares != nil {
		shareService = serviceshare.New(deps.ProjectShares, projectService)
	}
	var portalService *serviceportal.Service
	if deps.ProjectPortals != nil {
		portalOptions := []serviceportal.Option{serviceportal.WithAudit(auditLog)}
		if shareService != nil {
			portalOptions = append(portalOptions, serviceportal.WithShares(shareService))
		}
		if deps.GitHistory != nil {
			portalOptions = append(portalOptions, serviceportal.WithHistory(deps.GitHistory))
		}
		if usageService != nil {
			portalOptions = append(portalOptions, serviceportal.WithUsage(usageService))
		}
		portalService = serviceportal.New(
			deps.ProjectPortals,
			projectService,
			deps.AuthBaseURL,
			portalOptions...,
		)
	}
	var tmuxService *servicetmux.Service
	if deps.TmuxClient != nil {
		tmuxService = servicetmux.NewSessions(deps.TmuxClient)
	}

	// The health monitor is built last: it needs the project repository, the
	// workspace hub it broadcasts through, and the notification observer it
	// alerts through, all of which exist by now.
	healthService := servicehealth.New(servicehealth.Dependencies{
		Projects:  projects,
		Vitals:    deps.HealthVitals,
		Listeners: deps.ProjectContainers.Listeners,
		Publisher: workspace,
		Alerter:   runNotifications,
		Interval:  deps.HealthInterval,
	})
	healthService.Start(ctx)

	return Services{
		Chats:         chatService,
		ChatAccess:    chatAccessService,
		Projects:      projectService,
		Shares:        shareService,
		Portals:       portalService,
		Prompt:        promptService,
		Schedules:     scheduleService,
		ScheduleCaps:  scheduleCaps,
		AgentAuth:     agentAuth,
		Runs:          runs,
		Workspace:     workspace,
		Auth:          authService,
		Users:         userService,
		UserSettings:  userSettingsService,
		Notifications: notifications,
		Playbooks:     playbookService,
		Skills:        skillCatalog,
		Tmux:          tmuxService,
		Access:        accessVerifier,
		GlobalSkills:  globalSkillService,
		Usage:         usageService,
		Resources:     resourceService,
		Audit:         auditLog,
		Health:        healthService,
		Snapshots:     snapshotService,
		PostRun:       postRunDriver,
	}, nil
}

// hostFactsAdapter narrows the server-info collector to the capacity facts the
// resource policy needs, so the numbers an admin reads on the Info page and
// the numbers the aggregate guard enforces come from one source.
type hostFactsAdapter struct {
	collector serviceserverinfo.Collector
}

func (a hostFactsAdapter) Facts(ctx context.Context) serviceresources.HostFacts {
	if a.collector == nil {
		return serviceresources.HostFacts{}
	}
	snapshot := a.collector.Collect(ctx, time.Now())
	return serviceresources.HostFacts{
		MemoryBytes: snapshot.Memory.TotalBytes,
		CPUs:        snapshot.CPU.LogicalCores,
		DiskBytes:   snapshot.Storage.TotalBytes,
	}
}

// resourcePolicyAdapter translates between the resource service's own policy
// vocabulary and the ports the project service declares, keeping neither
// service dependent on the other's types.
type resourcePolicyAdapter struct {
	resources *serviceresources.Service
}

func (a resourcePolicyAdapter) Policy(ctx context.Context) serviceproject.ContainerPolicySnapshot {
	view := a.resources.Get(ctx)
	return serviceproject.ContainerPolicySnapshot{
		Defaults:             projectLimits(view.Settings.Defaults),
		MaxOverride:          projectLimits(view.Settings.MaxProjectOverride),
		MaxRunningContainers: view.Settings.MaxRunningContainers,
		Host: serviceproject.HostCapacity{
			MemoryBytes:        view.Host.MemoryBytes,
			CPUs:               view.Host.CPUs,
			DiskBytes:          view.Host.DiskBytes,
			ReserveMemoryBytes: view.Host.ReserveMemoryBytes,
			BudgetMemoryBytes:  view.Host.BudgetMemoryBytes,
			CommittedBytes:     view.Host.CommittedBytes,
			RunningContainers:  view.Host.RunningContainers,
		},
		DiskQuota: serviceproject.DiskQuotaSupport{
			Supported: view.DiskQuota.Supported,
			Pool:      view.DiskQuota.Pool,
			Driver:    view.DiskQuota.Driver,
			Detail:    view.DiskQuota.Detail,
		},
	}
}

func (a resourcePolicyAdapter) Validate(ctx context.Context, limits serviceproject.ContainerLimits) error {
	cores := 0.0
	if limits.CPU != "" {
		parsed, err := strconv.ParseFloat(limits.CPU, 64)
		if err != nil {
			return serviceproject.ErrInvalidLimits
		}
		cores = parsed
	}
	return a.resources.ValidateOverride(ctx, limits.Memory, cores, limits.Disk)
}

func (a resourcePolicyAdapter) AuthorizeStart(ctx context.Context, containerName, memoryLimit string, force bool) error {
	return a.resources.AuthorizeStart(ctx, containerName, memoryLimit, force)
}

func projectLimits(limits serviceresources.Limits) serviceproject.ContainerLimits {
	return serviceproject.ContainerLimits{
		CPU:    serviceproject.FormatCores(limits.CPU),
		Memory: limits.Memory,
		Disk:   limits.Disk,
	}
}

func (s Services) AuthEnabled() bool {
	return s.Auth != nil
}

func (s Services) Reconcile(ctx context.Context) error {
	if s.Projects == nil {
		return nil
	}
	return s.Projects.Reconcile(ctx)
}

// globalSkillDefaults adapts the global skills library to the chat service's
// default-skill port so an "always on" skill is preselected in every new
// project chat.
type globalSkillDefaults struct {
	global *serviceskills.GlobalService
}

func (d globalSkillDefaults) DefaultSkills(
	ctx context.Context,
	_ servicechat.ProjectID,
	provider servicechat.Provider,
) ([]servicechat.SkillRef, error) {
	defaults, err := d.global.DefaultSkills(ctx, serviceskills.Provider(provider))
	if err != nil {
		return nil, err
	}
	refs := make([]servicechat.SkillRef, 0, len(defaults))
	for _, skill := range defaults {
		refs = append(refs, servicechat.SkillRef{
			Name:     skill.Name,
			Command:  skill.Command,
			Provider: servicechat.Provider(skill.Provider),
			Source:   skill.Source,
		})
	}
	return refs, nil
}

// postRunStarter and postRunSchedules resolve their target at call time. The
// pointers are filled in during composition, long before an agent run can
// settle, so a nil dereference here would mean the composition root changed
// order — hence the explicit guards rather than a silent panic.
type postRunStarter struct {
	prompts **prompt.Service
}

func (s postRunStarter) Start(
	input prompt.StartInput,
	emitTransient func(servicechat.Event),
) (prompt.RunHandle, error) {
	if s.prompts == nil || *s.prompts == nil {
		return prompt.RunHandle{}, errors.New("prompt service is unavailable")
	}
	return (*s.prompts).Start(input, emitTransient)
}

type postRunSchedules struct {
	schedules **serviceschedule.Service
}

func (s postRunSchedules) HasTasksForChat(ctx context.Context, chatID servicechat.ID) bool {
	if s.schedules == nil || *s.schedules == nil {
		return false
	}
	return (*s.schedules).HasTasksForChat(ctx, chatID)
}

type scheduledPromptExecutor struct {
	prompts *prompt.Service
}

func (e scheduledPromptExecutor) StartScheduledPrompt(
	ctx context.Context,
	task serviceschedule.Task,
	text string,
) (serviceschedule.RunHandle, error) {
	if e.prompts == nil {
		return nil, errors.New("prompt service is unavailable")
	}
	run, err := e.prompts.Start(prompt.StartInput{
		ChatID: task.ChatID,
		Prompt: text,
		Actor: prompt.Actor{
			Email: task.OwnerEmail,
		},
		ScheduledTaskID: string(task.ID),
		ScheduledRunID:  task.ActiveRunID,
		ParentContext:   ctx,
	}, nil)
	if errors.Is(err, prompt.ErrPromptAlreadyRunning) {
		return nil, serviceschedule.ErrExecutorBusy
	}
	if err != nil {
		return nil, err
	}

	done := make(chan serviceschedule.RunResult, 1)
	go func() {
		defer close(done)
		result, ok := <-run.Done
		if !ok {
			done <- serviceschedule.RunResult{
				Err: errors.New("prompt completion channel closed without a result"),
			}
			return
		}
		done <- serviceschedule.RunResult{
			Output: result.Output,
			Err:    result.Err,
		}
	}()
	return scheduledPromptHandle{done: done}, nil
}

type scheduledPromptHandle struct {
	done <-chan serviceschedule.RunResult
}

func (h scheduledPromptHandle) Done() <-chan serviceschedule.RunResult {
	return h.done
}

// userDirectoryAdapter wraps *serviceuser.Service to satisfy
// serviceauth.UserDirectory. AddBootstrapAdmin is the one method the auth
// service needs that the regular user.Service.Add doesn't quite cover (no
// "addedBy" since it's the bootstrap path).
type userDirectoryAdapter struct {
	users *serviceuser.Service
}

func (a userDirectoryAdapter) IsAdmin(ctx context.Context, email string) (bool, error) {
	return a.users.IsAdmin(ctx, email)
}

func (a userDirectoryAdapter) IsRegistered(ctx context.Context, email string) (bool, error) {
	return a.users.IsRegistered(ctx, email)
}

func (a userDirectoryAdapter) AddBootstrapAdmin(ctx context.Context, email string) error {
	_, err := a.users.Add(ctx, email, serviceuser.RoleAdmin, "")
	return err
}

func (a userDirectoryAdapter) FirstAdmin(ctx context.Context) (*serviceauth.UserDirectoryEntry, error) {
	list, err := a.users.List(ctx)
	if err != nil {
		return nil, err
	}
	var oldest *serviceuser.User
	for i := range list {
		u := &list[i]
		if u.Role != serviceuser.RoleAdmin {
			continue
		}
		if oldest == nil || u.AddedAt < oldest.AddedAt {
			oldest = u
		}
	}
	if oldest == nil {
		return nil, nil
	}
	return &serviceauth.UserDirectoryEntry{Email: oldest.Email}, nil
}

func newAuth(
	ctx context.Context,
	store AuthStore,
	users *serviceuser.Service,
	baseURL string,
	auditLog serviceaudit.Recorder,
) (*serviceauth.Service, error) {
	if store == nil {
		return nil, errors.New("authentication store is required")
	}
	baseURL, err := serviceauth.NormalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	sessionKey, err := store.SessionKey(ctx)
	if err != nil {
		return nil, err
	}

	var directory serviceauth.UserDirectory
	if users != nil {
		directory = userDirectoryAdapter{users: users}
	}
	return serviceauth.New(
		ctx,
		store,
		directory,
		func(clientID, clientSecret, redirectURL string) serviceauth.OAuthProvider {
			return googleoauth.New(clientID, clientSecret, redirectURL)
		},
		baseURL,
		sessionKey,
		serviceauth.WithAudit(auditLog),
	)
}

type chatProjectResolver struct {
	projects *serviceproject.Service
}

func (r chatProjectResolver) WorkspaceForProject(ctx context.Context, id servicechat.ProjectID) (string, error) {
	return r.projects.WorkspaceForProject(ctx, serviceproject.ID(id))
}

type chatTmuxResolver struct {
	client    TmuxClient
	validName func(string) bool
}

func (r chatTmuxResolver) ValidName(name string) bool {
	return r.validName != nil && r.validName(name)
}

func (r chatTmuxResolver) Cwd(ctx context.Context, session string) (string, error) {
	return r.client.Cwd(session)
}
