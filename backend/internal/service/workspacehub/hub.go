package workspacehub

import (
	"sync"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

type Event struct {
	Type    string               `json:"type"`
	Chat    *servicechat.Meta    `json:"chat,omitempty"`
	Project *serviceproject.Meta `json:"project,omitempty"`
	// Health rides beside the project row rather than inside it: Meta is the
	// stored document, and a measurement that changes every minute has no
	// business being written to disk. An absent health on a project.health
	// event means "stop showing one for this project".
	Health *servicehealth.ProjectHealth `json:"health,omitempty"`
	ID     string                       `json:"id,omitempty"`
}

type Hub struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

type Subscription struct {
	hub    *Hub
	events chan Event
	mu     sync.Mutex
	closed bool
}

const subscriptionBuffer = 256

func New() *Hub {
	return &Hub{subs: map[*Subscription]struct{}{}}
}

func (h *Hub) Subscribe() *Subscription {
	sub := &Subscription{
		hub:    h,
		events: make(chan Event, subscriptionBuffer),
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (s *Subscription) Events() <-chan Event {
	return s.events
}

func (s *Subscription) Close() {
	s.hub.mu.Lock()
	delete(s.hub.subs, s)
	s.hub.mu.Unlock()
	s.close()
}

func (h *Hub) PublishChatUpsert(chat servicechat.Meta) {
	h.Publish(Event{Type: "chat.upsert", Chat: &chat})
}

func (h *Hub) PublishChatDelete(id servicechat.ID) {
	h.Publish(Event{Type: "chat.delete", ID: string(id)})
}

func (h *Hub) PublishProjectUpsert(project serviceproject.Meta) {
	h.Publish(Event{Type: "project.upsert", Project: &project})
}

func (h *Hub) PublishProjectDelete(id serviceproject.ID) {
	h.Publish(Event{Type: "project.delete", ID: string(id)})
}

// PublishProjectHealth broadcasts one project's health verdict. It satisfies
// the health monitor's Publisher port; a nil health clears the row.
func (h *Hub) PublishProjectHealth(id serviceproject.ID, health *servicehealth.ProjectHealth) {
	h.Publish(Event{Type: "project.health", ID: string(id), Health: health})
}

func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if sub.trySend(ev) {
			continue
		}
		delete(h.subs, sub)
		sub.close()
	}
}

func (s *Subscription) trySend(ev Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.events <- ev:
		return true
	default:
		return false
	}
}

func (s *Subscription) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.events)
}
