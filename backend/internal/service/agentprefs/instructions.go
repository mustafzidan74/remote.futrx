package agentprefs

import (
	"strings"
	"unicode/utf8"
)

// Heading is the title of the managed block inside /workspace/AGENTS.md. It
// exists so a human opening the file understands why the text is there and
// that editing it is pointless.
const Heading = "## Reply preferences (managed by remote.futrx)"

// Instructions renders the managed block body for a resolved preference.
// `language` is the effective reply language — the per-user override when the
// user set one, otherwise the platform value. An empty result means "inject
// nothing", which is what a default deployment produces.
func Instructions(p Preferences, language string) string {
	reply := replySentence(language, p.Tone)
	extra := strings.TrimSpace(p.ExtraInstructions)
	if reply == "" && extra == "" {
		return ""
	}

	var out strings.Builder
	out.WriteString(Heading)
	out.WriteString("\n\n")
	if reply != "" {
		out.WriteString("- ")
		out.WriteString(reply)
		out.WriteString("\n")
	}
	if extra != "" {
		if reply != "" {
			out.WriteString("\n")
		}
		out.WriteString(extra)
		out.WriteString("\n")
	}
	return out.String()
}

// Preamble renders the one-paragraph form prepended to a run's prompt, for
// providers that read a system prompt before they read any file. It carries a
// bounded slice of the extra instructions: the full text is always in the
// workspace file, and this copy rides on every single prompt.
func Preamble(p Preferences, language string) string {
	reply := replySentence(language, p.Tone)
	extra := collapse(p.ExtraInstructions)
	if utf8.RuneCountInString(extra) > preambleExtraBudget {
		extra = string([]rune(extra)[:preambleExtraBudget]) + " […]"
	}
	switch {
	case reply == "" && extra == "":
		return ""
	case extra == "":
		return reply
	case reply == "":
		return extra
	default:
		return reply + " " + extra
	}
}

// replySentence is the single sentence both channels are built from, so the
// file and the preamble can never drift into saying different things.
func replySentence(language string, tone Tone) string {
	clauses := make([]string, 0, 3)
	if clause := languageClause(language); clause != "" {
		clauses = append(clauses, clause)
		clauses = append(
			clauses,
			"keep code, identifiers, commands and file paths in English",
		)
	}
	if clause := toneClause(tone); clause != "" {
		clauses = append(clauses, clause)
	}
	if len(clauses) == 0 {
		return ""
	}
	return strings.Join(clauses, "; ") + "."
}

// languageClause names the language to answer in. "auto" produces nothing:
// mirroring the user is what every agent already does, so saying it out loud
// would only spend tokens.
func languageClause(language string) string {
	language = normalizeLanguage(language)
	switch language {
	case LanguageAuto:
		return ""
	case LanguageEnglish:
		return "Reply in English unless the user writes in another language"
	case LanguageArabic:
		return "Reply in Modern Standard Arabic (العربية الفصحى المبسطة) unless the user writes in another language"
	case LanguageEgyptianArabic:
		return "Reply in Egyptian Arabic (عامية مصرية مبسطة) unless the user writes in another language"
	default:
		return "Reply in " + collapse(language) + " unless the user writes in another language"
	}
}

func toneClause(tone Tone) string {
	switch tone {
	case ToneConcise:
		return "be concise"
	case ToneDetailed:
		return "be thorough — explain your reasoning and call out trade-offs"
	default:
		return ""
	}
}

// collapse folds a multi-line value onto one line so it can be embedded in a
// prompt preamble without breaking whatever framing the provider adds.
func collapse(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
