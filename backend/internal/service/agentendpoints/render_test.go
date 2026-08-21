package agentendpoints

import (
	"strings"
	"testing"
)

// The operator's own first-party token. Every test in this file that touches
// leakage uses this exact string, so a grep for it in a rendered artifact is
// an unambiguous failure.
const operatorAnthropicToken = "sk-ant-oat01-OPERATOR-FIRST-PARTY-TOKEN"

func claudeProfile() Endpoint {
	return Endpoint{
		ID:        "zhipu-glm",
		Label:     "Zhipu GLM",
		CLI:       CLIClaude,
		BaseURL:   "https://open.bigmodel.cn/api/anthropic",
		APIKeyRef: "ZHIPU_API_KEY",
		Models:    []Model{{ID: "glm-4.6", Label: "GLM-4.6"}, {ID: "glm-4.5-air"}},
		Enabled:   true,
	}
}

func codexProfile() Endpoint {
	return Endpoint{
		ID:        "openrouter",
		Label:     "OpenRouter",
		CLI:       CLICodex,
		BaseURL:   "https://openrouter.ai/api/v1",
		APIKeyRef: "OPENROUTER_API_KEY",
		WireAPI:   WireResponses,
		Models:    []Model{{ID: "z-ai/glm-4.6"}},
		Enabled:   true,
	}
}

func TestRenderClaudeEnvironment(t *testing.T) {
	t.Parallel()

	runtime, err := Render(claudeProfile(), "glm-4.6", "vendor-key-123456")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if runtime.CLI != CLIClaude {
		t.Fatalf("cli = %q, want claude", runtime.CLI)
	}
	if len(runtime.Args) != 0 {
		t.Errorf("claude endpoints must contribute no CLI args, got %v", runtime.Args)
	}

	cases := []struct {
		name string
		key  string
		want string
	}{
		{"base url is the vendor's own", EnvAnthropicBaseURL, "https://open.bigmodel.cn/api/anthropic"},
		{"auth token carries the operator's vendor key", EnvAnthropicAuthToken, "vendor-key-123456"},
		{"api key is blanked, never populated", EnvAnthropicAPIKey, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, present := runtime.Env[testCase.key]
			if !present {
				t.Fatalf("%s is absent from the rendered environment", testCase.key)
			}
			if got != testCase.want {
				t.Errorf("%s = %q, want %q", testCase.key, got, testCase.want)
			}
		})
	}
}

// A profile with no headers must not publish an empty custom-headers
// variable: the CLI would parse it and the operator would have no way to tell
// an empty setting from an absent one.
func TestRenderClaudeOmitsEmptyCustomHeaders(t *testing.T) {
	t.Parallel()

	runtime, err := Render(claudeProfile(), "glm-4.6", "vendor-key-123456")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, present := runtime.Env[EnvAnthropicCustomHeaders]; present {
		t.Errorf("%s must be absent when the profile declares no headers", EnvAnthropicCustomHeaders)
	}
}

func TestRenderClaudeCustomHeadersAreDeterministic(t *testing.T) {
	t.Parallel()

	profile := claudeProfile()
	profile.Headers = map[string]string{"X-Title": "remote.futrx", "HTTP-Referer": "https://example.test"}

	first, err := Render(profile, "glm-4.6", "vendor-key-123456")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	second, err := Render(profile, "glm-4.6", "vendor-key-123456")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if first.Env[EnvAnthropicCustomHeaders] != second.Env[EnvAnthropicCustomHeaders] {
		t.Fatalf("header rendering is not deterministic")
	}
	// Name order, not map order.
	want := "HTTP-Referer: https://example.test\nX-Title: remote.futrx"
	if got := first.Env[EnvAnthropicCustomHeaders]; got != want {
		t.Errorf("custom headers = %q, want %q", got, want)
	}
}

func TestRenderCodexProviderTable(t *testing.T) {
	t.Parallel()

	runtime, err := Render(codexProfile(), "z-ai/glm-4.6", "or-key-abcdef")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	joined := strings.Join(runtime.Args, " ")

	cases := []struct {
		name string
		want string
	}{
		{"selects the provider", `model_provider="openrouter"`},
		{"names it", `model_providers.openrouter.name="OpenRouter"`},
		{"points at the vendor's own base url", `model_providers.openrouter.base_url="https://openrouter.ai/api/v1"`},
		{"names the env var holding the key", `model_providers.openrouter.env_key="` + EnvCodexAPIKey + `"`},
		{"declares the wire protocol", `model_providers.openrouter.wire_api="responses"`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if !strings.Contains(joined, testCase.want) {
				t.Errorf("rendered args %v do not contain %q", runtime.Args, testCase.want)
			}
		})
	}

	// Every override must arrive as its own `-c` flag with a single value.
	for index := 0; index < len(runtime.Args); index += 2 {
		if runtime.Args[index] != "-c" {
			t.Fatalf("arg %d = %q, want -c", index, runtime.Args[index])
		}
	}
	if len(runtime.Args)%2 != 0 {
		t.Fatalf("odd number of codex args: %v", runtime.Args)
	}
}

// The key is the one thing that must never appear on a command line: a
// command line is visible to every process in the container.
func TestRenderCodexKeepsKeyOutOfArguments(t *testing.T) {
	t.Parallel()

	const key = "or-key-super-secret-value"
	runtime, err := Render(codexProfile(), "z-ai/glm-4.6", key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, arg := range runtime.Args {
		if strings.Contains(arg, key) {
			t.Fatalf("resolved key leaked onto the codex command line: %q", arg)
		}
	}
	if runtime.Env[EnvCodexAPIKey] != key {
		t.Errorf("%s = %q, want the resolved key", EnvCodexAPIKey, runtime.Env[EnvCodexAPIKey])
	}
	if got, present := runtime.Env[EnvOpenAIAPIKey]; !present || got != "" {
		t.Errorf("%s = %q (present=%v), want blanked", EnvOpenAIAPIKey, got, present)
	}
}

// The headline guarantee: whatever the operator's own first-party credentials
// are, rendering a third-party endpoint cannot carry them anywhere.
func TestRenderNeverCarriesTheOperatorsFirstPartyToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		profile Endpoint
		model   string
	}{
		{"claude cli", claudeProfile(), "glm-4.6"},
		{"codex cli", codexProfile(), "z-ai/glm-4.6"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runtime, err := Render(testCase.profile, testCase.model, "the-vendors-own-key")
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for name, value := range runtime.Env {
				if strings.Contains(value, operatorAnthropicToken) {
					t.Fatalf("operator token leaked into %s", name)
				}
			}
			for _, arg := range runtime.Args {
				if strings.Contains(arg, operatorAnthropicToken) {
					t.Fatalf("operator token leaked into CLI arg %q", arg)
				}
			}
			// Stronger: the rendered environment for a third-party endpoint
			// must publish an *empty* value for whichever first-party key
			// variable its CLI reads, so a stray one in the container or in a
			// project secret cannot survive.
			blanked := EnvAnthropicAPIKey
			if testCase.profile.CLI == CLICodex {
				blanked = EnvOpenAIAPIKey
			}
			if got, present := runtime.Env[blanked]; !present || got != "" {
				t.Errorf("%s = %q (present=%v), want an explicit empty value", blanked, got, present)
			}
		})
	}
}

// A run must not be launched half-configured: without a resolved key the CLI
// would reach the vendor unauthenticated and the operator would read a 401
// instead of a sentence about their vault.
func TestRenderRefusesWithoutAResolvedKey(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", "   "} {
		if _, err := Render(claudeProfile(), "glm-4.6", key); err == nil {
			t.Fatalf("Render with key %q: want an error", key)
		}
	}
}

func TestRunModel(t *testing.T) {
	t.Parallel()

	profile := claudeProfile()
	cases := []struct {
		name    string
		profile Endpoint
		asked   string
		want    string
	}{
		{"an offered model is honoured", profile, "glm-4.5-air", "glm-4.5-air"},
		{"a foreign model falls back to the profile's first", profile, "claude-sonnet-4-5", "glm-4.6"},
		{"an empty ask falls back too", profile, "", "glm-4.6"},
		{"a profile listing nothing asks for nothing", Endpoint{CLI: CLIClaude}, "glm-4.6", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := RunModel(testCase.profile, testCase.asked); got != testCase.want {
				t.Errorf("RunModel(%q) = %q, want %q", testCase.asked, got, testCase.want)
			}
		})
	}
}

// A vendor label carrying a quote must not be able to break out of the TOML
// value and become configuration of its own.
func TestRenderCodexQuotesHostileValues(t *testing.T) {
	t.Parallel()

	profile := codexProfile()
	profile.Label = `Ev"il" \ provider`
	profile.Headers = map[string]string{"X-Title": "line\nbreak"}

	runtime, err := Render(profile, "z-ai/glm-4.6", "or-key-abcdef")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	joined := strings.Join(runtime.Args, " ")
	if strings.Contains(joined, `Ev"il"`) {
		t.Errorf("an embedded quote survived unescaped: %v", runtime.Args)
	}
	if !strings.Contains(joined, `Ev\"il\"`) {
		t.Errorf("expected escaped quotes in %v", runtime.Args)
	}
	if strings.Contains(joined, "line\nbreak") {
		t.Errorf("a raw newline survived into a TOML value: %v", runtime.Args)
	}
}

func TestNormalizeWireAPI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"responses", WireResponses},
		{"chat", WireChat},
		{"CHAT", WireChat},
		{"  chat  ", WireChat},
		{"", WireResponses},
		{"nonsense", WireResponses},
	}
	for _, testCase := range cases {
		if got := NormalizeWireAPI(testCase.in); got != testCase.want {
			t.Errorf("NormalizeWireAPI(%q) = %q, want %q", testCase.in, got, testCase.want)
		}
	}
}

func TestMaskValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		text  string
		value string
		want  string
	}{
		{
			name:  "an echoed key is masked",
			text:  "config: key=or-key-abcdef done",
			value: "or-key-abcdef",
			want:  "config: key=•••••••• done",
		},
		{
			name:  "a very short value is left alone",
			text:  "the cat sat",
			value: "cat",
			want:  "the cat sat",
		},
		{
			name:  "empty output stays empty",
			text:  "",
			value: "or-key-abcdef",
			want:  "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := MaskValue(testCase.text, testCase.value); got != testCase.want {
				t.Errorf("MaskValue = %q, want %q", got, testCase.want)
			}
		})
	}
}
