package transport

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	service "github.com/futrx-com/remote.futrx.com/internal/service"
	serviceagentprefs "github.com/futrx-com/remote.futrx.com/internal/service/agentprefs"
	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicetemplates "github.com/futrx-com/remote.futrx.com/internal/service/container/templates"
	servicedashboard "github.com/futrx-com/remote.futrx.com/internal/service/dashboard"
	servicegithistory "github.com/futrx-com/remote.futrx.com/internal/service/githistory"
	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceportal "github.com/futrx-com/remote.futrx.com/internal/service/portal"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
	servicesearch "github.com/futrx-com/remote.futrx.com/internal/service/search"
	serviceselfupdate "github.com/futrx-com/remote.futrx.com/internal/service/selfupdate"
	serviceserverinfo "github.com/futrx-com/remote.futrx.com/internal/service/serverinfo"
	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
	serviceworkspacefiles "github.com/futrx-com/remote.futrx.com/internal/service/workspacefiles"
	serviceworkspaceide "github.com/futrx-com/remote.futrx.com/internal/service/workspaceide"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
	httphandlers "github.com/futrx-com/remote.futrx.com/internal/transport/http/handlers"
	httpmiddleware "github.com/futrx-com/remote.futrx.com/internal/transport/http/middleware"
	wstransport "github.com/futrx-com/remote.futrx.com/internal/transport/ws"
)

type TmuxClient interface {
	wstransport.TmuxSessionClient
}

type Dependencies struct {
	Services       service.Services
	TmuxClient     TmuxClient
	Static         fs.FS
	DataDir        string
	PublicHostname string
	ServerInfo     *serviceserverinfo.Service
	SelfUpdate     *serviceselfupdate.Service
	Files          *serviceworkspacefiles.Service
	GitHistory     *servicegithistory.Service
	IDE            *serviceworkspaceide.Service
	Templates      *servicetemplates.Service
	// TrashRetention is how long a soft-deleted project survives; the trash
	// listing reports the resulting expiry per project.
	TrashRetention time.Duration
}

func NewHTTPHandler(deps Dependencies) (http.Handler, error) {
	agentAuthBindings := deps.Services.AgentAuth.Bindings()
	// A nil audit service is still a usable Recorder (its methods are
	// nil-safe), so handlers can take it unconditionally.
	var auditLog serviceaudit.Recorder = deps.Services.Audit
	var auth httptransport.RouteRegistrar
	var middleware httptransport.Middleware
	if deps.Services.Auth != nil {
		auth = httphandlers.NewAuthHandler(deps.Services.Auth, deps.Services.Access, auditLog).
			WithShares(deps.Services.Shares)
		providerAuthPrefixes := make([]string, 0, len(agentAuthBindings)*2)
		for _, binding := range agentAuthBindings {
			provider := string(binding.ID())
			providerAuthPrefixes = append(
				providerAuthPrefixes,
				"/api/"+provider+"/",
				"/ws/"+provider+"/auth-status",
			)
		}
		middleware = httpmiddleware.NewAuth(deps.Services.Auth).
			RequireLocalAdminSetup(deps.Services.Auth.LocalAdminConfigured).
			RequireProviderLogin(
				deps.Services.AgentAuth.AnyAuthenticated,
				providerAuthPrefixes...,
			)
	}

	gate := newAccessGate(deps.Services.Auth, deps.Services.Projects, deps.Services.Chats)

	uploads, err := httptransport.NewUploadHandlerWithGate(deps.Services.Chats, deps.DataDir, gate)
	if err != nil {
		return nil, err
	}
	uploads = uploads.WithAudit(auditLog)
	chatSocket := wstransport.NewChatSocket(deps.Services.Chats, deps.Services.Runs, deps.Services.Prompt)
	terminalSocket := wstransport.NewContainerTerminalSocket(
		deps.Services.Chats,
		deps.Services.Projects,
	).WithAudit(auditLog)
	workspaceSocket := wstransport.NewWorkspaceSocket(
		deps.Services.Chats,
		deps.Services.Projects,
		deps.Services.Workspace,
	)
	if deps.Services.Health != nil {
		workspaceSocket = workspaceSocket.WithHealth(deps.Services.Health)
	}
	if deps.Services.Auth != nil {
		chatSocket = chatSocket.WithAccessChecker(gate)
		terminalSocket = terminalSocket.WithAccessChecker(gate)
		workspaceSocket = workspaceSocket.WithVisibility(gate)
	}
	// One handler serves both halves of the GitHub integration: the panel
	// routes, mounted under the project handler that has already checked
	// membership, and the public webhook route, registered on its own below.
	gitHubHandler := httphandlers.NewGitHubHandler(
		githubService(deps.Services.GitHub),
		deps.Services.Auth,
	).WithAudit(auditLog)
	// One handler serves both halves of the MCP registry too: the admin
	// routes registered on the mux, and the project panel mounted under the
	// project handler.
	mcpHandler := httphandlers.NewMCPHandler(
		mcpService(deps.Services.MCP),
		deps.Services.Auth,
	)
	scheduleHandler := httphandlers.NewScheduleHandler(
		deps.Services.Schedules,
		deps.Services.ScheduleCaps,
		deps.Services.Auth,
	)
	usageHandler := httphandlers.NewUsageHandler(deps.Services.Usage, deps.Services.Auth)
	chatHandler := httphandlers.NewChatHandler(
		deps.Services.Chats,
		deps.Services.ChatAccess,
		deps.Services.Auth,
		deps.Files,
		deps.GitHistory,
		deps.IDE,
	).WithSchedules(scheduleHandler).WithAudit(auditLog)

	return httptransport.NewHandler(httptransport.Handlers{
		Sessions: httphandlers.NewTmuxHandler(deps.Services.Tmux).WithAudit(auditLog),
		Chats:    chatHandler,
		Projects: httphandlers.NewProjectHandler(
			deps.Services.Projects,
			deps.Services.Users,
			deps.Services.Auth,
			deps.PublicHostname,
		).WithShares(deps.Services.Shares).
			WithSnapshots(deps.Services.Snapshots, deps.TrashRetention).
			WithScreenshots(deps.Services.Screenshots).
			WithPortal(portalService(deps.Services.Portals)).
			WithClientMessages(clientMessageService(deps.Services.Notifications)).
			WithGitHub(gitHubHandler).
			WithMCP(mcpHandler).
			WithUsage(usageHandler),
		ProjectHealth: httphandlers.NewProjectHealthHandler(
			deps.Services.Projects,
			deps.Services.Health,
			deps.Services.Auth,
		),
		// The home screen's single aggregating read. A deployment without the
		// aggregator answers 503 here rather than losing the route.
		Dashboard: httphandlers.NewDashboardHandler(
			dashboardService(deps.Services.Dashboard),
			deps.Services.Auth,
		),
		Users: httphandlers.NewUsersHandler(deps.Services.Users, deps.Services.Auth),
		AgentAuth: httphandlers.NewAgentAuthHandler(
			agentAuthBindings,
			deps.Services.Auth,
		).WithAudit(auditLog),
		UserSettings: httphandlers.NewUserSettingsHandler(
			deps.Services.UserSettings,
			deps.Services.Auth,
		),
		Notifications: httphandlers.NewNotificationsHandler(
			deps.Services.Notifications,
			deps.Services.Auth,
		),
		// External uptime monitoring. GET/HEAD /healthz is public and
		// session-less by design — the auth middleware only gates /api and
		// /ws, so the route reaches its own rate limiter untouched.
		Monitoring: httphandlers.NewMonitoringHandler(
			monitoringService(deps.Services.Monitoring),
			deps.Services.Auth,
		),
		// Voice input. The composer's browser path never reaches the server;
		// these routes exist for the optional server-transcription fallback
		// and its admin panel.
		Transcribe: httphandlers.NewTranscribeHandler(
			transcriptionService(deps.Services.Transcription),
			deps.Services.Auth,
		).WithAudit(auditLog),
		// The client portal is the one public, session-less page the
		// application serves; the auth middleware only gates /api and /ws, so
		// this route reaches its own token check.
		Portal: httphandlers.NewPortalHandler(portalService(deps.Services.Portals)),
		// The second public, session-less route: a 24h token that shows one
		// stored preview screenshot to a chat app that cannot carry pictures.
		ScreenshotLinks: screenshotLinkHandler(deps.Services.Screenshots),
		// The third: GitHub's own deliveries, authenticated by an HMAC
		// signature over the raw body rather than by a session.
		GitHubHooks: gitHubHookRoutes(gitHubHandler),
		ServerInfo:  httphandlers.NewServerInfoHandler(deps.ServerInfo),
		SelfUpdate:  httphandlers.NewSelfUpdateHandler(deps.SelfUpdate, deps.Services.Auth),
		Skills:      httphandlers.NewSkillHandler(deps.Services.Skills),
		GlobalSkills: httphandlers.NewGlobalSkillHandler(
			deps.Services.GlobalSkills,
			deps.Services.Auth,
		),
		Playbooks: httphandlers.NewPlaybookHandler(
			deps.Services.Playbooks,
			deps.Services.Auth,
		),
		// Personal snippets and client message templates. Every route derives
		// the owner from the session, so there is no admin view of them.
		Snippets: httphandlers.NewSnippetHandler(deps.Services.Snippets, deps.Services.Auth),
		AgentPreferences: httphandlers.NewAgentPreferencesHandler(
			agentPreferencesService(deps.Services.AgentPrefs),
			deps.Services.Auth,
		),
		Search: httphandlers.NewSearchHandler(
			searchService(deps.Services.Search),
			deps.Services.Auth,
		),
		GlobalSecrets: httphandlers.NewGlobalSecretsHandler(
			globalSecretsService(deps.Services.GlobalSecrets),
			deps.Services.Auth,
		),
		// The admin half of the MCP registry. Its project half is mounted by
		// the project handler, which has already resolved membership.
		MCPServers: mcpHandler,
		AdminResources: httphandlers.NewAdminResourcesHandler(
			deps.Services.Resources,
			httptransport.NewPrincipalResolver(deps.Services.Auth),
		),
		Templates:        httphandlers.NewTemplateHandler(deps.Templates),
		BrowserInspector: httphandlers.NewBrowserInspectorHandler(),
		Schedules:        scheduleHandler,
		Usage:            usageHandler,
		Audit:            httphandlers.NewAuditHandler(deps.Services.Audit, deps.Services.Auth),
		Uploads:          uploads,
		TmuxWS:           wstransport.NewTmuxSocket(deps.TmuxClient),
		TerminalWS:       terminalSocket,
		ChatWS:           chatSocket,
		WorkspaceWS:      workspaceSocket,
		AgentAuthWS:      wstransport.NewAgentAuthSocket(agentAuthBindings),
		Auth:             auth,
		Middleware:       middleware,
		Static:           httptransport.NewStaticHandler(deps.Static),
	}), nil
}

// dashboardService narrows the concrete home-dashboard aggregator to the
// transport's interface while keeping a nil service nil, so the route answers
// 503 instead of panicking on a deployment without one.
func dashboardService(service *servicedashboard.Service) httphandlers.DashboardService {
	if service == nil {
		return nil
	}
	return service
}

// screenshotLinkHandler keeps a nil screenshot service nil, so a deployment
// without one simply never registers the public route.
func screenshotLinkHandler(service *servicescreenshot.Service) httptransport.RouteRegistrar {
	if service == nil {
		return nil
	}
	handler := httphandlers.NewScreenshotLinkHandler(service)
	if handler == nil {
		return nil
	}
	return handler
}

// agentPreferencesService and searchService narrow their concrete services to
// the transport's interfaces while keeping a nil service nil, so the handlers
// answer 503 on a deployment without the backing store instead of panicking.
func agentPreferencesService(service *serviceagentprefs.Service) httphandlers.AgentPreferencesService {
	if service == nil {
		return nil
	}
	return service
}

func searchService(service *servicesearch.Service) httphandlers.SearchService {
	if service == nil {
		return nil
	}
	return service
}

// mcpService narrows the concrete MCP registry to the transport's interface
// while keeping a nil service nil, so the routes answer 503 on a deployment
// without a registry store instead of panicking.
func mcpService(service *servicemcp.Service) httphandlers.MCPService {
	if service == nil {
		return nil
	}
	return service
}

// globalSecretsService narrows the concrete vault service to the transport's
// interface while keeping a nil service nil, so the handler answers 503 on a
// deployment without a vault store instead of panicking.
func globalSecretsService(service *serviceglobalsecrets.Service) httphandlers.GlobalSecretsService {
	if service == nil {
		return nil
	}
	return service
}

// monitoringService narrows the concrete monitoring service to the
// transport's interface while keeping a nil service nil, so /healthz reports
// 503 instead of panicking on a deployment without a monitoring store.
func monitoringService(service *servicemonitoring.Service) httphandlers.MonitoringService {
	if service == nil {
		return nil
	}
	return service
}

// transcriptionService narrows the concrete transcription service to the
// transport's interface while keeping a nil service nil, so the handler can
// report 503 instead of panicking on a deployment without a settings store.
func transcriptionService(service *servicetranscribe.Service) httphandlers.TranscriptionService {
	if service == nil {
		return nil
	}
	return service
}

// githubService narrows the concrete GitHub service to the transport's
// interface while keeping a nil service nil, so a deployment without a
// settings store or a container runtime reports 503 instead of panicking.
func githubService(service *servicegithub.Service) httphandlers.GitHubService {
	if service == nil {
		return nil
	}
	return service
}

// gitHubHookRoutes keeps a nil handler out of the route table entirely: a
// typed nil pointer stored in the interface would still be dispatched to, and
// would panic on registration.
func gitHubHookRoutes(handler *httphandlers.GitHubHandler) httptransport.RouteRegistrar {
	if handler == nil {
		return nil
	}
	return handler
}

// portalService narrows the concrete portal service to the transport's
// interface while keeping a nil service nil, so handlers can report 503
// instead of panicking on a deployment without a portal store.
func portalService(service *serviceportal.Service) httphandlers.PortalService {
	if service == nil {
		return nil
	}
	return service
}

// clientMessageService keeps a nil notification service nil, so a deployment
// without one answers 503 on /api/projects/{id}/client-message rather than
// panicking.
func clientMessageService(service *servicenotify.Service) httphandlers.ClientMessageService {
	if service == nil {
		return nil
	}
	return service
}

func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return httptransport.NewServer(addr, handler)
}

// accessGate is the small adapter that satisfies the per-WS / per-upload
// gating interfaces. It centralizes "who is the caller?" and "can they
// reach this project?" lookups so each socket / upload site doesn't have
// to wire up the auth + project + chat services itself.
type accessGate struct {
	auth      *serviceauth.Service
	principal *httptransport.PrincipalResolver
	projects  *serviceproject.Service
	chats     chatProjectFinder
}

type chatProjectFinder interface {
	Get(ctx context.Context, id servicechat.ID) (servicechat.Meta, error)
}

func newAccessGate(auth *serviceauth.Service, projects *serviceproject.Service, chats chatProjectFinder) *accessGate {
	if auth == nil {
		return nil
	}
	return &accessGate{
		auth: auth, principal: httptransport.NewPrincipalResolver(auth),
		projects: projects, chats: chats,
	}
}

func (g *accessGate) CallerAndAdmin(ctx context.Context, r *http.Request) (string, bool, error) {
	if g == nil || g.auth == nil {
		return "", true, nil
	}
	session, err := g.principal.Session(r)
	if err != nil || session == nil {
		return "", false, err
	}
	isAdmin, _ := g.auth.IsAdmin(ctx, session.Email)
	return session.Email, isAdmin, nil
}

// CallerSubject returns the OAuth subject of the caller's session, empty for
// the local admin (whose settings are keyed by email instead). It exists so an
// agent run can find the personal preferences of the person who started it.
func (g *accessGate) CallerSubject(r *http.Request) string {
	if g == nil || g.principal == nil {
		return ""
	}
	session, err := g.principal.Session(r)
	if err != nil || session == nil {
		return ""
	}
	return session.Sub
}

func (g *accessGate) HasAccess(ctx context.Context, projectID serviceproject.ID, email string) (bool, error) {
	if g == nil || g.projects == nil {
		return false, nil
	}
	return g.projects.HasAccess(ctx, projectID, email)
}

func (g *accessGate) CanUploadToChat(ctx context.Context, r *http.Request, chatID servicechat.ID) (bool, error) {
	if g == nil {
		return true, nil
	}
	email, isAdmin, err := g.CallerAndAdmin(ctx, r)
	if err != nil || email == "" {
		return false, err
	}
	if isAdmin {
		return true, nil
	}
	if g.chats == nil {
		return false, nil
	}
	meta, err := g.chats.Get(ctx, chatID)
	if err != nil {
		return false, err
	}
	if meta.ProjectID == "" {
		return true, nil
	}
	return g.HasAccess(ctx, serviceproject.ID(meta.ProjectID), email)
}
