package usage

import (
	"context"
	"sort"
	"strings"
)

// RoutedByDefault is the routedBy value a run carries when the automatic
// router fell through to the policy default rather than matching a rule. It
// mirrors the routing service's own spelling; the ledger keeps its own copy so
// it never has to import the policy that produced the value.
const RoutedByDefault = "default"

// maxTopRules is how many rules the savings card names. Three is what fits
// beside the money without turning the card into a table.
const maxTopRules = 3

// RoutingReference is the ledger's view of the automatic model routing
// policy: the destination a routed run is compared against, the two poles it
// is classified by, and what each rule is called. Everything is pre-resolved
// by the composition root, so the ledger never reasons about rules.
type RoutingReference struct {
	// Enabled is the policy's master switch. A disabled policy still gets a
	// card, because runs routed while it was on are still in the window.
	Enabled bool
	// DefaultModel is the model id the baseline is priced at — the same
	// string prices.json matches on. Empty means the default is that
	// provider's own Auto, which cannot be priced, so no baseline is
	// computed.
	DefaultModel string
	// DefaultKey, CheapKey and ExpensiveKey are "provider/model" spellings,
	// matching Record.RoutedModel.
	DefaultKey   string
	CheapKey     string
	ExpensiveKey string
	// DefaultLabel, CheapLabel and ExpensiveLabel are how the card names them.
	DefaultLabel   string
	CheapLabel     string
	ExpensiveLabel string
	// RuleLabels maps a rule id to its human name.
	RuleLabels map[string]string
}

// RoutingSource supplies the reference above. Nil leaves the usage summary
// without a routing card, which is the right answer for a deployment that
// never configured routing.
type RoutingSource interface {
	RoutingReference(ctx context.Context) (RoutingReference, bool)
}

// RoutingRuleHits is one row of the "top rules" list.
type RoutingRuleHits struct {
	RuleID string `json:"ruleId"`
	Label  string `json:"label"`
	Runs   int64  `json:"runs"`
	// CostUSD is what this rule's runs actually cost.
	CostUSD float64 `json:"costUsd"`
}

// RoutingSummary is the "Auto routing" card: how many runs the router placed
// at each pole this period, what they cost, and what the same work would have
// cost at the policy's default model.
//
// Every money figure here is an estimate. The baseline is a counterfactual —
// nobody ran those tokens through the default model — and it is priced from
// the editable table in prices.json, not from a provider invoice.
type RoutingSummary struct {
	Enabled bool `json:"enabled"`
	// RoutedRuns is every run the router placed; Cheap/Expensive/Other split
	// it by which pole the destination matched.
	RoutedRuns    int64 `json:"routedRuns"`
	CheapRuns     int64 `json:"cheapRuns"`
	ExpensiveRuns int64 `json:"expensiveRuns"`
	OtherRuns     int64 `json:"otherRuns"`
	// PricedRuns is how many routed runs both halves of the comparison could
	// be computed for. The rest contribute nothing to the money below.
	PricedRuns int64 `json:"pricedRuns"`
	// RoutedCostUSD is what the priced routed runs actually cost;
	// BaselineCostUSD is what they would have cost at the default model.
	RoutedCostUSD   float64 `json:"routedCostUsd"`
	BaselineCostUSD float64 `json:"baselineCostUsd"`
	// EstimatedSavedUSD is baseline minus actual. It goes negative when
	// routing sent work to a model dearer than the default, which is a real
	// answer and is reported rather than clamped.
	EstimatedSavedUSD float64 `json:"estimatedSavedUsd"`

	DefaultModel   string `json:"defaultModel,omitempty"`
	CheapModel     string `json:"cheapModel,omitempty"`
	ExpensiveModel string `json:"expensiveModel,omitempty"`

	TopRules []RoutingRuleHits `json:"topRules"`
}

// routingAccumulator folds records into a RoutingSummary during the same scan
// the rest of the summary uses, so the card costs no extra pass over the
// ledger.
type routingAccumulator struct {
	reference RoutingReference
	prices    PriceTable
	summary   RoutingSummary
	hits      map[string]*RoutingRuleHits
}

func newRoutingAccumulator(reference RoutingReference, prices PriceTable) *routingAccumulator {
	return &routingAccumulator{
		reference: reference,
		prices:    prices,
		summary: RoutingSummary{
			Enabled:        reference.Enabled,
			DefaultModel:   reference.DefaultLabel,
			CheapModel:     reference.CheapLabel,
			ExpensiveModel: reference.ExpensiveLabel,
		},
		hits: map[string]*RoutingRuleHits{},
	}
}

func (a *routingAccumulator) add(record Record) {
	if a == nil || record.RoutedBy == "" {
		return
	}
	a.summary.RoutedRuns++

	switch record.RoutedModel {
	case a.reference.CheapKey:
		a.summary.CheapRuns++
	case a.reference.ExpensiveKey:
		a.summary.ExpensiveRuns++
	default:
		a.summary.OtherRuns++
	}

	hit, ok := a.hits[record.RoutedBy]
	if !ok {
		hit = &RoutingRuleHits{RuleID: record.RoutedBy, Label: a.ruleLabel(record.RoutedBy)}
		a.hits[record.RoutedBy] = hit
	}
	hit.Runs++
	if record.CostUSD != nil {
		hit.CostUSD += *record.CostUSD
	}

	// The baseline only exists when both halves are known: what the run cost,
	// and what the default model would have charged for the same tokens.
	if record.CostUSD == nil || a.reference.DefaultModel == "" {
		return
	}
	baseline, ok := a.prices.Estimate(
		a.reference.DefaultModel,
		record.InputTokens,
		record.OutputTokens,
		record.CacheReadTokens,
		record.CacheWriteTokens,
	)
	if !ok {
		return
	}
	a.summary.PricedRuns++
	a.summary.RoutedCostUSD += *record.CostUSD
	a.summary.BaselineCostUSD += baseline
}

func (a *routingAccumulator) ruleLabel(ruleID string) string {
	if label := strings.TrimSpace(a.reference.RuleLabels[ruleID]); label != "" {
		return label
	}
	switch {
	case ruleID == RoutedByDefault:
		return "Policy default"
	case strings.HasPrefix(ruleID, "heuristic:"):
		return "Built-in heuristic (" + strings.TrimPrefix(ruleID, "heuristic:") + ")"
	default:
		return ruleID
	}
}

func (a *routingAccumulator) result() *RoutingSummary {
	if a == nil {
		return nil
	}
	a.summary.EstimatedSavedUSD = a.summary.BaselineCostUSD - a.summary.RoutedCostUSD
	rules := make([]RoutingRuleHits, 0, len(a.hits))
	for _, hit := range a.hits {
		rules = append(rules, *hit)
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Runs != rules[j].Runs {
			return rules[i].Runs > rules[j].Runs
		}
		return rules[i].RuleID < rules[j].RuleID
	})
	if len(rules) > maxTopRules {
		rules = rules[:maxTopRules]
	}
	a.summary.TopRules = rules
	return &a.summary
}

// routingAccumulatorFor builds the accumulator for one summary, or nil when
// this deployment has no routing policy or no price table to compare against.
func (s *Service) routingAccumulatorFor(ctx context.Context) *routingAccumulator {
	if s == nil || s.routing == nil {
		return nil
	}
	reference, ok := s.routing.RoutingReference(ctx)
	if !ok {
		return nil
	}
	prices, err := s.repo.Prices(ctx)
	if err != nil {
		// Without prices the run counts are still worth showing; only the
		// money is unavailable, and Estimate simply never succeeds.
		prices = PriceTable{}
	}
	return newRoutingAccumulator(reference, prices)
}
