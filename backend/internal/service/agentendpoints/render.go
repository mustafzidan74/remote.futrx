package agentendpoints

// Turning one stored profile into the environment and command-line
// configuration its CLI needs for a single run.
//
// Everything here is pure and deterministic: the same profile and the same
// resolved key always render the same environment and the same argument list,
// which is what makes the rendering testable without a container anywhere
// near it.
//
// # Why nothing is written to a config file
//
// A run's endpoint configuration must not outlive the run. `/root/.codex` is
// bind-mounted from the host and shared by every chat in a project, so a
// `[model_providers.*]` block written into config.toml would silently point
// the *next* chat at a third party too. The codex CLI accepts the same
// configuration as `-c key=value` overrides on its own command line — the
// mechanism this repository already uses for `model_reasoning_effort`,
// `service_tier`, and the Agent Browser's MCP server — so the whole provider
// table is passed per run and nothing is left behind. The claude CLI needs no
// file at all: its compatibility mode is two environment variables.

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Environment variable names.
//
// The claude trio is what Zhipu GLM's and Moonshot/Kimi's own "use with
// Claude Code" documentation instructs a user to export. ANTHROPIC_API_KEY is
// set *empty* rather than left alone: the container's own environment or a
// project secret could otherwise hold an Anthropic key, and a run pointed at
// a third party must not carry one.
const (
	EnvAnthropicBaseURL   = "ANTHROPIC_BASE_URL"
	EnvAnthropicAuthToken = "ANTHROPIC_AUTH_TOKEN"
	EnvAnthropicAPIKey    = "ANTHROPIC_API_KEY"
	// EnvAnthropicCustomHeaders carries extra request headers to the claude
	// CLI, one `Name: Value` pair per line.
	EnvAnthropicCustomHeaders = "ANTHROPIC_CUSTOM_HEADERS"

	// EnvCodexAPIKey is the variable a rendered codex provider names in its
	// `env_key`. One fixed name is used for every profile because the value
	// only has to survive from `lxc exec --env` to the CLI's own read of it,
	// and a fixed name is one less thing an operator can typo.
	EnvCodexAPIKey = "REMOTE_ENDPOINT_API_KEY"
	// EnvOpenAIAPIKey is blanked for the same reason ANTHROPIC_API_KEY is.
	EnvOpenAIAPIKey = "OPENAI_API_KEY"
)

// Runtime is one profile rendered for one run: the environment to publish and
// the extra CLI arguments to append. Both are complete — an adapter applies
// them verbatim and adds nothing of its own.
type Runtime struct {
	ID    string
	Label string
	CLI   CLI
	// Model is the model id this run should ask the endpoint for. It is the
	// chat's own choice when the profile offers it, and the profile's first
	// model otherwise.
	Model string
	// Env is published to the CLI process. It always contains the blanking
	// entries, so a stray host credential cannot reach a third party.
	Env map[string]string
	// Args are appended to the CLI's argument list. Empty for the claude CLI,
	// whose compatibility mode is environment-only.
	Args []string
}

// EnvNames lists the environment names this runtime publishes, sorted.
func (r Runtime) EnvNames() []string {
	names := make([]string, 0, len(r.Env))
	for name := range r.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Render turns a profile plus its resolved vault value into a Runtime.
//
// The key is the only secret involved and it enters through exactly this one
// argument. A caller with no value renders nothing: an endpoint whose key
// cannot be resolved must fail the run with a clear message rather than reach
// a vendor unauthenticated and fail there.
func Render(endpoint Endpoint, model, apiKey string) (Runtime, error) {
	if strings.TrimSpace(apiKey) == "" {
		return Runtime{}, ErrKeyUnresolved{Endpoint: endpoint.ID, Key: endpoint.APIKeyRef}
	}
	runtime := Runtime{
		ID:    endpoint.ID,
		Label: endpoint.Label,
		CLI:   endpoint.CLI,
		Model: RunModel(endpoint, model),
	}
	switch endpoint.CLI {
	case CLIClaude:
		runtime.Env = renderClaudeEnv(endpoint, apiKey)
	case CLICodex:
		runtime.Env = renderCodexEnv(apiKey)
		runtime.Args = RenderCodexArgs(endpoint)
	default:
		return Runtime{}, ErrInvalidCLI
	}
	return runtime, nil
}

// RunModel resolves which model id a run asks for: the chat's own choice when
// the profile offers it, otherwise the profile's first model. A chat carrying
// a model from some other provider must never be forwarded verbatim — the
// endpoint would reject a name it has never heard of, and the operator would
// see a vendor error instead of a configuration mistake.
func RunModel(endpoint Endpoint, model string) string {
	if endpoint.Offers(model) {
		return strings.TrimSpace(model)
	}
	return endpoint.DefaultModel()
}

// renderClaudeEnv is the Anthropic-compatible shape: a base URL, a bearer
// token, and a blanked API key.
func renderClaudeEnv(endpoint Endpoint, apiKey string) map[string]string {
	env := map[string]string{
		EnvAnthropicBaseURL:   endpoint.BaseURL,
		EnvAnthropicAuthToken: apiKey,
		// Blanked, never populated: see the note on the constants above.
		EnvAnthropicAPIKey: "",
	}
	if headers := renderHeaderLines(endpoint.Headers); headers != "" {
		env[EnvAnthropicCustomHeaders] = headers
	}
	return env
}

// renderCodexEnv publishes the key under the name the rendered provider's
// `env_key` points at, and blanks the operator's own OpenAI key.
func renderCodexEnv(apiKey string) map[string]string {
	return map[string]string{
		EnvCodexAPIKey:  apiKey,
		EnvOpenAIAPIKey: "",
	}
}

// renderHeaderLines renders `Name: Value` pairs, one per line, in name order.
func renderHeaderLines(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	lines := make([]string, 0, len(headers))
	for _, name := range sortedKeys(headers) {
		lines = append(lines, name+": "+headers[name])
	}
	return strings.Join(lines, "\n")
}

// RenderCodexArgs renders the `-c` overrides that stand in for a
// `[model_providers.<id>]` block in ~/.codex/config.toml, plus the
// `model_provider` selection that activates it.
//
// The key is deliberately absent: it travels in the environment the provider
// table's `env_key` names, which is the vendor-documented way to keep a
// credential out of the configuration itself.
func RenderCodexArgs(endpoint Endpoint) []string {
	id := endpoint.ID
	table := "model_providers." + id
	args := []string{
		"-c", "model_provider=" + tomlString(id),
		"-c", table + ".name=" + tomlString(codexProviderName(endpoint)),
		"-c", table + ".base_url=" + tomlString(endpoint.BaseURL),
		"-c", table + ".env_key=" + tomlString(EnvCodexAPIKey),
		"-c", table + ".wire_api=" + tomlString(NormalizeWireAPI(endpoint.WireAPI)),
	}
	for _, name := range sortedKeys(endpoint.Headers) {
		args = append(
			args,
			"-c", table+".http_headers."+name+"="+tomlString(endpoint.Headers[name]),
		)
	}
	return args
}

// codexProviderName is what the CLI displays for the provider. The label is
// used when it is set, falling back to the id so the table always has a name.
func codexProviderName(endpoint Endpoint) string {
	if label := strings.TrimSpace(endpoint.Label); label != "" {
		return label
	}
	return endpoint.ID
}

// NormalizeWireAPI collapses anything unrecognized onto the Responses API.
// Recent codex builds dropped the older Chat Completions wire, so defaulting
// the other way would quietly produce a provider the CLI cannot drive.
func NormalizeWireAPI(wire string) string {
	if strings.EqualFold(strings.TrimSpace(wire), WireChat) {
		return WireChat
	}
	return WireResponses
}

// MaskValue replaces every occurrence of one resolved value in text. It is
// the last gate before CLI output reaches an API response or an audit entry:
// an endpoint that echoes its own configuration must not leak the key through
// the Test action.
//
// Values shorter than four characters are left alone; masking them would
// replace ordinary substrings all over the output without protecting anything
// worth protecting.
func MaskValue(text, value string) string {
	if text == "" || len(value) < 4 {
		return text
	}
	return strings.ReplaceAll(text, value, "••••••••")
}

func sortedKeys(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// tomlString renders a TOML basic string, so a value carrying a quote, a
// backslash, or a control character is data rather than syntax when the codex
// CLI parses the right-hand side of a `-c key=value` override.
func tomlString(value string) string {
	var out strings.Builder
	out.Grow(len(value) + 2)
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&out, `\u%04X`, r)
				continue
			}
			if r == utf8.RuneError {
				out.WriteString(`�`)
				continue
			}
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
	return out.String()
}
