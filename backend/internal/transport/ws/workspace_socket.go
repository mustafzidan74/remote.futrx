package wstransport

import (
	"context"
	"net/http"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/workspacehub"
	"github.com/gorilla/websocket"
)

type ChatLister interface {
	List(ctx context.Context) ([]servicechat.Meta, error)
}

type ProjectLister interface {
	List(ctx context.Context) ([]serviceproject.Meta, error)
}

// WorkspaceVisibility filters the initial snapshot so users only see chats
// + projects they can reach. Provided by the auth wiring; if nil, the
// snapshot is unfiltered (single-user dev mode).
type WorkspaceVisibility interface {
	CallerAndAdmin(ctx context.Context, r *http.Request) (string, bool, error)
	HasAccess(ctx context.Context, projectID serviceproject.ID, email string) (bool, error)
}

type WorkspaceSocket struct {
	chats      ChatLister
	projects   ProjectLister
	hub        *workspacehub.Hub
	visibility WorkspaceVisibility
	health     HealthSource
}

type workspaceSnapshot struct {
	Type     string                `json:"type"`
	Chats    []servicechat.Meta    `json:"chats"`
	Projects []serviceproject.Meta `json:"projects"`
	// Health carries the monitor's latest verdict for the projects in this
	// snapshot, so a reconnecting sidebar draws its status dots immediately
	// instead of waiting up to a full sweep for the first broadcast.
	Health []servicehealth.ProjectHealth `json:"health,omitempty"`
}

func NewWorkspaceSocket(chats ChatLister, projects ProjectLister, hub *workspacehub.Hub) *WorkspaceSocket {
	return &WorkspaceSocket{chats: chats, projects: projects, hub: hub}
}

func (s *WorkspaceSocket) WithVisibility(v WorkspaceVisibility) *WorkspaceSocket {
	s.visibility = v
	return s
}

// WithHealth attaches the project health monitor. Without it the snapshot
// carries no health rows and project.health events never arrive.
func (s *WorkspaceSocket) WithHealth(health HealthSource) *WorkspaceSocket {
	s.health = health
	return s
}

func (s *WorkspaceSocket) RegisterRoutes(mux *http.ServeMux, upgrader websocket.Upgrader) {
	mux.HandleFunc("/ws/workspace", s.Handle(upgrader))
}

func (s *WorkspaceSocket) Handle(upgrader websocket.Upgrader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.handle(upgrader, w, r)
	}
}

func (s *WorkspaceSocket) handle(upgrader websocket.Upgrader, w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		http.Error(w, "workspace stream unavailable", http.StatusServiceUnavailable)
		return
	}

	email, isAdmin := "", true
	if s.visibility != nil {
		em, admin, err := s.visibility.CallerAndAdmin(r.Context(), r)
		if err != nil || em == "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		email, isAdmin = em, admin
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 16)

	sub := s.hub.Subscribe()
	defer sub.Close()

	chats, err := s.chats.List(r.Context())
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		return
	}
	projects, err := s.projects.List(r.Context())
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		return
	}

	if s.visibility != nil && !isAdmin {
		projects = s.filterProjects(r.Context(), projects, email)
		allowed := projectIDSet(projects)
		chats = s.filterChats(chats, allowed)
	}
	visible := newVisibilityCache(s.visibility, email, isAdmin, projects)

	done := make(chan struct{})
	go func() {
		defer conn.Close()
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if err := conn.WriteJSON(workspaceSnapshot{
			Type:     "workspace.snapshot",
			Chats:    chats,
			Projects: projects,
			Health:   s.healthFor(projects),
		}); err != nil {
			return
		}

		for {
			select {
			case ev, ok := <-sub.Events():
				if !ok {
					return
				}
				// Health rows carry per-container consumption, so they are
				// gated on membership even though the pre-existing project
				// broadcasts are not.
				if ev.Type == "project.health" &&
					!visible.allows(serviceproject.ID(ev.ID)) {
					continue
				}
				_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
				if err := conn.WriteJSON(ev); err != nil {
					return
				}
			case <-ticker.C:
				deadline := time.Now().Add(15 * time.Second)
				if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), deadline); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			close(done)
			return
		}
	}
}

// healthFor returns the cached verdicts for the projects in one snapshot, in
// the same order.
func (s *WorkspaceSocket) healthFor(projects []serviceproject.Meta) []servicehealth.ProjectHealth {
	if s.health == nil {
		return nil
	}
	ids := make([]serviceproject.ID, 0, len(projects))
	for _, project := range projects {
		ids = append(ids, project.ID)
	}
	return s.health.Snapshot(ids)
}

func (s *WorkspaceSocket) filterProjects(ctx context.Context, in []serviceproject.Meta, email string) []serviceproject.Meta {
	out := make([]serviceproject.Meta, 0, len(in))
	for _, p := range in {
		ok, err := s.visibility.HasAccess(ctx, p.ID, email)
		if err == nil && ok {
			out = append(out, p)
		}
	}
	return out
}

func (s *WorkspaceSocket) filterChats(in []servicechat.Meta, allowedProjects map[serviceproject.ID]struct{}) []servicechat.Meta {
	out := make([]servicechat.Meta, 0, len(in))
	for _, c := range in {
		if c.ProjectID == "" {
			out = append(out, c)
			continue
		}
		if _, ok := allowedProjects[serviceproject.ID(c.ProjectID)]; ok {
			out = append(out, c)
		}
	}
	return out
}

func projectIDSet(projects []serviceproject.Meta) map[serviceproject.ID]struct{} {
	m := make(map[serviceproject.ID]struct{}, len(projects))
	for _, p := range projects {
		m[p.ID] = struct{}{}
	}
	return m
}
