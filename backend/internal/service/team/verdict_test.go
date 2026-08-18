package team

import (
	"strings"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// The verdict is the only branch signal the loop has, and agents reach it
// through a reply-preference preamble that may put the whole answer in Arabic,
// through markdown that bolds the line, and through CLIs that bullet it. Each
// case below is one of those realities.
func TestParseVerdictReadsTheMarkerThroughNoise(t *testing.T) {
	tests := []struct {
		name         string
		role         string
		output       string
		wantKind     string
		wantFindings string
	}{
		{
			name:         "a bare reviewer verdict with findings after it",
			role:         servicechat.TeamRoleReviewer,
			output:       "Looked at the diff.\nVERDICT: FIX\n1. auth.go:22 — the token is never revoked.",
			wantKind:     servicechat.TeamVerdictFix,
			wantFindings: "1. auth.go:22 — the token is never revoked.",
		},
		{
			name:         "a ship verdict with nothing after it falls back to what came before",
			role:         servicechat.TeamRoleReviewer,
			output:       "The change is small and covered by tests.\n\nVERDICT: SHIP\n",
			wantKind:     servicechat.TeamVerdictShip,
			wantFindings: "The change is small and covered by tests.",
		},
		{
			name:     "markdown emphasis around the line",
			role:     servicechat.TeamRoleReviewer,
			output:   "- **VERDICT: FIX** — see below\nmissing null check",
			wantKind: servicechat.TeamVerdictFix,
		},
		{
			name:     "a backticked verdict inside a bullet",
			role:     servicechat.TeamRoleReviewer,
			output:   "* `VERDICT: SHIP`",
			wantKind: servicechat.TeamVerdictShip,
		},
		{
			name:     "trailing punctuation after the value",
			role:     servicechat.TeamRoleReviewer,
			output:   "VERDICT: SHIP.",
			wantKind: servicechat.TeamVerdictShip,
		},
		{
			name:     "a space before the colon",
			role:     servicechat.TeamRoleReviewer,
			output:   "VERDICT : FIX",
			wantKind: servicechat.TeamVerdictFix,
		},
		{
			name:     "lowercase from a model that ignored the casing",
			role:     servicechat.TeamRoleReviewer,
			output:   "verdict: ship",
			wantKind: servicechat.TeamVerdictShip,
		},
		{
			name:     "an Arabic sentence wrapped around the marker",
			role:     servicechat.TeamRoleReviewer,
			output:   "راجعت التغييرات في الملفات.\nالحكم النهائي VERDICT: FIX يحتاج إصلاحاً",
			wantKind: servicechat.TeamVerdictFix,
		},
		{
			name:     "bidi isolates an RTL reply drops around the Latin token",
			role:     servicechat.TeamRoleReviewer,
			output:   "الخلاصة: ⁦VERDICT: SHIP⁩",
			wantKind: servicechat.TeamVerdictShip,
		},
		{
			name: "the instruction quoted before the real answer does not win",
			role: servicechat.TeamRoleReviewer,
			output: "I will end with VERDICT: SHIP or VERDICT: FIX as asked.\n" +
				"Now the review.\nVERDICT: FIX\nrace in the queue drain",
			wantKind:     servicechat.TeamVerdictFix,
			wantFindings: "race in the queue drain",
		},
		{
			name:     "prose that merely mentions the word is not a verdict",
			role:     servicechat.TeamRoleReviewer,
			output:   "My VERDICT is that this looks fine to me.",
			wantKind: servicechat.TeamVerdictUnknown,
		},
		{
			name:     "an unknown value is not a verdict",
			role:     servicechat.TeamRoleReviewer,
			output:   "VERDICT: MAYBE",
			wantKind: servicechat.TeamVerdictUnknown,
		},
		{
			name:     "no marker at all",
			role:     servicechat.TeamRoleReviewer,
			output:   "Everything looks good, ship it.",
			wantKind: servicechat.TeamVerdictUnknown,
		},
		{
			name:         "a tester pass with its assertion output",
			role:         servicechat.TeamRoleTester,
			output:       "Ran 3 specs.\nTESTS: PASS\n3 passed (4.1s)",
			wantKind:     servicechat.TeamVerdictPass,
			wantFindings: "3 passed (4.1s)",
		},
		{
			name:     "a tester fail with a parenthesised count",
			role:     servicechat.TeamRoleTester,
			output:   "**TESTS: FAIL** (1 of 3)\nexpected 200, got 500",
			wantKind: servicechat.TeamVerdictFail,
		},
		{
			name:     "the reviewer marker does not satisfy the tester",
			role:     servicechat.TeamRoleTester,
			output:   "VERDICT: SHIP",
			wantKind: servicechat.TeamVerdictUnknown,
		},
		{
			name:     "an unknown seat never produces a verdict",
			role:     "designer",
			output:   "VERDICT: SHIP",
			wantKind: servicechat.TeamVerdictUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseVerdict(test.role, test.output)
			if got.Kind != test.wantKind {
				t.Fatalf("kind = %q, want %q", got.Kind, test.wantKind)
			}
			if test.wantFindings != "" && got.Findings != test.wantFindings {
				t.Errorf("findings = %q, want %q", got.Findings, test.wantFindings)
			}
		})
	}
}

// Findings ride into the next prompt and into meta.json, so a runaway answer
// has to be cut — and cut at the end, which is where an agent puts its
// conclusions.
func TestParseVerdictBoundsFindings(t *testing.T) {
	tail := "the last thing it said"
	output := "VERDICT: FIX\n" + strings.Repeat("noise ", 2000) + tail

	got := ParseVerdict(servicechat.TeamRoleReviewer, output)

	if got.Kind != servicechat.TeamVerdictFix {
		t.Fatalf("kind = %q", got.Kind)
	}
	if len(got.Findings) > maxFindingsBytes+8 {
		t.Errorf("findings kept %d bytes, want at most %d", len(got.Findings), maxFindingsBytes)
	}
	if !strings.HasSuffix(got.Findings, tail) {
		t.Errorf("findings dropped the tail of the answer: %q", got.Findings)
	}
}
