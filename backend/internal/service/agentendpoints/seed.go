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
// Seed ships templates only for endpoints that were verified to work.
//
// There are no codex templates, and that is a finding rather than an
// oversight. Measured against codex-cli 0.145.0, the version this platform
// pins, on 2026-08-22:
//
//   - `wire_api = "chat"` is refused when the config loads — "no longer
//     supported ... set wire_api = \"responses\"". Google Gemini, Groq and
//     Cerebras publish Chat Completions only, so their configs never load.
//   - With `wire_api = "responses"`, codex reaches the provider but sends no
//     credential: OpenRouter answers "Missing Authentication header". Neither
//     `env_key` nor `env_http_headers` put the key on the wire, from a real
//     config.toml or from `-c` overrides. `codex doctor` reports "auth is
//     provided by the active model provider" and "requires OpenAI auth false",
//     so codex believes it is configured — it just does not authenticate.
//
// The good news in the same test: codex does **not** need its own ChatGPT
// login to drive a custom provider. Once the credential path works, an
// operator can use these endpoints without a Codex subscription.
//
// CLICodex stays supported so an operator with a working recipe can add a
// profile by hand. Shipping templates that cannot authenticate would only send
// someone chasing their own key.
func Seed() []Endpoint {
	return []Endpoint{
		{
			ID:      "zhipu-glm",
			Label:   "Zhipu GLM",
			CLI:     CLIClaude,
			BaseURL: "https://api.z.ai/api/anthropic",
			Models: []Model{
				{ID: "glm-4.7", Label: "GLM-4.7"},
				{ID: "glm-4.5-air", Label: "GLM-4.5 Air"},
			},
			Notes: "Zhipu's own Anthropic-compatible endpoint, published for Claude Code. " +
				"Uses ANTHROPIC_BASE_URL + ANTHROPIC_AUTH_TOKEN. " +
				"This is the international host; mainland-China accounts use " +
				"https://open.bigmodel.cn/api/anthropic and a key from one host is " +
				"rejected by the other.",
		},
		{
			ID:      "moonshot-kimi",
			Label:   "Moonshot Kimi",
			CLI:     CLIClaude,
			BaseURL: "https://api.moonshot.ai/anthropic",
			Models: []Model{
				{ID: "kimi-k3", Label: "Kimi K3"},
				{ID: "kimi-k2.7-code-highspeed", Label: "Kimi K2.7 Code"},
			},
			Notes: "Moonshot's own Anthropic-compatible endpoint, published for Claude Code. " +
				"Uses ANTHROPIC_BASE_URL + ANTHROPIC_AUTH_TOKEN. " +
				"Mainland China accounts use https://api.moonshot.cn/anthropic instead.",
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
