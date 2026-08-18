package usersettings

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrNotFound               = errors.New("user settings not found")
	ErrInvalidIdentity        = errors.New("user settings identity is required")
	ErrInvalidTheme           = errors.New("invalid appearance theme")
	ErrInvalidChatProvider    = errors.New("invalid chat provider")
	ErrInvalidChatMode        = errors.New("invalid chat mode")
	ErrInvalidReasoningEffort = errors.New("invalid reasoning effort")
	ErrInvalidServiceTier     = errors.New("invalid service tier")
	ErrInvalidReplyLanguage   = errors.New("invalid reply language")
)

// MaxReplyLanguageLength caps a custom reply-language label. It mirrors the
// platform-side cap in service/agentprefs so neither side can store a value
// the other refuses.
const MaxReplyLanguageLength = 64

type Key string

const LocalAdminKey Key = "local-admin"

type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeDark   Theme = "dark"
	ThemeLight  Theme = "light"
)

type Settings struct {
	Appearance Appearance `json:"appearance"`
	Chat       Chat       `json:"chat"`
	Agent      Agent      `json:"agent"`
	UpdatedAt  int64      `json:"updatedAt,omitempty"`
}

// Agent holds this user's personal overrides of the platform-wide agent
// preferences. Only the reply language is personal: tone and house rules are
// platform policy an individual cannot opt out of.
type Agent struct {
	// ReplyLanguage overrides the platform reply language for runs this user
	// starts. Empty means "follow the platform setting"; "auto" is a real
	// choice meaning "mirror whatever I write in".
	ReplyLanguage string `json:"replyLanguage"`
}

type Appearance struct {
	Theme Theme `json:"theme"`
}

type ChatProvider string

const (
	ChatProviderClaude      ChatProvider = "claude"
	ChatProviderCodex       ChatProvider = "codex"
	ChatProviderKimi        ChatProvider = "kimi"
	ChatProviderAntigravity ChatProvider = "antigravity"
)

type ChatMode string

const (
	ChatModeChat     ChatMode = "chat"
	ChatModePlan     ChatMode = "plan"
	ChatModeCode     ChatMode = "code"
	ChatModeReview   ChatMode = "review"
	ChatModeDebug    ChatMode = "debug"
	ChatModeFullAuto ChatMode = "full-auto"
)

type ReasoningEffort string

const (
	ReasoningEffortAuto    ReasoningEffort = ""
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
	ReasoningEffortMax     ReasoningEffort = "max"
	ReasoningEffortUltra   ReasoningEffort = "ultra"
)

type ServiceTier string

const (
	ServiceTierAuto     ServiceTier = ""
	ServiceTierDefault  ServiceTier = "default"
	ServiceTierPriority ServiceTier = "priority"
	ServiceTierFast     ServiceTier = "fast"
)

type Chat struct {
	Provider        ChatProvider    `json:"provider"`
	Model           string          `json:"model"`
	Mode            ChatMode        `json:"mode"`
	ReasoningEffort ReasoningEffort `json:"reasoningEffort"`
	ServiceTier     ServiceTier     `json:"serviceTier"`
}

type UpdateInput struct {
	Appearance *AppearanceUpdate `json:"appearance,omitempty"`
	Chat       *ChatUpdate       `json:"chat,omitempty"`
	Agent      *AgentUpdate      `json:"agent,omitempty"`
}

type AgentUpdate struct {
	ReplyLanguage *string `json:"replyLanguage,omitempty"`
}

type AppearanceUpdate struct {
	Theme *Theme `json:"theme,omitempty"`
}

type ChatUpdate struct {
	Provider        *ChatProvider    `json:"provider,omitempty"`
	Model           *string          `json:"model,omitempty"`
	Mode            *ChatMode        `json:"mode,omitempty"`
	ReasoningEffort *ReasoningEffort `json:"reasoningEffort,omitempty"`
	ServiceTier     *ServiceTier     `json:"serviceTier,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		Appearance: Appearance{Theme: ThemeSystem},
		Chat: Chat{
			Provider:        ChatProviderCodex,
			Model:           "",
			Mode:            ChatModeCode,
			ReasoningEffort: ReasoningEffortAuto,
			ServiceTier:     ServiceTierAuto,
		},
		Agent: Agent{ReplyLanguage: ""},
	}
}

// ValidReplyLanguage accepts an empty value ("follow the platform"), any
// built-in id, and any single-line custom label within the cap.
func ValidReplyLanguage(language string) bool {
	return utf8.RuneCountInString(language) <= MaxReplyLanguageLength &&
		!strings.ContainsAny(language, "\n\r")
}

func ValidTheme(theme Theme) bool {
	switch theme {
	case ThemeSystem, ThemeDark, ThemeLight:
		return true
	default:
		return false
	}
}

func ValidChatProvider(provider ChatProvider) bool {
	switch provider {
	case ChatProviderClaude, ChatProviderCodex, ChatProviderKimi, ChatProviderAntigravity:
		return true
	default:
		return false
	}
}

func ValidChatMode(mode ChatMode) bool {
	switch mode {
	case ChatModeChat, ChatModePlan, ChatModeCode, ChatModeReview, ChatModeDebug, ChatModeFullAuto:
		return true
	default:
		return false
	}
}

func ValidReasoningEffort(effort ReasoningEffort) bool {
	switch effort {
	case ReasoningEffortAuto, ReasoningEffortNone, ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax, ReasoningEffortUltra:
		return true
	default:
		return false
	}
}

func ValidServiceTier(tier ServiceTier) bool {
	switch tier {
	case ServiceTierAuto, ServiceTierDefault, ServiceTierPriority, ServiceTierFast:
		return true
	default:
		return false
	}
}
