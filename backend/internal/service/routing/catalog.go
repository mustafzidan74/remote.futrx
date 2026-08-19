package routing

import "strings"

// The model catalog. It mirrors the composer's picker
// (frontend/src/config/chatCatalog.ts) because routing may only ever name a
// model a human could have picked by hand: a rule pointing at a model the
// deployment does not offer falls back rather than handing a CLI an id it
// will reject mid-run.
//
// "" is every provider's Auto entry — the CLI's own default — and is always a
// valid destination.
var catalog = []ProviderModels{
	{
		Provider: "claude",
		Label:    "Claude",
		Models: []CatalogItem{
			{Value: "", Label: "Auto"},
			{Value: "fable", Label: "Fable"},
			{Value: "opus", Label: "Opus"},
			{Value: "sonnet", Label: "Sonnet"},
			{Value: "haiku", Label: "Haiku"},
		},
	},
	{
		Provider: "codex",
		Label:    "Codex",
		Models: []CatalogItem{
			{Value: "", Label: "Auto"},
			{Value: "gpt-5.6-sol", Label: "GPT-5.6 Sol"},
			{Value: "gpt-5.5", Label: "GPT-5.5"},
			{Value: "gpt-5.4", Label: "GPT-5.4"},
			{Value: "gpt-5.4-mini", Label: "GPT-5.4 Mini"},
			{Value: "gpt-5.3-codex", Label: "GPT-5.3 Codex"},
		},
	},
	{
		Provider: "kimi",
		Label:    "Kimi",
		Models:   []CatalogItem{{Value: "", Label: "Auto"}},
	},
	{
		Provider: "antigravity",
		Label:    "Antigravity",
		Models:   []CatalogItem{{Value: "", Label: "Auto"}},
	},
}

// Catalog returns the per-provider model lists, in picker order.
func Catalog() []ProviderModels {
	out := make([]ProviderModels, 0, len(catalog))
	for _, entry := range catalog {
		out = append(out, ProviderModels{
			Provider: entry.Provider,
			Label:    entry.Label,
			Models:   append([]CatalogItem(nil), entry.Models...),
		})
	}
	return out
}

// KnownModel reports whether the catalog offers this provider/model pair.
func KnownModel(provider, model string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	for _, entry := range catalog {
		if entry.Provider != provider {
			continue
		}
		for _, item := range entry.Models {
			if item.Value == model {
				return true
			}
		}
		return false
	}
	return false
}

// ProviderLabel renders a provider id the way the UI names it.
func ProviderLabel(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, entry := range catalog {
		if entry.Provider == provider {
			return entry.Label
		}
	}
	if provider == "" {
		return "Unknown"
	}
	return provider
}

// ModelLabel renders a model id the way the picker names it, falling back to
// the raw id for a model the catalog does not carry.
func ModelLabel(provider, model string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	if model == "" {
		return "Auto"
	}
	for _, entry := range catalog {
		if entry.Provider != provider {
			continue
		}
		for _, item := range entry.Models {
			if item.Value == model {
				return item.Label
			}
		}
	}
	return model
}
