package notify

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

var testImage = Image{
	Filename: "1772000000000-abc123.png",
	Data:     []byte("\x89PNG\r\n\x1a\nfake"),
	Caption:  "Demo Shop — preview :3000/",
	LinkURL:  "https://remote.example.test/s/screenshot/abcd1234-tok.png",
}

func TestSendImagePrefersPixelsAndFallsBackToText(t *testing.T) {
	pictures := newImageSink("pictures")
	text := newRecordingSink("text", 0)
	offline := newRecordingSink("offline", 0)
	offline.configured = false
	notifier := NewNotifier(
		func() Config { return Config{Enabled: true} },
		WithSinks(pictures, text, offline),
		WithBackoff(0),
	)

	results := notifier.SendImage(context.Background(), Event{Event: KindScreenshot}, testImage)

	if len(results) != 3 {
		t.Fatalf("results = %#v, want one row per sink", results)
	}
	if !results[0].Delivered || !results[1].Delivered {
		t.Fatalf("results = %#v, want both configured sinks delivered", results)
	}
	if results[2].Configured || results[2].Error != "not configured" {
		t.Fatalf("unconfigured sink row = %#v", results[2])
	}
	if string(pictures.received().Data) != string(testImage.Data) {
		t.Fatal("the image sink did not receive the bytes")
	}
	_, delivered := text.counts()
	if len(delivered) != 1 {
		t.Fatalf("text sink deliveries = %d, want 1", len(delivered))
	}
	if delivered[0].Summary != testImage.Caption {
		t.Fatalf("text summary = %q, want the caption", delivered[0].Summary)
	}
	if delivered[0].URL != testImage.LinkURL {
		t.Fatalf("text url = %q, want the login-less link", delivered[0].URL)
	}
}

func TestNeedsPublicLink(t *testing.T) {
	tests := []struct {
		name  string
		sinks []Sink
		want  bool
	}{
		{
			name:  "only picture-capable sinks",
			sinks: []Sink{newImageSink("pictures")},
			want:  false,
		},
		{
			name:  "a text-only sink is configured",
			sinks: []Sink{newImageSink("pictures"), newRecordingSink("text", 0)},
			want:  true,
		},
		{
			name:  "the text-only sink is not configured",
			sinks: []Sink{unconfiguredSink("text")},
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notifier := NewNotifier(func() Config { return Config{} }, WithSinks(test.sinks...))
			if got := notifier.NeedsPublicLink(); got != test.want {
				t.Fatalf("NeedsPublicLink() = %t, want %t", got, test.want)
			}
		})
	}
}

// TestWhatsAppNeedsPublicLinkPerProvider covers the real sink: one type, two
// providers, only one of which can carry a picture.
func TestWhatsAppNeedsPublicLinkPerProvider(t *testing.T) {
	tests := []struct {
		name string
		cfg  WhatsAppConfig
		want bool
	}{
		{
			name: "cloud api uploads media",
			cfg: WhatsAppConfig{
				Provider: WhatsAppProviderCloud,
				Cloud: WhatsAppCloudConfig{
					PhoneNumberID: "111", AccessToken: "tok", Recipient: "20100000000",
				},
			},
			want: false,
		},
		{
			name: "cloud api behind a template carries text only",
			cfg: WhatsAppConfig{
				Provider: WhatsAppProviderCloud,
				Cloud: WhatsAppCloudConfig{
					PhoneNumberID: "111", AccessToken: "tok", Recipient: "20100000000",
					TemplateName: "run_update",
				},
			},
			want: true,
		},
		{
			name: "callmebot cannot send pictures at all",
			cfg: WhatsAppConfig{
				Provider:  WhatsAppProviderCallMeBot,
				CallMeBot: WhatsAppCallMeConfig{Phone: "+20100000000", APIKey: "key"},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{Enabled: true, WhatsApp: test.cfg}
			notifier := NewNotifier(
				func() Config { return cfg },
				WithSinks(NewWhatsAppSink(http.DefaultClient)),
			)
			if got := notifier.NeedsPublicLink(); got != test.want {
				t.Fatalf("NeedsPublicLink() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestTelegramSendImagePostsAPhoto(t *testing.T) {
	var (
		gotPath    string
		gotChat    string
		gotCaption string
		gotFile    []byte
		gotName    string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("content type = %q, want multipart", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			body, _ := io.ReadAll(part)
			switch part.FormName() {
			case "chat_id":
				gotChat = string(body)
			case "caption":
				gotCaption = string(body)
			case "photo":
				gotFile = body
				gotName = part.FileName()
			}
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	sink := NewTelegramSink(server.Client()).WithBaseURL(server.URL)
	cfg := Config{Telegram: TelegramConfig{BotToken: "123:secret", ChatID: "-42"}}
	err := sink.SendImage(context.Background(), cfg,
		Event{Event: KindScreenshot, URL: "https://remote.example.test/"}, testImage)
	if err != nil {
		t.Fatalf("SendImage(): %v", err)
	}
	if gotPath != "/bot123:secret/sendPhoto" {
		t.Fatalf("path = %q, want the sendPhoto method", gotPath)
	}
	if gotChat != "-42" {
		t.Fatalf("chat_id = %q", gotChat)
	}
	if !strings.Contains(gotCaption, "Preview screenshot") ||
		!strings.Contains(gotCaption, "Demo Shop") {
		t.Fatalf("caption = %q, want the headline and the project", gotCaption)
	}
	if string(gotFile) != string(testImage.Data) || gotName != testImage.Filename {
		t.Fatalf("photo part = %q named %q", gotFile, gotName)
	}
}

func TestWhatsAppCloudSendImageUploadsThenSends(t *testing.T) {
	var (
		uploadHit  bool
		uploadType string
		sent       map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/media"):
			uploadHit = true
			_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil {
				t.Errorf("upload content type: %v", err)
			}
			reader := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := reader.NextPart()
				if err != nil {
					break
				}
				if part.FormName() == "file" {
					uploadType = part.Header.Get("Content-Type")
				}
				_, _ = io.ReadAll(part)
			}
			w.Write([]byte(`{"id":"media-77"}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode message: %v", err)
			}
			w.Write([]byte(`{"messages":[{"id":"wamid"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	sink := NewWhatsAppSink(server.Client()).WithBaseURLs(server.URL, "")
	cfg := Config{WhatsApp: WhatsAppConfig{
		Provider: WhatsAppProviderCloud,
		Cloud: WhatsAppCloudConfig{
			PhoneNumberID: "111", AccessToken: "tok", Recipient: "20100000000",
		},
	}}
	if err := sink.SendImage(context.Background(), cfg, Event{Event: KindScreenshot}, testImage); err != nil {
		t.Fatalf("SendImage(): %v", err)
	}
	if !uploadHit {
		t.Fatal("the media upload never happened")
	}
	if uploadType != "image/png" {
		t.Fatalf("upload part type = %q, want image/png", uploadType)
	}
	if sent["type"] != "image" {
		t.Fatalf("message type = %v, want image", sent["type"])
	}
	image, _ := sent["image"].(map[string]any)
	if image["id"] != "media-77" {
		t.Fatalf("message referenced %v, want the uploaded media id", image["id"])
	}
	if caption, _ := image["caption"].(string); !strings.Contains(caption, "Demo Shop") {
		t.Fatalf("caption = %q", caption)
	}
}

func TestWhatsAppCallMeBotSendImageFallsBackToTheLink(t *testing.T) {
	var gotText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotText = r.URL.Query().Get("text")
		w.Write([]byte("Message queued"))
	}))
	defer server.Close()

	sink := NewWhatsAppSink(server.Client()).WithBaseURLs("", server.URL)
	cfg := Config{WhatsApp: WhatsAppConfig{
		Provider:  WhatsAppProviderCallMeBot,
		CallMeBot: WhatsAppCallMeConfig{Phone: "+20100000000", APIKey: "key"},
	}}
	if err := sink.SendImage(context.Background(), cfg, Event{Event: KindScreenshot}, testImage); err != nil {
		t.Fatalf("SendImage(): %v", err)
	}
	if !strings.Contains(gotText, testImage.LinkURL) {
		t.Fatalf("text = %q, want the login-less link", gotText)
	}
	if !strings.Contains(gotText, "Demo Shop") {
		t.Fatalf("text = %q, want the caption", gotText)
	}
}

/* ------------------------------------------------------------------ *
 * fakes
 * ------------------------------------------------------------------ */

// imageSink is a sink that carries pixels. The text-only side of the fan-out
// is exercised with notifier_test.go's recordingSink, which is exactly what a
// sink without SendImage looks like.
type imageSink struct {
	name string

	mu    sync.Mutex
	image Image
	event Event
}

func newImageSink(name string) *imageSink { return &imageSink{name: name} }

// unconfiguredSink is a text sink an operator has not filled in.
func unconfiguredSink(name string) *recordingSink {
	sink := newRecordingSink(name, 0)
	sink.configured = false
	return sink
}

func (s *imageSink) Name() string { return s.name }

func (s *imageSink) Configured(Config) bool { return true }

func (s *imageSink) Send(context.Context, Config, Event) error { return nil }

func (s *imageSink) SendImage(_ context.Context, _ Config, event Event, image Image) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.image = image
	s.event = event
	return nil
}

func (s *imageSink) received() Image {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.image
}
