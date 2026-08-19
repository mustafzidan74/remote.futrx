package auxmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Completion is one request to the auxiliary model, already resolved: the
// service has picked the endpoint, the token cap, and the credential, so a
// provider client makes no policy decisions of its own.
type Completion struct {
	BaseURL      string
	Model        string
	APIKey       string
	SystemPrompt string
	UserText     string
	MaxTokens    int
}

// Completer is one provider shape. It is an interface so the tests can point
// at an httptest server, and so the service can be exercised with a stub that
// never touches a socket.
type Completer interface {
	// Complete returns the model's answer as plain text. A non-nil error
	// means the caller must fall back; it is never surfaced to a user.
	Complete(ctx context.Context, req Completion) (string, error)
}

// errorBodyLimit bounds how much of a provider's error page is read into a
// message that reaches the admin panel and the server log.
const errorBodyLimit = 512

// responseBodyLimit bounds a successful response. These jobs produce a
// sentence; a megabyte of JSON is a misconfigured endpoint, not an answer.
const responseBodyLimit = 1 << 20

// ClientFor picks the provider client for a configuration.
func ClientFor(provider string, httpClient *http.Client) Completer {
	if strings.ToLower(strings.TrimSpace(provider)) == ProviderOpenAICompatible {
		return openAICompatibleClient{http: httpClient}
	}
	return ollamaClient{http: httpClient}
}

/* ------------------------------------------------------------------ *
 * Ollama
 * ------------------------------------------------------------------ */

// ollamaClient speaks Ollama's own chat API. `stream:false` matters: the
// default is a stream of JSON objects, and none of these jobs shows partial
// text to anybody, so a single response is both simpler and cheaper.
type ollamaClient struct {
	http *http.Client
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	// NumPredict is Ollama's name for the answer's token cap.
	NumPredict int `json:"num_predict"`
	// Temperature is low but not zero: a title is better for a little
	// variety, and greedy decoding on a 3B model tends to echo the prompt.
	Temperature float64 `json:"temperature"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

func (c ollamaClient) Complete(ctx context.Context, req Completion) (string, error) {
	body, err := json.Marshal(ollamaRequest{
		Model: req.Model,
		Messages: []ollamaMessage{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.UserText},
		},
		Stream:  false,
		Options: ollamaOptions{NumPredict: req.MaxTokens, Temperature: 0.2},
	})
	if err != nil {
		return "", err
	}
	raw, err := post(ctx, c.http, OllamaChatURL(req.BaseURL), body, req.APIKey)
	if err != nil {
		return "", err
	}
	var decoded ollamaResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("the endpoint did not answer with Ollama chat JSON")
	}
	if strings.TrimSpace(decoded.Error) != "" {
		return "", errors.New(decoded.Error)
	}
	text := strings.TrimSpace(decoded.Message.Content)
	if text == "" {
		return "", errors.New("the model returned no text")
	}
	return text, nil
}

/* ------------------------------------------------------------------ *
 * OpenAI-compatible
 * ------------------------------------------------------------------ */

// openAICompatibleClient speaks the /v1/chat/completions shape every hosted
// endpoint and most local servers implement.
type openAICompatibleClient struct {
	http *http.Client
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []ollamaMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
	Stream      bool            `json:"stream"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c openAICompatibleClient) Complete(ctx context.Context, req Completion) (string, error) {
	body, err := json.Marshal(openAIRequest{
		Model: req.Model,
		Messages: []ollamaMessage{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.UserText},
		},
		MaxTokens:   req.MaxTokens,
		Temperature: 0.2,
		Stream:      false,
	})
	if err != nil {
		return "", err
	}
	raw, err := post(ctx, c.http, ChatCompletionsURL(req.BaseURL), body, req.APIKey)
	if err != nil {
		return "", err
	}
	var decoded openAIResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("the endpoint did not answer with chat-completions JSON")
	}
	if message := strings.TrimSpace(decoded.Error.Message); message != "" {
		return "", errors.New(message)
	}
	if len(decoded.Choices) == 0 {
		return "", errors.New("the model returned no choices")
	}
	text := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if text == "" {
		return "", errors.New("the model returned no text")
	}
	return text, nil
}

/* ------------------------------------------------------------------ *
 * Shared transport
 * ------------------------------------------------------------------ */

// post is the one place either client touches the network. The bearer header
// is set only when a key is stored, so a loopback Ollama is never sent an
// `Authorization: Bearer ` with nothing after it.
func post(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	body []byte,
	apiKey string,
) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(apiKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, transportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, errorBodyLimit))
		text := strings.TrimSpace(string(detail))
		if text == "" {
			return nil, fmt.Errorf("the endpoint responded %d", response.StatusCode)
		}
		return nil, fmt.Errorf("the endpoint responded %d: %s", response.StatusCode, collapse(text))
	}
	return io.ReadAll(io.LimitReader(response.Body, responseBodyLimit))
}

// transportError unwraps *url.Error so the message names what went wrong
// rather than restating the URL, which the panel already shows.
func transportError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}

// collapse folds a provider's multi-line error page into one line so it fits
// a settings panel and a log entry.
func collapse(text string) string {
	fields := strings.Fields(text)
	return strings.Join(fields, " ")
}
