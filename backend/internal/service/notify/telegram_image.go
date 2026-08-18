package notify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

var _ ImageSink = (*TelegramSink)(nil)

// telegramCaptionLimit is the Bot API ceiling for a photo caption. It is much
// smaller than the sendMessage limit, so the caption is trimmed rather than
// letting a long project name fail the whole delivery.
const telegramCaptionLimit = 1000

// SendImage posts the picture through sendPhoto. Telegram renders the photo
// inline in the chat, which is the entire point of this path: a link would
// make the recipient sign in to see what the agent built.
func (s *TelegramSink) SendImage(ctx context.Context, cfg Config, event Event, image Image) error {
	token := strings.TrimSpace(cfg.Telegram.BotToken)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("chat_id", strings.TrimSpace(cfg.Telegram.ChatID)); err != nil {
		return fmt.Errorf("encode telegram photo: %w", err)
	}
	if err := writer.WriteField("caption", TelegramCaption(event, image)); err != nil {
		return fmt.Errorf("encode telegram photo: %w", err)
	}
	if err := writer.WriteField("parse_mode", "HTML"); err != nil {
		return fmt.Errorf("encode telegram photo: %w", err)
	}
	part, err := writer.CreateFormFile("photo", imageFilename(image))
	if err != nil {
		return fmt.Errorf("encode telegram photo: %w", err)
	}
	if _, err := part.Write(image.Data); err != nil {
		return fmt.Errorf("encode telegram photo: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("encode telegram photo: %w", err)
	}

	endpoint := s.baseURL + "/bot" + url.PathEscape(token) + "/sendPhoto"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body.Bytes()))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := s.client.Do(request)
	if err != nil {
		// Never surface the URL: it embeds the bot token.
		return fmt.Errorf("telegram request failed: %s", redactToken(err.Error(), token))
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf(
			"telegram responded %d: %s",
			response.StatusCode,
			redactToken(strings.TrimSpace(string(payload)), token),
		)
	}
	return nil
}

// TelegramCaption renders the photo caption as Telegram-flavoured HTML. Every
// interpolated value is escaped, so a project name cannot inject markup.
func TelegramCaption(event Event, image Image) string {
	var out strings.Builder
	out.WriteString("\U0001f4f7 <b>")
	out.WriteString(escapeTelegramHTML(EventHeadline(event)))
	out.WriteString("</b>")
	if caption := strings.TrimSpace(image.Caption); caption != "" {
		out.WriteString("\n")
		out.WriteString(escapeTelegramHTML(caption))
	}
	if link := strings.TrimSpace(event.URL); link != "" {
		out.WriteString("\n\n<a href=\"")
		out.WriteString(escapeTelegramHTML(link))
		out.WriteString("\">Open in Remote</a>")
	}
	return truncate(out.String(), telegramCaptionLimit)
}

// imageFilename keeps the multipart part name sane when a caller supplies
// none: Telegram and Meta both reject an empty filename.
func imageFilename(image Image) string {
	name := strings.TrimSpace(image.Filename)
	if name == "" {
		return "screenshot.png"
	}
	// Never let a stored name introduce path separators into a form part.
	name = strings.ReplaceAll(name, "/", "-")
	return strings.ReplaceAll(name, "\\", "-")
}
