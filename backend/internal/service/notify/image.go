package notify

import (
	"context"
	"strings"
	"time"
)

// KindScreenshot is the event a preview screenshot travels under. It is a
// user-initiated action like KindTest, so it bypasses the per-event toggles:
// somebody pressed "send this picture", and silently dropping it because the
// "run finished" toggle is off would be indefensible.
const KindScreenshot Kind = "screenshot"

// imageRequestTimeout is the per-attempt budget for a picture. It is larger
// than requestTimeout because the body is megabytes rather than a JSON blob,
// and a phone-network upload to Meta's media endpoint is not a 10-second job.
const imageRequestTimeout = 60 * time.Second

// imageFanOutTimeout bounds the whole synchronous fan-out, retries included,
// so one wedged upload cannot hold the capture request open indefinitely.
const imageFanOutTimeout = 3 * time.Minute

// Image is one picture delivered alongside an event.
//
// LinkURL is the fallback for sinks that carry text only. It is minted by the
// caller and is deliberately optional: a deployment whose only sink can send
// pictures never publishes a login-less link at all.
type Image struct {
	Filename string
	MIME     string
	Data     []byte
	Caption  string
	LinkURL  string
}

// ImageSink is the optional half of Sink: a sink that can deliver a picture
// implements it, and everything else falls back to a text message carrying the
// caption and, when one exists, the link.
type ImageSink interface {
	Sink
	SendImage(ctx context.Context, cfg Config, event Event, image Image) error
}

// ConditionalImageSink is implemented by a sink whose ability to carry a
// picture depends on how it is configured. The WhatsApp sink is the reason it
// exists: the Cloud API uploads media, CallMeBot is one GET with the message in
// the query string, and both live behind the same sink.
type ConditionalImageSink interface {
	CanSendImage(cfg Config) bool
}

// canSendImage answers "will this sink deliver actual pixels under cfg?".
func canSendImage(sink Sink, cfg Config) bool {
	imageSink, ok := sink.(ImageSink)
	if !ok {
		return false
	}
	if conditional, ok := imageSink.(ConditionalImageSink); ok {
		return conditional.CanSendImage(cfg)
	}
	return true
}

// NeedsPublicLink reports whether a configured sink can only deliver text, so
// a picture would reach it as a link or not at all. The screenshot service asks
// before minting one: publishing a login-less link for a Telegram-only install
// would widen exposure for nothing.
func (n *Notifier) NeedsPublicLink() bool {
	if n == nil {
		return false
	}
	cfg := n.currentConfig()
	for _, sink := range n.sinks {
		if sink.Configured(cfg) && !canSendImage(sink, cfg) {
			return true
		}
	}
	return false
}

// AnyConfigured reports whether at least one sink could receive anything.
func (n *Notifier) AnyConfigured() bool {
	if n == nil {
		return false
	}
	cfg := n.currentConfig()
	for _, sink := range n.sinks {
		if sink.Configured(cfg) {
			return true
		}
	}
	return false
}

// SendImage delivers one picture to every configured sink synchronously and
// reports each outcome.
//
// It bypasses the queue for the same reason SendTest does: the person who
// pressed the button is waiting for the answer, and "queued" is not an answer
// when a bot token is wrong. Sinks that cannot carry binary content receive the
// ordinary text message instead, with the caption as its summary.
func (n *Notifier) SendImage(ctx context.Context, event Event, image Image) []SinkResult {
	if n == nil {
		return nil
	}
	if strings.TrimSpace(image.MIME) == "" {
		image.MIME = "image/png"
	}
	cfg := n.currentConfig()
	results := make([]SinkResult, 0, len(n.sinks))
	for _, sink := range n.sinks {
		result := SinkResult{Sink: sink.Name(), Configured: sink.Configured(cfg)}
		if !result.Configured {
			result.Error = "not configured"
			results = append(results, result)
			continue
		}
		if err := n.sendImageWithRetry(ctx, sink, cfg, event, image); err != nil {
			result.Error = err.Error()
		} else {
			result.Delivered = true
		}
		results = append(results, result)
	}
	return results
}

func (n *Notifier) sendImageWithRetry(
	ctx context.Context,
	sink Sink,
	cfg Config,
	event Event,
	image Image,
) error {
	if imageSink, ok := sink.(ImageSink); ok {
		var lastErr error
		for attempt := 0; attempt < deliveryAttempts; attempt++ {
			if attempt > 0 && !n.wait(ctx, n.backoffFor(attempt-1)) {
				return lastErr
			}
			attemptCtx, cancel := context.WithTimeout(ctx, imageRequestTimeout)
			err := imageSink.SendImage(attemptCtx, cfg, event, image)
			cancel()
			if err == nil {
				return nil
			}
			lastErr = err
		}
		return lastErr
	}
	return n.sendWithRetry(ctx, sink, cfg, TextEventFor(event, image))
}

// TextEventFor folds a picture into the plain event a text-only sink can
// deliver: the caption becomes the summary and the login-less link becomes the
// event's URL, replacing the chat deep link that would otherwise send an
// unauthenticated recipient to a sign-in page.
func TextEventFor(event Event, image Image) Event {
	out := event
	if caption := strings.TrimSpace(image.Caption); caption != "" {
		out.Summary = caption
	}
	if link := strings.TrimSpace(image.LinkURL); link != "" {
		out.URL = link
	}
	return out
}

// SendImage is the service-level entry point used by the screenshot service.
func (s *Service) SendImage(ctx context.Context, event Event, image Image) []SinkResult {
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, imageFanOutTimeout)
	defer cancel()
	if event.Event == "" {
		event.Event = KindScreenshot
	}
	if event.At == 0 {
		event.At = s.now().UnixMilli()
	}
	return s.notifier.SendImage(ctx, event, image)
}

// ImageSinksConfigured reports whether anything at all could receive a
// picture, so the UI can hide the "send it" buttons instead of offering a
// guaranteed failure.
func (s *Service) ImageSinksConfigured() bool {
	if s == nil {
		return false
	}
	return s.notifier.AnyConfigured()
}

// NeedsPublicLink reports whether a text-only sink is configured.
func (s *Service) NeedsPublicLink() bool {
	if s == nil {
		return false
	}
	return s.notifier.NeedsPublicLink()
}
