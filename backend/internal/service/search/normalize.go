// Package search provides full-text search across chat transcripts.
//
// The corpus is small enough — one server, flat JSONL chat logs — that a scan
// over normalized text beats a real inverted index on every axis that matters
// here: no term dictionary to keep in sync with an append-only log, no on-disk
// format to migrate, and no build step per query. What it does need is a
// bound, which is why the index is capped by both entry count and bytes.
package search

import (
	"strings"
	"unicode"
)

// Fold returns the matching form of a string.
//
// Latin text is lower-cased. Arabic is folded the way a reader actually types
// it rather than the way it is stored: diacritics and tatweel are dropped, and
// the letter forms people spell interchangeably are unified. Without this,
// searching "احمد" would miss "أحمد" and "مصريه" would miss "مصرية", which is
// most of the misses in practice.
//
// Whitespace collapses to single spaces so a line break never hides a match.
func Fold(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	pendingSpace := false
	for _, r := range value {
		if isDroppedMark(r) {
			continue
		}
		if unicode.IsSpace(r) {
			// Leading whitespace is dropped and runs collapse; the space is
			// only emitted once something actually follows it.
			pendingSpace = out.Len() > 0
			continue
		}
		if pendingSpace {
			out.WriteRune(' ')
			pendingSpace = false
		}
		out.WriteRune(foldRune(r))
	}
	return out.String()
}

// foldRune maps one rune onto its canonical matching form.
func foldRune(r rune) rune {
	switch r {
	// Alef family: hamza carriers and the wasla all match a bare alef.
	case 'أ', 'إ', 'آ', 'ٱ', 'ٲ', 'ٳ':
		return 'ا'
	// Teh marbuta is routinely typed as heh.
	case 'ة':
		return 'ه'
	// Alef maqsura is routinely typed as yeh, and the reverse in Egypt.
	case 'ى':
		return 'ي'
	// Hamza on waw / yeh: keep the base letter.
	case 'ؤ':
		return 'و'
	case 'ئ':
		return 'ي'
	}
	if digit, ok := arabicDigit(r); ok {
		return digit
	}
	return unicode.ToLower(r)
}

// arabicDigit maps the Arabic-Indic and Extended Arabic-Indic digit blocks
// onto ASCII, so "٢٠٢٥" and "2025" are the same query.
func arabicDigit(r rune) (rune, bool) {
	switch {
	case r >= '٠' && r <= '٩':
		return '0' + (r - '٠'), true
	case r >= '۰' && r <= '۹':
		return '0' + (r - '۰'), true
	default:
		return r, false
	}
}

// isDroppedMark reports the runes that carry no matching signal: Arabic
// tashkeel, Quranic annotation marks, the tatweel elongation character, and
// the zero-width and bidi controls that leak in from copy-paste.
func isDroppedMark(r rune) bool {
	switch {
	case r >= '\u0610' && r <= '\u061A': // Arabic honorifics and signs
		return true
	case r >= '\u064B' && r <= '\u065F': // tashkeel
		return true
	case r == '\u0640': // tatweel
		return true
	case r == '\u0670': // superscript alef
		return true
	case r >= '\u06D6' && r <= '\u06ED': // Quranic annotation
		return true
	case r >= '\u200B' && r <= '\u200F': // zero-width and bidi marks
		return true
	case r == '\uFEFF': // byte-order mark
		return true
	default:
		return false
	}
}
