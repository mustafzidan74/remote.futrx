// remote.futrx is the self-hosted control plane for configured coding agents
// and their isolated project workspaces.
//
// Backend serves:
//   - Static SPA (Preact/Vite bundle) embedded via go:embed
//   - HTTP APIs for users, agents, chats, projects, files, and operations
//   - WebSockets for workspace state, agent runs, auth status, and terminals

package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	remote "github.com/futrx-com/remote.futrx.com"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
	"github.com/futrx-com/remote.futrx.com/internal/config"
	configconstants "github.com/futrx-com/remote.futrx.com/internal/config/constants"
	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/githubcli"
	containerlighthouse "github.com/futrx-com/remote.futrx.com/internal/integration/containers/lighthouse"
	containerscreenshot "github.com/futrx-com/remote.futrx.com/internal/integration/containers/screenshot"
	"github.com/futrx-com/remote.futrx.com/internal/integration/gitcli"
	"github.com/futrx-com/remote.futrx.com/internal/integration/hostarchive"
	"github.com/futrx-com/remote.futrx.com/internal/integration/hostbackup"
	"github.com/futrx-com/remote.futrx.com/internal/integration/hostfs"
	"github.com/futrx-com/remote.futrx.com/internal/integration/hostinfo"
	"github.com/futrx-com/remote.futrx.com/internal/integration/lxc"
	"github.com/futrx-com/remote.futrx.com/internal/integration/sshprobe"
	"github.com/futrx-com/remote.futrx.com/internal/integration/tmuxcli"
	"github.com/futrx-com/remote.futrx.com/internal/integration/updatecli"
	service "github.com/futrx-com/remote.futrx.com/internal/service"
	servicedrain "github.com/futrx-com/remote.futrx.com/internal/service/drain"
	servicegithistory "github.com/futrx-com/remote.futrx.com/internal/service/githistory"
	servicemaintenance "github.com/futrx-com/remote.futrx.com/internal/service/maintenance"
	serviceselfupdate "github.com/futrx-com/remote.futrx.com/internal/service/selfupdate"
	serviceserverinfo "github.com/futrx-com/remote.futrx.com/internal/service/serverinfo"
	serviceworkspacefiles "github.com/futrx-com/remote.futrx.com/internal/service/workspacefiles"
	serviceworkspaceide "github.com/futrx-com/remote.futrx.com/internal/service/workspaceide"
	"github.com/futrx-com/remote.futrx.com/internal/stores"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileproject"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileskillsglobal"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filesnapshot"
	"github.com/futrx-com/remote.futrx.com/internal/transport"
	"github.com/futrx-com/remote.futrx.com/internal/version"
)

func main() {
	// This executable is the process composition root. The sections below
	// follow dependency direction from configuration and outbound adapters to
	// application policy, inbound transport, and process-owned runtime work.

	// Configuration and composition inputs: load process settings, choose the
	// executable mode, and validate values shared by the layers composed below.
	ctx := context.Background()
	cfg := config.Load()
	// Non-server subcommands run and exit before anything is started: an
	// operator asking for a setup token does not want a whole platform
	// booted underneath them.
	if runCLICommand(ctx, cfg, os.Args) {
		return
	}

	// Persistence adapters: open file-backed repositories and the disposable,
	// durable indexes they own.
	storeSet, err := stores.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("init stores: %v", err)
	}
	publicHostname, err := config.PublicHostname(cfg.BaseURL)
	if err != nil {
		log.Fatalf("configure public hostname: %v", err)
	}

	// Outbound integrations and container composition: bind compiled agent
	// providers and LXD-backed capabilities behind application-facing contracts.
	agentModules, err := config.NewAgentModules()
	if err != nil {
		log.Fatalf("configure agent modules: %v", err)
	}

	lxcClient := lxc.New()
	containerStack := config.NewContainerStack(
		lxcClient,
		agentModules.Profiles(),
		config.ContainerStackOptions{
			AgentInstructions: provisioning.InstructionsTemplate(publicHostname),
			GlobalSkillsDir:   fileskillsglobal.Dir(cfg.DataDir),
			PublicHostname:    publicHostname,
			ProjectSecrets:    storeSet.ProjectSecrets,
		},
	)
	// Snapshot archives and trashed workspaces are host data, not DATA_DIR
	// metadata: they sit next to the live workspaces they were taken from.
	snapshotArchiver := hostarchive.NewArchiver(filesnapshot.ArchiveRoot)
	projectTrash := hostarchive.NewTrashStorage(filesnapshot.TrashRoot)

	// Application services and startup reconciliation: compose policy from
	// persistence contracts and outbound capabilities, then initialize it.
	maintenanceGuard := servicemaintenance.New(cfg.DataDir)
	// New runs are refused once a restart starts draining; see drainOnSignal.
	startGate := servicedrain.NewGate(maintenanceGuard)
	selfUpdateService := serviceselfupdate.New(
		version.Version,
		cfg.InstallDir,
		cfg.DataDir,
		updatecli.New(),
	)

	tmuxClient := tmuxcli.New()
	// One host collector serves both the server-info page and the resource
	// policy, so displayed capacity and enforced capacity never disagree.
	hostCollector := hostinfo.New()
	// The host backup marker is read, never written: the nightly snapshots are
	// the operator's `remote-backup` timer's job. Both the server-info page and
	// the home dashboard's "no recent backup" alert read this one prober.
	backupProber := hostbackup.New(hostbackup.DefaultRoot)
	// Built before the service set because the client portal's changelog
	// reads through it; the chat history routes take the same instance.
	gitHistoryService := servicegithistory.New(gitcli.NewHistoryClient())
	serviceSet, err := service.New(ctx, service.Dependencies{
		Chats:             storeSet.Chats,
		Projects:          storeSet.Projects,
		ProjectSecrets:    storeSet.ProjectSecrets,
		ProjectAccess:     storeSet.ProjectAccess,
		ProjectShares:     storeSet.ProjectShares,
		Snapshots:         storeSet.Snapshots,
		SnapshotArchive:   snapshotArchiver,
		ProjectStorage:    projectTrash,
		WorkspacePreparer: containerStack.Preparer,
		Database:          containerStack.Database,
		ProjectPortals:    storeSet.ProjectPortals,
		Schedules:         storeSet.Schedules,
		ScheduleHistory:   storeSet.ScheduleHistory,
		ScheduleWorkspace: containerStack.ScheduleWork,
		Auth:              storeSet.Auth,
		TwoFactor:         storeSet.TwoFactor,
		SessionRegistry:   storeSet.SessionRegistry,
		Users:             storeSet.Users,
		UserSettings:      storeSet.UserSettings,
		Notifications:     storeSet.Notifications,
		Monitoring:        storeSet.Monitoring,
		AuxModel:          storeSet.AuxModel,
		Providers:         storeSet.Providers,
		ProviderUsage:     storeSet.ProviderUsage,
		MonitoringLXD:     lxcClient,
		SiteWatch:         storeSet.SiteWatch,
		Version:           version.Version,
		Transcription:     storeSet.Transcription,
		Playbooks:         storeSet.Playbooks,
		Snippets:          storeSet.Snippets,
		AgentPreferences:  storeSet.AgentPreferences,
		GlobalSkills:      storeSet.GlobalSkills,
		GlobalSecrets:     storeSet.GlobalSecrets,
		GitHub:            storeSet.GitHub,
		// `git` and `gh` run inside the project's container, never on the
		// host, so the GitHub credential stays in the container's environment
		// where LXD already put it.
		GitHubCLI: githubcli.NewAdapter(lxcClient),
		SecretsContainers: service.SecretsContainerDependencies{
			Environment: containerStack.Environment,
			Material:    containerStack.Secrets,
		},
		MCPServers:    storeSet.MCPServers,
		ProjectMCP:    storeSet.ProjectMCP,
		MCPContainers: containerStack.MCP,

		AgentEndpoints: storeSet.AgentEndpoints,

		AgentQuota:              storeSet.AgentQuota,
		AgentEndpointContainers: containerStack.AgentEndpoints,
		SSHProber:               sshprobe.New(),
		Usage:                   storeSet.Usage,
		ResourceSettings:        storeSet.Resources,
		ModelRouting:            storeSet.ModelRouting,
		ResourceFleet:           containerStack.Resources,
		HostCollector:           hostCollector,
		Backups:                 backupProber,
		GitHistory:              gitHistoryService,
		Audit:                   storeSet.Audit,
		Journal:                 storeSet.Journal,
		AuditRetention:          cfg.Audit.RetentionMonths,
		TrashRetention:          cfg.Trash.Retention,
		AuthBaseURL:             cfg.BaseURL,
		ProjectContainers:       containerStack.ProjectDependencies(),
		HealthVitals:            containerStack.Inspection,
		HealthInterval:          cfg.Health.Interval,
		AgentContainers:         containerStack.AgentDependencies(),
		Push:                    storeSet.Push,
		AgentModules:            agentModules,
		AgentAPIKeys:            storeSet.AgentAPIKeys,
		AgentOptions: service.AgentOptions{
			CapabilityTimeout:          cfg.Agent.CapabilityTimeout,
			CapabilityCacheTTL:         cfg.Agent.CapabilityCacheTTL,
			DegradedCapabilityCacheTTL: cfg.Agent.DegradedCapabilityCacheTTL,
			CredentialSyncTimeout:      cfg.Agent.CredentialSyncTimeout,
			BrowserIdleTTL:             cfg.Agent.BrowserIdleTTL,
		},
		AuthOptions: service.AuthOptions{
			PendingLoginTTL:     cfg.Auth.PendingLoginTTL,
			EnrollmentTTL:       cfg.Auth.EnrollmentTTL,
			RecoveryCodeCount:   cfg.Auth.RecoveryCodeCount,
			SessionHistoryLimit: cfg.Auth.SessionHistoryLimit,
			SetupTokenTTL:       cfg.Auth.SetupTokenTTL,
		},
		TmuxClient:    tmuxClient,
		ValidTmuxName: tmuxcli.ValidName,
		ScheduleLimits: service.ScheduleLimits{
			MinInterval:        cfg.Schedule.MinInterval,
			MaxConcurrentRuns:  cfg.Schedule.MaxConcurrentRuns,
			MaxTasksPerProject: cfg.Schedule.MaxTasksPerProject,
		},
		// One file-backed store answers both screenshot ports; the capture
		// itself runs Playwright inside the project's own container.
		Screenshots: service.ScreenshotDependencies{
			Records:  storeSet.Screenshots,
			Blobs:    storeSet.Screenshots,
			Capturer: containerscreenshot.NewAdapter(lxcClient),
		},
		// Local page audits run the Lighthouse CLI in the project's own
		// container, against the same loopback preview the screenshots use.
		Lighthouse: service.LighthouseDependencies{
			Records: storeSet.Lighthouse,
			Runner:  containerlighthouse.NewAdapter(lxcClient),
		},
		// Before/after comparison keeps its own records and images but shares
		// the browser: one adapter, so the two features can never disagree
		// about what a page looks like.
		Visual: service.VisualDependencies{
			Records:  storeSet.Visual,
			Blobs:    storeSet.Visual,
			Capturer: containerscreenshot.NewAdapter(lxcClient),
		},
		PromptStartGate: startGate,
	})
	if err != nil {
		log.Fatalf("init services: %v", err)
	}
	log.Printf(
		"auth: local admin enabled; Google OAuth configured=%t; BASE_URL=%s",
		serviceSet.Auth.GoogleOAuthEnabled(),
		cfg.BaseURL,
	)
	if seeded, err := serviceSet.GlobalSkills.SeedBuiltins(ctx); err != nil {
		log.Printf("global skills: seed warning: %v", err)
	} else if seeded > 0 {
		log.Printf("global skills: installed %d built-in skills", seeded)
	}
	// On a first boot nobody exists to authorise the local-admin claim, so the
	// setup token is minted and printed here and nowhere else: the operator's
	// terminal is the one channel a passer-by loading the page cannot reach.
	// Issuing on every gated start also rotates it, so a token that leaked
	// before a restart is already dead.
	announceSetupToken(ctx, serviceSet.Auth, cfg.BaseURL, log.Writer())
	if err := serviceSet.Reconcile(ctx); err != nil {
		log.Printf("services: reconcile warning: %v", err)
	}

	// Inbound delivery and transport adapters: prepare embedded assets and
	// delivery-facing collaborators, then bind application services to HTTP and
	// WebSocket endpoints.
	static, err := fs.Sub(remote.PublicFS, "public")
	if err != nil {
		log.Fatal(err)
	}
	serverInfoService := serviceserverinfo.New(
		hostCollector,
		version.Version,
		cfg.DataDir,
		fileproject.WorkspaceRoot,
		serviceserverinfo.WithBackupProbe(backupProber),
	)
	selfUpdateService.SetAudit(serviceSet.Audit)
	workspaceFileService := serviceworkspacefiles.New(hostfs.NewWorkspaceFileStore())
	codeServerBaseURL, err := config.CodeServerBaseURL(cfg.BaseURL)
	if err != nil {
		log.Fatalf("configure IDE URL: %v", err)
	}

	handler, err := transport.NewHTTPHandler(transport.Dependencies{
		Services:       serviceSet,
		TmuxClient:     tmuxClient,
		Static:         static,
		DataDir:        cfg.DataDir,
		PublicHostname: publicHostname,
		ServerInfo:     serverInfoService,
		SelfUpdate:     selfUpdateService,
		Files:          workspaceFileService,
		GitHistory:     gitHistoryService,
		IDE:            serviceworkspaceide.New(codeServerBaseURL, fileproject.WorkspaceRoot),
		Templates:      containerStack.Templates,
		TrashRetention: cfg.Trash.Retention,
	})
	if err != nil {
		log.Fatalf("init http handler: %v", err)
	}

	// Runtime lifecycle: launch process-owned background work and start the
	// HTTP listener. Background scheduling stays at this composition boundary.
	address := cfg.Addr()
	server := transport.NewHTTPServer(address, handler)
	startChatIndexWarmup(
		ctx,
		storeSet,
		configconstants.StartupChatIndexWarmupChatLimit,
		log.Default(),
	)
	log.Printf("remote.futrx listening on %s", address)
	go drainOnSignal(server, startGate, serviceSet.Runs, drainTimeout())
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	// ListenAndServe returns as soon as Shutdown begins; give Shutdown the
	// moment it needs to close idle connections before the process ends.
	time.Sleep(time.Second)
}

// defaultDrainTimeout bounds how long a restart waits for runs in flight. The
// systemd unit's TimeoutStopSec must be longer, or systemd kills the process
// mid-wait.
const defaultDrainTimeout = 10 * time.Minute

// drainTimeout reads REMOTE_DRAIN_TIMEOUT (a Go duration, "0" to stop at
// once), falling back to defaultDrainTimeout.
func drainTimeout() time.Duration {
	raw := os.Getenv("REMOTE_DRAIN_TIMEOUT")
	if raw == "" {
		return defaultDrainTimeout
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < 0 {
		log.Printf("drain: ignoring REMOTE_DRAIN_TIMEOUT=%q, using %s", raw, defaultDrainTimeout)
		return defaultDrainTimeout
	}
	return parsed
}

// runWaiter is the part of the run hub a restart waits on.
type runWaiter interface {
	ActiveRuns() int
	WaitIdle(ctx context.Context) error
}

// drainOnSignal turns SIGTERM or SIGINT into a graceful stop: refuse new runs,
// let the runs in flight finish (up to limit), then close the HTTP server so
// main returns. The HTTP server keeps serving while it waits, so the people
// watching those runs still see them through to the end.
func drainOnSignal(server *http.Server, gate *servicedrain.Gate, runs runWaiter, limit time.Duration) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	sig := <-signals
	gate.Start()
	if active := runs.ActiveRuns(); active > 0 && limit > 0 {
		log.Printf("drain: %s received; refusing new runs and waiting up to %s for %d running", sig, limit, active)
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		go func() {
			// A second signal means the operator wants out now.
			<-signals
			log.Printf("drain: second signal, stopping without waiting")
			cancel()
		}()
		if err := runs.WaitIdle(ctx); err != nil {
			log.Printf("drain: stopping with %d run(s) still in flight", runs.ActiveRuns())
		} else {
			log.Printf("drain: all runs finished")
		}
		cancel()
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("drain: http shutdown: %v", err)
	}
}
