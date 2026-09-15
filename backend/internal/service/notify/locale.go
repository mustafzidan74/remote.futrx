package notify

import (
	"fmt"
	"strings"
	"time"
)

// Notification language. Messages leave the platform — a phone, a group chat —
// so their language is a setting of the notification configuration, not of the
// person who happens to be signed in. English is the default and the empty
// value; the webhook carries machine-readable fields and is never translated.
const (
	LanguageEnglish = "en"
	LanguageArabic  = "ar"
)

// normalizeLanguage maps anything but a supported code to English ("").
func normalizeLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case LanguageArabic:
		return LanguageArabic
	default:
		return ""
	}
}

// arabic holds the fixed text of message-shaped notifications. Keys are the
// English the formatters write, so a missing entry degrades to English.
var arabic = map[string]string{
	// Headlines (EventHeadline).
	"Agent run finished":         "انتهى تشغيل الوكيل",
	"Agent run failed":           "فشل تشغيل الوكيل",
	"Agent needs your attention": "الوكيل يحتاج إلى انتباهك",
	"Scheduled task failed":      "فشلت المهمة المجدولة",
	"Scheduled task skipped":     "تم تخطي المهمة المجدولة",
	"Scheduled task ran":         "تم تشغيل المهمة المجدولة",
	"Project health critical":    "حالة المشروع حرجة",
	"Project health recovered":   "تعافت حالة المشروع",
	"Project health warning":     "تحذير بشأن حالة المشروع",
	"Client site down":           "موقع العميل متوقف",
	"Client site back up":        "عاد موقع العميل للعمل",
	"Client site warning":        "تحذير بشأن موقع العميل",
	"Remote system event":        "حدث في نظام Remote",
	"Weekly usage report":        "تقرير الاستهلاك الأسبوعي",
	"Preview screenshot":         "لقطة شاشة للمعاينة",
	"Message for the client":     "رسالة للعميل",
	"Test notification":          "إشعار تجريبي",
	"Remote notification":        "إشعار من Remote",
	// Field labels and links (Telegram).
	"Project":        "المشروع",
	"Chat":           "المحادثة",
	"Agent":          "الوكيل",
	"Status":         "الحالة",
	"Open in Remote": "فتح في Remote",
	// Status values.
	StatusFinished:   "انتهى",
	StatusFailed:     "فشل",
	StatusCancelled:  "أُلغي",
	StatusWaiting:    "في الانتظار",
	StatusSucceeded:  "نجح",
	StatusSkipped:    "تم التخطي",
	StatusHealthWarn: "تحذير",
	StatusHealthCrit: "حرج",
	StatusHealthOK:   "سليم",
	StatusStarted:    "بدأ",
	// Weekly digest.
	"Open Settings → Usage in Remote for the full breakdown.": "افتح الإعدادات ← الاستهلاك في Remote للاطلاع على التفاصيل كاملة.",
}

// localize returns the Arabic for a fixed English string when the language
// asks for it, and the English otherwise.
func localize(language, english string) string {
	if normalizeLanguage(language) == LanguageArabic {
		if translated, ok := arabic[english]; ok {
			return translated
		}
	}
	return english
}

// headline is EventHeadline in the event's language.
func headline(event Event) string {
	return localize(event.Language, EventHeadline(event))
}

var arabicMonths = [...]string{
	"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو",
	"يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر",
}

// formatDigestRangeArabic mirrors FormatDigestRange with Arabic month names
// and Latin digits: "11–17 أغسطس".
func formatDigestRangeArabic(fromMilli, toMilli int64, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	from := time.UnixMilli(fromMilli).In(location)
	last := time.UnixMilli(toMilli - 1).In(location)
	if toMilli <= fromMilli {
		last = from
	}
	day := func(t time.Time, withMonth, withYear bool) string {
		out := fmt.Sprintf("%d", t.Day())
		if withMonth {
			out += " " + arabicMonths[t.Month()-1]
		}
		if withYear {
			out += fmt.Sprintf(" %d", t.Year())
		}
		return out
	}
	if from.Year() != last.Year() {
		return day(from, true, true) + "–" + day(last, true, true)
	}
	if from.Month() != last.Month() {
		return day(from, true, false) + "–" + day(last, true, false)
	}
	return day(from, false, false) + "–" + day(last, true, false)
}

// digestSummaryArabic is DigestSummary in Arabic. Counts are written as
// "label: n" so no noun has to agree with its number.
func digestSummaryArabic(digest Digest, location *time.Location) string {
	var out strings.Builder
	out.WriteString("استهلاك الأسبوع ")
	out.WriteString(formatDigestRangeArabic(digest.From, digest.To, location))
	if digest.Empty() {
		out.WriteString(" — لا توجد تشغيلات للوكلاء.")
		return out.String()
	}
	out.WriteString(" — الإجمالي ")
	out.WriteString(formatUSD(digest.TotalCostUSD))
	out.WriteString(fmt.Sprintf(" (التشغيلات: %d)", digest.Runs))

	shown := digest.Projects
	overflow := 0
	if len(shown) > digestProjectsInMessage {
		overflow = len(shown) - digestProjectsInMessage
		shown = shown[:digestProjectsInMessage]
	}
	for _, project := range shown {
		out.WriteString(" · ")
		out.WriteString(project.Name)
		out.WriteString(" ")
		out.WriteString(formatUSD(project.CostUSD))
		out.WriteString(fmt.Sprintf(" (التشغيلات: %d)", project.Runs))
	}
	if overflow > 0 {
		out.WriteString(fmt.Sprintf(" · مشاريع أخرى: %d", overflow))
	}
	if model := strings.TrimSpace(digest.TopModel); model != "" {
		out.WriteString(" · النموذج الأكثر استخداما ")
		out.WriteString(model)
	}
	return out.String()
}

// digestSummaryIn renders the weekly report body in the configured language.
func digestSummaryIn(language string, digest Digest, location *time.Location) string {
	if normalizeLanguage(language) == LanguageArabic {
		return digestSummaryArabic(digest, location)
	}
	return DigestSummary(digest, location)
}
