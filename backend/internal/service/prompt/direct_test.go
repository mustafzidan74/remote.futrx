package prompt

import (
	"context"
	"errors"
	"strings"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

type recordingResponder struct {
	got     DirectRequest
	answer  string
	failure error
}

func (r *recordingResponder) Answer(_ context.Context, req DirectRequest) (DirectResult, error) {
	r.got = req
	if r.failure != nil {
		return DirectResult{}, r.failure
	}
	return DirectResult{Text: r.answer}, nil
}

func (r *recordingResponder) Choices(context.Context) []DirectChoice { return nil }

// TestTheModelIsToldItHasNoTools pins the framing.
//
// Without it these models answer as though the repository were in front of
// them — "I'll go ahead and update that file" — and then nothing happens. The
// operator is left thinking the run failed rather than that they picked a
// model that cannot do it.
func TestTheModelIsToldItHasNoTools(t *testing.T) {
	responder := &recordingResponder{answer: "done"}
	service := &Service{direct: responder}

	var events []ChatEvent
	err := service.answerDirectly(
		context.Background(),
		servicechat.ID("c1"),
		ChatMeta{DirectModel: servicechat.DirectModel{Source: servicechat.DirectSourceLocal, Model: "qwen3:1.7b"}},
		"fix the login bug",
		Actor{},
		"",
		nil,
		func(e ChatEvent) { events = append(events, e) },
	)
	if err != nil {
		t.Fatalf("answerDirectly() = %v", err)
	}

	system := responder.got.SystemPrompt
	for _, want := range []string{"NO tools", "cannot read or write files", "switch the chat to a coding agent"} {
		if !strings.Contains(system, want) {
			t.Errorf("the system prompt should say %q:\n%s", want, system)
		}
	}
	if responder.got.UserText != "fix the login bug" {
		t.Errorf("user text = %q, want the prompt through unchanged", responder.got.UserText)
	}
}

// TestTheAnswerLooksLikeAnyOtherTurn keeps the transcript uniform: whatever
// answered, the events a reader sees are the same.
func TestTheAnswerLooksLikeAnyOtherTurn(t *testing.T) {
	service := &Service{direct: &recordingResponder{answer: "Yes — that regex is greedy."}}

	var events []ChatEvent
	if err := service.answerDirectly(
		context.Background(),
		servicechat.ID("c1"),
		ChatMeta{DirectModel: servicechat.DirectModel{Source: servicechat.DirectSourcePool, ProviderID: "gemini"}},
		"is this regex greedy?",
		Actor{},
		"",
		nil,
		func(e ChatEvent) { events = append(events, e) },
	); err != nil {
		t.Fatalf("answerDirectly() = %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("emitted %d events, want the question, the answer and a completion: %+v", len(events), events)
	}
	// The question is persisted here and nowhere else: there is no CLI on this
	// path to echo it, so without this a reloaded chat shows answers alone.
	if events[0].Type != "user" || events[0].Text != "is this regex greedy?" {
		t.Errorf("first event = %+v, want the user's question", events[0])
	}
	if events[1].Type != "assistant_text" || events[1].Text != "Yes — that regex is greedy." {
		t.Errorf("second event = %+v, want the answer as assistant_text", events[1])
	}
	if events[2].Type != "complete" {
		t.Errorf("last event = %q, want the turn marked complete", events[2].Type)
	}
}

// TestAFailureNamesTheModel is what turns "something went wrong" into an
// action: the operator has several models on offer and needs to know which one
// refused.
func TestAFailureNamesTheModel(t *testing.T) {
	service := &Service{direct: &recordingResponder{failure: errors.New("quota exhausted")}}

	var events []ChatEvent
	err := service.answerDirectly(
		context.Background(),
		servicechat.ID("c1"),
		ChatMeta{DirectModel: servicechat.DirectModel{
			Source: servicechat.DirectSourcePool, ProviderID: "gemini", Model: "gemini-flash-latest",
		}},
		"hello",
		Actor{},
		"",
		nil,
		func(e ChatEvent) { events = append(events, e) },
	)
	if err == nil {
		t.Fatal("a refused turn must return an error")
	}
	if len(events) != 2 || events[1].Type != "error" {
		t.Fatalf("events = %+v, want the question then an error", events)
	}
	for _, want := range []string{"gemini-flash-latest", "quota exhausted"} {
		if !strings.Contains(events[1].Message, want) {
			t.Errorf("message %q should mention %q", events[1].Message, want)
		}
	}
}

// TestNoResponderFailsLoudly covers a misconfiguration: a chat can only carry
// a direct model if the picker offered one, so reaching here with nothing
// wired means the deployment is inconsistent, not that the user did something
// wrong. Silently running an agent instead would spend a subscription the
// operator was explicitly avoiding.
func TestNoResponderFailsLoudly(t *testing.T) {
	service := &Service{}
	var events []ChatEvent
	err := service.answerDirectly(
		context.Background(),
		servicechat.ID("c1"),
		ChatMeta{DirectModel: servicechat.DirectModel{Source: servicechat.DirectSourceLocal}},
		"hello",
		Actor{},
		"",
		nil,
		func(e ChatEvent) { events = append(events, e) },
	)
	if !errors.Is(err, ErrNoDirectResponder) {
		t.Fatalf("err = %v, want ErrNoDirectResponder", err)
	}
	if len(events) != 2 || events[1].Type != "error" {
		t.Fatalf("events = %+v, want the question then an error", events)
	}
}
