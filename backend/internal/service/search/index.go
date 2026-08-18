package search

import (
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Highlight markers bracket the matched span inside a snippet. STX and ETX are
// used rather than anything printable because a transcript can contain any
// printable sequence a caller might otherwise pick as a sentinel; indexed text
// has them stripped, so a marker in a snippet can only ever come from a match.
const (
	HighlightStart = "\x02"
	HighlightEnd   = "\x03"
)

// snippetContext is how much text surrounds the match on each side.
const snippetContext = 80

// Roles an indexed entry can carry. Tool-call payloads are deliberately
// absent: they are mostly JSON and file contents, and indexing them turns
// every query into a wall of noise.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTitle     = "title"
)

// Default index bounds. The corpus is unbounded — chat logs never rotate — but
// the process memory is not, so the newest entries win.
const (
	// DefaultMaxEntries caps how many messages stay searchable.
	DefaultMaxEntries = 200_000
	// DefaultMaxBytes caps the indexed source text. Each entry also keeps a
	// folded copy for matching, so peak cost is roughly twice this number.
	DefaultMaxBytes = 48 << 20
	// maxEntryBytes caps one coalesced assistant turn. A very long turn is
	// split rather than grown without limit, which keeps both the merge and
	// the snippet scan bounded.
	maxEntryBytes = 64 << 10
)

// Entry is one searchable message. Assistant text arrives as a stream of small
// deltas, so consecutive deltas of the same turn are coalesced into one entry
// — otherwise a phrase spanning two deltas would never be found and every
// snippet would be a two-word fragment.
type Entry struct {
	ChatID string
	Role   string
	At     int64
	Seq    int64
	Text   string
	folded string
}

func (e Entry) size() int {
	return len(e.Text) + len(e.folded)
}

// Match is one hit, before chat and project names are attached.
type Match struct {
	Entry
	ProjectID string
	Snippet   string
}

// chatInfo is the per-chat context every entry borrows: which project it
// belongs to and what it is called. Keeping it here rather than on each entry
// means a rename or a project move costs one map write instead of a rewrite of
// the whole transcript — and it keeps the append path free of any lookup.
type chatInfo struct {
	ProjectID string
	Title     string
	folded    string
	At        int64
}

// Index is the in-memory corpus.
type Index struct {
	mu      sync.RWMutex
	entries []Entry
	chats   map[string]chatInfo
	// openAssistant maps a chat to the absolute position of its currently
	// growing assistant entry. Absolute positions survive eviction, which
	// physical slice indices would not.
	openAssistant map[string]int64
	// base is how many entries have been evicted, i.e. the absolute position
	// of entries[0].
	base       int64
	bytes      int
	maxEntries int
	maxBytes   int
	// evicted counts entries dropped to stay inside the bounds, so the API can
	// tell a caller their history is only partially searchable.
	evicted int
}

func NewIndex(maxEntries, maxBytes int) *Index {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return &Index{
		chats:         map[string]chatInfo{},
		openAssistant: map[string]int64{},
		maxEntries:    maxEntries,
		maxBytes:      maxBytes,
	}
}

// Stats describes how much of the history is currently searchable.
type Stats struct {
	Entries int `json:"entries"`
	Chats   int `json:"chats"`
	Bytes   int `json:"bytes"`
	Evicted int `json:"evicted"`
}

// Stats reports the index's size, for the startup log and the API footer.
func (i *Index) Stats() Stats {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return Stats{
		Entries: len(i.entries),
		Chats:   len(i.chats),
		Bytes:   i.bytes,
		Evicted: i.evicted,
	}
}

// SetChat records, or replaces, a chat's project and title.
func (i *Index) SetChat(chatID, projectID, title string, at int64) {
	if chatID == "" {
		return
	}
	title = sanitize(title)

	i.mu.Lock()
	defer i.mu.Unlock()
	i.chats[chatID] = chatInfo{
		ProjectID: projectID,
		Title:     title,
		folded:    Fold(title),
		At:        at,
	}
}

// Add appends one message. Empty text is ignored, which is what keeps
// structural events and empty streaming deltas out of the corpus.
func (i *Index) Add(entry Entry) {
	entry.Text = sanitize(entry.Text)
	if entry.Text == "" || entry.ChatID == "" {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if entry.Role != RoleAssistant {
		// Anything the user says closes the agent's turn, so the next
		// assistant text starts a fresh entry.
		delete(i.openAssistant, entry.ChatID)
		i.appendLocked(entry)
		return
	}
	if i.mergeAssistantLocked(entry) {
		i.trimLocked()
		return
	}
	i.appendLocked(entry)
	i.openAssistant[entry.ChatID] = i.base + int64(len(i.entries)) - 1
}

// mergeAssistantLocked folds a delta into the chat's open assistant entry and
// reports whether it did. It refuses once the entry reaches maxEntryBytes, so
// the caller starts a new one.
func (i *Index) mergeAssistantLocked(entry Entry) bool {
	position, open := i.openAssistant[entry.ChatID]
	if !open {
		return false
	}
	physical := int(position - i.base)
	if physical < 0 || physical >= len(i.entries) {
		delete(i.openAssistant, entry.ChatID)
		return false
	}
	target := &i.entries[physical]
	if target.Role != RoleAssistant || target.ChatID != entry.ChatID {
		delete(i.openAssistant, entry.ChatID)
		return false
	}
	if len(target.Text)+len(entry.Text) > maxEntryBytes {
		delete(i.openAssistant, entry.ChatID)
		return false
	}

	i.bytes -= target.size()
	target.Text = joinDelta(target.Text, entry.Text)
	target.folded = Fold(target.Text)
	target.Seq = entry.Seq
	i.bytes += target.size()
	return true
}

func (i *Index) appendLocked(entry Entry) {
	entry.folded = Fold(entry.Text)
	if entry.folded == "" {
		return
	}
	i.entries = append(i.entries, entry)
	i.bytes += entry.size()
	i.trimLocked()
}

// joinDelta concatenates two stream fragments. A delta may start mid-word, so
// a separator is inserted only when neither side already has whitespace.
func joinDelta(current, next string) string {
	if current == "" {
		return next
	}
	if endsWithSpace(current) || startsWithSpace(next) {
		return current + next
	}
	return current + " " + next
}

func endsWithSpace(value string) bool {
	r, size := utf8.DecodeLastRuneInString(value)
	return size > 0 && unicode.IsSpace(r)
}

func startsWithSpace(value string) bool {
	r, size := utf8.DecodeRuneInString(value)
	return size > 0 && unicode.IsSpace(r)
}

// RemoveChat drops everything a deleted chat contributed.
func (i *Index) RemoveChat(chatID string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.chats, chatID)
	delete(i.openAssistant, chatID)

	kept := make([]Entry, 0, len(i.entries))
	for _, entry := range i.entries {
		if entry.ChatID == chatID {
			i.bytes -= entry.size()
			continue
		}
		kept = append(kept, entry)
	}
	// Removing from the middle invalidates every absolute position, so the
	// open-assistant map is cleared wholesale: the cost is one extra entry per
	// streaming chat, which is cheaper than tracking a shifting offset.
	if len(kept) != len(i.entries) {
		i.openAssistant = map[string]int64{}
		i.base = 0
	}
	i.entries = kept
}

// Query returns matches newest first, at most limit of them.
//
// `allowed` is the membership filter. It is applied before any text work, so a
// caller can never learn from timing or from a count that a chat they cannot
// see exists. `projectID` narrows the search to one project when set.
func (i *Index) Query(
	query string,
	projectID string,
	limit int,
	allowed func(chatID string) bool,
) []Match {
	needle := Fold(query)
	if needle == "" || limit <= 0 {
		return nil
	}

	i.mu.RLock()
	defer i.mu.RUnlock()

	matches := make([]Match, 0, limit)
	// visit reports whether the scan should continue.
	visit := func(entry Entry, folded string) bool {
		info, known := i.chats[entry.ChatID]
		if !known {
			return true
		}
		if projectID != "" && info.ProjectID != projectID {
			return true
		}
		if allowed != nil && !allowed(entry.ChatID) {
			return true
		}
		if !strings.Contains(folded, needle) {
			return true
		}
		matches = append(matches, Match{
			Entry:     entry,
			ProjectID: info.ProjectID,
			Snippet:   snippet(entry.Text, needle),
		})
		return len(matches) < limit
	}

	// Titles first: a chat whose name matches is almost always what the user
	// meant, and there are few enough of them that scanning them is free.
	for _, title := range i.sortedTitlesLocked() {
		if !visit(title, title.folded) {
			return matches
		}
	}

	for index := len(i.entries) - 1; index >= 0; index-- {
		if !visit(i.entries[index], i.entries[index].folded) {
			break
		}
	}
	return matches
}

func (i *Index) sortedTitlesLocked() []Entry {
	titles := make([]Entry, 0, len(i.chats))
	for chatID, info := range i.chats {
		if info.folded == "" {
			continue
		}
		titles = append(titles, Entry{
			ChatID: chatID,
			Role:   RoleTitle,
			At:     info.At,
			Text:   info.Title,
			folded: info.folded,
		})
	}
	sort.SliceStable(titles, func(a, b int) bool { return titles[a].At > titles[b].At })
	return titles
}

// trimLocked enforces both bounds by dropping the oldest entries. The byte
// bound never empties the index completely: one oversized message stays
// searchable rather than silently disappearing.
func (i *Index) trimLocked() {
	dropped := 0
	for {
		remaining := len(i.entries) - dropped
		overCount := remaining > i.maxEntries
		overBytes := i.bytes > i.maxBytes && remaining > 1
		if !overCount && !overBytes {
			break
		}
		i.bytes -= i.entries[dropped].size()
		dropped++
	}
	if dropped == 0 {
		return
	}
	i.entries = append(i.entries[:0], i.entries[dropped:]...)
	i.base += int64(dropped)
	i.evicted += dropped
}

// sanitize strips the highlight sentinels from indexed text, so a snippet's
// markers can only ever delimit a real match.
func sanitize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.ContainsAny(value, HighlightStart+HighlightEnd) {
		return value
	}
	return strings.NewReplacer(HighlightStart, "", HighlightEnd, "").Replace(value)
}

// snippet cuts roughly ±snippetContext characters around the first match and
// wraps the matched span in the highlight markers.
//
// The match was found in folded text, so the source offset is re-derived here
// by folding the original one rune at a time. That work happens only for the
// handful of entries that actually matched, which is what lets the index store
// no offset table at all.
func snippet(text, needle string) string {
	start, end, ok := foldedSpan(text, needle)
	if !ok {
		return truncate(text, snippetContext*2)
	}

	prefix, trimmedStart := contextBefore(text[:start])
	suffix, trimmedEnd := contextAfter(text[end:])

	var out strings.Builder
	if trimmedStart {
		out.WriteString("…")
	}
	out.WriteString(prefix)
	out.WriteString(HighlightStart)
	out.WriteString(text[start:end])
	out.WriteString(HighlightEnd)
	out.WriteString(suffix)
	if trimmedEnd {
		out.WriteString("…")
	}
	return collapseWhitespace(out.String())
}

// foldedSpan locates, in the source string, the byte range whose folded form
// contains needle. It mirrors Fold exactly; the two must stay in step, which
// is why both live in this package.
func foldedSpan(text, needle string) (start, end int, ok bool) {
	var folded strings.Builder
	folded.Grow(len(text))
	// offsets[k] is the source byte offset that produced folded byte k; the
	// final element closes the range.
	offsets := make([]int, 0, len(text)+1)
	pendingSpace := false
	// spaceAt is where the pending whitespace run started. The emitted space
	// is attributed to that position rather than to the rune that follows it,
	// so a match ending at a word boundary does not swallow the separator.
	spaceAt := 0

	for index, r := range text {
		if isDroppedMark(r) {
			continue
		}
		if unicode.IsSpace(r) {
			if !pendingSpace {
				spaceAt = index
			}
			pendingSpace = folded.Len() > 0
			continue
		}
		if pendingSpace {
			offsets = append(offsets, spaceAt)
			folded.WriteRune(' ')
			pendingSpace = false
		}
		mapped := foldRune(r)
		for count := 0; count < utf8.RuneLen(mapped); count++ {
			offsets = append(offsets, index)
		}
		folded.WriteRune(mapped)
	}
	offsets = append(offsets, len(text))

	at := strings.Index(folded.String(), needle)
	if at < 0 || at+len(needle) >= len(offsets) {
		return 0, 0, false
	}
	return offsets[at], offsets[at+len(needle)], true
}

func contextBefore(text string) (string, bool) {
	runes := []rune(text)
	if len(runes) <= snippetContext {
		return text, false
	}
	return string(runes[len(runes)-snippetContext:]), true
}

func contextAfter(text string) (string, bool) {
	runes := []rune(text)
	if len(runes) <= snippetContext {
		return text, false
	}
	return string(runes[:snippetContext]), true
}

func truncate(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return collapseWhitespace(text)
	}
	return collapseWhitespace(string(runes[:max])) + "…"
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
