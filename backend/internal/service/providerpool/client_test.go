package providerpool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAnEmptyAnswerSaysWhetherTheBudgetRanOut covers the failure that looked
// like a broken provider and was not.
//
// Gemini's current flash models think before they answer, out of the same
// completion budget. With the probe's original 64 tokens they returned HTTP
// 200, no text, finish_reason "length" — and the pool reported "the model
// returned no text", which sends an operator to check their key. The budget
// was the problem.
func TestAnEmptyAnswerSaysWhetherTheBudgetRanOut(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		wantContains string
	}{
		{
			name:         "out of room",
			finishReason: "length",
			wantContains: "before writing an answer",
		},
		{
			name:         "genuinely nothing to say",
			finishReason: "stop",
			wantContains: "returned no text",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"choices":[{"message":{"content":""},"finish_reason":%q}],"usage":{"prompt_tokens":3,"completion_tokens":0}}`, test.finishReason)
			}))
			defer server.Close()

			_, err := NewHTTPCompleter(server.Client()).Complete(context.Background(), Call{
				Kind:      KindOpenAI,
				BaseURL:   server.URL,
				Model:     "thinker",
				APIKey:    "k",
				UserText:  "hi",
				MaxTokens: 64,
			})
			if err == nil {
				t.Fatal("an empty answer must be an error, not an empty success")
			}
			if !strings.Contains(err.Error(), test.wantContains) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), test.wantContains)
			}
		})
	}
}

// TestTheProbeBudgetLeavesRoomForThinking pins the constant itself. 64 was not
// enough for a one-word answer from a reasoning model.
func TestTheProbeBudgetLeavesRoomForThinking(t *testing.T) {
	if TestMaxTokens < 256 {
		t.Fatalf("TestMaxTokens = %d: too small for a model that thinks before answering", TestMaxTokens)
	}
}
