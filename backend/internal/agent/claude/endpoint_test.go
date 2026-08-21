package claude

import (
	"slices"
	"strings"
	"testing"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
)

func endpointRequest() agent.RunRequest {
	return agent.RunRequest{
		Model: "glm-4.6",
		Endpoint: &agent.Endpoint{
			ID:    "zhipu-glm",
			Label: "Zhipu GLM",
			CLI:   agent.ProviderClaude,
			Model: "glm-4.6",
			Env: map[string]string{
				"ANTHROPIC_BASE_URL":   "https://open.bigmodel.cn/api/anthropic",
				"ANTHROPIC_AUTH_TOKEN": "vendor-key-123456",
				"ANTHROPIC_API_KEY":    "",
			},
		},
	}
}

// The Anthropic-compatible mode is environment-only. A profile that added
// flags would be a sign the shape had drifted into something the vendor never
// documented.
func TestEndpointAddsNoClaudeFlags(t *testing.T) {
	provider := New(nil, provisioning.ContainerDependencies{})
	withEndpoint := provider.args(endpointRequest())
	plain := provider.args(agent.RunRequest{Model: "glm-4.6"})

	if !slices.Equal(withEndpoint, plain) {
		t.Fatalf("endpoint changed the claude command line\n got: %#v\nwant: %#v", withEndpoint, plain)
	}
	for _, arg := range withEndpoint {
		if strings.Contains(arg, "vendor-key-123456") {
			t.Fatalf("the endpoint key reached the command line: %q", arg)
		}
	}
}

// The host (loose-chat) path merges the endpoint over os.Environ(). Whatever
// first-party Anthropic credential that environment carries must be displaced
// rather than joined.
func TestHostEnvironmentDisplacesTheOperatorsAnthropicKey(t *testing.T) {
	base := []string{
		"HOME=/root",
		"IS_SANDBOX=1",
		"ANTHROPIC_API_KEY=sk-ant-oat01-OPERATOR-FIRST-PARTY-TOKEN",
		"ANTHROPIC_BASE_URL=https://api.anthropic.com",
	}
	merged := agent.WithEndpointEnvironment(base, endpointRequest().Endpoint)

	values := map[string]string{}
	for _, entry := range merged {
		name, value, _ := strings.Cut(entry, "=")
		values[name] = value
	}
	if values["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("the operator's Anthropic token survived: %q", values["ANTHROPIC_API_KEY"])
	}
	if values["ANTHROPIC_BASE_URL"] != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("base URL = %q, want the endpoint's", values["ANTHROPIC_BASE_URL"])
	}
	if values["ANTHROPIC_AUTH_TOKEN"] != "vendor-key-123456" {
		t.Errorf("auth token = %q, want the vendor key", values["ANTHROPIC_AUTH_TOKEN"])
	}
	if values["IS_SANDBOX"] != "1" {
		t.Errorf("an unrelated variable was disturbed: IS_SANDBOX=%q", values["IS_SANDBOX"])
	}
}
