package httptransport

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type RouteRegistrar interface {
	RegisterRoutes(*http.ServeMux)
}

type WebSocketRegistrar interface {
	RegisterRoutes(*http.ServeMux, websocket.Upgrader)
}

type Middleware interface {
	Wrap(http.Handler) http.Handler
}

type Handlers struct {
	Sessions         RouteRegistrar
	Chats            RouteRegistrar
	Projects         RouteRegistrar
	ProjectHealth    RouteRegistrar
	Dashboard        RouteRegistrar
	Users            RouteRegistrar
	AgentAuth        RouteRegistrar
	UserSettings     RouteRegistrar
	Notifications    RouteRegistrar
	Monitoring       RouteRegistrar
	Transcribe       RouteRegistrar
	Portal           RouteRegistrar
	ScreenshotLinks  RouteRegistrar
	GitHubHooks      RouteRegistrar
	ServerInfo       RouteRegistrar
	SelfUpdate       RouteRegistrar
	AdminResources   RouteRegistrar
	ModelRouting     RouteRegistrar
	Skills           RouteRegistrar
	GlobalSkills     RouteRegistrar
	Playbooks        RouteRegistrar
	Snippets         RouteRegistrar
	AgentPreferences RouteRegistrar
	Search           RouteRegistrar
	GlobalSecrets    RouteRegistrar
	Templates        RouteRegistrar
	BrowserInspector RouteRegistrar
	Schedules        RouteRegistrar
	Usage            RouteRegistrar
	Audit            RouteRegistrar
	Uploads          RouteRegistrar
	TmuxWS           WebSocketRegistrar
	TerminalWS       WebSocketRegistrar
	ChatWS           WebSocketRegistrar
	WorkspaceWS      WebSocketRegistrar
	AgentAuthWS      WebSocketRegistrar
	Auth             RouteRegistrar
	Middleware       Middleware
	Static           http.Handler
}

func NewHandler(handlers Handlers) http.Handler {
	mux := http.NewServeMux()

	register := func(handler RouteRegistrar) {
		if handler != nil {
			handler.RegisterRoutes(mux)
		}
	}

	register(handlers.Sessions)
	register(handlers.Chats)
	register(handlers.Projects)
	register(handlers.ProjectHealth)
	register(handlers.Dashboard)
	register(handlers.Users)
	register(handlers.AgentAuth)
	register(handlers.UserSettings)
	register(handlers.Notifications)
	register(handlers.Monitoring)
	register(handlers.Transcribe)
	register(handlers.Portal)
	register(handlers.ScreenshotLinks)
	register(handlers.GitHubHooks)
	register(handlers.ServerInfo)
	register(handlers.SelfUpdate)
	register(handlers.AdminResources)
	register(handlers.ModelRouting)
	register(handlers.Skills)
	register(handlers.GlobalSkills)
	register(handlers.Playbooks)
	register(handlers.Snippets)
	register(handlers.AgentPreferences)
	register(handlers.Search)
	register(handlers.GlobalSecrets)
	register(handlers.Templates)
	register(handlers.BrowserInspector)
	register(handlers.Schedules)
	register(handlers.Usage)
	register(handlers.Audit)
	register(handlers.Uploads)

	upgrader := NewUpgrader()
	if handlers.TmuxWS != nil {
		handlers.TmuxWS.RegisterRoutes(mux, upgrader)
	}
	if handlers.TerminalWS != nil {
		handlers.TerminalWS.RegisterRoutes(mux, upgrader)
	}
	if handlers.ChatWS != nil {
		handlers.ChatWS.RegisterRoutes(mux, upgrader)
	}
	if handlers.WorkspaceWS != nil {
		handlers.WorkspaceWS.RegisterRoutes(mux, upgrader)
	}
	if handlers.AgentAuthWS != nil {
		handlers.AgentAuthWS.RegisterRoutes(mux, upgrader)
	}
	if handlers.Auth != nil {
		handlers.Auth.RegisterRoutes(mux)
	}
	if handlers.Static != nil {
		mux.Handle("/", handlers.Static)
	}

	var handler http.Handler = mux
	if handlers.Middleware != nil {
		handler = handlers.Middleware.Wrap(handler)
	}
	return handler
}

func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: long-lived WebSockets.
	}
}
