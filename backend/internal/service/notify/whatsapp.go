package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// SinkWhatsApp is the stable identifier of the WhatsApp sink. There is one
// sink, not two: an operator picks a provider and the sink dispatches to it,
// so the test endpoint reports a single "whatsapp" row either way.
const SinkWhatsApp = "whatsapp"

// WhatsAppProvider selects the gateway used to reach WhatsApp.
type WhatsAppProvider string

const (
	// WhatsAppProviderNone is the stored value when WhatsApp is switched off.
	WhatsAppProviderNone WhatsAppProvider = ""
	// WhatsAppProviderCloud is Meta's official WhatsApp Cloud API.
	WhatsAppProviderCloud WhatsAppProvider = "cloud"
	// WhatsAppProviderCallMeBot is the free CallMeBot personal gateway.
	WhatsAppProviderCallMeBot WhatsAppProvider = "callmebot"
)

const (
	// cloudAPIBase is Meta's Graph API origin. The version is pinned: a
	// silent bump could change the message envelope under a working install.
	cloudAPIBase = "https://graph.facebook.com/v20.0"
	// callMeBotAPIBase is the CallMeBot WhatsApp endpoint.
	callMeBotAPIBase = "https://api.callmebot.com/whatsapp.php"

	// defaultTemplateLanguage is the language code sent with a template
	// message. Meta matches the template by name *and* language, so this has
	// to agree with how the template was approved.
	defaultTemplateLanguage = "en_US"

	// whatsAppMessageLimit keeps messages short. The Cloud API allows 4096
	// characters, but CallMeBot carries the text in a query string, so both
	// providers share the smaller budget.
	whatsAppMessageLimit = 900
)

// WhatsAppConfig is the stored WhatsApp configuration. Both providers are
// persisted so switching back and forth does not lose credentials, but only
// the selected one is ever used.
type WhatsAppConfig struct {
	Provider  WhatsAppProvider     `json:"provider,omitempty"`
	Cloud     WhatsAppCloudConfig  `json:"cloud"`
	CallMeBot WhatsAppCallMeConfig `json:"callmebot"`
}

// WhatsAppCloudConfig holds Meta Cloud API credentials.
type WhatsAppCloudConfig struct {
	// PhoneNumberID is the numeric ID of the sending business number, not the
	// number itself.
	PhoneNumberID string `json:"phoneNumberId,omitempty"`
	AccessToken   string `json:"accessToken,omitempty"`
	// Recipient is the destination in E.164 without a leading plus, the shape
	// Meta expects (for example 2010xxxxxxxx).
	Recipient string `json:"recipient,omitempty"`
	// TemplateName selects a pre-approved template. Empty means free-form
	// text, which Meta only accepts inside a 24 hour customer service window.
	TemplateName string `json:"templateName,omitempty"`
	// TemplateLanguage is the approved language code of TemplateName. Empty
	// means defaultTemplateLanguage.
	TemplateLanguage string `json:"templateLanguage,omitempty"`
}

// WhatsAppCallMeConfig holds CallMeBot credentials.
type WhatsAppCallMeConfig struct {
	// Phone is the operator's own WhatsApp number in E.164, stored with the
	// leading plus.
	Phone  string `json:"phone,omitempty"`
	APIKey string `json:"apikey,omitempty"`
}

// PublicWhatsApp is the admin-facing view: secrets become masks.
type PublicWhatsApp struct {
	Configured bool                 `json:"configured"`
	Provider   WhatsAppProvider     `json:"provider,omitempty"`
	Cloud      PublicWhatsAppCloud  `json:"cloud"`
	CallMeBot  PublicWhatsAppCallMe `json:"callmebot"`
}

type PublicWhatsAppCloud struct {
	Configured        bool   `json:"configured"`
	PhoneNumberID     string `json:"phoneNumberId,omitempty"`
	AccessTokenMasked string `json:"accessTokenMasked,omitempty"`
	Recipient         string `json:"recipient,omitempty"`
	TemplateName      string `json:"templateName,omitempty"`
	TemplateLanguage  string `json:"templateLanguage,omitempty"`
}

type PublicWhatsAppCallMe struct {
	Configured   bool   `json:"configured"`
	Phone        string `json:"phone,omitempty"`
	APIKeyMasked string `json:"apikeyMasked,omitempty"`
}

// WhatsAppInput is the admin PUT body for WhatsApp. Secrets follow the same
// write-only semantics as the other sinks: blank keeps, clear removes.
type WhatsAppInput struct {
	Provider  WhatsAppProvider    `json:"provider"`
	Cloud     WhatsAppCloudInput  `json:"cloud"`
	CallMeBot WhatsAppCallMeInput `json:"callmebot"`
}

type WhatsAppCloudInput struct {
	PhoneNumberID    string `json:"phoneNumberId"`
	AccessToken      string `json:"accessToken"`
	ClearAccessToken bool   `json:"clearAccessToken"`
	Recipient        string `json:"recipient"`
	TemplateName     string `json:"templateName"`
	TemplateLanguage string `json:"templateLanguage"`
}

type WhatsAppCallMeInput struct {
	Phone       string `json:"phone"`
	APIKey      string `json:"apikey"`
	ClearAPIKey bool   `json:"clearApikey"`
}

// WhatsAppConfigured reports whether the selected WhatsApp provider has
// everything it needs.
func (c Config) WhatsAppConfigured() bool { return c.WhatsApp.normalize().configured() }

func (w WhatsAppConfig) normalize() WhatsAppConfig {
	w.Provider = normalizeWhatsAppProvider(w.Provider)
	w.Cloud.PhoneNumberID = strings.TrimSpace(w.Cloud.PhoneNumberID)
	w.Cloud.AccessToken = strings.TrimSpace(w.Cloud.AccessToken)
	w.Cloud.Recipient = NormalizePhone(w.Cloud.Recipient, false)
	w.Cloud.TemplateName = strings.TrimSpace(w.Cloud.TemplateName)
	w.Cloud.TemplateLanguage = strings.TrimSpace(w.Cloud.TemplateLanguage)
	w.CallMeBot.Phone = NormalizePhone(w.CallMeBot.Phone, true)
	w.CallMeBot.APIKey = strings.TrimSpace(w.CallMeBot.APIKey)
	return w
}

func normalizeWhatsAppProvider(provider WhatsAppProvider) WhatsAppProvider {
	switch WhatsAppProvider(strings.ToLower(strings.TrimSpace(string(provider)))) {
	case WhatsAppProviderCloud:
		return WhatsAppProviderCloud
	case WhatsAppProviderCallMeBot:
		return WhatsAppProviderCallMeBot
	default:
		return WhatsAppProviderNone
	}
}

// configured reports whether the *selected* provider can send. A stored but
// unselected provider never makes the sink configured.
func (w WhatsAppConfig) configured() bool {
	switch w.Provider {
	case WhatsAppProviderCloud:
		return w.Cloud.configured()
	case WhatsAppProviderCallMeBot:
		return w.CallMeBot.configured()
	default:
		return false
	}
}

func (c WhatsAppCloudConfig) configured() bool {
	return strings.TrimSpace(c.PhoneNumberID) != "" &&
		strings.TrimSpace(c.AccessToken) != "" &&
		strings.TrimSpace(c.Recipient) != ""
}

func (c WhatsAppCallMeConfig) configured() bool {
	return strings.TrimSpace(c.Phone) != "" && strings.TrimSpace(c.APIKey) != ""
}

// templateLanguage is the code sent to Meta, defaulted so an operator who
// leaves the field blank still gets the common approval language.
func (c WhatsAppCloudConfig) templateLanguage() string {
	if language := strings.TrimSpace(c.TemplateLanguage); language != "" {
		return language
	}
	return defaultTemplateLanguage
}

func (w WhatsAppConfig) public() PublicWhatsApp {
	w = w.normalize()
	return PublicWhatsApp{
		Configured: w.configured(),
		Provider:   w.Provider,
		Cloud: PublicWhatsAppCloud{
			Configured:        w.Cloud.configured(),
			PhoneNumberID:     w.Cloud.PhoneNumberID,
			AccessTokenMasked: MaskSecret(w.Cloud.AccessToken),
			Recipient:         w.Cloud.Recipient,
			TemplateName:      w.Cloud.TemplateName,
			TemplateLanguage:  w.Cloud.TemplateLanguage,
		},
		CallMeBot: PublicWhatsAppCallMe{
			Configured:   w.CallMeBot.configured(),
			Phone:        w.CallMeBot.Phone,
			APIKeyMasked: MaskSecret(w.CallMeBot.APIKey),
		},
	}
}

// apply folds an update onto the stored WhatsApp configuration. It mirrors the
// Telegram and webhook rules: a blank secret keeps what is stored, an explicit
// clear flag removes it, and clearing the destination drops the retained
// secret so a half-configured credential cannot linger.
func (w WhatsAppConfig) apply(input WhatsAppInput) WhatsAppConfig {
	current := w.normalize()
	next := WhatsAppConfig{
		Provider: normalizeWhatsAppProvider(input.Provider),
		Cloud: WhatsAppCloudConfig{
			PhoneNumberID:    strings.TrimSpace(input.Cloud.PhoneNumberID),
			AccessToken:      current.Cloud.AccessToken,
			Recipient:        NormalizePhone(input.Cloud.Recipient, false),
			TemplateName:     strings.TrimSpace(input.Cloud.TemplateName),
			TemplateLanguage: strings.TrimSpace(input.Cloud.TemplateLanguage),
		},
		CallMeBot: WhatsAppCallMeConfig{
			Phone:  NormalizePhone(input.CallMeBot.Phone, true),
			APIKey: current.CallMeBot.APIKey,
		},
	}

	submittedToken := strings.TrimSpace(input.Cloud.AccessToken)
	switch {
	case input.Cloud.ClearAccessToken:
		next.Cloud.AccessToken = ""
	case submittedToken != "":
		next.Cloud.AccessToken = submittedToken
	}
	if submittedToken == "" && (next.Cloud.PhoneNumberID == "" || next.Cloud.Recipient == "") {
		next.Cloud.AccessToken = ""
	}

	submittedKey := strings.TrimSpace(input.CallMeBot.APIKey)
	switch {
	case input.CallMeBot.ClearAPIKey:
		next.CallMeBot.APIKey = ""
	case submittedKey != "":
		next.CallMeBot.APIKey = submittedKey
	}
	if submittedKey == "" && next.CallMeBot.Phone == "" {
		next.CallMeBot.APIKey = ""
	}
	return next
}

// NormalizePhone reduces an operator-entered number to E.164 digits. keepPlus
// controls the leading "+": CallMeBot documents its phone parameter with one,
// Meta's `to` field wants bare digits.
func NormalizePhone(value string, keepPlus bool) string {
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	number := digits.String()
	if number == "" {
		return ""
	}
	if keepPlus {
		return "+" + number
	}
	return number
}

// WhatsAppSink delivers through whichever provider the configuration selects.
type WhatsAppSink struct {
	client        *http.Client
	cloudBase     string
	callMeBotBase string
}

func NewWhatsAppSink(client *http.Client) *WhatsAppSink {
	return &WhatsAppSink{
		client:        client,
		cloudBase:     cloudAPIBase,
		callMeBotBase: callMeBotAPIBase,
	}
}

// WithBaseURLs points the sink at different origins. Tests use it to aim at an
// httptest server; an empty argument leaves that provider's origin alone.
func (s *WhatsAppSink) WithBaseURLs(cloudBase, callMeBotBase string) *WhatsAppSink {
	if cloudBase != "" {
		s.cloudBase = strings.TrimRight(cloudBase, "/")
	}
	if callMeBotBase != "" {
		s.callMeBotBase = callMeBotBase
	}
	return s
}

func (s *WhatsAppSink) Name() string { return SinkWhatsApp }

func (s *WhatsAppSink) Configured(cfg Config) bool { return cfg.WhatsAppConfigured() }

func (s *WhatsAppSink) Send(ctx context.Context, cfg Config, event Event) error {
	whatsapp := cfg.Normalize().WhatsApp
	switch whatsapp.Provider {
	case WhatsAppProviderCloud:
		return s.sendCloud(ctx, whatsapp.Cloud, event)
	case WhatsAppProviderCallMeBot:
		return s.sendCallMeBot(ctx, whatsapp.CallMeBot, event)
	default:
		return fmt.Errorf("no WhatsApp provider is selected")
	}
}

// sendCloud posts to the Cloud API. Meta only accepts free-form text inside a
// 24 hour window opened by the recipient messaging the business number; once
// that window closes, only a pre-approved template goes through. The sink
// therefore sends type=text when no template is configured and type=template
// (summary as the first body parameter) when one is.
func (s *WhatsAppSink) sendCloud(ctx context.Context, cfg WhatsAppCloudConfig, event Event) error {
	token := strings.TrimSpace(cfg.AccessToken)
	body, err := json.Marshal(cloudPayload(cfg, event))
	if err != nil {
		return fmt.Errorf("encode whatsapp payload: %w", err)
	}

	endpoint := s.cloudBase + "/" + url.PathEscape(strings.TrimSpace(cfg.PhoneNumberID)) + "/messages"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build whatsapp request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("whatsapp cloud request failed: %s", redactToken(err.Error(), token))
	}
	return s.finish(response, "whatsapp cloud", token)
}

// cloudPayload is the exact JSON envelope Meta expects, split out so a test
// can assert its shape without a live account.
func cloudPayload(cfg WhatsAppCloudConfig, event Event) map[string]any {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                strings.TrimSpace(cfg.Recipient),
	}
	message := WhatsAppMessage(event)
	template := strings.TrimSpace(cfg.TemplateName)
	if template == "" {
		payload["type"] = "text"
		payload["text"] = map[string]any{"preview_url": false, "body": message}
		return payload
	}
	payload["type"] = "template"
	payload["template"] = map[string]any{
		"name":     template,
		"language": map[string]any{"code": cfg.templateLanguage()},
		"components": []any{
			map[string]any{
				"type": "body",
				"parameters": []any{
					map[string]any{"type": "text", "text": message},
				},
			},
		},
	}
	return payload
}

// sendCallMeBot issues the documented GET. Everything is URL-encoded through
// url.Values, so a summary containing &, =, or a newline cannot break out of
// the query string.
func (s *WhatsAppSink) sendCallMeBot(ctx context.Context, cfg WhatsAppCallMeConfig, event Event) error {
	apikey := strings.TrimSpace(cfg.APIKey)
	query := url.Values{}
	query.Set("phone", strings.TrimSpace(cfg.Phone))
	query.Set("text", WhatsAppMessage(event))
	query.Set("apikey", apikey)

	endpoint := s.callMeBotBase + "?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build whatsapp request: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		// The URL carries the API key, so the transport error is redacted too.
		return fmt.Errorf("callmebot request failed: %s", redactToken(err.Error(), apikey))
	}
	return s.finish(response, "callmebot", apikey)
}

// finish drains and classifies a provider response, keeping the credential out
// of every error string that reaches the admin UI and the server log.
func (s *WhatsAppSink) finish(response *http.Response, label, secret string) error {
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf(
			"%s responded %d: %s",
			label,
			response.StatusCode,
			redactToken(truncate(strings.TrimSpace(string(payload)), 200), secret),
		)
	}
	return nil
}

// WhatsAppMessage renders an event as plain text. WhatsApp does not render
// HTML, so nothing is escaped and nothing may be marked up: emoji, a one-line
// headline, the summary, and the deep link.
func WhatsAppMessage(event Event) string {
	var out strings.Builder
	out.WriteString(whatsAppIcon(event))
	out.WriteString(" ")
	out.WriteString(EventHeadline(event))

	if project := strings.TrimSpace(event.ProjectName); project != "" {
		out.WriteString(" — ")
		out.WriteString(project)
	}
	if chat := strings.TrimSpace(event.ChatTitle); chat != "" {
		out.WriteString("\n")
		out.WriteString(chat)
	}
	if summary := strings.TrimSpace(event.Summary); summary != "" {
		out.WriteString("\n")
		out.WriteString(truncate(summary, 400))
	}
	if link := strings.TrimSpace(event.URL); link != "" {
		out.WriteString("\n")
		out.WriteString(link)
	}
	return truncate(out.String(), whatsAppMessageLimit)
}

func whatsAppIcon(event Event) string {
	switch event.Event {
	case KindRunFinished:
		return "✅"
	case KindRunFailed:
		return "❌"
	case KindNeedsAttention:
		return "❗"
	case KindDigest:
		return "📊"
	case KindScheduledRun:
		if event.Status == StatusFailed {
			return "❌"
		}
		return "⏰"
	default:
		return "\U0001f514"
	}
}
