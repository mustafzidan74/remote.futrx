package notify

import (
	"strings"
	"testing"
	"time"
)

func TestArabicMessagesTranslateFixedTextOnly(t *testing.T) {
	event := Event{
		Event:       KindRunFinished,
		ProjectName: "shop",
		ChatTitle:   "Fix checkout",
		Status:      StatusFinished,
		Summary:     "All tests pass.",
		URL:         "https://remote.example/chat/1",
		Language:    LanguageArabic,
	}

	telegram := TelegramMessage(event)
	for _, want := range []string{"انتهى تشغيل الوكيل", "<b>المشروع:</b> shop", "<b>الحالة:</b> انتهى", "فتح في Remote", "All tests pass."} {
		if !strings.Contains(telegram, want) {
			t.Fatalf("telegram message lacks %q:\n%s", want, telegram)
		}
	}
	whatsapp := WhatsAppMessage(event)
	if !strings.HasPrefix(whatsapp, "✅ انتهى تشغيل الوكيل — shop") {
		t.Fatalf("whatsapp message = %q", whatsapp)
	}

	event.Language = ""
	if got := TelegramMessage(event); !strings.Contains(got, "Agent run finished") || !strings.Contains(got, "Open in Remote") {
		t.Fatalf("english message changed: %s", got)
	}
}

func TestLanguageIsNormalizedAndPublished(t *testing.T) {
	cfg := Config{}.Apply(UpdateInput{Language: " AR "})
	if cfg.Language != LanguageArabic {
		t.Fatalf("language = %q, want ar", cfg.Language)
	}
	if public := cfg.Public(); public.Language != LanguageArabic {
		t.Fatalf("public language = %q", public.Language)
	}
	if got := (Config{Language: "fr"}).Normalize().Language; got != "" {
		t.Fatalf("unsupported language normalized to %q, want English", got)
	}
}

func TestArabicDigestSummary(t *testing.T) {
	from := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC).UnixMilli()
	to := time.Date(2026, time.August, 18, 0, 0, 0, 0, time.UTC).UnixMilli()
	digest := Digest{
		From:         from,
		To:           to,
		Runs:         3,
		TotalCostUSD: 1.5,
		Projects:     []DigestProject{{Name: "shop", Runs: 3, CostUSD: 1.5}},
		TopModel:     "claude-sonnet",
	}
	got := digestSummaryIn(LanguageArabic, digest, time.UTC)
	want := "استهلاك الأسبوع 11–17 أغسطس — الإجمالي $1.50 (التشغيلات: 3) · shop $1.50 (التشغيلات: 3) · النموذج الأكثر استخداما claude-sonnet"
	if got != want {
		t.Fatalf("digest summary\n got: %s\nwant: %s", got, want)
	}
	if english := digestSummaryIn("", digest, time.UTC); english != DigestSummary(digest, time.UTC) {
		t.Fatalf("english digest changed: %s", english)
	}
}
