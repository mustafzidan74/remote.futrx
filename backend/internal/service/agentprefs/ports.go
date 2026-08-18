package agentprefs

import "context"

// Repository persists the single preferences document. The bool result of
// Load distinguishes "never configured" from "an admin saved the defaults on
// purpose", which is the same shape the playbook store uses.
type Repository interface {
	Load(ctx context.Context) (Preferences, bool, error)
	Save(ctx context.Context, prefs Preferences) error
}

// UserOverrides resolves one user's personal reply-language override. An empty
// result means the user never set one and the platform value applies.
type UserOverrides interface {
	ReplyLanguage(ctx context.Context, identity Identity) string
}

// ProjectDirectory answers the one project fact the ApplyToNewProjects rule
// needs. The bool reports whether the project could be resolved at all.
type ProjectDirectory interface {
	CreatedAt(ctx context.Context, projectID string) (int64, bool)
}

// Identity is who a run belongs to. Sub is the OAuth subject when the session
// carried one; user settings are keyed by subject first, email second, so both
// are needed to find the right document.
type Identity struct {
	Email string
	Sub   string
}
