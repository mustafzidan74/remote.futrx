package httphandlers

import (
	"encoding/json"
	"net/http"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// The handler is the only place a caller can reach the team policy, so the
// bounds it refuses are the bounds the loop actually runs under.
func TestChatPatchValidatesTeamSettings(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "a bare toggle is accepted",
			body:       `{"team":{"enabled":true}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "a full cast is accepted",
			body:       `{"team":{"enabled":true,"maxLoops":3,"autoFix":true,"roles":{"reviewer":{"provider":"codex","enabled":true},"tester":{"provider":"kimi","model":"kimi-k2","enabled":true}}}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "an empty provider hands the choice back to the platform",
			body:       `{"team":{"roles":{"reviewer":{"provider":""}}}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "zero loops is refused",
			body:       `{"team":{"enabled":true,"maxLoops":0}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a loop count above the cap is refused",
			body:       `{"team":{"maxLoops":99}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "an unknown provider is refused rather than defaulted to codex",
			body:       `{"team":{"roles":{"reviewer":{"provider":"gpt5"}}}}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, _ := newPolicyChatHandler(t)

			recorder := patchChat(t, handler, test.body)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, test.wantStatus, recorder.Body)
			}
		})
	}
}

func TestChatPatchArmsAndDisarmsTeamMode(t *testing.T) {
	handler, repo := newPolicyChatHandler(t)

	recorder := patchChat(t, handler, `{"team":{"enabled":true,"maxLoops":3,"autoFix":true,`+
		`"roles":{"reviewer":{"provider":"codex","enabled":true},"tester":{"provider":"codex","enabled":true}}}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body)
	}
	var armed servicechat.Meta
	if err := json.Unmarshal(recorder.Body.Bytes(), &armed); err != nil {
		t.Fatal(err)
	}
	if !armed.Team.Enabled || armed.Team.MaxLoops != 3 || !armed.Team.AutoFix {
		t.Fatalf("armed policy = %+v", armed.Team)
	}
	if armed.Team.Roles.Reviewer.Provider != servicechat.ProviderCodex ||
		!armed.Team.Roles.Reviewer.Enabled {
		t.Errorf("reviewer seat = %+v", armed.Team.Roles.Reviewer)
	}
	if armed.Team.Phase != servicechat.TeamPhaseIdle {
		t.Errorf("phase = %q, want a fresh session", armed.Team.Phase)
	}

	// Loops already spent must not survive a re-arm, or a chat could only be
	// team-reviewed once.
	repo.meta.Team.LoopsUsed = 3
	repo.meta.Team.Phase = servicechat.TeamPhaseDone
	repo.meta.Team.Roles.Reviewer.ChatID = "aaaabbbb"

	if code := patchChat(t, handler, `{"team":{"enabled":false}}`).Code; code != http.StatusOK {
		t.Fatalf("disarm status = %d", code)
	}
	if repo.meta.Team.Enabled || repo.meta.Team.Phase != servicechat.TeamPhaseIdle {
		t.Errorf("disarmed policy = %+v", repo.meta.Team)
	}

	if code := patchChat(t, handler, `{"team":{"enabled":true}}`).Code; code != http.StatusOK {
		t.Fatalf("re-arm status = %d", code)
	}
	if repo.meta.Team.LoopsUsed != 0 {
		t.Errorf("loopsUsed = %d, want 0 after re-arming", repo.meta.Team.LoopsUsed)
	}
	if repo.meta.Team.Roles.Reviewer.ChatID != "aaaabbbb" {
		t.Errorf("re-arming lost the reviewer's companion chat")
	}
}

// The loop's runtime state belongs to the team service. A client that tries to
// dictate the phase, the counter, or a companion chat id must be ignored, or
// anyone with PATCH access could aim a synthetic run at a chat of their choice.
func TestChatPatchIgnoresTeamRuntimeStateFromTheBody(t *testing.T) {
	handler, repo := newPolicyChatHandler(t)
	repo.meta.Team = servicechat.TeamPolicy{
		Enabled:   true,
		MaxLoops:  2,
		LoopsUsed: 1,
		Phase:     servicechat.TeamPhaseReviewing,
		Roles: servicechat.TeamRoles{
			Reviewer: servicechat.TeamRole{Provider: servicechat.ProviderCodex, Enabled: true, ChatID: "aaaabbbb"},
		},
	}

	recorder := patchChat(t, handler,
		`{"team":{"maxLoops":4,"phase":"done","loopsUsed":0,"verdict":"ship",`+
			`"roles":{"reviewer":{"chatId":"deadbeef"}}}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body)
	}

	if repo.meta.Team.MaxLoops != 4 {
		t.Errorf("maxLoops = %d, want the configured value applied", repo.meta.Team.MaxLoops)
	}
	if repo.meta.Team.Phase != servicechat.TeamPhaseReviewing {
		t.Errorf("phase = %q, want the loop's own state untouched", repo.meta.Team.Phase)
	}
	if repo.meta.Team.LoopsUsed != 1 {
		t.Errorf("loopsUsed = %d, want the loop's own counter untouched", repo.meta.Team.LoopsUsed)
	}
	if repo.meta.Team.Roles.Reviewer.ChatID != "aaaabbbb" {
		t.Errorf("reviewer chat = %q, want a body-supplied id ignored", repo.meta.Team.Roles.Reviewer.ChatID)
	}
}
