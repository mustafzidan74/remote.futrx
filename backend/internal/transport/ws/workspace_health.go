package wstransport

import (
	"context"
	"time"

	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// HealthSource supplies the cached health verdicts for the projects in a
// connection's snapshot. It is optional: without it the socket still streams
// projects and chats, and the sidebar falls back to plain lifecycle dots.
type HealthSource interface {
	Snapshot(ids []serviceproject.ID) []servicehealth.ProjectHealth
}

// accessLookupTimeout bounds one membership check made on the socket's write
// goroutine, which has no request context of its own once the connection is
// hijacked.
const accessLookupTimeout = 5 * time.Second

// visibilityCache answers "may this connection see this project?" without
// re-reading the access list on every broadcast. Health rows arrive once a
// minute per project, so one memoized lookup per project per connection is the
// difference between a cheap filter and a file read per message.
type visibilityCache struct {
	visibility WorkspaceVisibility
	email      string
	isAdmin    bool
	known      map[serviceproject.ID]bool
}

func newVisibilityCache(
	visibility WorkspaceVisibility,
	email string,
	isAdmin bool,
	visible []serviceproject.Meta,
) *visibilityCache {
	known := make(map[serviceproject.ID]bool, len(visible))
	for _, project := range visible {
		known[project.ID] = true
	}
	return &visibilityCache{visibility: visibility, email: email, isAdmin: isAdmin, known: known}
}

// allows reports whether the connection's user may see rows for a project.
// Projects created after the snapshot are looked up once and remembered, so a
// project a user gains access to mid-session still lights up without a
// reconnect.
func (c *visibilityCache) allows(id serviceproject.ID) bool {
	if c == nil || c.visibility == nil || c.isAdmin {
		return true
	}
	if allowed, seen := c.known[id]; seen {
		return allowed
	}
	ctx, cancel := context.WithTimeout(context.Background(), accessLookupTimeout)
	defer cancel()
	allowed, err := c.visibility.HasAccess(ctx, id, c.email)
	if err != nil {
		// A failed lookup is not a denial worth caching: try again next time
		// rather than blanking the project for the rest of the session.
		return false
	}
	c.known[id] = allowed
	return allowed
}
