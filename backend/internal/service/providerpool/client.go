package providerpool

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

// The three wire shapes.
//
// Unlike the auxiliary model's client, this one hands the whole response back
// up: the status, the headers, and the token counts. That is the point — the
// pool's decisions about cooling a provider down and about what its meters
// say are made from exactly those three things.

// Call is one completion request, already resolved. A completer makes no
// policy decisions of its own: the pool has picked the provider, the model,
// the credential and the token cap before this struct exists.
type Call struct {
	Kind         Kind
	BaseURL      string
	Model        string
	APIKey       string
	SystemPrompt string
	UserText     string
	MaxTokens    int
}

// CallResult is one completion answer plus everything the pool needs to
// account for it.
type CallResult struct {
	Text             string
	PromptTokens     int
	CompletionTokens int
	// TokenSource is SourceReported when the provider returned a usage block
	// and SourceCounted when the counts are our four-characters-per-token
	// estimate.
	TokenSource string
	Header      http.Header
	Status      int
}

// CallError is a failed request that still carries what the provider said.
// The status and the headers are what decide the cooldown, so they must not
// be flattened into a string on the way up.
type CallError struct {
	Status  int
	Header  http.Header
	Message string
}

func (e *CallError) Error() string {
	if e == nil {
		return ""
	}
	if e.Status == 0 {
		return e.Message
	}
	if e.Message == "" {
		return fmt.Sprintf("the provider responded %d", e.Status)
	}
	return fmt.Sprintf("the provider responded %d: %s", e.Status, e.Message)
}

// errorBodyLimit bounds how much of a provider's error page reaches an admin
// panel and the server log.
const errorBodyLimit = 512

// responseBodyLimit bounds a successful response. These jobs produce a
// paragraph; a megabyte of JSON is a misconfigured endpoint, not an answer.
const responseBodyLimit = 1 << 20

// httpCompleter is the production Completer. One instance serves every
// provider; the wire shape is chosen per call from Call.Kind.
type httpCompleter struct {
	http *http.Client
}

// NewHTTPCompleter builds the completer the service uses in production.
func NewHTTPCompleter(client *http.Client) Completer {
	if client == nil {
		client = http.DefaultClient
	}
	return httpCompleter{http: client}
}

func (c httpCompleter) Complete(ctx context.Context, call Call) (CallResult, error) {
	switch call.Kind {
	case KindGemini:
		return c.gemini(ctx, call)
	case KindAnthropic:
		return c.anthropic(ctx, call)
	default:
		return c.openAI(ctx, call)
	}
}

/* ------------------------------------------------------------------ *
 * OpenAI-compatible
 * ------------------------------------------------------------------ */

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
	Stream      bool            `json:"stream"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		// FinishReason separates "the model had nothing to say" from "the
		// model ran out of room to say it", which look identical in the body.
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ChatCompletionsURL is where an OpenAI-compatible provider is posted to.
//
// The base URLs in this registry are complete API roots — `.../openai/v1`,
// `.../v1beta/openai`, or a bare host for GitHub Models — so the path is
// simply appended. This is deliberately unlike the auxiliary model, which
// guesses a missing `/v1` because operators paste half a URL there; here the
// seeds carry the right root and an operator adding a provider copies one.
func ChatCompletionsURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/chat/completions"
}

func (c httpCompleter) openAI(ctx context.Context, call Call) (CallResult, error) {
	messages := make([]openAIMessage, 0, 2)
	if system := strings.TrimSpace(call.SystemPrompt); system != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: system})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: call.UserText})

	body, err := json.Marshal(openAIRequest{
		Model:       call.Model,
		Messages:    messages,
		MaxTokens:   call.MaxTokens,
		Temperature: 0.2,
		Stream:      false,
	})
	if err != nil {
		return CallResult{}, err
	}
	header := http.Header{}
	if key := strings.TrimSpace(call.APIKey); key != "" {
		header.Set("Authorization", "Bearer "+key)
	}
	raw, response, err := c.post(ctx, ChatCompletionsURL(call.BaseURL), body, header)
	if err != nil {
		return CallResult{}, err
	}

	var decoded openAIResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return CallResult{}, &CallError{
			Status:  response.StatusCode,
			Header:  response.Header,
			Message: "the endpoint did not answer with chat-completions JSON",
		}
	}
	if message := strings.TrimSpace(decoded.Error.Message); message != "" {
		return CallResult{}, &CallError{Status: response.StatusCode, Header: response.Header, Message: message}
	}
	if len(decoded.Choices) == 0 {
		return CallResult{}, &CallError{Status: response.StatusCode, Header: response.Header, Message: "the model returned no choices"}
	}
	text := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if text == "" {
		// A reasoning model spends the completion budget on thinking before it
		// writes a word, and the two share one allowance. Run out and the
		// answer comes back HTTP 200, empty, with finish_reason "length" — a
		// working provider that looks broken. Say which it was: one is fixed
		// by raising max tokens, the other is not fixable at all.
		message := "the model returned no text"
		if strings.EqualFold(decoded.Choices[0].FinishReason, "length") {
			message = fmt.Sprintf(
				"the model used its whole %d-token budget before writing an answer "+
					"(finish_reason \"length\") — reasoning models need a larger one",
				call.MaxTokens,
			)
		}
		return CallResult{}, &CallError{Status: response.StatusCode, Header: response.Header, Message: message}
	}
	return withTokens(CallResult{
		Text:             text,
		PromptTokens:     decoded.Usage.PromptTokens,
		CompletionTokens: decoded.Usage.CompletionTokens,
		Header:           response.Header,
		Status:           response.StatusCode,
	}, call, text), nil
}

/* ------------------------------------------------------------------ *
 * Google Gemini (native)
 * ------------------------------------------------------------------ */

type geminiRequest struct {
	Contents          []geminiContent      `json:"contents"`
	SystemInstruction *geminiContent       `json:"systemInstruction,omitempty"`
	GenerationConfig  geminiGenerationSpec `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationSpec struct {
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Temperature     float64 `json:"temperature"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateContentURL is Google's native endpoint for one model.
func GenerateContentURL(baseURL, model string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return base + "/models/" + url.PathEscape(strings.TrimSpace(model)) + ":generateContent"
}

func (c httpCompleter) gemini(ctx context.Context, call Call) (CallResult, error) {
	request := geminiRequest{
		Contents: []geminiContent{{
			Role:  "user",
			Parts: []geminiPart{{Text: call.UserText}},
		}},
		GenerationConfig: geminiGenerationSpec{
			MaxOutputTokens: call.MaxTokens,
			Temperature:     0.2,
		},
	}
	if system := strings.TrimSpace(call.SystemPrompt); system != "" {
		request.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: system}}}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return CallResult{}, err
	}
	header := http.Header{}
	if key := strings.TrimSpace(call.APIKey); key != "" {
		// The header form rather than ?key=, so the credential never lands in
		// a proxy access log.
		header.Set("x-goog-api-key", key)
	}
	raw, response, err := c.post(ctx, GenerateContentURL(call.BaseURL, call.Model), body, header)
	if err != nil {
		return CallResult{}, err
	}

	var decoded geminiResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return CallResult{}, &CallError{
			Status:  response.StatusCode,
			Header:  response.Header,
			Message: "the endpoint did not answer with generateContent JSON",
		}
	}
	if message := strings.TrimSpace(decoded.Error.Message); message != "" {
		return CallResult{}, &CallError{Status: response.StatusCode, Header: response.Header, Message: message}
	}
	var text strings.Builder
	for _, candidate := range decoded.Candidates {
		for _, part := range candidate.Content.Parts {
			text.WriteString(part.Text)
		}
		if text.Len() > 0 {
			break
		}
	}
	answer := strings.TrimSpace(text.String())
	if answer == "" {
		return CallResult{}, &CallError{Status: response.StatusCode, Header: response.Header, Message: "the model returned no text"}
	}
	return withTokens(CallResult{
		Text:             answer,
		PromptTokens:     decoded.UsageMetadata.PromptTokenCount,
		CompletionTokens: decoded.UsageMetadata.CandidatesTokenCount,
		Header:           response.Header,
		Status:           response.StatusCode,
	}, call, answer), nil
}

/* ------------------------------------------------------------------ *
 * Anthropic messages
 * ------------------------------------------------------------------ */

// AnthropicVersion is the dated API contract this client speaks.
const AnthropicVersion = "2023-06-01"

type anthropicRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []openAIMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// MessagesURL is Anthropic's endpoint.
func MessagesURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/messages"
}

func (c httpCompleter) anthropic(ctx context.Context, call Call) (CallResult, error) {
	body, err := json.Marshal(anthropicRequest{
		Model:     call.Model,
		MaxTokens: call.MaxTokens,
		System:    strings.TrimSpace(call.SystemPrompt),
		Messages:  []openAIMessage{{Role: "user", Content: call.UserText}},
	})
	if err != nil {
		return CallResult{}, err
	}
	header := http.Header{}
	header.Set("anthropic-version", AnthropicVersion)
	if key := strings.TrimSpace(call.APIKey); key != "" {
		header.Set("x-api-key", key)
	}
	raw, response, err := c.post(ctx, MessagesURL(call.BaseURL), body, header)
	if err != nil {
		return CallResult{}, err
	}

	var decoded anthropicResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return CallResult{}, &CallError{
			Status:  response.StatusCode,
			Header:  response.Header,
			Message: "the endpoint did not answer with messages JSON",
		}
	}
	if message := strings.TrimSpace(decoded.Error.Message); message != "" {
		return CallResult{}, &CallError{Status: response.StatusCode, Header: response.Header, Message: message}
	}
	var text strings.Builder
	for _, block := range decoded.Content {
		if block.Type == "text" || block.Type == "" {
			text.WriteString(block.Text)
		}
	}
	answer := strings.TrimSpace(text.String())
	if answer == "" {
		return CallResult{}, &CallError{Status: response.StatusCode, Header: response.Header, Message: "the model returned no text"}
	}
	return withTokens(CallResult{
		Text:             answer,
		PromptTokens:     decoded.Usage.InputTokens,
		CompletionTokens: decoded.Usage.OutputTokens,
		Header:           response.Header,
		Status:           response.StatusCode,
	}, call, answer), nil
}

/* ------------------------------------------------------------------ *
 * Shared transport
 * ------------------------------------------------------------------ */

// post is the one place any of the three clients touches the network. It
// returns the response value as well as the body so the caller keeps the
// headers, which is the whole reason this is not auxmodel's post.
func (c httpCompleter) post(
	ctx context.Context,
	endpoint string,
	body []byte,
	header http.Header,
) ([]byte, *http.Response, error) {
	client := c.http
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, &CallError{Message: err.Error()}
	}
	request.Header.Set("Content-Type", "application/json")
	for name, values := range header {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		// A zero Status marks "never reached the vendor", which is what tells
		// the pool apart from a refusal the vendor actually sent.
		return nil, nil, &CallError{Message: collapse(transportError(err).Error())}
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, errorBodyLimit))
		return nil, response, &CallError{
			Status:  response.StatusCode,
			Header:  response.Header,
			Message: collapse(providerErrorMessage(detail)),
		}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseBodyLimit))
	if err != nil {
		return nil, response, &CallError{
			Status:  response.StatusCode,
			Header:  response.Header,
			Message: collapse(err.Error()),
		}
	}
	return raw, response, nil
}

// providerErrorMessage digs the human sentence out of an error body. Every
// vendor wraps it differently and several wrap it twice; failing that, the
// raw body is better than nothing.
func providerErrorMessage(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		for _, candidate := range []string{envelope.Error.Message, envelope.Message, envelope.Detail} {
			if trimmed := strings.TrimSpace(candidate); trimmed != "" {
				return trimmed
			}
		}
	}
	return text
}

// withTokens fills in an estimate when the provider reported no usage block,
// and marks which of the two happened so the meter can say so.
func withTokens(result CallResult, call Call, answer string) CallResult {
	if result.PromptTokens > 0 || result.CompletionTokens > 0 {
		result.TokenSource = SourceReported
		return result
	}
	result.PromptTokens = estimateTokens(call.SystemPrompt) + estimateTokens(call.UserText)
	result.CompletionTokens = estimateTokens(answer)
	result.TokenSource = SourceCounted
	return result
}

// transportError unwraps *url.Error so a message names what went wrong rather
// than restating the URL the panel already shows.
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
	return strings.Join(strings.Fields(text), " ")
}
