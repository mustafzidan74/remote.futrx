package agentendpoints

// The templates a fresh install starts with.
//
// Every one of them ships **disabled** and with no `apiKeyRef`. Seeding is a
// convenience — the base URL and the model ids are the fiddly part — not a
// decision to send anybody's work to a third party. Enabling one is an
// explicit administrator action that also has to name a Secrets-vault key,
// and the profile's own Test button exists so the operator can confirm the
// values before a client's code ever depends on them.
//
// # Where these values came from
//
// Every entry here is a vendor's OWN published compatibility endpoint,
// documented by that vendor for use with the CLI named in `CLI`. None of them
// is a first-party endpoint being impersonated, and none needs a spoofed user
// agent, a cookie, or a replayed session. A vendor without such a published
// path has no template here and cannot be given one.
//
// The `Notes` on each entry name its source. Vendors move URLs and rename
// models, so the values are a starting point an operator confirms with the
// Test action, not a guarantee.

// Seed returns the template profiles, in the order the admin table shows
// them. The slice is freshly built on every call, so a caller may modify it.
func Seed() []Endpoint {
	return []Endpoint{
		{
			ID:      "zhipu-glm",
			Label:   "Zhipu GLM",
			CLI:     CLIClaude,
			BaseURL: "https://open.bigmodel.cn/api/anthropic",
			Models: []Model{
				{ID: "glm-4.6", Label: "GLM-4.6"},
				{ID: "glm-4.5-air", Label: "GLM-4.5 Air"},
			},
			Notes: "Zhipu's own Anthropic-compatible endpoint, published for Claude Code. " +
				"Uses ANTHROPIC_BASE_URL + ANTHROPIC_AUTH_TOKEN. " +
				"The international Z.ai platform serves the same API at https://api.z.ai/api/anthropic.",
		},
		{
			ID:      "moonshot-kimi",
			Label:   "Moonshot Kimi",
			CLI:     CLIClaude,
			BaseURL: "https://api.moonshot.ai/anthropic",
			Models: []Model{
				{ID: "kimi-k2-turbo-preview", Label: "Kimi K2 Turbo"},
				{ID: "kimi-k2-0905-preview", Label: "Kimi K2"},
			},
			Notes: "Moonshot's own Anthropic-compatible endpoint, published for Claude Code. " +
				"Uses ANTHROPIC_BASE_URL + ANTHROPIC_AUTH_TOKEN. " +
				"Mainland China accounts use https://api.moonshot.cn/anthropic instead.",
		},
		{
			ID:      "openrouter",
			Label:   "OpenRouter",
			CLI:     CLICodex,
			BaseURL: "https://openrouter.ai/api/v1",
			WireAPI: WireResponses,
			Models: []Model{
				{ID: "z-ai/glm-4.6", Label: "GLM-4.6"},
				{ID: "moonshotai/kimi-k2", Label: "Kimi K2"},
				{ID: "deepseek/deepseek-chat", Label: "DeepSeek Chat"},
			},
			Notes: "OpenRouter's OpenAI-compatible gateway, configured the way the Codex CLI " +
				"documents a custom provider: base_url + env_key + wire_api. " +
				"Model ids carry the vendor prefix OpenRouter's model page shows.",
		},
		{
			ID:      "google-gemini",
			Label:   "Google Gemini",
			CLI:     CLICodex,
			BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/",
			WireAPI: WireChat,
			Models: []Model{
				{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro"},
				{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash"},
			},
			Notes: "Google's own OpenAI-compatibility layer for the Gemini API. " +
				"It speaks Chat Completions, not the Responses API, so a codex build that " +
				"requires wire_api=responses will need a translating proxy — run Test first.",
		},
		{
			ID:      "groq",
			Label:   "Groq",
			CLI:     CLICodex,
			BaseURL: "https://api.groq.com/openai/v1",
			WireAPI: WireChat,
			Models: []Model{
				{ID: "moonshotai/kimi-k2-instruct", Label: "Kimi K2 Instruct"},
				{ID: "llama-3.3-70b-versatile", Label: "Llama 3.3 70B"},
			},
			Notes: "Groq's own OpenAI-compatible endpoint. Chat Completions only — see the " +
				"Gemini note about wire_api.",
		},
		{
			ID:      "cerebras",
			Label:   "Cerebras",
			CLI:     CLICodex,
			BaseURL: "https://api.cerebras.ai/v1",
			WireAPI: WireChat,
			Models: []Model{
				{ID: "qwen-3-coder-480b", Label: "Qwen3 Coder 480B"},
				{ID: "llama-3.3-70b", Label: "Llama 3.3 70B"},
			},
			Notes: "Cerebras Inference's own OpenAI-compatible endpoint. Chat Completions only — " +
				"see the Gemini note about wire_api.",
		},
	}
}

// SeedIDs is the set of template ids, for a store deciding whether a stored
// document already carries the templates.
func SeedIDs() []string {
	seeds := Seed()
	ids := make([]string, 0, len(seeds))
	for _, endpoint := range seeds {
		ids = append(ids, endpoint.ID)
	}
	return ids
}
