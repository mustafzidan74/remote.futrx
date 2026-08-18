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
	serviceagentprefs "github.com/futrx-com/remote.futrx.com/internal/service/agentprefs"
	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
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
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
	servicesearch "github.com/futrx-com/remote.futrx.com/internal/service/search"
	serviceserverinfo "github.com/futrx-com/remote.futrx.com/internal/service/serverinfo"
	serviceshare "github.com/futrx-com/remote.futrx.com/internal/service/share"
	serviceskills "github.com/futrx-com/remote.futrx.com/internal/service/skills"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	servicetmux "github.com/futrx-com/remote.futrx.com/internal/service/tmux"
	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
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
	Chats           servicechat.Repository
	Projects        serviceproject.Repository
	ProjectSecrets  serviceproject.SecretsRepository
	ProjectAccess   serviceproject.AccessRepository
	ProjectShares   serviceshare.Repository
	Snapshots       servicesnapshot.Repository
	SnapshotArchive servicesnapshot.Archive
	// Screenshots groups the three ports behind preview captures. Any of them
	// missing leaves the screenshot routes reporting 503.
	Screenshots       ScreenshotDependencies
	ProjectStorage    serviceproject.ProjectStorage
	WorkspacePreparer servicesnapshot.Preparer
	Database          servicesnapshot.Database
	Schedules         serviceschedule.Repository
	Auth              AuthStore
	Users             serviceuser.Repository
	UserSettings      serviceusersettings.Repository
	Notifications     servicenotify.Store
	// Monitoring backs the public /healthz endpoint and the outbound
	// heartbeat. Nil leaves both unavailable.
	Monitoring servicemonitoring.Store
	// MonitoringLXD is the container daemon probe behind the "lxd" check of
	// /healthz. Nil reports that check as skipped rather than degraded.
	MonitoringLXD servicemonitoring.LXD
	// Version is stamped into /healthz and the "Remote started" event. It is
	// the only fact the public health endpoint reveals about this host.
	Version       string
	Transcription servicetranscribe.Store
	Playbooks     serviceplaybooks.Repository
	GlobalSkills  serviceskills.GlobalRepository
	// AgentPreferences backs the platform-wide agent reply preferences. Nil
	// leaves the panel unavailable and injects nothing into any run.
	AgentPreferences serviceagentprefs.Repository
	// GlobalSecrets backs the platform secrets vault. Nil leaves the vault
	// unavailable: projects keep their own secrets and nothing is inherited.
	GlobalSecrets serviceglobalsecrets.Store
	// SecretsContainers are the two container ports the vault materializes
	// through. Nil leaves entries stored but never pushed anywhere.
	SecretsContainers SecretsContainerDependencies
	// SSHProber runs the host-side connectivity check for an SSH target.
	SSHProber        serviceglobalsecrets.SSHProber
	Usage            serviceusage.Repository
	ResourceSettings serviceresources.Repository
	ResourceFleet    serviceresources.Fleet
	HostCollector    serviceserverinfo.Collector
	ProjectPortals   serviceportal.Repository
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

// SecretsContainerDependencies groups the container capabilities the secrets
// vault materializes through. They are supplied separately from
// ProjectContainers because the vault reaches containers the project service
// never asked it to touch (a background resync after an admin edit).
type SecretsContainerDependencies struct {
	Environment serviceglobalsecrets.ContainerEnvironment
	Material    serviceglobalsecrets.ContainerMaterializer
}

// ScreenshotDependencies groups what preview screenshots need: the record
// index, the PNG blobs, and the in-container browser that takes them. They are
// supplied together because a deployment either has all three or has none.
type ScreenshotDependencies struct {
	Records  servicescreenshot.Repository
	Blobs    servicescreenshot.Blobs
	Capturer servicescreenshot.Capturer
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
	Monitoring    *servicemonitoring.Service
	Transcription *servicetranscribe.Service
	Playbooks     *serviceplaybooks.Service
	AgentPrefs    *serviceagentprefs.Service
	Search        *servicesearch.Service
	GlobalSecrets *serviceglobalsecrets.Service
	Skills        *serviceskills.Catalog
	Tmux          *servicetmux.Service
	Access        *serviceauth.AccessVerifier
	GlobalSkills  *serviceskills.GlobalService
	Usage         *serviceusage.Service
	Resources     *serviceresources.Service
	Audit         *serviceaudit.Service
	Health        *servicehealth.Service
	Snapshots     *servicesnapshot.Service
	Screenshots   *servicescreenshot.Service
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
	// The search index is created late (it needs the chat access service) but
	// the repository decorator that feeds it is created here, so the handle is
	// filled in afterwards — the same late-binding shape the run hub uses.
	chatSearchIndex := &chatSearchIndexer{}
	chats := notifyingChatRepository{
		Repository: deps.Chats,
		workspace:  workspace,
		search:     chatSearchIndex,
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

	// The vault and the project service each need the other: a project sync
	// pulls from the vault, and a vault edit pushes to running project
	// containers. The vault is built first because it depends only on ports,
	// and it learns about projects immediately afterwards.
	var globalSecrets *serviceglobalsecrets.Service
	if deps.GlobalSecrets != nil {
		secretOptions := []serviceglobalsecrets.Option{
			serviceglobalsecrets.WithAudit(auditLog),
			serviceglobalsecrets.WithContainers(
				deps.SecretsContainers.Environment,
				deps.SecretsContainers.Material,
			),
		}
		if deps.SSHProber != nil {
			secretOptions = append(secretOptions, serviceglobalsecrets.WithSSHProber(deps.SSHProber))
		}
		globalSecrets = serviceglobalsecrets.New(deps.GlobalSecrets, secretOptions...)
	}

	projectOptions := []serviceproject.Option{
		serviceproject.WithAudit(auditLog),
		serviceproject.WithStorage(deps.ProjectStorage),
	}
	if globalSecrets != nil {
		projectOptions = append(
			projectOptions,
			serviceproject.WithGlobalSecrets(globalSecretsAdapter{secrets: globalSecrets}),
		)
	}
	projectService := serviceproject.New(
		projects,
		deps.ProjectContainers,
		deps.ProjectSecrets,
		deps.ProjectAccess,
		projectOptions...,
	)
	if globalSecrets != nil {
		globalSecrets.SetProjects(secretSyncTargets{projects: projectService})
	}
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
	userSettingsService := serviceusersettings.New(deps.UserSettings)
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
	// Preview screenshots depend on both the project directory (to find a
	// container) and the notification service (to push a picture out), so they
	// are built once both exist.
	var screenshotService *servicescreenshot.Service
	if deps.Screenshots.Records != nil && deps.Screenshots.Blobs != nil &&
		deps.Screenshots.Capturer != nil {
		screenshotService = servicescreenshot.New(
			deps.Screenshots.Records,
			deps.Screenshots.Blobs,
			deps.Screenshots.Capturer,
			projectService,
			servicescreenshot.WithAudit(auditLog),
			servicescreenshot.WithBaseURL(deps.AuthBaseURL),
			servicescreenshot.WithNotifier(screenshotNotifier{notifications: notifications}),
		)
	}
	// Voice dictation's optional server fallback. It holds no state beyond the
	// cached settings document, so it is built here and simply handed to the
	// transcription handler.
	transcription := servicetranscribe.New(ctx, deps.Transcription)
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
	// The reply-preference service is built before the prompt service so a run
	// can carry the preamble, and before the container stack is told about it
	// so the very first run already regenerates the managed AGENTS.md block.
	agentPreferences := serviceagentprefs.New(
		deps.AgentPreferences,
		serviceagentprefs.WithAudit(auditLog),
		serviceagentprefs.WithUserOverrides(userReplyLanguage{settings: userSettingsService}),
		serviceagentprefs.WithProjects(projectCreationDates{projects: projectService}),
	)
	if deps.AgentPreferences != nil {
		bindWorkspacePreferences(deps.AgentContainers.Workspace, agentPreferences)
	}

	promptOptions := []prompt.Option{
		prompt.WithScheduleToolIssuer(scheduleCaps),
		prompt.WithRunObserver(runNotifications),
		prompt.WithRunObserver(postRunDriver),
		prompt.WithAudit(auditLog),
		prompt.WithReplyPreferences(replyPreferencePreamble{prefs: agentPreferences}),
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

	// External uptime monitoring is built after the notification observer
	// because starting it announces the restart through that observer. A box
	// cannot alert about its own death from inside, so this is the half that
	// makes the outside world able to.
	monitoringService := servicemonitoring.New(ctx, servicemonitoring.Dependencies{
		Store:     deps.Monitoring,
		LXD:       deps.MonitoringLXD,
		Announcer: runNotifications,
		Version:   deps.Version,
	})
	monitoringService.Start(ctx)

	// Full-text chat search. The index is built in the background because a
	// large history takes seconds to walk and nothing else may wait on it;
	// live updates arrive through the notifying chat repository above, which
	// is why the indexer is attached to it rather than polled.
	searchService := servicesearch.New(
		chats,
		servicesearch.WithAccess(chatAccessService),
		servicesearch.WithProjects(projectService),
	)
	chatSearchIndex.attach(searchService)
	searchService.Start(ctx)

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
		Monitoring:    monitoringService,
		Transcription: transcription,
		Playbooks:     playbookService,
		AgentPrefs:    agentPreferences,
		Search:        searchService,
		GlobalSecrets: globalSecrets,
		Skills:        skillCatalog,
		Tmux:          tmuxService,
		Access:        accessVerifier,
		GlobalSkills:  globalSkillService,
		Usage:         usageService,
		Resources:     resourceService,
		Audit:         auditLog,
		Health:        healthService,
		Snapshots:     snapshotService,
		Screenshots:   screenshotService,
		PostRun:       postRunDriver,
	}, nil
}

// screenshotNotifier translates between the screenshot service's picture port
// and the notification service's sink vocabulary, so neither package imports
// the other's types.
type screenshotNotifier struct {
	notifications *servicenotify.Service
}

func (n screenshotNotifier) Configured() bool {
	return n.notifications.ImageSinksConfigured()
}

func (n screenshotNotifier) NeedsPublicLink() bool {
	return n.notifications.NeedsPublicLink()
}

func (n screenshotNotifier) SendImage(
	ctx context.Context,
	image servicescreenshot.Image,
) []servicescreenshot.DeliveryResult {
	results := n.notifications.SendImage(ctx, servicenotify.Event{
		Event:  servicenotify.KindScreenshot,
		Status: servicenotify.StatusFinished,
	}, servicenotify.Image{
		Filename: image.Filename,
		Data:     image.Data,
		Caption:  image.Caption,
		LinkURL:  image.LinkURL,
	})
	out := make([]servicescreenshot.DeliveryResult, 0, len(results))
	for _, result := range results {
		out = append(out, servicescreenshot.DeliveryResult{
			Sink:      result.Sink,
			Delivered: result.Delivered,
			Error:     result.Error,
		})
	}
	return out
}

// globalSecretsAdapter translates between the vault's own vocabulary and the
// port the project service declares, so neither package imports the other.
type globalSecretsAdapter struct {
	secrets *serviceglobalsecrets.Service
}

func (a globalSecretsAdapter) SyncContainer(
	ctx context.Context,
	projectID, containerName string,
	ownKeys []string,
) error {
	return a.secrets.SyncContainer(ctx, projectID, containerName, ownKeys)
}

func (a globalSecretsAdapter) InheritedForProject(
	ctx context.Context,
	projectID string,
	ownKeys []string,
) ([]serviceproject.InheritedSecret, error) {
	inherited, err := a.secrets.InheritedForProject(ctx, projectID, ownKeys)
	if err != nil {
		return nil, err
	}
	list := make([]serviceproject.InheritedSecret, 0, len(inherited))
	for _, entry := range inherited {
		list = append(list, serviceproject.InheritedSecret{
			Key:         entry.Key,
			Kind:        string(entry.Kind),
			Source:      entry.Source,
			Shadowed:    entry.Shadowed,
			Path:        entry.Path,
			Description: entry.Description,
		})
	}
	return list, nil
}

// secretSyncTargets is the reverse direction: the fleet listing the vault
// fans a change out over.
type secretSyncTargets struct {
	projects *serviceproject.Service
}

func (t secretSyncTargets) SecretTargets(ctx context.Context) ([]serviceglobalsecrets.SecretTarget, error) {
	targets, err := t.projects.SecretSyncTargets(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]serviceglobalsecrets.SecretTarget, 0, len(targets))
	for _, target := range targets {
		list = append(list, serviceglobalsecrets.SecretTarget{
			ProjectID:     target.ProjectID,
			ContainerName: target.ContainerName,
			Running:       target.Running,
			OwnKeys:       target.OwnKeys,
		})
	}
	return list, nil
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

// replyPreferencePreamble adapts the reply-preference service to the port the
// prompt service declares, so neither package imports the other's vocabulary.
type replyPreferencePreamble struct {
	prefs *serviceagentprefs.Service
}

func (p replyPreferencePreamble) RunPreamble(ctx context.Context, email, sub, projectID string) string {
	return p.prefs.RunPreamble(
		ctx,
		serviceagentprefs.Identity{Email: email, Sub: sub},
		projectID,
	)
}

// userReplyLanguage reads one user's personal reply-language override out of
// the user settings document. Settings are keyed by OAuth subject when the
// session had one and by email otherwise, which is why both halves travel.
type userReplyLanguage struct {
	settings *serviceusersettings.Service
}

func (u userReplyLanguage) ReplyLanguage(ctx context.Context, identity serviceagentprefs.Identity) string {
	if u.settings == nil {
		return ""
	}
	key, err := serviceusersettings.KeyFromSession(identity.Email, identity.Sub)
	if err != nil {
		return ""
	}
	settings, err := u.settings.Get(ctx, key)
	if err != nil {
		return ""
	}
	return settings.Agent.ReplyLanguage
}

// projectCreationDates answers the one project fact the "new projects only"
// scope needs, without handing the preference service the project service.
type projectCreationDates struct {
	projects *serviceproject.Service
}

func (d projectCreationDates) CreatedAt(ctx context.Context, projectID string) (int64, bool) {
	if d.projects == nil || projectID == "" {
		return 0, false
	}
	meta, err := d.projects.Get(ctx, serviceproject.ID(projectID))
	if err != nil {
		return 0, false
	}
	return meta.CreatedAt, true
}

// workspacePreferenceSink is the optional capability a workspace provisioner
// advertises when it can host the managed reply-preference block. The
// container stack is built before this package runs, so the binding is a
// runtime type assertion rather than a constructor argument.
type workspacePreferenceSink interface {
	SetReplyPreferences(func(ctx context.Context, projectID string) (string, error))
}

func bindWorkspacePreferences(
	workspace provisioning.WorkspaceProvisioner,
	prefs *serviceagentprefs.Service,
) {
	sink, ok := workspace.(workspacePreferenceSink)
	if !ok || sink == nil {
		return
	}
	sink.SetReplyPreferences(prefs.WorkspaceBlock)
}

// chatSearchIndexer is the late-bound handle the chat repository decorator
// pushes events through. It is a separate type rather than a plain pointer
// because the decorator is built before the index exists and must be a no-op
// until it does.
type chatSearchIndexer struct {
	search *servicesearch.Service
}

func (i *chatSearchIndexer) attach(search *servicesearch.Service) {
	if i != nil {
		i.search = search
	}
}

func (i *chatSearchIndexer) IndexChat(meta servicechat.Meta) {
	if i == nil || i.search == nil {
		return
	}
	i.search.IndexChat(meta)
}

func (i *chatSearchIndexer) IndexEvent(id servicechat.ID, event servicechat.Event) {
	if i == nil || i.search == nil {
		return
	}
	i.search.IndexEvent(id, event)
}

func (i *chatSearchIndexer) RemoveChat(id servicechat.ID) {
	if i == nil || i.search == nil {
		return
	}
	i.search.RemoveChat(id)
}
