package providerpool

// The shipped provider templates.
//
// Read the package comment first: these numbers are seed data, not truth.
// Every vendor below changes its free tier without notice and several of them
// document the caps per *model* rather than per account, so a single set of
// numbers on a provider row can only ever be an approximation. They are here
// so an operator starts from something rather than from an empty form, they
// are all marked with SeedLimitsNote, and every one of them is editable in
// the panel. No code path in this package treats a seeded limit as a fact:
// what the vendor's own rate-limit headers report always wins, and the
// counted numbers are labelled as counted.
//
// Seeds ship disabled and with no credential, so installing them changes
// nothing until an operator adds a key and switches one on.

// Seeds returns the shipped templates, in the priority order they get on a
// fresh install: the fastest and most generous free tiers first.
func Seeds() []Provider {
	return []Provider{
		{
			ID:      "groq",
			Label:   "Groq",
			Kind:    KindOpenAI,
			BaseURL: "https://api.groq.com/openai/v1",
			Models: []Model{
				{
					ID:            "llama-3.3-70b-versatile",
					Label:         "Llama 3.3 70B Versatile",
					ContextTokens: 131072,
					GoodFor:       []Capability{CapabilityText, CapabilityCode},
				},
				{
					ID:            "llama-3.1-8b-instant",
					Label:         "Llama 3.1 8B Instant",
					ContextTokens: 131072,
					GoodFor:       []Capability{CapabilityText, CapabilityBulk},
				},
			},
			Limits:     Limits{RPM: intp(30), RPD: intp(1000), TPM: intp(12000), TPD: intp(100000)},
			Priority:   10,
			LimitsNote: SeedLimitsNote,
			Notes:      "Free key from console.groq.com. The published caps are per model, so treat this row as the tightest of them.",
		},
		{
			ID:      "cerebras",
			Label:   "Cerebras",
			Kind:    KindOpenAI,
			BaseURL: "https://api.cerebras.ai/v1",
			Models: []Model{
				{
					ID:            "llama3.1-8b",
					Label:         "Llama 3.1 8B",
					ContextTokens: 32768,
					GoodFor:       []Capability{CapabilityText, CapabilityBulk},
				},
				{
					ID:            "llama-3.3-70b",
					Label:         "Llama 3.3 70B",
					ContextTokens: 65536,
					GoodFor:       []Capability{CapabilityText, CapabilityCode},
				},
			},
			Limits:     Limits{RPM: intp(30), RPD: intp(14400), TPM: intp(60000), TPD: intp(1000000)},
			Priority:   20,
			LimitsNote: SeedLimitsNote,
			Notes:      "Free key from cloud.cerebras.ai. Very fast; the daily request allowance is the largest of the seeds.",
		},
		{
			ID:      "gemini",
			Label:   "Google Gemini",
			Kind:    KindOpenAI,
			BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/",
			Models: []Model{
				{
					ID:            "gemini-2.5-flash",
					Label:         "Gemini 2.5 Flash",
					ContextTokens: 1048576,
					GoodFor:       []Capability{CapabilityText, CapabilityCode},
				},
				{
					ID:            "gemini-2.5-flash-lite",
					Label:         "Gemini 2.5 Flash-Lite",
					ContextTokens: 1048576,
					GoodFor:       []Capability{CapabilityText, CapabilityBulk},
				},
			},
			Limits:     Limits{RPM: intp(10), RPD: intp(250), TPM: intp(250000)},
			Priority:   30,
			LimitsNote: SeedLimitsNote,
			Notes:      "Free key from aistudio.google.com. This row uses Google's OpenAI-compatible surface; the free caps are published per model and differ between Flash and Flash-Lite.",
		},
		{
			ID:      "openrouter",
			Label:   "OpenRouter",
			Kind:    KindOpenAI,
			BaseURL: "https://openrouter.ai/api/v1",
			Models: []Model{
				{
					ID:            "meta-llama/llama-3.3-70b-instruct:free",
					Label:         "Llama 3.3 70B Instruct (free)",
					ContextTokens: 65536,
					GoodFor:       []Capability{CapabilityText, CapabilityCode},
				},
				{
					ID:            "google/gemma-3-27b-it:free",
					Label:         "Gemma 3 27B (free)",
					ContextTokens: 96000,
					GoodFor:       []Capability{CapabilityText, CapabilityBulk},
				},
			},
			Limits:     Limits{RPM: intp(20), RPD: intp(50)},
			Priority:   40,
			LimitsNote: SeedLimitsNote,
			Notes:      "Free key from openrouter.ai. Only model ids ending in :free are free, and the daily allowance is documented as rising once credits have been purchased.",
		},
		{
			ID:      "mistral",
			Label:   "Mistral",
			Kind:    KindOpenAI,
			BaseURL: "https://api.mistral.ai/v1",
			Models: []Model{
				{
					ID:            "mistral-small-latest",
					Label:         "Mistral Small",
					ContextTokens: 128000,
					GoodFor:       []Capability{CapabilityText, CapabilityCode},
				},
				{
					ID:            "open-mistral-nemo",
					Label:         "Mistral Nemo",
					ContextTokens: 128000,
					GoodFor:       []Capability{CapabilityText, CapabilityBulk},
				},
			},
			Limits:     Limits{RPM: intp(60), TPM: intp(500000), MonthlyTokens: intp(1000000000)},
			Priority:   50,
			LimitsNote: SeedLimitsNote,
			Notes:      "Free key from console.mistral.ai after activating the experiment plan. The free tier is documented as a per-second request rate plus a monthly token allowance.",
		},
		{
			ID:      "zhipu-glm",
			Label:   "Zhipu GLM",
			Kind:    KindOpenAI,
			BaseURL: "https://open.bigmodel.cn/api/paas/v4",
			Models: []Model{
				{
					ID:            "glm-4-flash",
					Label:         "GLM-4-Flash",
					ContextTokens: 128000,
					GoodFor:       []Capability{CapabilityText, CapabilityBulk},
				},
				{
					ID:            "glm-4.5-flash",
					Label:         "GLM-4.5-Flash",
					ContextTokens: 128000,
					GoodFor:       []Capability{CapabilityText, CapabilityCode},
				},
			},
			// Deliberately empty: Zhipu documents the free Flash models as
			// free but gates them on concurrency rather than on a published
			// request or token cap. Inventing numbers here would be worse
			// than showing "not documented" and letting the operator fill in
			// what their own account says.
			Limits:     Limits{},
			Priority:   60,
			LimitsNote: SeedLimitsNote,
			Notes:      "Free key from open.bigmodel.cn. The Flash models are free; the published throttle is a concurrency ceiling rather than a request or token cap, so no limits are seeded here.",
		},
		{
			ID:      "moonshot",
			Label:   "Moonshot (Kimi)",
			Kind:    KindOpenAI,
			BaseURL: "https://api.moonshot.cn/v1",
			Models: []Model{
				{
					ID:            "moonshot-v1-8k",
					Label:         "Moonshot v1 8k",
					ContextTokens: 8192,
					GoodFor:       []Capability{CapabilityText, CapabilityBulk},
				},
				{
					ID:            "moonshot-v1-32k",
					Label:         "Moonshot v1 32k",
					ContextTokens: 32768,
					GoodFor:       []Capability{CapabilityText, CapabilityCode},
				},
			},
			// Also deliberately empty: Moonshot's allowance is trial credit
			// plus a tier-dependent concurrency limit, not a free tier with
			// published request or token caps.
			Limits:     Limits{},
			Priority:   70,
			LimitsNote: SeedLimitsNote,
			Notes:      "Key from platform.moonshot.cn. What you get is trial credit rather than a standing free tier, and the throttle depends on your account tier — set your own limits here once you know them.",
		},
		{
			ID:      "github-models",
			Label:   "GitHub Models",
			Kind:    KindOpenAI,
			BaseURL: "https://models.inference.ai.azure.com",
			Models: []Model{
				{
					ID:            "gpt-4o-mini",
					Label:         "GPT-4o mini",
					ContextTokens: 128000,
					GoodFor:       []Capability{CapabilityText, CapabilityBulk},
				},
				{
					ID:            "gpt-4o",
					Label:         "GPT-4o",
					ContextTokens: 128000,
					GoodFor:       []Capability{CapabilityText, CapabilityCode},
				},
			},
			Limits:     Limits{RPM: intp(15), RPD: intp(150), TPM: intp(8000)},
			Priority:   80,
			LimitsNote: SeedLimitsNote,
			Notes:      "Uses a GitHub personal access token with the models scope. The published caps differ between the low and high model tiers; this row carries the low tier.",
		},
	}
}

// SeedInto installs the shipped templates once. It returns the registry it was
// given, untouched, if seeding has already happened — an operator who deleted
// every seed does not get them back on the next restart.
func SeedInto(registry Registry) (Registry, bool) {
	if registry.Seeded {
		return registry, false
	}
	existing := make(map[string]bool, len(registry.Providers))
	for _, provider := range registry.Providers {
		existing[provider.ID] = true
	}
	added := false
	for _, seed := range Seeds() {
		if existing[seed.ID] {
			continue
		}
		seed.Enabled = false
		seed.APIKey = ""
		seed.APIKeyRef = ""
		seed.Seed = true
		registry.Providers = append(registry.Providers, seed)
		added = true
	}
	registry.Seeded = true
	return registry.Normalize(), added || len(registry.Providers) > 0
}

// intp is the pointer literal the nullable limits need. Written out once here
// rather than at every call site.
func intp(value int) *int { return &value }
