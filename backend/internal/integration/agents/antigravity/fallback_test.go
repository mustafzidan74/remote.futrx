package antigravity

import "testing"

func TestCapacityFailureRecognisesGoogleUnavailability(t *testing.T) {
	ok, model := capacityFailure("API error (attempt 1): UNAVAILABLE (code 503): No capacity available for model gemini-3.8-flash-high on the server")
	if !ok || model != "gemini-3.8-flash-high" {
		t.Fatalf("capacityFailure = (%v, %q)", ok, model)
	}
	for _, text := range []string{
		"agy run failed: exit status 1; output: not authenticated",
		"RESOURCE_EXHAUSTED: quota exceeded for this account",
		"invalid argument: unknown model",
	} {
		if ok, _ := capacityFailure(text); ok {
			t.Fatalf("%q must not be treated as a capacity failure", text)
		}
	}
}

// The catalog agy listed on this platform.
var recordedCatalog = []string{
	"gemini-3.8-flash-high", "gemini-3.8-flash-medium", "gemini-3.8-flash-low",
	"gemini-3.7-flash-high", "gemini-3.7-flash-medium", "gemini-3.7-flash-low",
	"gemini-3.6-flash-high", "gemini-3.6-flash-medium", "gemini-3.6-flash-low",
	"gemini-3.1-pro-high", "gemini-3.1-pro-low",
	"claude-sonnet-4-6", "gpt-oss-120b-medium",
}

func TestCapacityFallbackOrder(t *testing.T) {
	tried := map[string]bool{}
	var chain []string
	origin := "gemini-3.8-flash-high"
	tried[origin] = true
	for i := 0; i < 5; i++ {
		next := nextCapacityFallback(origin, recordedCatalog, tried)
		if next == "" {
			break
		}
		chain = append(chain, next)
		tried[next] = true
	}
	want := []string{
		"gemini-3.7-flash-high", "gemini-3.6-flash-high",
		"gemini-3.8-flash-medium", "gemini-3.7-flash-medium", "gemini-3.6-flash-medium",
	}
	if len(chain) != len(want) {
		t.Fatalf("chain = %v, want %v", chain, want)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("chain = %v, want %v", chain, want)
		}
	}
}

func TestCapacityFallbackStaysInItsFamily(t *testing.T) {
	if next := nextCapacityFallback("gemini-3.1-pro-high", recordedCatalog, map[string]bool{}); next != "gemini-3.1-pro-low" {
		t.Fatalf("pro fallback = %q", next)
	}
	if next := nextCapacityFallback("claude-sonnet-4-6", recordedCatalog, map[string]bool{}); next != "" {
		t.Fatalf("non-gemini model got fallback %q", next)
	}
	if next := nextCapacityFallback("gemini-3.6-flash-low", recordedCatalog, map[string]bool{}); next != "" {
		t.Fatalf("the lowest flash model got fallback %q", next)
	}
}
