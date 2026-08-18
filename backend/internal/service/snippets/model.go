// Package snippets owns each user's personal prompt library and the
// client-facing message templates that live beside it.
//
// A snippet is a saved piece of text the composer can insert: either a prompt
// written for an agent (`audience: "agent"`) or a message written for a human
// client (`audience: "client"`, carrying an Arabic and an English variant).
// Both kinds may contain `{{placeholder}}` tokens; resolving them is the
// caller's job, exactly as it is for playbooks, because only the client knows
// what the composer currently holds.
//
// The library is per user and private: nothing here is shared, and no
// administrator route reads another member's snippets.
package snippets

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	// ErrInvalidSnippet reports a snippet the service refuses to persist. It
	// is the only validation error the handler maps to 400.
	ErrInvalidSnippet = errors.New("invalid snippet")
	// ErrNotFound reports an id that is not in this owner's library. A
	// snippet belonging to somebody else is reported the same way, so one
	// user can never probe another's ids.
	ErrNotFound = errors.New("snippet not found")
	// ErrInvalidOwner reports a request with no resolvable identity.
	ErrInvalidOwner = errors.New("snippet owner is required")
	// ErrUnavailable reports a deployment with no snippet store configured.
	ErrUnavailable = errors.New("snippets are not configured on this server")
)

const (
	// MaxSnippets caps one user's library so the picker stays usable and a
	// single import can never balloon the stored document.
	MaxSnippets = 200
	// MaxTags caps the tags on one snippet.
	MaxTags = 10

	maxIDLength       = 64
	maxTitleLength    = 120
	maxBodyLength     = 8000
	maxShortcutLength = 32
	maxTagLength      = 32
)

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	shortcutPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// Audience says who the text is written for. It is the one field that decides
// where a snippet shows up: agent snippets in the composer, client templates
// in the project's "Message client" panel.
type Audience string

const (
	AudienceAgent  Audience = "agent"
	AudienceClient Audience = "client"
)

// Language identifies a client template's variant.
type Language string

const (
	LanguageEnglish Language = "en"
	LanguageArabic  Language = "ar"
)

// Variants holds the two languages a client template is written in. An agent
// snippet leaves both empty and uses Body.
type Variants struct {
	AR string `json:"ar,omitempty"`
	EN string `json:"en,omitempty"`
}

// Snippet is one entry of a user's library.
type Snippet struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Audience Audience `json:"audience"`
	Variants Variants `json:"variants"`
	Tags     []string `json:"tags,omitempty"`
	// Shortcut is the word `/s-<shortcut>` types. Empty means the snippet is
	// reachable from the picker only.
	Shortcut  string `json:"shortcut,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	// Uses counts insertions. It is what "most used first" sorts on, and the
	// only field the /use route touches.
	Uses int `json:"uses"`
}

// Input is the editable half of a snippet: everything the editor submits and
// nothing the server owns.
type Input struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Audience Audience `json:"audience"`
	Variants Variants `json:"variants"`
	Tags     []string `json:"tags"`
	Shortcut string   `json:"shortcut"`
}

// Text returns the wording to use for a language. A client template falls back
// to its other variant, then to the shared body, so a half-translated template
// still produces something to send rather than an empty message.
func (s Snippet) Text(language Language) string {
	if s.Audience != AudienceClient {
		return s.Body
	}
	primary, secondary := s.Variants.EN, s.Variants.AR
	if language == LanguageArabic {
		primary, secondary = s.Variants.AR, s.Variants.EN
	}
	for _, candidate := range []string{primary, secondary, s.Body} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

// Normalize trims a snippet and drops the parts that carry no information. It
// never invents values: an entry with no id keeps none, so callers can tell a
// new snippet from a stored one.
func Normalize(item Snippet) Snippet {
	item.ID = strings.ToLower(strings.TrimSpace(item.ID))
	item.Title = strings.TrimSpace(item.Title)
	item.Body = strings.TrimSpace(item.Body)
	item.Shortcut = normalizeShortcut(item.Shortcut)
	item.Audience = normalizeAudience(item.Audience)
	item.Variants.AR = strings.TrimSpace(item.Variants.AR)
	item.Variants.EN = strings.TrimSpace(item.Variants.EN)
	item.Tags = normalizeTags(item.Tags)
	if item.Uses < 0 {
		item.Uses = 0
	}
	return item
}

// NormalizeList normalizes every entry and drops the ones that carry no id,
// which is the shape both the store and the API expose.
func NormalizeList(list []Snippet) []Snippet {
	out := make([]Snippet, 0, len(list))
	for _, item := range list {
		item = Normalize(item)
		if item.ID == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func normalizeAudience(audience Audience) Audience {
	if Audience(strings.ToLower(strings.TrimSpace(string(audience)))) == AudienceClient {
		return AudienceClient
	}
	return AudienceAgent
}

// NormalizeLanguage maps anything a client sends to one of the two variants.
// English is the default because it is the fallback every template carries.
func NormalizeLanguage(language Language) Language {
	if Language(strings.ToLower(strings.TrimSpace(string(language)))) == LanguageArabic {
		return LanguageArabic
	}
	return LanguageEnglish
}

// normalizeShortcut accepts the spellings a user is likely to paste — with the
// leading slash, with the `s-` prefix the command uses — and stores the bare
// word, so `/s-wpfix`, `s-wpfix`, and `wpfix` are one shortcut and not three.
func normalizeShortcut(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "/")
	value = strings.TrimPrefix(value, "s-")
	return value
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Validate reports why a normalized library cannot be stored. Callers are
// expected to normalize first; Validate never mutates.
func Validate(list []Snippet) error {
	if len(list) > MaxSnippets {
		return fmt.Errorf("%w: at most %d snippets are allowed", ErrInvalidSnippet, MaxSnippets)
	}
	ids := make(map[string]struct{}, len(list))
	shortcuts := make(map[string]struct{}, len(list))
	for _, item := range list {
		if err := validateOne(item); err != nil {
			return err
		}
		if _, dup := ids[item.ID]; dup {
			return fmt.Errorf("%w: duplicate id %q", ErrInvalidSnippet, item.ID)
		}
		ids[item.ID] = struct{}{}
		if item.Shortcut == "" {
			continue
		}
		if _, dup := shortcuts[item.Shortcut]; dup {
			return fmt.Errorf("%w: shortcut %q is already used", ErrInvalidSnippet, item.Shortcut)
		}
		shortcuts[item.Shortcut] = struct{}{}
	}
	return nil
}

func validateOne(item Snippet) error {
	if item.ID == "" || len(item.ID) > maxIDLength || !idPattern.MatchString(item.ID) {
		return fmt.Errorf(
			"%w: id %q must be lowercase letters, digits, or dashes (max %d)",
			ErrInvalidSnippet, item.ID, maxIDLength,
		)
	}
	if item.Title == "" || utf8.RuneCountInString(item.Title) > maxTitleLength {
		return fmt.Errorf("%w: %q needs a title of at most %d characters", ErrInvalidSnippet, item.ID, maxTitleLength)
	}
	if item.Body == "" && item.Variants.AR == "" && item.Variants.EN == "" {
		return fmt.Errorf("%w: %q needs a body or a language variant", ErrInvalidSnippet, item.ID)
	}
	for _, text := range []string{item.Body, item.Variants.AR, item.Variants.EN} {
		if utf8.RuneCountInString(text) > maxBodyLength {
			return fmt.Errorf("%w: %q is longer than %d characters", ErrInvalidSnippet, item.ID, maxBodyLength)
		}
	}
	if item.Shortcut != "" &&
		(len(item.Shortcut) > maxShortcutLength || !shortcutPattern.MatchString(item.Shortcut)) {
		return fmt.Errorf(
			"%w: shortcut %q must be lowercase letters, digits, or dashes (max %d)",
			ErrInvalidSnippet, item.Shortcut, maxShortcutLength,
		)
	}
	if len(item.Tags) > MaxTags {
		return fmt.Errorf(
			"%w: %q carries %d tags, at most %d are allowed",
			ErrInvalidSnippet, item.ID, len(item.Tags), MaxTags,
		)
	}
	for _, tag := range item.Tags {
		if utf8.RuneCountInString(tag) > maxTagLength {
			return fmt.Errorf("%w: tag %q is longer than %d characters", ErrInvalidSnippet, tag, maxTagLength)
		}
	}
	return nil
}

// Sort orders a library the way every reader wants it: what gets used most,
// first. Ties fall back to the most recently edited, then to the title, so the
// order is total and a round trip through the store is stable.
func Sort(list []Snippet) []Snippet {
	out := append([]Snippet(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.Uses != right.Uses {
			return left.Uses > right.Uses
		}
		if left.UpdatedAt != right.UpdatedAt {
			return left.UpdatedAt > right.UpdatedAt
		}
		return left.Title < right.Title
	})
	return out
}

// reservedIDs are the words the collection route spends on its own verbs. A
// generated id never takes one, so `/api/me/snippets/import` can never also
// address a snippet.
var reservedIDs = map[string]struct{}{"import": {}, "export": {}}

// Reserved reports whether an id is one the routes claim for themselves.
func Reserved(id string) bool {
	_, found := reservedIDs[strings.ToLower(strings.TrimSpace(id))]
	return found
}

// NewID derives a stable, readable id from a title, avoiding the ids already
// taken. An untitled snippet still gets one, because the id is a URL segment
// and "" is not addressable.
func NewID(title string, taken map[string]struct{}) string {
	base := slug(title)
	if base == "" {
		base = "snippet"
	}
	if len(base) > maxIDLength-6 {
		base = strings.Trim(base[:maxIDLength-6], "-")
	}
	candidate := base
	for index := 2; ; index++ {
		_, used := taken[candidate]
		if !used && !Reserved(candidate) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, index)
	}
}

func slug(value string) string {
	var out strings.Builder
	previousDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			previousDash = false
			continue
		}
		if !previousDash && out.Len() > 0 {
			out.WriteRune('-')
			previousDash = true
		}
	}
	return strings.Trim(out.String(), "-")
}
