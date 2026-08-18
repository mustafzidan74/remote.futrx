package transcribe

// The OpenAI audio-transcriptions client. The request is built as a multipart
// body streamed from a pipe, so a 25 MB clip never sits in memory twice and
// the audio bytes are only ever in flight — nothing here writes them anywhere.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// OpenAIEndpoint is the production transcription endpoint. Tests point the
// client at an httptest server instead.
const OpenAIEndpoint = "https://api.openai.com/v1/audio/transcriptions"

// providerErrorLimit caps how much of a provider error body is quoted back to
// the caller. Provider errors are short; a runaway HTML page is not.
const providerErrorLimit = 512

// ProviderRequest is one transcription call. Audio is consumed exactly once.
type ProviderRequest struct {
	Audio    io.Reader
	Filename string
	MimeType string
	Model    string
	APIKey   string
	// Language is the ISO-639-1 hint, already reduced by LanguageHint. Empty
	// means "let the provider detect it".
	Language string
}

// Transcriber is the provider port. The service depends on this rather than
// on net/http so a test can assert what was sent without a real key.
type Transcriber interface {
	Transcribe(ctx context.Context, req ProviderRequest) (string, error)
}

// OpenAIClient talks to the OpenAI audio-transcriptions API.
type OpenAIClient struct {
	endpoint string
	http     *http.Client
}

var _ Transcriber = (*OpenAIClient)(nil)

// NewOpenAIClient builds a client. An empty endpoint uses the production URL;
// a nil http client uses one bounded by RequestTimeout.
func NewOpenAIClient(endpoint string, client *http.Client) *OpenAIClient {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = OpenAIEndpoint
	}
	if client == nil {
		client = &http.Client{Timeout: RequestTimeout}
	}
	return &OpenAIClient{endpoint: endpoint, http: client}
}

func (c *OpenAIClient) Transcribe(ctx context.Context, req ProviderRequest) (string, error) {
	if c == nil {
		return "", fmt.Errorf("transcription provider is unavailable")
	}
	if strings.TrimSpace(req.APIKey) == "" {
		return "", fmt.Errorf("no API key is configured")
	}

	// Stream the multipart body: the handler hands us the still-uploading
	// request body, so buffering it whole would double the memory ceiling.
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		writer.CloseWithError(c.writeForm(form, req))
	}()
	defer reader.Close()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, reader)
	if err != nil {
		return "", fmt.Errorf("build transcription request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(req.APIKey))
	request.Header.Set("Content-Type", form.FormDataContentType())

	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("reach the transcription provider: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf(
			"transcription provider returned %d: %s",
			response.StatusCode,
			providerError(response.Body),
		)
	}

	// Both whisper-1 and the gpt-4o transcribe models answer with {"text": …}
	// in the default json response format.
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("parse transcription response: %w", err)
	}
	return strings.TrimSpace(payload.Text), nil
}

// writeForm builds the multipart body. It runs on its own goroutine feeding
// the pipe, so every failure has to reach the reader through CloseWithError.
func (c *OpenAIClient) writeForm(form *multipart.Writer, req ProviderRequest) error {
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		filename = "audio.webm"
	}
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("build audio part: %w", err)
	}
	if _, err := io.Copy(part, req.Audio); err != nil {
		return fmt.Errorf("stream audio: %w", err)
	}
	fields := map[string]string{"model": req.Model}
	if req.Language != "" {
		fields["language"] = req.Language
	}
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			return fmt.Errorf("build %s field: %w", name, err)
		}
	}
	return form.Close()
}

// providerError quotes a bounded slice of a failed response so the operator
// sees "invalid_api_key" instead of a bare status code.
func providerError(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, providerErrorLimit))
	if err != nil || len(raw) == 0 {
		return "no detail"
	}
	// The documented error shape is {"error":{"message":…}}; fall back to the
	// raw bytes for a gateway that answers with something else entirely.
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}
	return strings.TrimSpace(string(raw))
}
