package auxmodel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capturedRequest is what a fake endpoint saw, so a test can assert the wire
// shape without re-implementing either provider's JSON.
type capturedRequest struct {
	path   string
	auth   string
	method string
	body   map[string]any
}

// fakeEndpoint answers one canned reply and records the request. It is the
// httptest double both provider tests are built on.
func fakeEndpoint(t *testing.T, status int, reply string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	seen := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen.path = r.URL.Path
		seen.auth = r.Header.Get("Authorization")
		seen.method = r.Method
		_ = json.Unmarshal(raw, &seen.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(server.Close)
	return server, seen
}

func TestOllamaClientRequestShape(t *testing.T) {
	server, seen := fakeEndpoint(t, http.StatusOK,
		`{"message":{"role":"assistant","content":"  Fix the login redirect  "}}`)

	client := ollamaClient{http: server.Client()}
	answer, err := client.Complete(context.Background(), Completion{
		BaseURL:      server.URL,
		Model:        "qwen2.5:3b",
		SystemPrompt: "you name things",
		UserText:     "the login page redirects in a loop",
		MaxTokens:    40,
	})
	if err != nil {
		t.Fatalf("Complete() = %v, want the model's answer", err)
	}
	if answer != "Fix the login redirect" {
		t.Fatalf("answer = %q, want the trimmed content", answer)
	}
	if seen.method != http.MethodPost || seen.path != "/api/chat" {
		t.Fatalf("request = %s %s, want POST /api/chat", seen.method, seen.path)
	}
	if seen.auth != "" {
		t.Fatalf("Authorization = %q, want none when no key is stored", seen.auth)
	}
	if stream, _ := seen.body["stream"].(bool); stream {
		t.Fatal("stream must be false: nothing here shows partial text")
	}
	options, _ := seen.body["options"].(map[string]any)
	if predict, _ := options["num_predict"].(float64); int(predict) != 40 {
		t.Fatalf("num_predict = %v, want the per-job token cap", options["num_predict"])
	}
	messages, _ := seen.body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want a system and a user turn", len(messages))
	}
}

func TestOpenAICompatibleClientRequestShape(t *testing.T) {
	server, seen := fakeEndpoint(t, http.StatusOK,
		`{"choices":[{"message":{"content":"feat(auth): add device login"}}]}`)

	client := openAICompatibleClient{http: server.Client()}
	answer, err := client.Complete(context.Background(), Completion{
		BaseURL:      server.URL,
		Model:        "gpt-4o-mini",
		APIKey:       "sk-test-key",
		SystemPrompt: "you write commit subjects",
		UserText:     "auth/device.go | 40 ++++",
		MaxTokens:    60,
	})
	if err != nil {
		t.Fatalf("Complete() = %v, want the model's answer", err)
	}
	if answer != "feat(auth): add device login" {
		t.Fatalf("answer = %q", answer)
	}
	if seen.path != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", seen.path)
	}
	if seen.auth != "Bearer sk-test-key" {
		t.Fatalf("Authorization = %q, want a bearer token", seen.auth)
	}
	if tokens, _ := seen.body["max_tokens"].(float64); int(tokens) != 60 {
		t.Fatalf("max_tokens = %v, want the per-job token cap", seen.body["max_tokens"])
	}
}

func TestProviderErrorsAreReportedWithoutTheWholeErrorPage(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		status   int
		reply    string
		wantSub  string
	}{
		{
			name:     "ollama reports a model that is not pulled",
			provider: ProviderOllama,
			status:   http.StatusOK,
			reply:    `{"error":"model 'qwen2.5:3b' not found, try pulling it first"}`,
			wantSub:  "not found",
		},
		{
			name:     "an http failure carries the status",
			provider: ProviderOpenAICompatible,
			status:   http.StatusUnauthorized,
			reply:    "{\n  \"error\": {\n    \"message\": \"bad key\"\n  }\n}",
			wantSub:  "401",
		},
		{
			name:     "an empty answer is a failure, not an empty title",
			provider: ProviderOpenAICompatible,
			status:   http.StatusOK,
			reply:    `{"choices":[{"message":{"content":"   "}}]}`,
			wantSub:  "no text",
		},
		{
			name:     "a page that is not chat JSON is reported as such",
			provider: ProviderOllama,
			status:   http.StatusOK,
			reply:    `<html>hello</html>`,
			wantSub:  "Ollama chat JSON",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := fakeEndpoint(t, test.status, test.reply)
			client := ClientFor(test.provider, server.Client())
			_, err := client.Complete(context.Background(), Completion{
				BaseURL: server.URL, Model: "m", UserText: "hi", MaxTokens: 20,
			})
			if err == nil {
				t.Fatal("Complete() = nil, want an error the caller falls back from")
			}
			if !strings.Contains(err.Error(), test.wantSub) {
				t.Fatalf("error = %q, want it to mention %q", err, test.wantSub)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Fatalf("error spans lines and will not fit a settings panel: %q", err)
			}
		})
	}
}

func TestClientForPicksTheProviderShape(t *testing.T) {
	if _, ok := ClientFor(ProviderOpenAICompatible, nil).(openAICompatibleClient); !ok {
		t.Fatal("openai-compatible did not select the chat-completions client")
	}
	for _, provider := range []string{ProviderOllama, "", "nonsense"} {
		if _, ok := ClientFor(provider, nil).(ollamaClient); !ok {
			t.Fatalf("provider %q did not fall back to the Ollama client", provider)
		}
	}
}
