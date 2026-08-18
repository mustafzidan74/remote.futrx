// Package agentprefs owns the platform-wide agent reply preferences: the
// language an agent answers in, how verbose it is, and any extra house rules
// an admin wants every project to inherit.
//
// The preference is policy, not content. It reaches a running agent through
// two independent channels, because neither one alone covers every provider:
//
//  1. a managed block inside the project's /workspace/AGENTS.md, regenerated
//     on every run start so an edit propagates without recreating anything;
//  2. a short preamble prepended to the run's prompt, which applies even
//     before the agent has read any file.
//
// A per-user reply-language override lives in the user settings document and
// wins over the platform value for that user's own runs.
package agentprefs

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ErrInvalidPreferences reports a document an admin submitted that the service
// refuses to persist. It is the only validation error handlers map to 400.
var ErrInvalidPreferences = errors.New("invalid agent preferences")

const (
	// MaxExtraInstructionsLength caps the free-form house rules. They ride on
	// every prompt, so the cap is a cost control as much as a storage one.
	MaxExtraInstructionsLength = 4000
	// MaxReplyLanguageLength caps a custom language label ("Levantine Arabic",
	// "Brazilian Portuguese", ...).
	MaxReplyLanguageLength = 64
	// preambleExtraBudget caps how much of the extra instructions the prompt
	// preamble carries. The full text always reaches the workspace file.
	preambleExtraBudget = 600
)

// Built-in reply languages. Any other non-empty value is treated as a custom
// language label and rendered verbatim, which is what lets an operator ask for
// a dialect the platform never enumerated.
const (
	// LanguageAuto mirrors the user's own language, which is the default.
	LanguageAuto           = "auto"
	LanguageEnglish        = "en"
	LanguageArabic         = "ar"
	LanguageEgyptianArabic = "ar-EG"
)

// Tone is how much prose an answer should carry.
type Tone string

const (
	ToneDefault  Tone = "default"
	ToneConcise  Tone = "concise"
	ToneDetailed Tone = "detailed"
)

// ApplyTo decides which projects inherit the preference.
type ApplyTo string

const (
	// ApplyToAll injects the preference into every project and loose chat.
	ApplyToAll ApplyTo = "all"
	// ApplyToNewProjects injects it only into projects created at or after the
	// preference was last saved, so tightening the house rules never rewrites
	// the instructions of work already in flight. Loose chats (no project)
	// are excluded, because they have no creation instant to compare.
	ApplyToNewProjects ApplyTo = "newProjectsOnly"
)

// Preferences is the whole stored document.
type Preferences struct {
	ReplyLanguage     string  `json:"replyLanguage"`
	Tone              Tone    `json:"tone"`
	ExtraInstructions string  `json:"extraInstructions"`
	ApplyTo           ApplyTo `json:"applyTo"`
	UpdatedAt         int64   `json:"updatedAt,omitempty"`
	UpdatedBy         string  `json:"updatedBy,omitempty"`
}

// UpdateInput is a partial edit: an absent field keeps its stored value.
type UpdateInput struct {
	ReplyLanguage     *string  `json:"replyLanguage,omitempty"`
	Tone              *Tone    `json:"tone,omitempty"`
	ExtraInstructions *string  `json:"extraInstructions,omitempty"`
	ApplyTo           *ApplyTo `json:"applyTo,omitempty"`
}

// Defaults is the document a deployment that never opened the panel behaves
// as: mirror the user's language, no tone steer, no house rules. It injects
// nothing, which is what keeps this feature invisible until it is configured.
func Defaults() Preferences {
	return Preferences{
		ReplyLanguage: LanguageAuto,
		Tone:          ToneDefault,
		ApplyTo:       ApplyToAll,
	}
}

// Normalize trims and lower-cases the enumerated fields. Language labels keep
// their case, because a custom label is prose an operator typed.
func Normalize(p Preferences) Preferences {
	p.ReplyLanguage = normalizeLanguage(p.ReplyLanguage)
	p.Tone = Tone(strings.ToLower(strings.TrimSpace(string(p.Tone))))
	if p.Tone == "" {
		p.Tone = ToneDefault
	}
	p.ApplyTo = ApplyTo(strings.TrimSpace(string(p.ApplyTo)))
	if p.ApplyTo == "" {
		p.ApplyTo = ApplyToAll
	}
	p.ExtraInstructions = strings.TrimSpace(p.ExtraInstructions)
	p.UpdatedBy = strings.ToLower(strings.TrimSpace(p.UpdatedBy))
	return p
}

// Validate reports why a normalized document cannot be stored.
func Validate(p Preferences) error {
	if utf8.RuneCountInString(p.ReplyLanguage) > MaxReplyLanguageLength {
		return errors.Join(ErrInvalidPreferences, errors.New("reply language label is too long"))
	}
	if strings.ContainsAny(p.ReplyLanguage, "\n\r") {
		return errors.Join(ErrInvalidPreferences, errors.New("reply language must be a single line"))
	}
	if !ValidTone(p.Tone) {
		return errors.Join(ErrInvalidPreferences, errors.New("unknown tone"))
	}
	if !ValidApplyTo(p.ApplyTo) {
		return errors.Join(ErrInvalidPreferences, errors.New("unknown applyTo"))
	}
	if utf8.RuneCountInString(p.ExtraInstructions) > MaxExtraInstructionsLength {
		return errors.Join(ErrInvalidPreferences, errors.New("extra instructions are too long"))
	}
	return nil
}

func ValidTone(tone Tone) bool {
	switch tone {
	case ToneDefault, ToneConcise, ToneDetailed:
		return true
	default:
		return false
	}
}

func ValidApplyTo(applyTo ApplyTo) bool {
	switch applyTo {
	case ApplyToAll, ApplyToNewProjects:
		return true
	default:
		return false
	}
}

// ValidReplyLanguage accepts the built-ins plus any single-line custom label
// within the length cap. Emptiness means "auto".
func ValidReplyLanguage(language string) bool {
	language = strings.TrimSpace(language)
	return utf8.RuneCountInString(language) <= MaxReplyLanguageLength &&
		!strings.ContainsAny(language, "\n\r")
}

// normalizeLanguage maps the spellings a client may send onto the canonical
// built-in ids, and leaves anything else alone as a custom label.
func normalizeLanguage(language string) string {
	trimmed := strings.TrimSpace(language)
	if trimmed == "" {
		return LanguageAuto
	}
	switch strings.ToLower(trimmed) {
	case "auto", "":
		return LanguageAuto
	case "en", "en-us", "en-gb", "english":
		return LanguageEnglish
	case "ar", "arabic":
		return LanguageArabic
	case "ar-eg", "ar_eg", "egyptian arabic":
		return LanguageEgyptianArabic
	default:
		return trimmed
	}
}
