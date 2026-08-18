// Package portal owns the client portal: one public, read-only status page
// per project, reachable by anyone holding its token and by nobody else.
//
// The portal is deliberately narrower than a share link. A share link opens a
// running application; the portal only ever renders a summary the platform
// composed itself — name, container status, the preview ports that already
// have a live public link, recent commit subjects, and an operator note. It
// never proxies the workspace, never exposes the IDE or the agent browser, and
// never links a preview that would land the visitor on the platform login.
package portal

import (
	"errors"
	"strings"
	"time"
)

const (
	// TokenBytes is the entropy behind a portal token before base64 encoding.
	TokenBytes = 32
	// MaxBrandTitleLength bounds the client-facing heading.
	MaxBrandTitleLength = 80
	// MaxNoteLength bounds the operator note rendered on the page.
	MaxNoteLength = 2000
	// MaxNoteLines bounds how many lines that note may occupy.
	MaxNoteLines = 40
	// ChangelogCommits is how many commits the "Recent changes" list reads.
	ChangelogCommits = 15
	// UsageWindow is the period the optional activity line summarizes.
	UsageWindow = 7 * 24 * time.Hour
)

var (
	// ErrNotFound is the single answer to every failed public lookup: a
	// project that does not exist, a portal that is switched off, and a wrong
	// token are indistinguishable from outside.
	ErrNotFound = errors.New("this client portal is not available")
	// ErrRateLimited throttles token guessing.
	ErrRateLimited = errors.New("too many attempts, try again shortly")
	// ErrUnavailable reports a deployment with no portal store configured.
	ErrUnavailable = errors.New("client portals are not configured on this server")
)

// Portal is the stored per-project record at DATA_DIR/portals/<projectId>.json.
//
// Only the SHA-256 digest of the token is persisted, so a copy of DATA_DIR
// cannot be replayed against the public route.
type Portal struct {
	Enabled       bool   `json:"enabled"`
	TokenHash     string `json:"tokenHash,omitempty"`
	CreatedAt     int64  `json:"createdAt,omitempty"`
	UpdatedAt     int64  `json:"updatedAt,omitempty"`
	ShowPreview   bool   `json:"showPreview"`
	ShowChangelog bool   `json:"showChangelog"`
	ShowUsage     bool   `json:"showUsage"`
	BrandTitle    string `json:"brandTitle,omitempty"`
	Note          string `json:"note,omitempty"`
	// NoteUpdatedAt is when the note last changed, which the page prints so a
	// client can tell a fresh message from one that has been sitting there for
	// a month. Zero means "no note has ever been written".
	NoteUpdatedAt int64 `json:"noteUpdatedAt,omitempty"`
}

// DefaultPortal is what a project starts with: off, and — once enabled —
// showing the preview links and the changelog but never the usage figures.
func DefaultPortal() Portal {
	return Portal{
		ShowPreview:   true,
		ShowChangelog: true,
		ShowUsage:     false,
	}
}

// Live reports whether the record can authorize a public request.
func (p Portal) Live() bool {
	return p.Enabled && strings.TrimSpace(p.TokenHash) != ""
}

// Settings is the member-facing view. The token digest never crosses this
// boundary; URL is populated only by the enable and rotate responses.
type Settings struct {
	Enabled       bool   `json:"enabled"`
	ShowPreview   bool   `json:"showPreview"`
	ShowChangelog bool   `json:"showChangelog"`
	ShowUsage     bool   `json:"showUsage"`
	BrandTitle    string `json:"brandTitle,omitempty"`
	Note          string `json:"note,omitempty"`
	NoteUpdatedAt int64  `json:"noteUpdatedAt,omitempty"`
	CreatedAt     int64  `json:"createdAt,omitempty"`
	UpdatedAt     int64  `json:"updatedAt,omitempty"`
	// URL carries the one and only copy of the plaintext link, returned by
	// the request that minted it and never again.
	URL string `json:"url,omitempty"`
}

// UpdateInput is the member PUT body.
type UpdateInput struct {
	Enabled bool `json:"enabled"`
	// Rotate mints a fresh token for an already-enabled portal, invalidating
	// whatever link the client currently holds.
	Rotate        bool   `json:"rotate"`
	ShowPreview   bool   `json:"showPreview"`
	ShowChangelog bool   `json:"showChangelog"`
	ShowUsage     bool   `json:"showUsage"`
	BrandTitle    string `json:"brandTitle"`
	Note          string `json:"note"`
}

// view renders the member-facing settings for a stored record.
func (p Portal) view() Settings {
	return Settings{
		Enabled:       p.Enabled,
		ShowPreview:   p.ShowPreview,
		ShowChangelog: p.ShowChangelog,
		ShowUsage:     p.ShowUsage,
		BrandTitle:    p.BrandTitle,
		Note:          p.Note,
		NoteUpdatedAt: p.NoteUpdatedAt,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

// Page is the view model the public route renders. Every field is plain data:
// the template escapes it, and nothing here is raw HTML.
type Page struct {
	Title string
	// Direction is "rtl" or "ltr", picked from the text the operator wrote so
	// an Arabic brand title or note reads correctly without a language
	// setting.
	Direction string

	StatusLabel string
	Running     bool

	Previews     []PreviewLink
	PreviewsNote string

	Changelog     []ChangeDay
	ChangelogNote string

	// Note is the operator note, already split into lines. The template renders
	// one paragraph per line, which is the whole of the "markdown-lite"
	// contract: escape the HTML, keep the line breaks.
	Note []string
	// NoteUpdatedLabel dates the note. Empty when the operator never wrote
	// one, or when the record predates the field.
	NoteUpdatedLabel string

	ShowUsage bool
	UsageRuns int64

	UpdatedAtLabel string
}

// PreviewLink is one publicly reachable preview port.
type PreviewLink struct {
	Port int
	URL  string
}

// ChangeDay groups a day's commits under a human date.
type ChangeDay struct {
	Label   string
	Commits []ChangeCommit
}

// ChangeCommit is one commit as the client sees it: no email, no diff.
type ChangeCommit struct {
	ShortSHA string
	Subject  string
	Author   string
	Time     string
}

// sanitizeBrandTitle keeps the client-facing heading to one printable line.
func sanitizeBrandTitle(value string) string {
	return clampLine(value, MaxBrandTitleLength)
}

// sanitizeNote keeps the operator note printable and bounded while preserving
// the line breaks the page renders.
func sanitizeNote(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(value, "\n")
	if len(lines) > MaxNoteLines {
		lines = lines[:MaxNoteLines]
	}
	for index, line := range lines {
		lines[index] = clampLine(line, MaxNoteLength)
	}
	cleaned := strings.TrimSpace(strings.Join(lines, "\n"))
	runes := []rune(cleaned)
	if len(runes) > MaxNoteLength {
		cleaned = strings.TrimSpace(string(runes[:MaxNoteLength]))
	}
	return cleaned
}

// clampLine drops control characters and trims to limit runes.
func clampLine(value string, limit int) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	cleaned = strings.TrimSpace(cleaned)
	runes := []rune(cleaned)
	if len(runes) > limit {
		cleaned = strings.TrimSpace(string(runes[:limit]))
	}
	return cleaned
}

// isRTL reports whether text leans right-to-left, by counting the first strong
// directional rune. It covers Arabic and Hebrew, which is what this platform's
// operators actually write.
func isRTL(text string) bool {
	for _, r := range text {
		switch {
		case r >= 0x0590 && r <= 0x08FF, // Hebrew, Arabic, Syriac, Thaana, N'Ko
			r >= 0xFB1D && r <= 0xFDFF, // Hebrew and Arabic presentation forms
			r >= 0xFE70 && r <= 0xFEFF: // Arabic presentation forms-B
			return true
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			return false
		}
	}
	return false
}
