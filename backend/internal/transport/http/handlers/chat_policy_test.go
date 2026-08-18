package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// chatRepoStub is the smallest repository the chat service will run against.
// Only the metadata half is exercised here; the event log is not involved in a
// policy patch.
type chatRepoStub struct {
	meta servicechat.Meta
}

func (r *chatRepoStub) List(context.Context) ([]servicechat.Meta, error) {
	return []servicechat.Meta{r.meta}, nil
}

func (r *chatRepoStub) Create(_ context.Context, meta servicechat.Meta) (servicechat.Meta, error) {
	r.meta = meta
	return r.meta, nil
}

func (r *chatRepoStub) Get(_ context.Context, id servicechat.ID) (servicechat.Meta, error) {
	if id != r.meta.ID {
		return servicechat.Meta{}, servicechat.ErrNotFound
	}
	return r.meta, nil
}

func (r *chatRepoStub) Update(
	_ context.Context,
	id servicechat.ID,
	fn func(*servicechat.Meta),
) (servicechat.Meta, error) {
	if id != r.meta.ID {
		return servicechat.Meta{}, servicechat.ErrNotFound
	}
	fn(&r.meta)
	return r.meta, nil
}

func (r *chatRepoStub) Delete(context.Context, servicechat.ID) error { return nil }

func (r *chatRepoStub) ReadEvents(context.Context, servicechat.ID) ([]servicechat.Event, error) {
	return nil, nil
}

func (r *chatRepoStub) ReadEventsPage(
	context.Context,
	servicechat.ID,
	servicechat.EventPageQuery,
) (servicechat.EventPage, error) {
	return servicechat.EventPage{}, nil
}

func (r *chatRepoStub) ReadEventsAfter(
	context.Context,
	servicechat.ID,
	int64,
) ([]servicechat.Event, error) {
	return nil, nil
}

func (r *chatRepoStub) AppendEvent(
	_ context.Context,
	_ servicechat.ID,
	ev servicechat.Event,
) (servicechat.Event, error) {
	return ev, nil
}

func (r *chatRepoStub) TruncateEventsBefore(
	context.Context,
	servicechat.ID,
	int64,
) ([]servicechat.Event, error) {
	return nil, nil
}

func newPolicyChatHandler(t *testing.T) (*ChatHandler, *chatRepoStub) {
	t.Helper()
	repo := &chatRepoStub{meta: servicechat.Meta{ID: "abcd1234", Title: "Ship the checkout flow"}}
	chats := servicechat.New(repo, nil, nil, nil)
	// Auth is left nil so the handler runs with an anonymous caller; that is
	// exactly what makes the "actor never comes from the body" assertion
	// below meaningful.
	return NewChatHandler(chats, servicechat.NewAccessService(chats, nil), nil, nil, nil, nil), repo
}

func patchChat(t *testing.T, handler *ChatHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPatch, "/api/chats/abcd1234", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.HandleResource(recorder, request)
	return recorder
}

func TestChatPatchValidatesAutopilotLimits(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "a bare toggle is accepted",
			body:       `{"autopilot":{"enabled":true}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "limits inside the bounds are accepted",
			body:       `{"autopilot":{"enabled":true,"maxRounds":20,"maxDurationMin":600}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "auto-test is accepted on its own",
			body:       `{"autoTest":{"enabled":true}}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "zero rounds is refused",
			body:       `{"autopilot":{"enabled":true,"maxRounds":0}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a round count above the cap is refused",
			body:       `{"autopilot":{"maxRounds":9000}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a duration below the floor is refused",
			body:       `{"autopilot":{"maxDurationMin":1}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a duration beyond a day is refused",
			body:       `{"autopilot":{"maxDurationMin":1441}}`,
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

func TestChatPatchArmsAndDisarmsAutopilot(t *testing.T) {
	handler, repo := newPolicyChatHandler(t)

	recorder := patchChat(t, handler, `{"autopilot":{"enabled":true,"maxRounds":5,"maxDurationMin":45}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body)
	}
	var armed servicechat.Meta
	if err := json.Unmarshal(recorder.Body.Bytes(), &armed); err != nil {
		t.Fatal(err)
	}
	if !armed.Autopilot.Enabled || armed.Autopilot.MaxRounds != 5 || armed.Autopilot.MaxDurationMin != 45 {
		t.Fatalf("armed policy = %+v", armed.Autopilot)
	}
	if armed.Autopilot.StartedAt == 0 {
		t.Errorf("arming did not stamp a start time")
	}

	// Rounds already spent must not survive a re-arm, or a chat could only
	// ever be autopiloted once.
	repo.meta.Autopilot.RoundsUsed = 5
	if code := patchChat(t, handler, `{"autopilot":{"enabled":false}}`).Code; code != http.StatusOK {
		t.Fatalf("disarm status = %d", code)
	}
	if repo.meta.Autopilot.Enabled {
		t.Errorf("autopilot stayed armed after being switched off")
	}
	if repo.meta.Autopilot.RoundsUsed != 5 {
		t.Errorf("roundsUsed = %d, want the spent count kept for display", repo.meta.Autopilot.RoundsUsed)
	}

	if code := patchChat(t, handler, `{"autopilot":{"enabled":true}}`).Code; code != http.StatusOK {
		t.Fatalf("re-arm status = %d", code)
	}
	if repo.meta.Autopilot.RoundsUsed != 0 {
		t.Errorf("roundsUsed = %d, want 0 after re-arming", repo.meta.Autopilot.RoundsUsed)
	}
}

// The actor a synthetic run is attributed to comes from the session, so a
// request body that names someone else must be ignored entirely.
func TestChatPatchIgnoresAnActorFromTheBody(t *testing.T) {
	handler, repo := newPolicyChatHandler(t)

	body := `{"autopilot":{"enabled":true},"actorEmail":"attacker@example.com","enabledBy":"attacker@example.com"}`
	if code := patchChat(t, handler, body).Code; code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}

	if repo.meta.Autopilot.EnabledBy != "" {
		t.Errorf("enabledBy = %q, want it taken from the session and not the body", repo.meta.Autopilot.EnabledBy)
	}
}

func TestChatPatchLeavesPoliciesAloneWhenAbsent(t *testing.T) {
	handler, repo := newPolicyChatHandler(t)
	repo.meta.Autopilot = servicechat.AutopilotPolicy{
		Enabled:        true,
		MaxRounds:      6,
		MaxDurationMin: 60,
		RoundsUsed:     2,
		EnabledBy:      "operator@example.com",
	}
	repo.meta.AutoTest = servicechat.AutoTestPolicy{Enabled: true, EnabledBy: "operator@example.com"}

	if code := patchChat(t, handler, `{"title":"Renamed"}`).Code; code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}

	if !repo.meta.Autopilot.Enabled || repo.meta.Autopilot.RoundsUsed != 2 {
		t.Errorf("an unrelated patch disturbed autopilot: %+v", repo.meta.Autopilot)
	}
	if !repo.meta.AutoTest.Enabled {
		t.Errorf("an unrelated patch disturbed auto-test: %+v", repo.meta.AutoTest)
	}
}
