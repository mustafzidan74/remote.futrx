package prompt

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicerouting "github.com/futrx-com/remote.futrx.com/internal/service/routing"
	"github.com/futrx-com/remote.futrx.com/internal/service/runhub"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filechat"
)

// stubEndpoints answers with one rendered endpoint, or one error, and records
// what it was asked for.
type stubEndpoints struct {
	endpoint  agent.Endpoint
	err       error
	seenID    []string
	seenModel []string
}

func (e *stubEndpoints) RuntimeFor(
	_ context.Context,
	endpointID, model string,
) (agent.Endpoint, error) {
	e.seenID = append(e.seenID, endpointID)
	e.seenModel = append(e.seenModel, model)
	if e.err != nil {
		return agent.Endpoint{}, e.err
	}
	return e.endpoint, nil
}

func glmEndpoint() agent.Endpoint {
	return agent.Endpoint{
		ID:    "zhipu-glm",
		Label: "Zhipu GLM",
		CLI:   agent.ProviderClaude,
		Model: "glm-4.6",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":   "https://open.bigmodel.cn/api/anthropic",
			"ANTHROPIC_AUTH_TOKEN": "vendor-key-123456",
			"ANTHROPIC_API_KEY":    "",
		},
	}
}

// newTwoProviderPromptService registers both CLIs, so a test can assert
// *which* one a decision routed the turn to rather than only what it asked
// for.
func newTwoProviderPromptService(
	t *testing.T,
	claude, codex agent.Provider,
	options ...Option,
) (*Service, servicechat.Repository, servicechat.Meta) {
	t.Helper()
	store, err := filechat.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.Create(context.Background(), servicechat.Meta{
		ID:        "abcdef123456",
		Title:     "existing",
		Provider:  servicechat.ProviderClaude,
		Model:     "claude-sonnet-4-5",
		ProjectID: "aaaa1111",
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := agent.NewRegistry()
	for _, provider := range []agent.Provider{claude, codex} {
		if err := registry.Register(provider); err != nil {
			t.Fatal(err)
		}
	}
	return New(store, nil, nil, runhub.New(store), registry, options...), store, meta
}

// runEndpointTurn drives one turn on a chat configured by mutate.
func runEndpointTurn(
	t *testing.T,
	mutate func(*servicechat.Meta),
	options ...Option,
) (*requestProvider, servicechat.Repository, servicechat.ID, error) {
	t.Helper()
	provider := &requestProvider{id: agent.ProviderClaude}
	service, store, meta := newUsagePromptService(t, provider, &recordingLedger{}, options...)
	if mutate != nil {
		if _, err := store.Update(context.Background(), meta.ID, mutate); err != nil {
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
	result := <-handle.Done
	return provider, store, meta.ID, result.Err
}

// The default is unchanged behaviour: a chat naming no endpoint launches its
// CLI exactly as it did before the register existed.
func TestChatWithoutAnEndpointRunsUnchanged(t *testing.T) {
	endpoints := &stubEndpoints{endpoint: glmEndpoint()}
	provider, _, _, err := runEndpointTurn(t, nil, WithAgentEndpoints(endpoints))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if provider.requests[0].Endpoint != nil {
		t.Fatal("a chat naming no endpoint must carry none into the run")
	}
	if len(endpoints.seenID) != 0 {
		t.Fatalf("the register was consulted for a chat with no endpoint: %v", endpoints.seenID)
	}
}

func TestChatEndpointReachesTheRunRequest(t *testing.T) {
	endpoints := &stubEndpoints{endpoint: glmEndpoint()}
	provider, _, _, err := runEndpointTurn(t, func(m *servicechat.Meta) {
		m.EndpointID = "zhipu-glm"
		m.Model = "glm-4.6"
	}, WithAgentEndpoints(endpoints))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	request := provider.requests[0]
	if request.Endpoint == nil {
		t.Fatal("the run request carries no endpoint")
	}
	if request.Endpoint.ID != "zhipu-glm" {
		t.Errorf("endpoint id = %q, want zhipu-glm", request.Endpoint.ID)
	}
	// The register decides which model this turn asks for; the chat's stored
	// model is only a request.
	if request.Model != "glm-4.6" {
		t.Errorf("Model = %q, want the endpoint's resolved model", request.Model)
	}
	if endpoints.seenID[0] != "zhipu-glm" {
		t.Errorf("register asked for %q, want zhipu-glm", endpoints.seenID[0])
	}
}

// Selection precedence, stated as a table. An endpoint pins the chat: it wins
// over routing, and it decides which CLI answers.
func TestSelectionPrecedence(t *testing.T) {
	routed := servicerouting.Decision{
		Provider: "codex", Model: "gpt-5.5", Routed: true,
		RuleID: "chat-mode", RuleNote: "Chat mode is cheap",
	}

	cases := []struct {
		name         string
		endpointID   string
		modelPolicy  string
		withRouter   bool
		wantProvider agent.ProviderID
		wantModel    string
		wantEndpoint bool
		wantRouted   bool
	}{
		{
			name:         "no endpoint and no router: the chat's own choice",
			modelPolicy:  servicechat.ModelPolicyPinned,
			wantProvider: agent.ProviderClaude,
			wantModel:    "claude-sonnet-4-5",
		},
		{
			name:         "no endpoint, auto policy: the router decides",
			modelPolicy:  servicechat.ModelPolicyAuto,
			withRouter:   true,
			wantProvider: agent.ProviderCodex,
			wantModel:    "gpt-5.5",
			wantRouted:   true,
		},
		{
			name:         "an endpoint beats the router even on auto",
			endpointID:   "zhipu-glm",
			modelPolicy:  servicechat.ModelPolicyAuto,
			withRouter:   true,
			wantProvider: agent.ProviderClaude,
			wantModel:    "glm-4.6",
			wantEndpoint: true,
		},
		{
			name:         "an endpoint on a pinned chat also wins",
			endpointID:   "zhipu-glm",
			modelPolicy:  servicechat.ModelPolicyPinned,
			withRouter:   true,
			wantProvider: agent.ProviderClaude,
			wantModel:    "glm-4.6",
			wantEndpoint: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			router := &stubRouter{decision: routed}
			options := []Option{WithAgentEndpoints(&stubEndpoints{endpoint: glmEndpoint()})}
			if testCase.withRouter {
				options = append(options, WithModelRouter(router))
			}
			provider := &requestProvider{id: agent.ProviderClaude}
			codexProvider := &requestProvider{id: agent.ProviderCodex}
			service, store, meta := newTwoProviderPromptService(t, provider, codexProvider, options...)
			if _, err := store.Update(context.Background(), meta.ID, func(m *servicechat.Meta) {
				m.EndpointID = testCase.endpointID
				m.ModelPolicy = testCase.modelPolicy
			}); err != nil {
				t.Fatal(err)
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

			ran := provider
			if testCase.wantProvider == agent.ProviderCodex {
				ran = codexProvider
			}
			if len(ran.requests) != 1 {
				t.Fatalf("provider %s handled %d requests, want 1", testCase.wantProvider, len(ran.requests))
			}
			request := ran.requests[0]
			if request.Model != testCase.wantModel {
				t.Errorf("Model = %q, want %q", request.Model, testCase.wantModel)
			}
			if (request.Endpoint != nil) != testCase.wantEndpoint {
				t.Errorf("Endpoint present = %v, want %v", request.Endpoint != nil, testCase.wantEndpoint)
			}
			// An endpoint-pinned chat must not have consulted the router at
			// all: there is nothing left for it to choose.
			if testCase.wantEndpoint && len(router.seen) != 0 {
				t.Errorf("the router was consulted for an endpoint-pinned chat")
			}
			if testCase.wantRouted && len(router.seen) == 0 {
				t.Error("the router was not consulted")
			}
		})
	}
}

// The endpoint's CLI is authoritative. A chat whose stored provider drifted
// away from its endpoint must still run the endpoint's CLI, or it would ask a
// codex binary for a model only the Anthropic-compatible endpoint offers.
func TestEndpointCLIWinsOverAStaleChatProvider(t *testing.T) {
	provider := &requestProvider{id: agent.ProviderClaude}
	codexProvider := &requestProvider{id: agent.ProviderCodex}
	service, store, meta := newTwoProviderPromptService(
		t, provider, codexProvider,
		WithAgentEndpoints(&stubEndpoints{endpoint: glmEndpoint()}),
	)
	if _, err := store.Update(context.Background(), meta.ID, func(m *servicechat.Meta) {
		m.EndpointID = "zhipu-glm"
		m.Provider = servicechat.ProviderCodex
	}); err != nil {
		t.Fatal(err)
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

	if len(codexProvider.requests) != 0 {
		t.Fatal("the stale chat provider ran instead of the endpoint's CLI")
	}
	if len(provider.requests) != 1 {
		t.Fatalf("the endpoint's CLI handled %d requests, want 1", len(provider.requests))
	}
}

// Every configuration failure has to stop the turn with a readable message
// rather than launching a CLI that will come back with a vendor's 401.
func TestEndpointFailuresStopTheTurnBeforeTheCLI(t *testing.T) {
	cases := []struct {
		name      string
		endpoints AgentEndpoints
		wantIn    string
	}{
		{
			name:      "the register refuses",
			endpoints: &stubEndpoints{err: errors.New("this agent endpoint is disabled")},
			wantIn:    "disabled",
		},
		{
			name:      "the deployment has no register at all",
			endpoints: nil,
			wantIn:    "does not have configured",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			options := []Option{}
			if testCase.endpoints != nil {
				options = append(options, WithAgentEndpoints(testCase.endpoints))
			}
			provider, store, chatID, err := runEndpointTurn(t, func(m *servicechat.Meta) {
				m.EndpointID = "zhipu-glm"
			}, options...)

			if err == nil {
				t.Fatal("run: want an error")
			}
			if len(provider.requests) != 0 {
				t.Fatal("the CLI was launched despite an unresolvable endpoint")
			}

			var message string
			for _, event := range readEvents(t, store, chatID) {
				if event.Type == "error" {
					message = event.Message
				}
			}
			if !strings.Contains(message, testCase.wantIn) {
				t.Errorf("transcript error = %q, want it to mention %q", message, testCase.wantIn)
			}
			// The transcript must never carry a credential, and there is none
			// to carry here — but the message is the one place a careless
			// error could put one.
			if strings.Contains(message, "vendor-key") {
				t.Fatalf("a credential reached the transcript: %q", message)
			}
		})
	}
}
