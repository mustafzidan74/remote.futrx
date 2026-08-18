package notify

import (
	"context"
	"strings"
	"testing"
)

func TestSendMessageFansOutPlainText(t *testing.T) {
	sink := newRecordingSink("telegram", 0)
	// Notifications are switched off and every per-event toggle is false: a
	// message somebody wrote by hand still goes out, exactly like a test send.
	config := DefaultConfig()
	config.Enabled = false
	config.Events = EventToggles{}
	service := newTestService(t, &memoryStore{config: config}, sink)

	results := service.SendMessage(context.Background(), Event{
		ProjectName: "Acme Shop",
		Summary:     "Hello, the site is live.",
	})

	if len(results) != 1 || !results[0].Delivered {
		t.Fatalf("results = %+v, want one delivered sink", results)
	}
	_, delivered := sink.counts()
	if len(delivered) != 1 {
		t.Fatalf("delivered %d events, want 1", len(delivered))
	}
	event := delivered[0]
	switch {
	case event.Event != KindClientMessage:
		t.Fatalf("kind = %q, want %q", event.Event, KindClientMessage)
	case event.Summary != "Hello, the site is live.":
		t.Fatalf("summary = %q, want the text unchanged", event.Summary)
	case event.At == 0:
		t.Fatal("the event was not timestamped")
	}
}

func TestSendMessageIgnoresAnEmptyBody(t *testing.T) {
	sink := newRecordingSink("telegram", 0)
	service := newTestService(t, &memoryStore{config: DefaultConfig()}, sink)

	if results := service.SendMessage(context.Background(), Event{Summary: "   "}); results != nil {
		t.Fatalf("results = %+v, want nothing sent", results)
	}
	if _, delivered := sink.counts(); len(delivered) != 0 {
		t.Fatalf("delivered %d events for an empty message", len(delivered))
	}
}

func TestClientMessagesKeepTheirWholeText(t *testing.T) {
	// A run summary is clipped hard because it is a notification about work;
	// a message an operator wrote is the payload and must survive.
	long := strings.Repeat("ب", 800)
	message := WhatsAppMessage(Event{Event: KindClientMessage, Summary: long})
	notification := WhatsAppMessage(Event{Event: KindRunFinished, Summary: long})

	if len([]rune(message)) <= len([]rune(notification)) {
		t.Fatalf(
			"a client message (%d runes) is not longer than a run notification (%d runes)",
			len([]rune(message)), len([]rune(notification)),
		)
	}
	if !strings.Contains(TelegramMessage(Event{Event: KindClientMessage, Summary: long}), strings.Repeat("ب", 800)) {
		t.Fatal("Telegram truncated a client message that fits its own limit")
	}
}

func TestSinksConfiguredFollowsTheConfiguration(t *testing.T) {
	sink := newRecordingSink("telegram", 0)
	service := newTestService(t, &memoryStore{config: DefaultConfig()}, sink)
	if !service.SinksConfigured() {
		t.Fatal("a configured sink reported as unavailable")
	}

	sink.configured = false
	if service.SinksConfigured() {
		t.Fatal("an unconfigured sink reported as available")
	}

	var missing *Service
	if missing.SinksConfigured() {
		t.Fatal("a nil service reported a configured sink")
	}
}
