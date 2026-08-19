package prompt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicerouting "github.com/futrx-com/remote.futrx.com/internal/service/routing"
)

// stubRouter answers with one fixed decision and records what it was asked.
type stubRouter struct {
	decision servicerouting.Decision
	seen     []servicerouting.Input
}

func (r *stubRouter) Route(_ context.Context, input servicerouting.Input) servicerouting.Decision {
	r.seen = append(r.seen, input)
	return r.decision
}

// requestProvider captures the run request the prompt service builds, which is
// where a routing decision has to land.
type requestProvider struct {
	id       agent.ProviderID
	requests []agent.RunRequest
}

func (p *requestProvider) ID() agent.ProviderID                     { return p.id }
func (p *requestProvider) Parser(agent.RunRequest) agent.LineParser { return nil }

func (p *requestProvider) Run(_ context.Context, request agent.RunRequest, emit func(agent.Event)) error {
	p.requests = append(p.requests, request)
	emit(agent.Event{
		Type: agent.EventRunCompleted, Provider: request.Provider,
		Usage: json.RawMessage(`{"input_tokens":10,"output_tokens":5}`),
	})
	return nil
}

func runOneTurn(
	t *testing.T,
	chatModelPolicy string,
	router ModelRouter,
) (*requestProvider, *recordingLedger, servicechat.Repository, servicechat.ID) {
	t.Helper()
	provider := &requestProvider{id: agent.ProviderClaude}
	ledger := &recordingLedger{}
	options := []Option{}
	if router != nil {
		options = append(options, WithModelRouter(router))
	}
	service, store, meta := newUsagePromptService(t, provider, ledger, options...)
	if chatModelPolicy != "" {
		if _, err := store.Update(context.Background(), meta.ID, func(m *servicechat.Meta) {
			m.ModelPolicy = chatModelPolicy
			m.Mode = "chat"
		}); err != nil {
			t.Fatal(err)
		}
	}
	handle, err := service.Start(StartInput{
		ChatID:        meta.ID,
		Prompt:        "ping",
		Actor:         Actor{Email: "member@example.com"},
		ParentContext: context.Background(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-handle.Done
	return provider, ledger, store, meta.ID
}

func TestNilRouterLeavesTheRunExactlyAsItWas(t *testing.T) {
	provider, ledger, store, chatID := runOneTurn(t, "", nil)

	if len(provider.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(provider.requests))
	}
	if got := provider.requests[0].Model; got != "claude-sonnet-4-5" {
		t.Fatalf("Model = %q, want the chat's own", got)
	}
	if ledger.events[0].RoutedBy != "" || ledger.events[0].RoutedModel != "" {
		t.Fatalf("ledger carries routing without a router: %+v", ledger.events[0])
	}
	for _, event := range readEvents(t, store, chatID) {
		if event.Type == "user" && event.Routing != nil {
			t.Fatal("an unrouted turn must record no routing block")
		}
	}
}

func TestRoutedDecisionReachesTheRunRequestLedgerAndTranscript(t *testing.T) {
	router := &stubRouter{decision: servicerouting.Decision{
		Provider: "claude", Model: "haiku", ReasoningEffort: "low",
		Routed: true, RuleID: "chat-mode", RuleNote: "Chat mode is cheap",
		Reason: `rule "Chat mode is cheap" matched`,
	}}
	provider, ledger, store, chatID := runOneTurn(t, servicechat.ModelPolicyAuto, router)

	request := provider.requests[0]
	if request.Model != "haiku" {
		t.Fatalf("Model = %q, want the routed model", request.Model)
	}
	if request.Preferences.ReasoningEffort != "low" {
		t.Fatalf("ReasoningEffort = %q, want the routed effort", request.Preferences.ReasoningEffort)
	}

	event := ledger.events[0]
	if event.RoutedBy != "chat-mode" || event.RoutedModel != "claude/haiku" {
		t.Fatalf("ledger routing = %q/%q, want chat-mode/claude/haiku", event.RoutedBy, event.RoutedModel)
	}
	if event.Model != "haiku" {
		t.Fatalf("ledger model = %q, want the routed model", event.Model)
	}

	var routing *servicechat.EventRouting
	for _, stored := range readEvents(t, store, chatID) {
		if stored.Type == "user" {
			routing = stored.Routing
		}
	}
	if routing == nil {
		t.Fatal("the turn's user event must carry the routing block")
	}
	if routing.Rule != "Chat mode is cheap" || routing.Model != "haiku" {
		t.Fatalf("transcript routing = %+v", routing)
	}
	if routing.Reason == "" {
		t.Fatal("the transcript must record why the model was chosen")
	}
}

func TestRouterSeesThePinnedFlagAndTheTurnsShape(t *testing.T) {
	router := &stubRouter{decision: servicerouting.Decision{Provider: "claude", Model: "haiku", Routed: true}}
	runOneTurn(t, servicechat.ModelPolicyAuto, router)
	if len(router.seen) != 1 {
		t.Fatalf("router calls = %d, want 1", len(router.seen))
	}
	input := router.seen[0]
	if input.Pinned {
		t.Fatal("a chat set to auto must not be reported as pinned")
	}
	if input.Prompt != "ping" || input.Mode != "chat" || input.Provider != "claude" {
		t.Fatalf("router input = %+v", input)
	}
	if input.ProjectID != "aaaa1111" {
		t.Fatalf("ProjectID = %q, want the chat's project", input.ProjectID)
	}

	pinned := &stubRouter{decision: servicerouting.Decision{Provider: "claude", Model: "haiku", Routed: true}}
	runOneTurn(t, servicechat.ModelPolicyPinned, pinned)
	if !pinned.seen[0].Pinned {
		t.Fatal("a pinned chat must be reported as pinned so the policy can stand down")
	}
}

func TestARouterThatAnswersWithNothingNeverBlanksTheModel(t *testing.T) {
	router := &stubRouter{decision: servicerouting.Decision{}}
	provider, ledger, _, _ := runOneTurn(t, servicechat.ModelPolicyAuto, router)
	if provider.requests[0].Model != "claude-sonnet-4-5" {
		t.Fatalf("Model = %q, want the chat's own", provider.requests[0].Model)
	}
	if ledger.events[0].RoutedBy != "" {
		t.Fatalf("RoutedBy = %q, want nothing recorded", ledger.events[0].RoutedBy)
	}
}

func TestRoutingToAnotherProviderSelectsThatProvidersAdapter(t *testing.T) {
	// Registering only Codex proves the routed provider — not the chat's own
	// Claude — is what the run is looked up with.
	provider := &requestProvider{id: agent.ProviderCodex}
	ledger := &recordingLedger{}
	router := &stubRouter{decision: servicerouting.Decision{
		Provider: "codex", Model: "gpt-5.4-mini", Routed: true, RuleID: "cheap",
	}}
	service, store, meta := newUsagePromptService(
		t, provider, ledger, WithModelRouter(router),
	)
	if _, err := store.Update(context.Background(), meta.ID, func(m *servicechat.Meta) {
		m.ModelPolicy = servicechat.ModelPolicyAuto
	}); err != nil {
		t.Fatal(err)
	}
	handle, err := service.Start(StartInput{
		ChatID: meta.ID, Prompt: "ping", ParentContext: context.Background(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-handle.Done

	if len(provider.requests) != 1 {
		t.Fatalf("requests = %d, want the routed provider to have answered", len(provider.requests))
	}
	if provider.requests[0].Provider != agent.ProviderCodex {
		t.Fatalf("Provider = %q, want codex", provider.requests[0].Provider)
	}
	if ledger.events[0].RoutedModel != "codex/gpt-5.4-mini" {
		t.Fatalf("RoutedModel = %q", ledger.events[0].RoutedModel)
	}
}

func TestRoutedByFallsBackToTheDefaultSentinel(t *testing.T) {
	router := &stubRouter{decision: servicerouting.Decision{
		Provider: "claude", Model: "sonnet", Routed: true,
	}}
	_, ledger, _, _ := runOneTurn(t, servicechat.ModelPolicyAuto, router)
	if ledger.events[0].RoutedBy != servicerouting.RoutedByDefault {
		t.Fatalf("RoutedBy = %q, want %q", ledger.events[0].RoutedBy, servicerouting.RoutedByDefault)
	}
}

func readEvents(t *testing.T, store servicechat.Repository, id servicechat.ID) []servicechat.Event {
	t.Helper()
	events, err := store.ReadEvents(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return events
}
