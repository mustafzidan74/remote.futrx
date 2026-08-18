package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

var (
	_ ImageSink            = (*WhatsAppSink)(nil)
	_ ConditionalImageSink = (*WhatsAppSink)(nil)
)

// CanSendImage reports whether the selected provider carries pixels. Only the
// Cloud API does, and only when no approved template is configured: Meta
// matches a template by its own approved header, so a template install can
// carry the caption but not the picture.
func (s *WhatsAppSink) CanSendImage(cfg Config) bool {
	whatsapp := cfg.Normalize().WhatsApp
	return whatsapp.Provider == WhatsAppProviderCloud &&
		strings.TrimSpace(whatsapp.Cloud.TemplateName) == ""
}

// SendImage delivers the picture through whichever provider is selected.
//
// Only the Cloud API can carry binary content: CallMeBot's whole interface is
// one GET with the message in the query string, so a picture reaches it as a
// link or not at all. That asymmetry is why the screenshot service mints a
// login-less link for text-only sinks rather than for every delivery.
func (s *WhatsAppSink) SendImage(ctx context.Context, cfg Config, event Event, image Image) error {
	whatsapp := cfg.Normalize().WhatsApp
	switch whatsapp.Provider {
	case WhatsAppProviderCloud:
		return s.sendCloudImage(ctx, whatsapp.Cloud, event, image)
	case WhatsAppProviderCallMeBot:
		return s.sendCallMeBot(ctx, whatsapp.CallMeBot, TextEventFor(event, image))
	default:
		return fmt.Errorf("no WhatsApp provider is selected")
	}
}

// sendCloudImage is Meta's two-step upload: POST the bytes to /media for a
// media id, then send a type=image message referencing it. A template-only
// configuration cannot carry an image at all — Meta matches templates by their
// approved header — so that install falls back to the text path.
func (s *WhatsAppSink) sendCloudImage(
	ctx context.Context,
	cfg WhatsAppCloudConfig,
	event Event,
	image Image,
) error {
	if strings.TrimSpace(cfg.TemplateName) != "" {
		return s.sendCloud(ctx, cfg, TextEventFor(event, image))
	}
	mediaID, err := s.uploadCloudMedia(ctx, cfg, image)
	if err != nil {
		return err
	}

	token := strings.TrimSpace(cfg.AccessToken)
	body, err := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                strings.TrimSpace(cfg.Recipient),
		"type":              "image",
		"image": map[string]any{
			"id":      mediaID,
			"caption": WhatsAppCaption(event, image),
		},
	})
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

// uploadCloudMedia posts the bytes to /media and returns the media id Meta
// answers with. The id is short-lived on Meta's side, which is why the message
// is sent immediately afterwards rather than stored.
func (s *WhatsAppSink) uploadCloudMedia(
	ctx context.Context,
	cfg WhatsAppCloudConfig,
	image Image,
) (string, error) {
	token := strings.TrimSpace(cfg.AccessToken)
	mime := strings.TrimSpace(image.MIME)
	if mime == "" {
		mime = "image/png"
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("messaging_product", "whatsapp"); err != nil {
		return "", fmt.Errorf("encode whatsapp upload: %w", err)
	}
	if err := writer.WriteField("type", mime); err != nil {
		return "", fmt.Errorf("encode whatsapp upload: %w", err)
	}
	// CreateFormFile hardcodes application/octet-stream; Meta rejects the
	// upload unless the part declares the real image type.
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename=%q`, imageFilename(image)))
	header.Set("Content-Type", mime)
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", fmt.Errorf("encode whatsapp upload: %w", err)
	}
	if _, err := part.Write(image.Data); err != nil {
		return "", fmt.Errorf("encode whatsapp upload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("encode whatsapp upload: %w", err)
	}

	endpoint := s.cloudBase + "/" + url.PathEscape(strings.TrimSpace(cfg.PhoneNumberID)) + "/media"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body.Bytes()))
	if err != nil {
		return "", fmt.Errorf("build whatsapp upload request: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := s.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("whatsapp media upload failed: %s", redactToken(err.Error(), token))
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf(
			"whatsapp media upload responded %d: %s",
			response.StatusCode,
			redactToken(truncate(strings.TrimSpace(string(payload)), 200), token),
		)
	}
	var decoded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil || strings.TrimSpace(decoded.ID) == "" {
		return "", fmt.Errorf("whatsapp media upload returned no media id")
	}
	return decoded.ID, nil
}

// WhatsAppCaption renders the plain-text caption that travels with a picture.
// WhatsApp does not render HTML, so nothing is escaped and nothing is marked up.
func WhatsAppCaption(event Event, image Image) string {
	var out strings.Builder
	out.WriteString("\U0001f4f7 ")
	out.WriteString(EventHeadline(event))
	if caption := strings.TrimSpace(image.Caption); caption != "" {
		out.WriteString("\n")
		out.WriteString(caption)
	}
	if link := strings.TrimSpace(event.URL); link != "" {
		out.WriteString("\n")
		out.WriteString(link)
	}
	return truncate(out.String(), whatsAppMessageLimit)
}
