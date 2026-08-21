package providerpool

import (
	"context"
	"fmt"
	"strings"
)

// The bulk lane.
//
// One entry point for the features that will want a lot of cheap text —
// product descriptions, SEO copy, translating a few hundred UI strings — so
// that none of them grows its own provider client, its own key handling, and
// its own idea of what a reasonable request is.
//
// It is capped on both ends. A caller that wants more than this should be
// making several requests, which is also the only shape that lets the pool
// spread the work across providers as quotas run out.

const (
	// BulkMaxPromptTokens caps one bulk prompt. Eight thousand tokens is
	// comfortably inside every free-tier model in the shipped seeds, and the
	// smallest of them — Moonshot's 8k model — is exactly this size, so a
	// request at the ceiling still has somewhere to go.
	BulkMaxPromptTokens = 8000
	// BulkMaxCompletionTokens caps one bulk answer.
	BulkMaxCompletionTokens = 2000
	// BulkJob is the ledger label for everything through this lane.
	BulkJob = "bulk"
)

// ErrPromptTooLarge reports a bulk request over the prompt ceiling. It is
// separate from ErrInvalidProvider because it is the caller's problem rather
// than the operator's, and the handler maps it to 413.
var ErrPromptTooLarge = fmt.Errorf("the prompt is larger than the bulk lane allows")

// BulkInput is one request to the bulk lane.
type BulkInput struct {
	Prompt string
	System string
	// MaxTokens is clamped to BulkMaxCompletionTokens. Zero takes the cap.
	MaxTokens int
	// ProviderID pins one provider, for a caller that knows which free tier
	// it wants to spend. Empty follows the pool's own mode.
	ProviderID string
	// PreferModel pins a model id when the chosen provider offers it.
	PreferModel string
}

// EstimatePromptTokens is the size check the bulk lane applies, exported so a
// caller can check before it builds a request it cannot send.
//
// It is the same four-characters-per-token estimate the meters use, and it is
// wrong in the same direction for Arabic and for code — which is why the cap
// it guards is generous rather than tight.
func EstimatePromptTokens(system, prompt string) int {
	return estimateTokens(system) + estimateTokens(prompt)
}

// Bulk runs one request through the bulk lane. It is Complete with the lane's
// caps and its capability tag already applied, so no caller has to remember
// either one.
func (s *Service) Bulk(ctx context.Context, input BulkInput) (Result, error) {
	if s == nil {
		return Result{}, ErrNoProvider
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return Result{}, ErrEmptyPrompt
	}
	if estimated := EstimatePromptTokens(input.System, prompt); estimated > BulkMaxPromptTokens {
		return Result{}, fmt.Errorf("%w: about %d tokens, the ceiling is %d",
			ErrPromptTooLarge, estimated, BulkMaxPromptTokens)
	}
	maxTokens := input.MaxTokens
	if maxTokens <= 0 || maxTokens > BulkMaxCompletionTokens {
		maxTokens = BulkMaxCompletionTokens
	}
	return s.Complete(ctx, Request{
		Need: Need{
			Job:         BulkJob,
			Want:        CapabilityBulk,
			ProviderID:  input.ProviderID,
			PreferModel: input.PreferModel,
		},
		System:    input.System,
		Prompt:    prompt,
		MaxTokens: maxTokens,
	})
}

/* ------------------------------------------------------------------ *
 * The auxiliary model's view of the pool
 * ------------------------------------------------------------------ */

// AuxRequest is one auxiliary-model job routed through the pool. It mirrors
// auxmodel.PoolRequest without either package importing the other.
type AuxRequest struct {
	Job          string
	Capability   string
	SystemPrompt string
	UserText     string
	MaxTokens    int
}

// CompleteAux runs one auxiliary-model job and returns only the text. The
// auxiliary model does not care which provider answered — its whole contract
// with its callers is "a sentence, or a fallback" — so the failover stays
// invisible here exactly as it is to a chat title.
func (s *Service) CompleteAux(ctx context.Context, request AuxRequest) (string, error) {
	result, err := s.Complete(ctx, Request{
		Need: Need{
			Job:  request.Job,
			Want: Capability(strings.TrimSpace(request.Capability)),
		},
		System:    request.SystemPrompt,
		Prompt:    request.UserText,
		MaxTokens: request.MaxTokens,
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}
