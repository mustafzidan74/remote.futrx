package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func cloudConfig() Config {
	return Config{
		WhatsApp: WhatsAppConfig{
			Provider: WhatsAppProviderCloud,
			Cloud: WhatsAppCloudConfig{
				PhoneNumberID: "123456789012345",
				AccessToken:   "EAAG-secret-tok9",
				Recipient:     "2010xxxxxxxx",
			},
		},
	}
}

func callMeBotConfig() Config {
	return Config{
		WhatsApp: WhatsAppConfig{
			Provider: WhatsAppProviderCallMeBot,
			CallMeBot: WhatsAppCallMeConfig{
				Phone:  "+201001234567",
				APIKey: "998877",
			},
		},
	}
}

// captured records the one request a fake provider received.
type captured struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   []byte
}

func newCapturingServer(t *testing.T, status int, response string) (*httptest.Server, *captured) {
	t.Helper()
	seen := &captured{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen.method = r.Method
		seen.path = r.URL.Path
		seen.query = r.URL.Query()
		seen.header = r.Header.Clone()
		seen.body = body
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server, seen
}

func TestWhatsAppCloudSendsATextMessageWhenNoTemplateIsConfigured(t *testing.T) {
	server, seen := newCapturingServer(t, http.StatusOK, `{"messages":[{"id":"wamid.x"}]}`)
	sink := NewWhatsAppSink(server.Client()).WithBaseURLs(server.URL, "")
	cfg := cloudConfig()

	err := sink.Send(context.Background(), cfg, Event{
		Event:       KindRunFinished,
		ProjectName: "Acme API",
		Summary:     "Shipped the login fix.",
		URL:         "https://remote.example.com/?chat=abc123",
	})
	if err != nil {
		t.Fatalf("Send() = %v", err)
	}

	if seen.method != http.MethodPost {
		t.Fatalf("method = %q, want POST", seen.method)
	}
	if want := "/123456789012345/messages"; seen.path != want {
		t.Fatalf("path = %q, want %q", seen.path, want)
	}
	if got := seen.header.Get("Authorization"); got != "Bearer EAAG-secret-tok9" {
		t.Fatalf("Authorization = %q", got)
	}

	var payload struct {
		MessagingProduct string `json:"messaging_product"`
		RecipientType    string `json:"recipient_type"`
		To               string `json:"to"`
		Type             string `json:"type"`
		Text             struct {
			PreviewURL bool   `json:"preview_url"`
			Body       string `json:"body"`
		} `json:"text"`
	}
	if err := json.Unmarshal(seen.body, &payload); err != nil {
		t.Fatalf("decode body %s: %v", seen.body, err)
	}
	if payload.MessagingProduct != "whatsapp" || payload.RecipientType != "individual" {
		t.Fatalf("envelope = %+v", payload)
	}
	if payload.To != "2010" {
		// NormalizePhone keeps digits only; the fixture's placeholder x's drop.
		t.Fatalf("to = %q", payload.To)
	}
	if payload.Type != "text" {
		t.Fatalf("type = %q, want text", payload.Type)
	}
	if payload.Text.PreviewURL {
		t.Fatal("preview_url should be false so a link does not expand a card")
	}
	if !strings.Contains(payload.Text.Body, "Acme API") ||
		!strings.Contains(payload.Text.Body, "Shipped the login fix.") ||
		!strings.Contains(payload.Text.Body, "https://remote.example.com/?chat=abc123") {
		t.Fatalf("body = %q", payload.Text.Body)
	}
	if strings.Contains(payload.Text.Body, "<b>") || strings.Contains(payload.Text.Body, "&amp;") {
		t.Fatalf("WhatsApp bodies must be plain text, got %q", payload.Text.Body)
	}
}

func TestWhatsAppCloudSendsATemplateWithTheSummaryAsTheFirstBodyParameter(t *testing.T) {
	server, seen := newCapturingServer(t, http.StatusOK, `{}`)
	sink := NewWhatsAppSink(server.Client()).WithBaseURLs(server.URL, "")
	cfg := cloudConfig()
	cfg.WhatsApp.Cloud.TemplateName = "remote_alert"

	if err := sink.Send(context.Background(), cfg, Event{
		Event:   KindRunFailed,
		Summary: "The build broke.",
	}); err != nil {
		t.Fatalf("Send() = %v", err)
	}

	var payload struct {
		Type     string `json:"type"`
		Template struct {
			Name     string `json:"name"`
			Language struct {
				Code string `json:"code"`
			} `json:"language"`
			Components []struct {
				Type       string `json:"type"`
				Parameters []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"parameters"`
			} `json:"components"`
		} `json:"template"`
		Text *struct{} `json:"text"`
	}
	if err := json.Unmarshal(seen.body, &payload); err != nil {
		t.Fatalf("decode body %s: %v", seen.body, err)
	}
	if payload.Type != "template" {
		t.Fatalf("type = %q, want template", payload.Type)
	}
	if payload.Text != nil {
		t.Fatal("a template message must not also carry a text block")
	}
	if payload.Template.Name != "remote_alert" {
		t.Fatalf("template name = %q", payload.Template.Name)
	}
	if payload.Template.Language.Code != defaultTemplateLanguage {
		t.Fatalf("language = %q, want %q", payload.Template.Language.Code, defaultTemplateLanguage)
	}
	if len(payload.Template.Components) != 1 || payload.Template.Components[0].Type != "body" {
		t.Fatalf("components = %+v", payload.Template.Components)
	}
	parameters := payload.Template.Components[0].Parameters
	if len(parameters) != 1 || parameters[0].Type != "text" {
		t.Fatalf("parameters = %+v", parameters)
	}
	if !strings.Contains(parameters[0].Text, "The build broke.") {
		t.Fatalf("first body parameter = %q", parameters[0].Text)
	}
}

func TestWhatsAppCloudUsesTheConfiguredTemplateLanguage(t *testing.T) {
	server, seen := newCapturingServer(t, http.StatusOK, `{}`)
	sink := NewWhatsAppSink(server.Client()).WithBaseURLs(server.URL, "")
	cfg := cloudConfig()
	cfg.WhatsApp.Cloud.TemplateName = "remote_alert"
	cfg.WhatsApp.Cloud.TemplateLanguage = "ar"

	if err := sink.Send(context.Background(), cfg, Event{Event: KindTest}); err != nil {
		t.Fatalf("Send() = %v", err)
	}
	if !strings.Contains(string(seen.body), `"code":"ar"`) {
		t.Fatalf("body = %s", seen.body)
	}
}

func TestWhatsAppCloudErrorKeepsTheAccessTokenOut(t *testing.T) {
	server, _ := newCapturingServer(t, http.StatusUnauthorized, `{"error":{"message":"Bad token EAAG-secret-tok9"}}`)
	sink := NewWhatsAppSink(server.Client()).WithBaseURLs(server.URL, "")

	err := sink.Send(context.Background(), cloudConfig(), Event{Event: KindTest})
	if err == nil {
		t.Fatal("expected an error for a 401")
	}
	if strings.Contains(err.Error(), "EAAG-secret-tok9") {
		t.Fatalf("error leaked the access token: %v", err)
	}
	if !strings.Contains(err.Error(), "••••tok9") {
		t.Fatalf("error should carry the mask instead: %v", err)
	}
}

func TestCallMeBotURLEncodesEveryParameter(t *testing.T) {
	server, seen := newCapturingServer(t, http.StatusOK, "Message queued")
	sink := NewWhatsAppSink(server.Client()).WithBaseURLs("", server.URL+"/whatsapp.php")

	err := sink.Send(context.Background(), callMeBotConfig(), Event{
		Event:       KindRunFinished,
		ProjectName: "Tom & Jerry",
		Summary:     "a=1&b=2 done\nnext line",
		URL:         "https://remote.example.com/?chat=abc123",
	})
	if err != nil {
		t.Fatalf("Send() = %v", err)
	}

	if seen.method != http.MethodGet {
		t.Fatalf("method = %q, want GET", seen.method)
	}
	if seen.path != "/whatsapp.php" {
		t.Fatalf("path = %q", seen.path)
	}
	if got := seen.query.Get("phone"); got != "+201001234567" {
		t.Fatalf("phone = %q, want the E.164 form with a plus", got)
	}
	if got := seen.query.Get("apikey"); got != "998877" {
		t.Fatalf("apikey = %q", got)
	}
	text := seen.query.Get("text")
	// The ampersands and newline survived intact, which is only true if the
	// query string was properly encoded rather than concatenated.
	for _, want := range []string{"Tom & Jerry", "a=1&b=2 done", "\nnext line", "?chat=abc123"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, missing %q", text, want)
		}
	}
	if len(seen.query["text"]) != 1 {
		t.Fatalf("text arrived %d times; the payload broke out of its parameter", len(seen.query["text"]))
	}
}

func TestCallMeBotErrorKeepsTheAPIKeyOut(t *testing.T) {
	server, _ := newCapturingServer(t, http.StatusForbidden, "APIKey 998877 is invalid")
	sink := NewWhatsAppSink(server.Client()).WithBaseURLs("", server.URL+"/whatsapp.php")

	err := sink.Send(context.Background(), callMeBotConfig(), Event{Event: KindTest})
	if err == nil {
		t.Fatal("expected an error for a 403")
	}
	if strings.Contains(err.Error(), "998877") {
		t.Fatalf("error leaked the api key: %v", err)
	}
}

func TestWhatsAppConfiguredFollowsTheSelectedProvider(t *testing.T) {
	full := WhatsAppConfig{
		Cloud: WhatsAppCloudConfig{
			PhoneNumberID: "1", AccessToken: "t", Recipient: "20100",
		},
		CallMeBot: WhatsAppCallMeConfig{Phone: "+20100", APIKey: "k"},
	}

	tests := []struct {
		name     string
		whatsapp WhatsAppConfig
		want     bool
	}{
		{name: "no provider selected", whatsapp: full, want: false},
		{
			name:     "cloud selected and complete",
			whatsapp: WhatsAppConfig{Provider: WhatsAppProviderCloud, Cloud: full.Cloud},
			want:     true,
		},
		{
			name: "cloud selected but missing the recipient",
			whatsapp: WhatsAppConfig{
				Provider: WhatsAppProviderCloud,
				Cloud:    WhatsAppCloudConfig{PhoneNumberID: "1", AccessToken: "t"},
			},
			want: false,
		},
		{
			name:     "callmebot selected and complete",
			whatsapp: WhatsAppConfig{Provider: WhatsAppProviderCallMeBot, CallMeBot: full.CallMeBot},
			want:     true,
		},
		{
			name: "callmebot selected but missing the key",
			whatsapp: WhatsAppConfig{
				Provider:  WhatsAppProviderCallMeBot,
				CallMeBot: WhatsAppCallMeConfig{Phone: "+20100"},
			},
			want: false,
		},
		{
			name:     "cloud selected while only callmebot is filled in",
			whatsapp: WhatsAppConfig{Provider: WhatsAppProviderCloud, CallMeBot: full.CallMeBot},
			want:     false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{WhatsApp: test.whatsapp}
			if got := cfg.WhatsAppConfigured(); got != test.want {
				t.Fatalf("WhatsAppConfigured() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWhatsAppPublicMasksBothSecrets(t *testing.T) {
	cfg := Config{WhatsApp: WhatsAppConfig{
		Provider: WhatsAppProviderCloud,
		Cloud: WhatsAppCloudConfig{
			PhoneNumberID: "123456789012345",
			AccessToken:   "EAAGm0PX4ZCsecret9876",
			Recipient:     "+20 100 123 4567",
			TemplateName:  "remote_alert",
		},
		CallMeBot: WhatsAppCallMeConfig{Phone: "+201001234567", APIKey: "key-4321"},
	}}

	public := cfg.Public().WhatsApp

	if public.Cloud.AccessTokenMasked != "••••9876" {
		t.Fatalf("access token mask = %q", public.Cloud.AccessTokenMasked)
	}
	if public.CallMeBot.APIKeyMasked != "••••4321" {
		t.Fatalf("api key mask = %q", public.CallMeBot.APIKeyMasked)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"EAAGm0PX4ZCsecret9876", "key-4321"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("public view leaked %q: %s", secret, encoded)
		}
	}
	if public.Cloud.Recipient != "201001234567" {
		t.Fatalf("recipient = %q, want digits only", public.Cloud.Recipient)
	}
	if public.CallMeBot.Phone != "+201001234567" {
		t.Fatalf("callmebot phone = %q, want a leading plus", public.CallMeBot.Phone)
	}
	if !public.Configured || !public.Cloud.Configured || !public.CallMeBot.Configured {
		t.Fatalf("expected everything configured: %+v", public)
	}
}

func TestWhatsAppApplyPreservesSecretsUnlessResubmittedOrCleared(t *testing.T) {
	stored := WhatsAppConfig{
		Provider: WhatsAppProviderCloud,
		Cloud: WhatsAppCloudConfig{
			PhoneNumberID: "111", AccessToken: "stored-token", Recipient: "20100",
		},
		CallMeBot: WhatsAppCallMeConfig{Phone: "+20100", APIKey: "stored-key"},
	}
	keepDestinations := WhatsAppInput{
		Provider:  WhatsAppProviderCloud,
		Cloud:     WhatsAppCloudInput{PhoneNumberID: "111", Recipient: "20100"},
		CallMeBot: WhatsAppCallMeInput{Phone: "+20100"},
	}

	tests := []struct {
		name      string
		input     WhatsAppInput
		wantToken string
		wantKey   string
	}{
		{
			name:      "blank secrets keep the stored ones",
			input:     keepDestinations,
			wantToken: "stored-token",
			wantKey:   "stored-key",
		},
		{
			name: "a resubmitted secret replaces the stored one",
			input: WhatsAppInput{
				Provider:  WhatsAppProviderCloud,
				Cloud:     WhatsAppCloudInput{PhoneNumberID: "111", Recipient: "20100", AccessToken: "new-token"},
				CallMeBot: WhatsAppCallMeInput{Phone: "+20100", APIKey: "new-key"},
			},
			wantToken: "new-token",
			wantKey:   "new-key",
		},
		{
			name: "clear flags remove the stored secrets",
			input: WhatsAppInput{
				Provider: WhatsAppProviderCloud,
				Cloud: WhatsAppCloudInput{
					PhoneNumberID: "111", Recipient: "20100", ClearAccessToken: true,
				},
				CallMeBot: WhatsAppCallMeInput{Phone: "+20100", ClearAPIKey: true},
			},
			wantToken: "",
			wantKey:   "",
		},
		{
			name: "clearing the cloud recipient drops its retained token",
			input: WhatsAppInput{
				Provider:  WhatsAppProviderCloud,
				Cloud:     WhatsAppCloudInput{PhoneNumberID: "111"},
				CallMeBot: WhatsAppCallMeInput{Phone: "+20100"},
			},
			wantToken: "",
			wantKey:   "stored-key",
		},
		{
			name: "clearing the cloud phone number id drops its retained token",
			input: WhatsAppInput{
				Provider:  WhatsAppProviderCloud,
				Cloud:     WhatsAppCloudInput{Recipient: "20100"},
				CallMeBot: WhatsAppCallMeInput{Phone: "+20100"},
			},
			wantToken: "",
			wantKey:   "stored-key",
		},
		{
			name: "clearing the callmebot phone drops its retained key",
			input: WhatsAppInput{
				Provider: WhatsAppProviderCloud,
				Cloud:    WhatsAppCloudInput{PhoneNumberID: "111", Recipient: "20100"},
			},
			wantToken: "stored-token",
			wantKey:   "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := stored.apply(test.input)
			if next.Cloud.AccessToken != test.wantToken {
				t.Fatalf("access token = %q, want %q", next.Cloud.AccessToken, test.wantToken)
			}
			if next.CallMeBot.APIKey != test.wantKey {
				t.Fatalf("api key = %q, want %q", next.CallMeBot.APIKey, test.wantKey)
			}
		})
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		keepPlus bool
		want     string
	}{
		{name: "empty stays empty", value: "", keepPlus: true, want: ""},
		{name: "punctuation is dropped", value: "+20 (100) 123-4567", want: "201001234567"},
		{name: "plus is restored when asked", value: "20 100 123 4567", keepPlus: true, want: "+201001234567"},
		{name: "letters are dropped", value: "call me", keepPlus: true, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizePhone(test.value, test.keepPlus); got != test.want {
				t.Fatalf("NormalizePhone(%q, %t) = %q, want %q", test.value, test.keepPlus, got, test.want)
			}
		})
	}
}

func TestWhatsAppSendWithoutAProviderFails(t *testing.T) {
	sink := NewWhatsAppSink(http.DefaultClient)
	if err := sink.Send(context.Background(), Config{}, Event{Event: KindTest}); err == nil {
		t.Fatal("expected an error when no provider is selected")
	}
}
