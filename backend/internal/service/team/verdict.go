package team

import (
	"strings"
	"unicode"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// Verdict is what a companion run said, once the marker line has been found.
type Verdict struct {
	// Kind is one of the servicechat.TeamVerdict* constants, or Unknown when
	// the agent never emitted a marker.
	Kind string
	// Findings is what the agent wrote after the marker, falling back to what
	// it wrote before it when the marker was the last thing on the page.
	Findings string
}

// Verdict markers. The prompts ask for these exact words, and the parser is
// deliberately forgiving about everything around them: a reply-preference
// preamble can put the whole answer in Arabic, a model can bold the line, and
// a CLI can wrap it in a bullet — none of which changes the verdict.
const (
	reviewMarker = "VERDICT"
	testMarker   = "TESTS"
)

// ParseVerdict reads the marker line out of one companion run's output.
//
// Three properties are load-bearing:
//
//   - It scans every line and keeps the *last* match. Agents restate the
//     instruction ("I will end with VERDICT: SHIP or VERDICT: FIX") before
//     they answer it, and the answer is always the later one.
//   - It tolerates decoration around the marker: `**VERDICT: FIX**`, a
//     leading `- `, a trailing `.`, an Arabic sentence on either side, and the
//     bidirectional control characters an RTL reply drops around Latin words.
//   - It never guesses. A run with no marker returns Unknown, which stops the
//     loop, because reading silence as SHIP would let an agent that ignored
//     its instructions wave a broken change through.
func ParseVerdict(role, output string) Verdict {
	marker, kinds := markerFor(role)
	if marker == "" {
		return Verdict{Kind: servicechat.TeamVerdictUnknown}
	}

	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	found := -1
	kind := ""
	for index, line := range lines {
		matched := matchMarker(line, marker, kinds)
		if matched == "" {
			continue
		}
		found, kind = index, matched
	}
	if found < 0 {
		return Verdict{Kind: servicechat.TeamVerdictUnknown, Findings: tail(output)}
	}

	findings := strings.TrimSpace(strings.Join(lines[found+1:], "\n"))
	if findings == "" {
		// The prompt asks for findings after the verdict, but a model that
		// ends on the marker has still said something useful above it.
		findings = strings.TrimSpace(strings.Join(lines[:found], "\n"))
	}
	return Verdict{Kind: kind, Findings: tail(findings)}
}

// maxFindingsBytes bounds what travels into the next prompt and onto the
// stored timeline. Review findings are a list, not a transcript; a runaway
// answer would otherwise be pasted verbatim into the implementer's prompt and
// persisted into meta.json on every loop.
const maxFindingsBytes = 4000

func markerFor(role string) (string, map[string]string) {
	switch strings.TrimSpace(role) {
	case servicechat.TeamRoleReviewer:
		return reviewMarker, map[string]string{
			"ship": servicechat.TeamVerdictShip,
			"fix":  servicechat.TeamVerdictFix,
		}
	case servicechat.TeamRoleTester:
		return testMarker, map[string]string{
			"pass": servicechat.TeamVerdictPass,
			"fail": servicechat.TeamVerdictFail,
		}
	default:
		return "", nil
	}
}

// matchMarker looks for `MARKER: VALUE` anywhere in one line and returns the
// verdict it names, or "" when the line does not carry one.
func matchMarker(line, marker string, kinds map[string]string) string {
	cleaned := strings.ToUpper(stripDecoration(line))
	start := strings.Index(cleaned, marker)
	if start < 0 {
		return ""
	}
	rest := strings.TrimSpace(cleaned[start+len(marker):])
	// The colon is what separates the marker from its value. Without it the
	// line is prose that happens to contain the word.
	if !strings.HasPrefix(rest, ":") {
		return ""
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
	word := firstWord(rest)
	if word == "" {
		return ""
	}
	return kinds[strings.ToLower(word)]
}

// stripDecoration removes the characters a model or an RTL reply wraps around
// the marker without changing what it says: markdown emphasis, backticks,
// brackets, and the Unicode bidi controls that an Arabic sentence embeds
// around a Latin token.
func stripDecoration(line string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '*', '_', '`', '#', '>', '[', ']', '(', ')', '"', '\'', '«', '»', '”', '“':
			return -1
		}
		// U+200E..U+200F and U+202A..U+202E are the bidi marks and embedding
		// controls; U+2066..U+2069 are the isolates. All are invisible.
		if (r >= '‎' && r <= '‏') ||
			(r >= '‪' && r <= '‮') ||
			(r >= '⁦' && r <= '⁩') {
			return -1
		}
		return r
	}, line)
}

// firstWord returns the leading run of letters, so `SHIP.` and `FIX —` and
// `PASS (3 specs)` all resolve.
func firstWord(text string) string {
	end := strings.IndexFunc(text, func(r rune) bool { return !unicode.IsLetter(r) })
	if end < 0 {
		return text
	}
	return text[:end]
}

// tail keeps the end of a long passage rather than the start: an agent puts
// its conclusions last, and the conclusion is what the next hop needs.
func tail(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= maxFindingsBytes {
		return trimmed
	}
	cut := trimmed[len(trimmed)-maxFindingsBytes:]
	// Never split a multi-byte rune: advance to the next valid boundary.
	for len(cut) > 0 && !isRuneStart(cut[0]) {
		cut = cut[1:]
	}
	return "…" + strings.TrimSpace(cut)
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
