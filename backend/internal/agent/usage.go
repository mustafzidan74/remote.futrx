package agent

import "encoding/json"

// Usage is the provider-neutral token/cost payload carried by
// EventRunCompleted (and EventRunFailed when the CLI still reported a
// partial turn). Provider adapters translate their native shape into this
// one so downstream consumers — the chat UI, the usage ledger, and the
// offline rebuild that re-reads persisted chat events — never have to know
// which CLI produced a run.
//
// The JSON field names intentionally match Claude Code's stream-json
// vocabulary because that shape is already persisted in existing chat event
// logs and already understood by the frontend projector.
type Usage struct {
	InputTokens      int64 `json:"input_tokens,omitempty"`
	OutputTokens     int64 `json:"output_tokens,omitempty"`
	CacheReadTokens  int64 `json:"cache_read_input_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	ReasoningTokens  int64 `json:"reasoning_output_tokens,omitempty"`
	// CostUSD is set only when the provider itself reported a price for the
	// turn. A nil value means "unknown" — never "free".
	CostUSD    *float64 `json:"total_cost_usd,omitempty"`
	DurationMs int64    `json:"duration_ms,omitempty"`
	Turns      int64    `json:"num_turns,omitempty"`
	Model      string   `json:"model,omitempty"`
}

// Empty reports whether the payload carries nothing worth persisting.
func (u Usage) Empty() bool {
	return u == Usage{}
}

// TotalTokens is the billable token count across every bucket.
func (u Usage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

// Raw renders the payload for Event.Usage. Empty payloads render as nil so
// events stay free of `"usage":{}` noise.
func (u Usage) Raw() json.RawMessage {
	if u.Empty() {
		return nil
	}
	data, err := json.Marshal(u)
	if err != nil {
		return nil
	}
	return data
}

// ParseUsage decodes a persisted or in-flight usage blob. It accepts both the
// normalized shape above and the raw provider spellings still present in chat
// event logs written before normalization existed.
func ParseUsage(raw json.RawMessage) (Usage, bool) {
	if len(raw) == 0 {
		return Usage{}, false
	}
	var wire usageWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Usage{}, false
	}
	usage := Usage{
		InputTokens:      firstNonZero(wire.InputTokens, wire.PromptTokens),
		OutputTokens:     firstNonZero(wire.OutputTokens, wire.CompletionTokens),
		CacheReadTokens:  firstNonZero(wire.CacheReadTokens, wire.CachedInputTokens),
		CacheWriteTokens: wire.CacheWriteTokens,
		ReasoningTokens:  wire.ReasoningTokens,
		CostUSD:          wire.CostUSD,
		DurationMs:       wire.DurationMs,
		Turns:            wire.Turns,
		Model:            wire.Model,
	}
	if usage.Empty() {
		return Usage{}, false
	}
	return usage, true
}

// usageWire is the tolerant decode target: the normalized names plus the
// provider-native aliases we have observed on the wire.
type usageWire struct {
	InputTokens       int64    `json:"input_tokens"`
	OutputTokens      int64    `json:"output_tokens"`
	CacheReadTokens   int64    `json:"cache_read_input_tokens"`
	CacheWriteTokens  int64    `json:"cache_creation_input_tokens"`
	ReasoningTokens   int64    `json:"reasoning_output_tokens"`
	CachedInputTokens int64    `json:"cached_input_tokens"`
	PromptTokens      int64    `json:"prompt_tokens"`
	CompletionTokens  int64    `json:"completion_tokens"`
	CostUSD           *float64 `json:"total_cost_usd"`
	DurationMs        int64    `json:"duration_ms"`
	Turns             int64    `json:"num_turns"`
	Model             string   `json:"model"`
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
