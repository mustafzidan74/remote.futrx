package notify

import (
	"context"
	"strings"
	"time"
)

// KindClientMessage is the event an operator-composed message to a client
// travels under.
//
// Like KindTest and KindScreenshot it bypasses the per-event toggles and the
// global enable switch: somebody wrote a message and pressed send, and
// dropping it because the "run finished" toggle is off would be indefensible.
const KindClientMessage Kind = "clientMessage"

// messageFanOutTimeout bounds the whole synchronous fan-out so one wedged sink
// cannot hold the request open indefinitely.
const messageFanOutTimeout = 90 * time.Second

// SendMessage delivers one plain-text message to every configured sink and
// reports each outcome.
//
// The text travels in Summary, which is the field every sink already renders
// verbatim; nothing here formats, marks up, or truncates it beyond the limits
// each sink imposes on itself.
func (s *Service) SendMessage(ctx context.Context, event Event) []SinkResult {
	if s == nil || s.notifier == nil {
		return nil
	}
	if strings.TrimSpace(event.Summary) == "" {
		return nil
	}
	event.Event = KindClientMessage
	if event.At == 0 {
		event.At = s.now().UnixMilli()
	}
	fanOut, cancel := context.WithTimeout(ctx, messageFanOutTimeout)
	defer cancel()
	return s.notifier.SendTest(fanOut, event)
}

// SinksConfigured reports whether at least one sink could receive anything, so
// the UI can hide a send button instead of offering a guaranteed failure.
func (s *Service) SinksConfigured() bool {
	if s == nil {
		return false
	}
	return s.notifier.AnyConfigured()
}

// summaryBudget is how much of Summary a message-shaped sink carries. Agent
// output is clipped hard because it is a notification about work; a message an
// operator wrote for a client is the payload itself, so it gets whatever the
// sink can carry.
func summaryBudget(event Event, normal, full int) int {
	if event.Event == KindClientMessage {
		return full
	}
	return normal
}
