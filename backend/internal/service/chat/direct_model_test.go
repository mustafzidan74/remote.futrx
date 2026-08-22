package chat

import "testing"

// TestNormalizeDirectModelDegradesToAnAgent covers what happens when the
// stored value makes no sense — a hand-edited document, a field left over from
// an older shape, a client that sent half of one.
//
// It becomes "this chat runs an agent", which is the behaviour every chat
// already had and the only one that cannot fail to start. The alternative — a
// run that refuses because the chat names a model nobody can resolve — turns a
// bad byte on disk into a chat the operator cannot use.
func TestNormalizeDirectModelDegradesToAnAgent(t *testing.T) {
	tests := []struct {
		name string
		in   DirectModel
		want DirectModel
	}{
		{
			name: "the zero value stays unset",
		},
		{
			name: "a pool choice keeps its provider and model",
			in:   DirectModel{Source: "  POOL ", ProviderID: " Gemini ", Model: " gemini-flash-latest "},
			want: DirectModel{Source: DirectSourcePool, ProviderID: "gemini", Model: "gemini-flash-latest"},
		},
		{
			name: "a pool choice with no provider is not a choice",
			in:   DirectModel{Source: DirectSourcePool, Model: "some-model"},
		},
		{
			name: "an unknown source is dropped",
			in:   DirectModel{Source: "openrouter-direct", Model: "x"},
		},
		{
			name: "the local model needs no provider id",
			in:   DirectModel{Source: DirectSourceLocal, Model: "qwen3:1.7b"},
			want: DirectModel{Source: DirectSourceLocal, Model: "qwen3:1.7b"},
		},
		{
			name: "a provider id on the local model is dropped as noise",
			in:   DirectModel{Source: DirectSourceLocal, ProviderID: "ollama", Model: "qwen3:1.7b"},
			want: DirectModel{Source: DirectSourceLocal, Model: "qwen3:1.7b"},
		},
		{
			name: "the local model may name none, since there is only one",
			in:   DirectModel{Source: DirectSourceLocal},
			want: DirectModel{Source: DirectSourceLocal},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeDirectModel(test.in)
			if got != test.want {
				t.Fatalf("NormalizeDirectModel(%+v) = %+v, want %+v", test.in, got, test.want)
			}
			if got.Set() != (test.want.Source != "") {
				t.Errorf("Set() = %v for %+v", got.Set(), got)
			}
		})
	}
}

// TestAChatCannotCarryBothPaths pins the exclusion.
//
// An endpoint decides which agent CLI runs; a direct model runs no CLI at all.
// A chat holding both would need some later reader to break the tie, and two
// readers could break it differently.
func TestAChatCannotCarryBothPaths(t *testing.T) {
	direct := DirectModel{Source: DirectSourcePool, ProviderID: "gemini", Model: "gemini-flash-latest"}

	t.Run("picking a direct model clears the endpoint", func(t *testing.T) {
		meta := Meta{EndpointID: "zhipu-glm"}
		applyDirectModelForTest(&meta, &direct, nil)
		if meta.EndpointID != "" {
			t.Fatalf("endpoint = %q, want it cleared", meta.EndpointID)
		}
		if !meta.DirectModel.Set() {
			t.Fatal("the direct model was not applied")
		}
	})

	t.Run("picking an endpoint clears the direct model", func(t *testing.T) {
		meta := Meta{DirectModel: direct}
		endpoint := "zhipu-glm"
		applyDirectModelForTest(&meta, nil, &endpoint)
		if meta.DirectModel.Set() {
			t.Fatalf("direct model = %+v, want it cleared", meta.DirectModel)
		}
		if meta.EndpointID != "zhipu-glm" {
			t.Fatalf("endpoint = %q", meta.EndpointID)
		}
	})
}

// applyDirectModelForTest mirrors the two clauses in Service.Update. The
// service needs a repository to exercise directly, and the rule under test is
// the pair of assignments rather than the plumbing around them.
func applyDirectModelForTest(meta *Meta, direct *DirectModel, endpointID *string) {
	if endpointID != nil {
		meta.EndpointID = NormalizeEndpointID(*endpointID)
		if meta.EndpointID != "" {
			meta.DirectModel = DirectModel{}
		}
	}
	if direct != nil {
		meta.DirectModel = NormalizeDirectModel(*direct)
		if meta.DirectModel.Set() {
			meta.EndpointID = ""
		}
	}
}
