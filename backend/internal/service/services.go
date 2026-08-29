package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
	"github.com/futrx-com/remote.futrx.com/internal/integration/googleoauth"
	agentauth "github.com/futrx-com/remote.futrx.com/internal/service/agent/auth"
	serviceendpoints "github.com/futrx-com/remote.futrx.com/internal/service/agentendpoints"
	serviceagentprefs "github.com/futrx-com/remote.futrx.com/internal/service/agentprefs"
	serviceagentquota "github.com/futrx-com/remote.futrx.com/internal/service/agentquota"
	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicedashboard "github.com/futrx-com/remote.futrx.com/internal/service/dashboard"
	servicedirect "github.com/futrx-com/remote.futrx.com/internal/service/directmodels"
	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicelighthouse "github.com/futrx-com/remote.futrx.com/internal/service/lighthouse"
	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceplaybooks "github.com/futrx-com/remote.futrx.com/internal/service/playbooks"
	serviceportal "github.com/futrx-com/remote.futrx.com/internal/service/portal"
	servicepostrun "github.com/futrx-com/remote.futrx.com/internal/service/postrun"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
	serviceproviderpool "github.com/futrx-com/remote.futrx.com/internal/service/providerpool"
	serviceresources "github.com/futrx-com/remote.futrx.com/internal/service/resources"
	servicerouting "github.com/futrx-com/remote.futrx.com/internal/service/routing"
	"github.com/futrx-com/remote.futrx.com/internal/service/runhub"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	"github.com/futrx-com/remote.futrx.com/internal/service/schedulecapability"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
	servicesearch "github.com/futrx-com/remote.futrx.com/internal/service/search"
	serviceserverinfo "github.com/futrx-com/remote.futrx.com/internal/service/serverinfo"
	serviceshare "github.com/futrx-com/remote.futrx.com/internal/service/share"
	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
	serviceskills "github.com/futrx-com/remote.futrx.com/internal/service/skills"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	servicesnippets "github.com/futrx-com/remote.futrx.com/internal/service/snippets"
	serviceteam "github.com/futrx-com/remote.futrx.com/internal/service/team"
	servicetmux "github.com/futrx-com/remote.futrx.com/internal/service/tmux"
	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
	serviceuser "github.com/futrx-com/remote.futrx.com/internal/service/user"
	serviceusersettings "github.com/futrx-com/remote.futrx.com/internal/service/usersettings"
	servicevisualdiff "github.com/futrx-com/remote.futrx.com/internal/service/visualdiff"
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
	Screenshots ScreenshotDependencies
	// Visual groups what before/after comparison needs. It reuses the
	// screenshot capturer rather than owning a second browser adapter: both
	// point the same headless Chromium at the same loopback preview.
	Visual VisualDependencies
	// Lighthouse groups what local page audits need: the history and the CLI
	// runner inside the container.
	Lighthouse        LighthouseDependencies
	ProjectStorage    serviceproject.ProjectStorage
	WorkspacePreparer servicesnapshot.Preparer
	Database          servicesnapshot.Database
	Schedules         serviceschedule.Repository
	// ScheduleHistory persists the per-task run log behind the History drawer.
	// Nil leaves history empty rather than failing a run.
	ScheduleHistory serviceschedule.HistoryRepository
	// ScheduleWorkspace runs the in-container probes scheduled tasks need.
	// Nil closes the commandExitCode gate and leaves run history without file
	// information.
	ScheduleWorkspace ScheduleWorkspaceCommands
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
	// AuxModel backs the optional auxiliary text model (a local Ollama or any
	// OpenAI-compatible endpoint) that writes chat titles, notification
	// summaries, commit subjects, and client-message translations. Nil leaves
	// every one of those jobs doing exactly what it did before: the feature is
	// a nice-to-have and is never load bearing.
	AuxModel serviceauxmodel.Store
	// Providers and ProviderUsage back the free-tier provider pool: the
	// registry of connected API providers and the append-only usage ledger
	// beside it. A nil registry leaves the pool unavailable, which means the
	// /api/providers routes report 503 and every auxiliary job set to "pool"
	// quietly falls back to the local endpoint.
	Providers     serviceproviderpool.Store
	AgentQuota    serviceagentquota.Store
	ProviderUsage serviceproviderpool.UsageLog
	// SiteWatch backs the always-on watcher for the operator's client
	// websites. Nil leaves the Client sites page reporting 503 and schedules
	// nothing.
	SiteWatch servicesitewatch.Store
	// Version is stamped into /healthz and the "Remote started" event. It is
	// the only fact the public health endpoint reveals about this host.
	Version       string
	Transcription servicetranscribe.Store
	Playbooks     serviceplaybooks.Repository
	// Snippets backs every user's personal prompt library and client message
	// templates. Nil leaves the /api/me/snippets routes reporting 503.
	Snippets     servicesnippets.Repository
	GlobalSkills serviceskills.GlobalRepository
	// AgentPreferences backs the platform-wide agent reply preferences. Nil
	// leaves the panel unavailable and injects nothing into any run.
	AgentPreferences serviceagentprefs.Repository
	// GlobalSecrets backs the platform secrets vault. Nil leaves the vault
	// unavailable: projects keep their own secrets and nothing is inherited.
	GlobalSecrets serviceglobalsecrets.Store
	// GitHub is the per-project repository automation store, and GitHubCLI is
	// the container port every git and gh invocation goes through. Either one
	// nil leaves the whole integration reporting 503, including the public
	// webhook route.
	GitHub    servicegithub.Store
	GitHubCLI servicegithub.CLI
	// SecretsContainers are the two container ports the vault materializes
	// through. Nil leaves entries stored but never pushed anywhere.
	SecretsContainers SecretsContainerDependencies
	// MCPServers is the platform MCP registry and ProjectMCP the per-project
	// override document. Either nil leaves the registry routes reporting 503
	// and nothing is written into any container.
	MCPServers servicemcp.Store
	ProjectMCP servicemcp.ProjectStore
	// MCPContainers is the container port the registry materializes and
	// probes through. Nil leaves entries stored but never pushed anywhere.
	MCPContainers servicemcp.Containers
	// AgentEndpoints is the register of third-party, vendor-published agent
	// endpoints, and AgentEndpointContainers the port its Test probe runs
	// through. A nil store leaves every chat on its vendor's own endpoint,
	// which is what the platform did before the register existed.
	AgentEndpoints          serviceendpoints.Store
	AgentEndpointContainers serviceendpoints.Containers
	// SSHProber runs the host-side connectivity check for an SSH target.
	SSHProber        serviceglobalsecrets.SSHProber
	Usage            serviceusage.Repository
	ResourceSettings serviceresources.Repository
	// ModelRouting backs the automatic model routing policy. Nil leaves every
	// run on the model its chat names, which is what happened before routing
	// existed.
	ModelRouting  servicerouting.Repository
	ResourceFleet serviceresources.Fleet
	HostCollector serviceserverinfo.Collector
	// Backups probes the host's backup marker directory for the home
	// dashboard's "no recent backup" finding. Nil leaves that alert off, which
	// is the right answer for a host that never installed the backup timer.
	Backups        servicedashboard.Backups
	ProjectPortals serviceportal.Repository
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

// LighthouseDependencies groups what local Lighthouse audits need. There is no
// blob port: the stored summary is small enough to live in the history itself.
type LighthouseDependencies struct {
	Records servicelighthouse.Repository
	Runner  servicelighthouse.Runner
}

// VisualDependencies groups what before/after comparison needs: the per-project
// baseline record, the page images, and the in-container browser. Like
// screenshots, a deployment either has all three or has none.
type VisualDependencies struct {
	Records  servicevisualdiff.Repository
	Blobs    servicevisualdiff.Blobs
	Capturer servicevisualdiff.Capturer
}

// ScheduleWorkspaceCommands is the container-shaped half of the scheduled-task
// workspace port. The composition root pairs it with the project catalog so
// the schedule service only ever names a project, never a container.
type ScheduleWorkspaceCommands interface {
	RunCommand(
		ctx context.Context,
		containerName string,
		shellCommand string,
		timeout time.Duration,
	) (string, int, error)
	GitStatus(
		ctx context.Context,
		containerName string,
	) (repository bool, head, status, diffStat string, err error)
	GitShowStat(ctx context.Context, containerName, ref string) (string, error)
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
	AuxModel      *serviceauxmodel.Service
	// Providers is the free-tier provider pool. Nil on a deployment with no
	// registry store.
	Providers *serviceproviderpool.Service
	// DirectModels answers a chat from a completion API — a pool provider or
	// the local model — for chats an operator pointed at one.
	DirectModels *servicedirect.Service
	// AgentQuota holds the last subscription window each agent CLI reported.
	AgentQuota *serviceagentquota.Service
	// AuxJobs drives the chat-shaped auxiliary jobs (a better title, the
	// search subtitle) off settled runs, and serves the "rename this chat"
	// action. Nil on a deployment with no auxiliary model store.
	AuxJobs        *AuxJobDriver
	SiteWatch      *servicesitewatch.Service
	Transcription  *servicetranscribe.Service
	Playbooks      *serviceplaybooks.Service
	Snippets       *servicesnippets.Service
	AgentPrefs     *serviceagentprefs.Service
	Search         *servicesearch.Service
	GlobalSecrets  *serviceglobalsecrets.Service
	MCP            *servicemcp.Service
	AgentEndpoints *serviceendpoints.Service
	GitHub         *servicegithub.Service
	Skills         *serviceskills.Catalog
	Tmux           *servicetmux.Service
	Access         *serviceauth.AccessVerifier
	GlobalSkills   *serviceskills.GlobalService
	Usage          *serviceusage.Service
	Resources      *serviceresources.Service
	ModelRouting   *servicerouting.Service
	Audit          *serviceaudit.Service
	Health         *servicehealth.Service
	Snapshots      *servicesnapshot.Service
	Screenshots    *servicescreenshot.Service
	Lighthouse     *servicelighthouse.Service
	Visual         *servicevisualdiff.Service
	PostRun        *servicepostrun.Driver
	Team           *serviceteam.Driver
	Dashboard      *servicedashboard.Service
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
	// The MCP registry is built before the agent providers because each
	// provider's run path materializes it: the port is handed to them through
	// the same container-dependency bundle every other capability arrives on.
	var mcpService *servicemcp.Service
	if deps.MCPServers != nil && deps.ProjectMCP != nil {
		mcpOptions := []servicemcp.Option{
			servicemcp.WithAudit(auditLog),
			servicemcp.WithContainers(deps.MCPContainers),
			servicemcp.WithProjects(mcpProjectTargets{projects: projectService}),
		}
		if globalSecrets != nil {
			mcpOptions = append(mcpOptions, servicemcp.WithSecrets(mcpSecretsAdapter{secrets: globalSecrets}))
		}
		mcpService = servicemcp.New(deps.MCPServers, deps.ProjectMCP, mcpOptions...)
		deps.AgentContainers.MCP = mcpProvisioner{mcp: mcpService}
	}
	// The third-party agent endpoint register is built here too, for the same
	// reason: the prompt service resolves a chat's endpoint on the run path,
	// so the register has to exist before that service is composed.
	var agentEndpointService *serviceendpoints.Service
	if deps.AgentEndpoints != nil {
		endpointOptions := []serviceendpoints.Option{
			serviceendpoints.WithAudit(auditLog),
			serviceendpoints.WithContainers(deps.AgentEndpointContainers),
			serviceendpoints.WithProjects(agentEndpointTargets{projects: projectService}),
		}
		if globalSecrets != nil {
			endpointOptions = append(
				endpointOptions,
				serviceendpoints.WithSecrets(agentEndpointSecrets{secrets: globalSecrets}),
			)
		}
		agentEndpointService = serviceendpoints.New(deps.AgentEndpoints, endpointOptions...)
	}
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
	// Automatic model routing is built before both the ledger and the prompt
	// service: the ledger prices its savings card against the policy's
	// default model, and the prompt service asks it which model answers each
	// turn. Without a store it stays nil, and both collaborators fall back to
	// the behaviour they had before routing existed.
	var routingService *servicerouting.Service
	if deps.ModelRouting != nil {
		routingService = servicerouting.New(
			deps.ModelRouting,
			servicerouting.WithAudit(auditLog),
			servicerouting.WithProviders(routableProviders{registry: agentAuth}),
		)
	}
	// The usage ledger is built before the notification service so the weekly
	// cost digest has a source to aggregate; without a ledger the digest loop
	// simply never starts.
	var usageService *serviceusage.Service
	notifyOptions := []servicenotify.Option{}
	if deps.Usage != nil {
		usageOptions := []serviceusage.Option{}
		if routingService != nil {
			usageOptions = append(
				usageOptions,
				serviceusage.WithRoutingSource(routingReference{routing: routingService}),
			)
		}
		usageService = serviceusage.New(deps.Usage, projectService, chats, usageOptions...)
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
	// Local page audits need the project directory to find a container and
	// nothing else, and are switched off the same way screenshots are when
	// either half is absent.
	var lighthouseService *servicelighthouse.Service
	if deps.Lighthouse.Records != nil && deps.Lighthouse.Runner != nil {
		lighthouseService = servicelighthouse.New(
			deps.Lighthouse.Records,
			deps.Lighthouse.Runner,
			projectService,
			servicelighthouse.WithAudit(auditLog),
		)
	}
	// Before/after comparison needs the same three things screenshots do, and
	// is switched off the same way when any of them is absent.
	var visualService *servicevisualdiff.Service
	if deps.Visual.Records != nil && deps.Visual.Blobs != nil && deps.Visual.Capturer != nil {
		visualService = servicevisualdiff.New(
			deps.Visual.Records,
			deps.Visual.Blobs,
			deps.Visual.Capturer,
			projectService,
			servicevisualdiff.WithAudit(auditLog),
		)
	}
	// Voice dictation's optional server fallback. It holds no state beyond the
	// cached settings document, so it is built here and simply handed to the
	// transcription handler.
	transcription := servicetranscribe.New(ctx, deps.Transcription)
	// The auxiliary model is built before the observers that may use it. It
	// is the platform's own small text model — never a coding agent — and
	// every one of its callers below installs it as an *option*: switched
	// off, unreachable, or slow, each of them keeps doing what it did before.
	// The free-tier provider pool is built before the auxiliary model,
	// because a job routed to "pool" needs it to exist. It reads its own
	// registry, installs the shipped seed templates once, and rebuilds this
	// month's usage counters from its ledger.
	var providerPool *serviceproviderpool.Service
	if deps.Providers != nil {
		poolOptions := []serviceproviderpool.Option{
			serviceproviderpool.WithAudit(auditLog),
		}
		if deps.ProviderUsage != nil {
			poolOptions = append(poolOptions, serviceproviderpool.WithUsageLog(deps.ProviderUsage))
		}
		if globalSecrets != nil {
			// A provider may name a Secrets-vault key instead of carrying an
			// inline credential, which is the shape that keeps one key in one
			// place. Without a vault only inline keys resolve.
			poolOptions = append(
				poolOptions,
				serviceproviderpool.WithSecrets(vaultKeyReader{secrets: globalSecrets}),
			)
		}
		providerPool = serviceproviderpool.New(ctx, deps.Providers, poolOptions...)
	}
	var auxModel *serviceauxmodel.Service
	var auxJobs *AuxJobDriver
	if deps.AuxModel != nil {
		auxOptions := []serviceauxmodel.Option{}
		if providerPool != nil {
			auxOptions = append(auxOptions, serviceauxmodel.WithPool(auxPool{pool: providerPool}))
		}
		auxModel = serviceauxmodel.New(ctx, deps.AuxModel, auxOptions...)
		auxJobs = newAuxJobDriver(auxModel, chats)
	}
	// The direct-model responder joins the pool and the local model behind one
	// list. It is built even when both are absent — it simply offers nothing,
	// and every chat runs an agent as before.
	directModels := servicedirect.New(directPool(providerPool), directLocal(auxModel))

	// The agent CLIs mention their plan windows mid-run; this keeps the last
	// one so the dashboard has something to show between runs.
	agentQuota := serviceagentquota.New(ctx, deps.AgentQuota)

	runNotifications := &notifyObserver{
		notifications: notifications,
		chats:         chats,
		projects:      projectService,
		baseURL:       deps.AuthBaseURL,
	}
	if auxModel != nil {
		// One sentence in a phone notification instead of the raw tail of the
		// agent's last message. A nil answer means the tail still goes out.
		runNotifications.summarizer = auxRunSummarizer{aux: auxModel}
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
	// Team mode drives the same settled runs as the post-run driver, one hop
	// at a time, through the same prompt service. It is registered after the
	// post-run driver so the two see a run in a stable order; neither knows
	// about the other, and the stand-down rule lives in postrun.Decide.
	teamDriver := serviceteam.New(serviceteam.Dependencies{
		ChatFactory: chatService,
		Events:      runs,
		Providers:   connectedProviders{registry: agentAuth},
		Skills:      globalSkillNames{global: globalSkillService},
	})
	promptOptions := []prompt.Option{
		prompt.WithDirectResponder(directModels),
		prompt.WithQuotaRecorder(agentQuota),
		prompt.WithScheduleToolIssuer(scheduleCaps),
		prompt.WithRunObserver(runNotifications),
		prompt.WithRunObserver(postRunDriver),
		prompt.WithRunObserver(teamDriver),
		prompt.WithAudit(auditLog),
		// The auxiliary jobs observer schedules its work and returns; it never
		// holds up a run. A nil driver is dropped by WithRunObserver itself.
		prompt.WithRunObserver(auxRunObserver(auxJobs)),
		prompt.WithReplyPreferences(replyPreferencePreamble{prefs: agentPreferences}),
	}
	if usageService != nil {
		promptOptions = append(promptOptions, prompt.WithUsageRecorder(usageService))
	}
	if routingService != nil {
		promptOptions = append(promptOptions, prompt.WithModelRouter(routingService))
	}
	if agentEndpointService != nil {
		promptOptions = append(
			promptOptions,
			prompt.WithAgentEndpoints(agentEndpointRuntime{endpoints: agentEndpointService}),
		)
	}
	promptService = prompt.New(
		chats,
		deps.TmuxClient,
		projectService,
		runs,
		agents,
		promptOptions...,
	)
	scheduleOptions := []serviceschedule.Option{
		serviceschedule.WithMinInterval(deps.ScheduleLimits.MinInterval),
		serviceschedule.WithMaxConcurrentRuns(deps.ScheduleLimits.MaxConcurrentRuns),
		serviceschedule.WithMaxTasksPerProject(deps.ScheduleLimits.MaxTasksPerProject),
		serviceschedule.WithRunObserver(runNotifications),
		serviceschedule.WithAudit(auditLog),
		serviceschedule.WithHistory(deps.ScheduleHistory),
	}
	if deps.ScheduleWorkspace != nil {
		scheduleOptions = append(scheduleOptions, serviceschedule.WithWorkspace(
			scheduleWorkspace{projects: projectService, commands: deps.ScheduleWorkspace},
		))
	}
	if usageService != nil {
		scheduleOptions = append(scheduleOptions, serviceschedule.WithUsageLookup(
			scheduleUsageLookup{usage: usageService},
		))
	}
	scheduleService = serviceschedule.New(
		deps.Schedules,
		chatService,
		projectService,
		authService,
		scheduledPromptExecutor{prompts: promptService},
		scheduleOptions...,
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
	// Personal snippets are per user and seeded lazily on that user's first
	// read, so nothing has to happen here beyond building the service; a
	// deployment without a store simply leaves the routes unavailable.
	var snippetService *servicesnippets.Service
	if deps.Snippets != nil {
		snippetService = servicesnippets.New(deps.Snippets)
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
	// The client-site watcher is built alongside it and for the mirror-image
	// reason: monitoring is how the outside world learns this box died, and
	// this is how this box learns somebody else's website did. It costs no
	// agent tokens and no container time — one HEAD request per site per
	// interval, on the platform host's own bandwidth.
	siteWatchService := servicesitewatch.New(ctx, servicesitewatch.Dependencies{
		Store:   deps.SiteWatch,
		Access:  siteWatchAccess{projects: projectService},
		Catalog: siteWatchCatalog{projects: projects, secrets: deps.ProjectSecrets},
		Alerter: runNotifications,
	})
	siteWatchService.Start(ctx)
	// The GitHub integration is built here because it needs nearly everything
	// above it: projects to resolve a container, chats and the prompt service
	// to turn an inbound issue into a run, and the notification observer to
	// report that run with the issue's own link attached.
	var gitHubService *servicegithub.Service
	if deps.GitHub != nil && deps.GitHubCLI != nil {
		gitHubService = servicegithub.New(
			deps.GitHub,
			deps.GitHubCLI,
			projectService,
			servicegithub.WithAudit(auditLog),
			servicegithub.WithChats(chatService),
			servicegithub.WithStarter(postRunStarter{prompts: &promptService}),
			servicegithub.WithNotifier(gitHubNotifier{observer: runNotifications}),
			servicegithub.WithBaseURL(deps.AuthBaseURL),
			servicegithub.WithCommitSubjects(auxCommitMessages{aux: auxModel}),
		)
	}
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
	// The home dashboard is assembled last: it reads every service above and
	// owns no store of its own.
	dashboardService := newDashboardService(
		projectService,
		chatAccessService,
		usageService,
		healthService,
		scheduleService,
		snapshotService,
		notifications,
		monitoringService,
		siteWatchService,
		resourceService,
		deps.Backups,
		deps.TrashRetention,
	)
	return Services{
		Chats:          chatService,
		Projects:       projectService,
		Schedules:      scheduleService,
		Runs:           runs,
		Skills:         skillCatalog,
		Access:         accessVerifier,
		ChatAccess:     chatAccessService,
		Shares:         shareService,
		Portals:        portalService,
		Prompt:         promptService,
		ScheduleCaps:   scheduleCaps,
		AgentAuth:      agentAuth,
		Workspace:      workspace,
		Auth:           authService,
		Users:          userService,
		UserSettings:   userSettingsService,
		Notifications:  notifications,
		Monitoring:     monitoringService,
		AuxModel:       auxModel,
		Providers:      providerPool,
		DirectModels:   directModels,
		AgentQuota:     agentQuota,
		AuxJobs:        auxJobs,
		SiteWatch:      siteWatchService,
		Transcription:  transcription,
		Playbooks:      playbookService,
		Snippets:       snippetService,
		AgentPrefs:     agentPreferences,
		Search:         searchService,
		GlobalSecrets:  globalSecrets,
		MCP:            mcpService,
		AgentEndpoints: agentEndpointService,
		GitHub:         gitHubService,
		Tmux:           tmuxService,
		GlobalSkills:   globalSkillService,
		Usage:          usageService,
		ModelRouting:   routingService,
		Resources:      resourceService,
		Audit:          auditLog,
		Health:         healthService,
		Snapshots:      snapshotService,
		Screenshots:    screenshotService,
		Lighthouse:     lighthouseService,
		Visual:         visualService,
		PostRun:        postRunDriver,
		Team:           teamDriver,
		Dashboard:      dashboardService,
	}, nil
}

// connectedProviders answers "which providers can actually run a turn right
// now" from the agent auth registry. Team mode needs it to pick a reviewer
// that is a genuine second opinion rather than a provider nobody logged in to.
type connectedProviders struct {
	registry *agentauth.Registry
}

func (p connectedProviders) Connected() []servicechat.Provider {
	if p.registry == nil {
		return nil
	}
	bindings := p.registry.Bindings()
	providers := make([]servicechat.Provider, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Authenticated() {
			providers = append(providers, servicechat.Provider(binding.ID()))
		}
	}
	return providers
}

// routableProviders is the routing service's view of the same registry the
// team driver reads: which agents this host has a live credential for. It is
// a second adapter rather than a shared one because the two services speak
// different vocabularies — team mode names chat providers, routing names
// plain ids.
type routableProviders struct {
	registry *agentauth.Registry
}

func (p routableProviders) Connected() []string {
	if p.registry == nil {
		return nil
	}
	bindings := p.registry.Bindings()
	providers := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Authenticated() {
			providers = append(providers, string(binding.ID()))
		}
	}
	return providers
}

// routingReference resolves the routing policy into the flat reference the
// usage ledger prices its savings card against. The ledger never sees a rule:
// it gets the three destinations to compare and a rule-id-to-name map.
type routingReference struct {
	routing *servicerouting.Service
}

func (r routingReference) RoutingReference(ctx context.Context) (serviceusage.RoutingReference, bool) {
	if r.routing == nil {
		return serviceusage.RoutingReference{}, false
	}
	policy, ok := r.routing.Defaults(ctx)
	if !ok {
		return serviceusage.RoutingReference{}, false
	}
	labels := make(map[string]string, len(policy.Rules))
	for _, rule := range policy.Rules {
		labels[rule.ID] = rule.Note
	}
	return serviceusage.RoutingReference{
		Enabled:        policy.Enabled,
		DefaultModel:   policy.Default.Model,
		DefaultKey:     policy.Default.Key(),
		CheapKey:       policy.Cheap.Key(),
		ExpensiveKey:   policy.Expensive.Key(),
		DefaultLabel:   policy.Default.Label(),
		CheapLabel:     policy.Cheap.Label(),
		ExpensiveLabel: policy.Expensive.Label(),
		RuleLabels:     labels,
	}, true
}

// globalSkillNames narrows the team's wish list to skills the operator has
// actually published. A library read failure is not an error worth stopping a
// review for, so it reports "nothing known" and the team falls back to its
// optimistic defaults.
type globalSkillNames struct {
	global *serviceskills.GlobalService
}

func (s globalSkillNames) GlobalSkillNames(ctx context.Context) []string {
	if s.global == nil {
		return nil
	}
	entries, err := s.global.List(ctx)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names
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

// mcpProvisioner is the agent-facing face of the MCP registry: providers know
// only "materialize whatever this project should have and tell me the config
// path", never the registry's shape.
type mcpProvisioner struct {
	mcp *servicemcp.Service
}

func (p mcpProvisioner) EnsureMCPServers(
	ctx context.Context,
	containerName, projectID, providerID string,
) (string, error) {
	configPath, err := p.mcp.EnsureContainer(ctx, projectID, containerName)
	if err != nil {
		return "", err
	}
	// Only Claude Code needs a path on its command line; codex reads its own
	// config file at startup, so it is told nothing.
	if providerID != servicemcp.ProviderClaude {
		return "", nil
	}
	return configPath, nil
}

// mcpSecretsAdapter narrows the vault to the one read the MCP registry makes:
// the values behind a project's ${KEY} placeholders, on the materialization
// path only.
type mcpSecretsAdapter struct {
	secrets *serviceglobalsecrets.Service
}

func (a mcpSecretsAdapter) ValuesForProject(
	ctx context.Context,
	projectID string,
	keys []string,
) (map[string]string, error) {
	return a.secrets.ValuesForProject(ctx, projectID, keys)
}

// agentEndpointRuntime is the run path's face of the endpoint register:
// the prompt service asks only "render this profile for this run", never how
// a vendor's compatibility mode is spelled.
type agentEndpointRuntime struct {
	endpoints *serviceendpoints.Service
}

func (a agentEndpointRuntime) RuntimeFor(
	ctx context.Context,
	endpointID, model string,
) (agent.Endpoint, error) {
	runtime, err := a.endpoints.RuntimeFor(ctx, endpointID, model)
	if err != nil {
		return agent.Endpoint{}, err
	}
	return agent.Endpoint{
		ID:    runtime.ID,
		Label: runtime.Label,
		CLI:   agent.ProviderID(runtime.CLI),
		Model: runtime.Model,
		Env:   runtime.Env,
		Args:  runtime.Args,
	}, nil
}

// agentEndpointSecrets narrows the vault to the one read the endpoint
// register makes: the value behind a platform-wide key, on the run and probe
// paths only.
type agentEndpointSecrets struct {
	secrets *serviceglobalsecrets.Service
}

func (a agentEndpointSecrets) PlatformValues(
	ctx context.Context,
	keys []string,
) (map[string]string, error) {
	return a.secrets.PlatformValues(ctx, keys)
}

// agentEndpointTargets resolves a project to its container for the Test probe.
type agentEndpointTargets struct {
	projects *serviceproject.Service
}

func (t agentEndpointTargets) EndpointTarget(
	ctx context.Context,
	projectID string,
) (serviceendpoints.Target, error) {
	meta, err := t.projects.Get(ctx, serviceproject.ID(projectID))
	if err != nil {
		return serviceendpoints.Target{}, err
	}
	return serviceendpoints.Target{
		ProjectID:     projectID,
		ContainerName: meta.ContainerName,
		Running:       meta.Status == serviceproject.StatusRunning,
	}, nil
}

// mcpProjectTargets resolves a project to its container for the Test probe.
type mcpProjectTargets struct {
	projects *serviceproject.Service
}

func (t mcpProjectTargets) MCPTarget(ctx context.Context, projectID string) (servicemcp.Target, error) {
	meta, err := t.projects.Get(ctx, serviceproject.ID(projectID))
	if err != nil {
		return servicemcp.Target{}, err
	}
	return servicemcp.Target{
		ProjectID:     projectID,
		ContainerName: meta.ContainerName,
		Running:       meta.Status == serviceproject.StatusRunning,
	}, nil
}

// vaultKeyReader is the provider pool's view of the Secrets vault: one key
// name in, one value out. It exists so the pool declares a port rather than
// importing the vault, and so the *only* value-returning vault call the pool
// can reach is this one.
type vaultKeyReader struct {
	secrets *serviceglobalsecrets.Service
}

func (r vaultKeyReader) Value(ctx context.Context, key string) (string, bool, error) {
	return r.secrets.PlatformValue(ctx, key)
}

// auxPool is the provider pool seen from the auxiliary model. The two
// packages describe the same request in their own vocabularies and neither
// imports the other; this is where the two words meet.
type auxPool struct {
	pool *serviceproviderpool.Service
}

func (p auxPool) Available() bool { return p.pool.Available() }

func (p auxPool) Complete(
	ctx context.Context,
	request serviceauxmodel.PoolRequest,
) (string, error) {
	return p.pool.CompleteAux(ctx, serviceproviderpool.AuxRequest{
		Job:          request.Job,
		Capability:   request.Capability,
		SystemPrompt: request.SystemPrompt,
		UserText:     request.UserText,
		MaxTokens:    request.MaxTokens,
	})
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
//
// postRunStarter is shared with the team driver: both start follow-up runs
// through the same prompt service and neither exists before it does.
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

// scheduleWorkspace bridges the schedule service's project-shaped workspace
// port onto the container-shaped adapter. It is the only place that knows a
// scheduled task's project has a container behind it.
type scheduleWorkspace struct {
	projects *serviceproject.Service
	commands ScheduleWorkspaceCommands
}

var _ serviceschedule.Workspace = scheduleWorkspace{}

func (w scheduleWorkspace) container(
	ctx context.Context,
	projectID serviceproject.ID,
) (string, error) {
	if w.projects == nil || w.commands == nil {
		return "", errors.New("the project workspace is unavailable")
	}
	meta, err := w.projects.Get(ctx, projectID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(meta.ContainerName) == "" {
		return "", errors.New("the project has no container")
	}
	return meta.ContainerName, nil
}

func (w scheduleWorkspace) RunCommand(
	ctx context.Context,
	projectID serviceproject.ID,
	shellCommand string,
	timeout time.Duration,
) (serviceschedule.CommandResult, error) {
	container, err := w.container(ctx, projectID)
	if err != nil {
		return serviceschedule.CommandResult{}, err
	}
	output, code, err := w.commands.RunCommand(ctx, container, shellCommand, timeout)
	if err != nil {
		return serviceschedule.CommandResult{}, err
	}
	return serviceschedule.CommandResult{Output: output, ExitCode: code}, nil
}

func (w scheduleWorkspace) GitSnapshot(
	ctx context.Context,
	projectID serviceproject.ID,
) (serviceschedule.GitSnapshot, error) {
	container, err := w.container(ctx, projectID)
	if err != nil {
		return serviceschedule.GitSnapshot{}, err
	}
	repository, head, status, diffStat, err := w.commands.GitStatus(ctx, container)
	if err != nil {
		return serviceschedule.GitSnapshot{}, err
	}
	return serviceschedule.GitSnapshot{
		Repository: repository,
		Head:       head,
		Status:     status,
		DiffStat:   diffStat,
	}, nil
}

func (w scheduleWorkspace) GitShowStat(
	ctx context.Context,
	projectID serviceproject.ID,
	ref string,
) (string, error) {
	container, err := w.container(ctx, projectID)
	if err != nil {
		return "", err
	}
	return w.commands.GitShowStat(ctx, container, ref)
}

// scheduleUsageLookup answers "what did this run cost" by summing the ledger
// entries the chat produced inside the run's window. The scheduler is a system
// caller, so it reads with admin scope; the answer only ever reaches a caller
// already authorized for the task.
type scheduleUsageLookup struct {
	usage *serviceusage.Service
}

var _ serviceschedule.UsageLookup = scheduleUsageLookup{}

func (l scheduleUsageLookup) RunUsage(
	ctx context.Context,
	chatID servicechat.ID,
	fromMS, toMS int64,
) (serviceschedule.RunUsage, bool) {
	if l.usage == nil || chatID == "" || fromMS <= 0 || toMS < fromMS {
		return serviceschedule.RunUsage{}, false
	}
	page, err := l.usage.Records(ctx, serviceusage.RecordQuery{
		From:   fromMS,
		To:     toMS,
		ChatID: string(chatID),
		Limit:  200,
	}, "", true)
	if err != nil || len(page.Records) == 0 {
		return serviceschedule.RunUsage{}, false
	}
	usage := serviceschedule.RunUsage{}
	var cost float64
	priced := false
	for _, record := range page.Records {
		usage.Tokens += record.TotalTokens()
		if record.CostUSD != nil {
			cost += *record.CostUSD
			priced = true
		}
	}
	if priced {
		usage.CostUSD = &cost
	}
	return usage, true
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

// directPool and directLocal keep a nil service out of an interface.
//
// A nil *Service assigned to an interface is not a nil interface, so the
// responder would call through it and panic. Both sides are genuinely optional
// — a deployment may have no providers, or no local model — so the conversion
// has to be explicit rather than incidental.
func directPool(pool *serviceproviderpool.Service) servicedirect.Pool {
	if pool == nil {
		return nil
	}
	return pool
}

func directLocal(local *serviceauxmodel.Service) servicedirect.Local {
	if local == nil {
		return nil
	}
	return local
}
