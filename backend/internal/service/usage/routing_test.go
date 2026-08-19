package usage

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

type stubRouting struct {
	reference RoutingReference
	ok        bool
}

func (s stubRouting) RoutingReference(context.Context) (RoutingReference, bool) {
	return s.reference, s.ok
}

func routingReference() RoutingReference {
	return RoutingReference{
		Enabled:        true,
		DefaultModel:   "sonnet",
		DefaultKey:     "claude/sonnet",
		CheapKey:       "claude/haiku",
		ExpensiveKey:   "claude/opus",
		DefaultLabel:   "Claude Sonnet",
		CheapLabel:     "Claude Haiku",
		ExpensiveLabel: "Claude Opus",
		RuleLabels:     map[string]string{"chat-mode": "Chat mode is cheap"},
	}
}

// oneMillionTokens is a run whose arithmetic is easy to check by hand: a
// million input tokens and nothing else, so a $/MTok rate is the whole cost.
func oneMillionTokens(at int64, routedBy, routedModel string, actual float64) Record {
	return Record{
		At:          at,
		ChatID:      "c1",
		Provider:    "claude",
		Model:       "claude-haiku-4-5",
		InputTokens: 1_000_000,
		CostUSD:     cost(actual),
		RoutedBy:    routedBy,
		RoutedModel: routedModel,
	}
}

func TestSummaryRoutingCardCountsAndSavings(t *testing.T) {
	// Sonnet input is $3/MTok in the shipped table, so one million input
	// tokens has a $3.00 baseline whatever the run actually cost.
	ledger := newFakeRepository(
		oneMillionTokens(ms(2, 9), "chat-mode", "claude/haiku", 1),
		oneMillionTokens(ms(2, 10), "chat-mode", "claude/haiku", 1),
		oneMillionTokens(ms(2, 11), "hard-work", "claude/opus", 15),
		oneMillionTokens(ms(2, 12), RoutedByDefault, "claude/sonnet", 3),
		// Never routed: it must not appear in the card at all.
		oneMillionTokens(ms(2, 13), "", "", 3),
	)
	service := New(ledger, testProjects(), nil,
		WithRoutingSource(stubRouting{reference: routingReference(), ok: true}))

	summary, err := service.Summary(
		context.Background(),
		Query{From: ms(1, 0), To: ms(3, 0)},
		"admin@example.com",
		true,
	)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	card := summary.Routing
	if card == nil {
		t.Fatal("Summary.Routing is nil, want the auto-routing card")
	}
	if card.RoutedRuns != 4 {
		t.Fatalf("RoutedRuns = %d, want 4 (the unrouted run must be excluded)", card.RoutedRuns)
	}
	if card.CheapRuns != 2 || card.ExpensiveRuns != 1 || card.OtherRuns != 1 {
		t.Fatalf("split = cheap %d / expensive %d / other %d, want 2/1/1",
			card.CheapRuns, card.ExpensiveRuns, card.OtherRuns)
	}
	if card.PricedRuns != 4 {
		t.Fatalf("PricedRuns = %d, want 4", card.PricedRuns)
	}
	// Baseline: 4 runs x $3.00 = $12.00. Actual: 1 + 1 + 15 + 3 = $20.00.
	if math.Abs(card.BaselineCostUSD-12) > 1e-9 {
		t.Fatalf("BaselineCostUSD = %v, want 12", card.BaselineCostUSD)
	}
	if math.Abs(card.RoutedCostUSD-20) > 1e-9 {
		t.Fatalf("RoutedCostUSD = %v, want 20", card.RoutedCostUSD)
	}
	// Routing sent expensive work to Opus, so the honest answer is a loss.
	if math.Abs(card.EstimatedSavedUSD-(-8)) > 1e-9 {
		t.Fatalf("EstimatedSavedUSD = %v, want -8 (a loss must be reported, not clamped)",
			card.EstimatedSavedUSD)
	}
	if card.CheapModel != "Claude Haiku" || card.DefaultModel != "Claude Sonnet" {
		t.Fatalf("card labels = %+v, want the policy's own", card)
	}
}

func TestSummaryRoutingTopRulesAreTheThreeBusiest(t *testing.T) {
	records := []Record{}
	at := ms(2, 0)
	add := func(rule string, times int) {
		for i := 0; i < times; i++ {
			at++
			records = append(records, oneMillionTokens(at, rule, "claude/haiku", 1))
		}
	}
	add("chat-mode", 5)
	add("short-prompt", 4)
	add("heuristic:cheap", 3)
	add(RoutedByDefault, 2)

	service := New(newFakeRepository(records...), testProjects(), nil,
		WithRoutingSource(stubRouting{reference: routingReference(), ok: true}))
	summary, err := service.Summary(
		context.Background(),
		Query{From: ms(1, 0), To: ms(3, 0)},
		"admin@example.com",
		true,
	)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	top := summary.Routing.TopRules
	if len(top) != 3 {
		t.Fatalf("TopRules = %d rows, want 3", len(top))
	}
	want := []struct {
		id    string
		label string
		runs  int64
	}{
		{"chat-mode", "Chat mode is cheap", 5},
		{"short-prompt", "short-prompt", 4},
		{"heuristic:cheap", "Built-in heuristic (cheap)", 3},
	}
	for i, expected := range want {
		if top[i].RuleID != expected.id || top[i].Runs != expected.runs {
			t.Fatalf("TopRules[%d] = %+v, want %s x%d", i, top[i], expected.id, expected.runs)
		}
		if top[i].Label != expected.label {
			t.Fatalf("TopRules[%d].Label = %q, want %q", i, top[i].Label, expected.label)
		}
	}
}

func TestSummaryHasNoRoutingCardWithoutASource(t *testing.T) {
	service := New(testLedger(), testProjects(), nil)
	summary, err := service.Summary(
		context.Background(),
		Query{From: ms(1, 0), To: ms(9, 0)},
		"admin@example.com",
		true,
	)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.Routing != nil {
		t.Fatal("a deployment with no routing policy must not grow a routing card")
	}
}

func TestSummaryRoutingSkipsTheBaselineForAnUnpriceableDefault(t *testing.T) {
	reference := routingReference()
	// The default is that provider's Auto, which has no price-table entry.
	reference.DefaultModel = ""
	service := New(
		newFakeRepository(oneMillionTokens(ms(2, 9), "chat-mode", "claude/haiku", 1)),
		testProjects(), nil,
		WithRoutingSource(stubRouting{reference: reference, ok: true}),
	)
	summary, err := service.Summary(
		context.Background(),
		Query{From: ms(1, 0), To: ms(3, 0)},
		"admin@example.com",
		true,
	)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	card := summary.Routing
	if card.RoutedRuns != 1 {
		t.Fatalf("RoutedRuns = %d, want the run still counted", card.RoutedRuns)
	}
	if card.PricedRuns != 0 || card.EstimatedSavedUSD != 0 {
		t.Fatalf("card = %+v, want no money without a priceable baseline", card)
	}
}

func TestRecordRunRoundTripsTheRoutingDecision(t *testing.T) {
	ledger := newFakeRepository()
	service := New(ledger, testProjects(), nil)
	service.RecordRun(context.Background(), RunEvent{
		At:          ms(2, 9),
		ChatID:      "c1",
		Provider:    "claude",
		Model:       "haiku",
		Usage:       json.RawMessage(`{"input_tokens":10,"output_tokens":5}`),
		RoutedBy:    "  chat-mode  ",
		RoutedModel: "  Claude/Haiku  ",
	})
	if len(ledger.records) != 1 {
		t.Fatalf("records = %d, want 1", len(ledger.records))
	}
	record := ledger.records[0]
	if record.RoutedBy != "chat-mode" {
		t.Fatalf("RoutedBy = %q, want it trimmed", record.RoutedBy)
	}
	if record.RoutedModel != "claude/haiku" {
		t.Fatalf("RoutedModel = %q, want it trimmed and lower-cased", record.RoutedModel)
	}

	// And it survives the JSON the file store writes.
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	var decoded Record
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if decoded.RoutedBy != record.RoutedBy || decoded.RoutedModel != record.RoutedModel {
		t.Fatalf("round trip lost the routing fields: %+v", decoded)
	}
}

func TestRecordOmitsTheRoutingFieldsWhenNothingWasRouted(t *testing.T) {
	encoded, err := json.Marshal(Record{At: 1, ChatID: "c1", Provider: "claude"})
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	for _, key := range []string{"routedBy", "routedModel"} {
		if _, present := raw[key]; present {
			t.Fatalf("%q is present on an unrouted record; it must be omitted", key)
		}
	}
}

func TestRebuildRecoversRoutingFromTheTranscript(t *testing.T) {
	chats := fakeChats{
		metas: []servicechat.Meta{{ID: "c1", Provider: servicechat.ProviderClaude, Model: "haiku"}},
		events: map[servicechat.ID][]servicechat.Event{
			"c1": {
				{Seq: 1, T: ms(2, 9), Type: "user", Text: "ping", Routing: &servicechat.EventRouting{
					Provider: "claude", Model: "haiku", RuleID: "chat-mode", Rule: "Chat mode is cheap",
				}},
				{Seq: 2, T: ms(2, 9) + 500, Type: "complete", Usage: json.RawMessage(
					`{"input_tokens":100,"output_tokens":50,"model":"claude-haiku-4-5"}`)},
				// A second turn the policy default claimed.
				{Seq: 3, T: ms(2, 10), Type: "user", Text: "again", Routing: &servicechat.EventRouting{
					Provider: "claude", Model: "sonnet",
				}},
				{Seq: 4, T: ms(2, 10) + 500, Type: "complete", Usage: json.RawMessage(
					`{"input_tokens":100,"output_tokens":50,"model":"claude-sonnet-4-5"}`)},
				// A third turn nobody routed.
				{Seq: 5, T: ms(2, 11), Type: "user", Text: "third"},
				{Seq: 6, T: ms(2, 11) + 500, Type: "complete", Usage: json.RawMessage(
					`{"input_tokens":100,"output_tokens":50,"model":"claude-opus-4-5"}`)},
			},
		},
	}
	ledger := newFakeRepository()
	service := New(ledger, testProjects(), chats)
	if _, err := service.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if len(ledger.records) != 3 {
		t.Fatalf("records = %d, want 3", len(ledger.records))
	}
	want := []struct{ by, model string }{
		{"chat-mode", "claude/haiku"},
		{RoutedByDefault, "claude/sonnet"},
		{"", ""},
	}
	for i, expected := range want {
		got := ledger.records[i]
		if got.RoutedBy != expected.by || got.RoutedModel != expected.model {
			t.Fatalf("records[%d] routing = %q/%q, want %q/%q",
				i, got.RoutedBy, got.RoutedModel, expected.by, expected.model)
		}
	}
}
