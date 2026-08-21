package codex

import (
	"slices"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
)

// endpointRequest is a run pointed at a third-party OpenAI-compatible
// endpoint, rendered the way the service layer would render it.
func endpointRequest() agent.RunRequest {
	return agent.RunRequest{
		Model: "z-ai/glm-4.6",
		Endpoint: &agent.Endpoint{
			ID:    "openrouter",
			Label: "OpenRouter",
			CLI:   agent.ProviderCodex,
			Model: "z-ai/glm-4.6",
			Env: map[string]string{
				"REMOTE_ENDPOINT_API_KEY": "or-key-abcdef",
				"OPENAI_API_KEY":          "",
			},
			Args: []string{
				"-c", `model_provider="openrouter"`,
				"-c", `model_providers.openrouter.base_url="https://openrouter.ai/api/v1"`,
				"-c", `model_providers.openrouter.env_key="REMOTE_ENDPOINT_API_KEY"`,
				"-c", `model_providers.openrouter.wire_api="responses"`,
			},
		},
	}
}

// The provider table has to reach the CLI on this run's own command line.
// Writing it to /root/.codex/config.toml would leak the choice into every
// other chat in the same project, because that path is a shared bind mount.
func TestArgsCarryTheEndpointProviderTable(t *testing.T) {
	provider := New(nil, provisioning.ContainerDependencies{})
	args := provider.args(endpointRequest())

	for _, want := range endpointRequest().Endpoint.Args {
		if !slices.Contains(args, want) {
			t.Errorf("args %#v do not contain %q", args, want)
		}
	}
	if !slices.Contains(args, "--model") {
		t.Error("the endpoint's model must still be selected with --model")
	}
	// The prompt sentinel stays last, or codex reads the overrides as a
	// prompt instead of as configuration.
	if args[len(args)-1] != "-" {
		t.Errorf("last arg = %q, want the stdin sentinel", args[len(args)-1])
	}
}

// A run with no endpoint must produce exactly the command line it always did.
func TestArgsWithoutAnEndpointAreUnchanged(t *testing.T) {
	provider := New(nil, provisioning.ContainerDependencies{})
	args := provider.args(agent.RunRequest{Model: "gpt-5.5"})

	for _, arg := range args {
		if strings.Contains(arg, "model_provider") {
			t.Fatalf("a run with no endpoint emitted provider configuration: %q", arg)
		}
	}
}

// Per-run isolation, stated as a property: rendering the same request twice
// and rendering a plain request in between must not accumulate anything.
func TestEndpointArgsDoNotLeakBetweenRuns(t *testing.T) {
	provider := New(nil, provisioning.ContainerDependencies{})

	withEndpoint := provider.args(endpointRequest())
	plain := provider.args(agent.RunRequest{Model: "gpt-5.5"})
	withEndpointAgain := provider.args(endpointRequest())

	if !slices.Equal(withEndpoint, withEndpointAgain) {
		t.Fatalf("endpoint args are not stable\n first: %#v\nsecond: %#v", withEndpoint, withEndpointAgain)
	}
	if slices.Contains(plain, `model_provider="openrouter"`) {
		t.Fatal("a previous run's endpoint configuration leaked into a plain run")
	}
}

// The key must travel in the environment the provider table's env_key names,
// never as an argument: arguments are readable by every process in the
// container.
func TestEndpointKeyNeverReachesTheCommandLine(t *testing.T) {
	provider := New(nil, provisioning.ContainerDependencies{})
	args := provider.args(endpointRequest())

	for _, arg := range args {
		if strings.Contains(arg, "or-key-abcdef") {
			t.Fatalf("the endpoint key reached the command line: %q", arg)
		}
	}
}

// The host (loose-chat) path merges the endpoint over os.Environ(). The
// operator's own OpenAI key must not survive that merge.
func TestHostEnvironmentBlanksTheOperatorsOpenAIKey(t *testing.T) {
	base := codexEnv([]string{
		"HOME=/root",
		"OPENAI_API_KEY=sk-operator-first-party",
	})
	merged := agent.WithEndpointEnvironment(base, endpointRequest().Endpoint)

	for _, entry := range merged {
		name, value, _ := strings.Cut(entry, "=")
		if name == "OPENAI_API_KEY" && value != "" {
			t.Fatalf("the operator's OpenAI key survived: %q", entry)
		}
	}
	if !slices.Contains(merged, "REMOTE_ENDPOINT_API_KEY=or-key-abcdef") {
		t.Errorf("merged environment %#v is missing the endpoint key", merged)
	}
}
