package team

import (
	"errors"
	"strings"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

var errNoChatFactory = errors.New("team companion chats are unavailable")

// The global skills a companion seat wants. They are names in the operator's
// global library, not files this repository ships: an install without them
// still runs the loop, just without the protocol behind it.
const (
	// ReviewProtocolSkill is the review checklist the reviewer applies.
	ReviewProtocolSkill = "review-protocol"
	// PlaywrightSkill is the end-to-end pass the tester runs.
	PlaywrightSkill = "playwright-e2e"
	// guardSuffix marks the review guard skills (clean-code-guard,
	// test-guard, wp-guard, …). They are picked up by suffix rather than
	// listed, so a guard an operator publishes later joins reviews without a
	// code change here.
	guardSuffix = "-guard"
)

// CompanionSkills is the skill selection a companion chat starts with.
//
// The wish list is narrowed to what the global library actually holds, because
// selecting a skill that does not exist buys nothing and shows the operator a
// chip for a skill they cannot open. A nil library — the store is unavailable,
// or this is a test — means the wish list is used as-is: the loop is more
// useful with an optimistic guess than with no skills at all.
func CompanionSkills(
	role string,
	provider servicechat.Provider,
	available []string,
) []servicechat.SkillRef {
	wanted := wantedSkills(role, available)
	if len(wanted) == 0 {
		return nil
	}
	refs := make([]servicechat.SkillRef, 0, len(wanted))
	for _, name := range wanted {
		refs = append(refs, servicechat.SkillRef{
			Name:     name,
			Command:  name,
			Provider: provider,
			Source:   globalSkillSource,
		})
	}
	return refs
}

// globalSkillSource is the source tag the skills catalog uses for the global
// library. It is duplicated rather than imported so the team package does not
// depend on the skills service for one string.
const globalSkillSource = "global"

func wantedSkills(role string, available []string) []string {
	switch role {
	case servicechat.TeamRoleReviewer:
		return narrow(append([]string{ReviewProtocolSkill}, guards(available)...), available)
	case servicechat.TeamRoleTester:
		return narrow([]string{PlaywrightSkill}, available)
	default:
		return nil
	}
}

// guards returns every published skill whose name marks it a review guard.
func guards(available []string) []string {
	found := make([]string, 0, len(available))
	for _, name := range available {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), guardSuffix) {
			found = append(found, strings.TrimSpace(name))
		}
	}
	return found
}

func narrow(wanted, available []string) []string {
	if len(available) == 0 {
		return dedupe(wanted)
	}
	published := make(map[string]string, len(available))
	for _, name := range available {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			published[strings.ToLower(trimmed)] = trimmed
		}
	}
	kept := make([]string, 0, len(wanted))
	for _, name := range wanted {
		if actual, ok := published[strings.ToLower(strings.TrimSpace(name))]; ok {
			kept = append(kept, actual)
		}
	}
	return dedupe(kept)
}

func dedupe(names []string) []string {
	seen := make(map[string]bool, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, strings.TrimSpace(name))
	}
	if len(unique) == 0 {
		return nil
	}
	return unique
}

// CompanionMode is the chat mode a seat runs in. The reviewer gets review
// mode, which is the platform's word for "read and judge"; the tester gets
// code mode because writing an e2e spec means writing files.
func CompanionMode(role string) string {
	if role == servicechat.TeamRoleReviewer {
		return "review"
	}
	return "code"
}
