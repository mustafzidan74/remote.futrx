// Package playbooks owns the admin-managed library of one-click prompt
// templates offered in the chat composer.
//
// A playbook is a saved prompt plus the chat configuration it wants: the
// skills to preselect, and optionally the mode and provider to switch to.
// The prompt may carry `{{placeholder}}` tokens that the client resolves
// against the chat's project before the text reaches the composer.
package playbooks

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// ErrInvalidPlaybooks reports a library an admin submitted that the service
// refuses to persist. It is the only validation error the handler maps to 400.
var ErrInvalidPlaybooks = errors.New("invalid playbooks")

const (
	// MaxPlaybooks caps the library so the composer menu stays usable and one
	// PUT can never balloon the stored document.
	MaxPlaybooks = 50
	// MaxSkillsPerPlaybook caps the skills a single playbook preselects.
	MaxSkillsPerPlaybook = 20

	maxIDLength     = 64
	maxTitleLength  = 120
	maxIconRunes    = 8
	maxPromptLength = 8000
	maxHintLength   = 200
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Mode mirrors the chat composer's execution modes. An empty mode means
// "leave the chat's current mode alone".
type Mode string

// Provider mirrors the agent providers a chat can run. An empty provider means
// "leave the chat's current provider alone".
type Provider string

var validModes = map[Mode]struct{}{
	"chat": {}, "plan": {}, "code": {}, "review": {}, "debug": {}, "full-auto": {},
}

var validProviders = map[Provider]struct{}{
	"claude": {}, "codex": {}, "kimi": {}, "antigravity": {},
}

// SkillRef names a skill to preselect when a playbook is applied. It is the
// same shape the chat store persists in `selectedSkills`, so the frontend can
// hand it to the existing chat update API untouched.
type SkillRef struct {
	Name     string   `json:"name,omitempty"`
	Command  string   `json:"command,omitempty"`
	Provider Provider `json:"provider,omitempty"`
	Source   string   `json:"source,omitempty"`
}

// Playbook is one entry of the library.
type Playbook struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Icon     string     `json:"icon,omitempty"`
	Hint     string     `json:"hint,omitempty"`
	Prompt   string     `json:"prompt"`
	Skills   []SkillRef `json:"skills,omitempty"`
	Mode     Mode       `json:"mode,omitempty"`
	Provider Provider   `json:"provider,omitempty"`
	Order    int        `json:"order"`
}

// Normalize returns a library that is trimmed, ordered, and re-indexed. It is
// the shape both the store and the API expose, so a round trip through either
// is stable.
func Normalize(list []Playbook) []Playbook {
	out := make([]Playbook, 0, len(list))
	for _, item := range list {
		item.ID = strings.ToLower(strings.TrimSpace(item.ID))
		item.Title = strings.TrimSpace(item.Title)
		item.Icon = strings.TrimSpace(item.Icon)
		item.Hint = strings.TrimSpace(item.Hint)
		item.Prompt = strings.TrimSpace(item.Prompt)
		item.Mode = Mode(strings.ToLower(strings.TrimSpace(string(item.Mode))))
		item.Provider = Provider(strings.ToLower(strings.TrimSpace(string(item.Provider))))
		item.Skills = normalizeSkills(item.Skills)
		out = append(out, item)
	}
	// A stable sort keeps the submitted relative order for equal `order`
	// values, so an admin reordering by drag does not need unique numbers.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	for i := range out {
		out[i].Order = i
	}
	return out
}

func normalizeSkills(skills []SkillRef) []SkillRef {
	if len(skills) == 0 {
		return nil
	}
	out := make([]SkillRef, 0, len(skills))
	seen := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		skill.Name = strings.TrimSpace(skill.Name)
		skill.Command = strings.TrimSpace(skill.Command)
		skill.Source = strings.TrimSpace(skill.Source)
		skill.Provider = Provider(strings.ToLower(strings.TrimSpace(string(skill.Provider))))
		if skill.Command == "" {
			skill.Command = skill.Name
		}
		if skill.Name == "" {
			skill.Name = skill.Command
		}
		if skill.Command == "" {
			continue
		}
		key := strings.ToLower(string(skill.Provider) + "\x00" + skill.Source + "\x00" + skill.Command)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, skill)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Validate reports why a normalized library cannot be stored. Callers are
// expected to Normalize first; Validate never mutates.
func Validate(list []Playbook) error {
	if len(list) > MaxPlaybooks {
		return fmt.Errorf("%w: at most %d playbooks are allowed", ErrInvalidPlaybooks, MaxPlaybooks)
	}
	seen := make(map[string]struct{}, len(list))
	for _, item := range list {
		if item.ID == "" || len(item.ID) > maxIDLength || !idPattern.MatchString(item.ID) {
			return fmt.Errorf(
				"%w: id %q must be lowercase letters, digits, or dashes (max %d)",
				ErrInvalidPlaybooks, item.ID, maxIDLength,
			)
		}
		if _, dup := seen[item.ID]; dup {
			return fmt.Errorf("%w: duplicate id %q", ErrInvalidPlaybooks, item.ID)
		}
		seen[item.ID] = struct{}{}
		if item.Title == "" || utf8.RuneCountInString(item.Title) > maxTitleLength {
			return fmt.Errorf("%w: %q needs a title of at most %d characters", ErrInvalidPlaybooks, item.ID, maxTitleLength)
		}
		if utf8.RuneCountInString(item.Icon) > maxIconRunes {
			return fmt.Errorf("%w: %q icon must be at most %d characters", ErrInvalidPlaybooks, item.ID, maxIconRunes)
		}
		if utf8.RuneCountInString(item.Hint) > maxHintLength {
			return fmt.Errorf("%w: %q hint must be at most %d characters", ErrInvalidPlaybooks, item.ID, maxHintLength)
		}
		if item.Prompt == "" || utf8.RuneCountInString(item.Prompt) > maxPromptLength {
			return fmt.Errorf("%w: %q needs a prompt of at most %d characters", ErrInvalidPlaybooks, item.ID, maxPromptLength)
		}
		if item.Mode != "" {
			if _, ok := validModes[item.Mode]; !ok {
				return fmt.Errorf("%w: %q has unknown mode %q", ErrInvalidPlaybooks, item.ID, item.Mode)
			}
		}
		if item.Provider != "" {
			if _, ok := validProviders[item.Provider]; !ok {
				return fmt.Errorf("%w: %q has unknown provider %q", ErrInvalidPlaybooks, item.ID, item.Provider)
			}
		}
		if len(item.Skills) > MaxSkillsPerPlaybook {
			return fmt.Errorf(
				"%w: %q selects %d skills, at most %d are allowed",
				ErrInvalidPlaybooks, item.ID, len(item.Skills), MaxSkillsPerPlaybook,
			)
		}
		for _, skill := range item.Skills {
			if skill.Provider == "" {
				continue
			}
			if _, ok := validProviders[skill.Provider]; !ok {
				return fmt.Errorf(
					"%w: %q references skill %q for unknown provider %q",
					ErrInvalidPlaybooks, item.ID, skill.Command, skill.Provider,
				)
			}
		}
	}
	return nil
}
