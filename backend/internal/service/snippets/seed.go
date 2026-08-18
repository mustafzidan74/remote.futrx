package snippets

// Seed returns the client message templates a user's library starts with.
//
// It is written once, on the first read that finds no document for that user;
// from then on the library belongs to them and is never rewritten from here.
// A user who deletes every template keeps an empty library, because the
// document exists — deleting is a decision, not a reason to re-seed.
//
// Only client templates are seeded. A personal prompt library has to be
// personal to be worth anything, and guessing at somebody's prompts would
// just be four entries to delete; the client-facing messages, by contrast,
// are the same four notes every freelancer writes, in the two languages this
// platform is used in.
func Seed(now int64) []Snippet {
	templates := []struct {
		id       string
		title    string
		shortcut string
		tags     []string
		en       string
		ar       string
	}{
		{
			id:       "site-is-live",
			title:    "🚀 Site is live",
			shortcut: "live",
			tags:     []string{"client", "launch"},
			en: "Hello {{clientName}},\n\n" +
				"{{projectName}} is now live at {{previewUrl}}.\n\n" +
				"Please take a look and tell me if anything needs adjusting. " +
				"You can follow the project status any time at {{portalUrl}}.\n\n" +
				"Best regards",
			ar: "مرحباً {{clientName}}،\n\n" +
				"تم إطلاق {{projectName}} وهو متاح الآن على {{previewUrl}}.\n\n" +
				"يرجى الاطلاع عليه وإخباري إن كان هناك أي تعديل مطلوب. " +
				"ويمكنك متابعة حالة المشروع في أي وقت عبر {{portalUrl}}.\n\n" +
				"مع التحية",
		},
		{
			id:       "delivery-note",
			title:    "📦 Delivery note",
			shortcut: "delivery",
			tags:     []string{"client", "delivery"},
			en: "Hello {{clientName}},\n\n" +
				"This is the delivery for {{projectName}}, dated {{date}}.\n\n" +
				"What is included:\n" +
				"- \n- \n\n" +
				"Preview: {{previewUrl}}\n" +
				"Project status page: {{portalUrl}}\n\n" +
				"Please review it and send me your notes within the agreed review window.\n\n" +
				"Best regards",
			ar: "مرحباً {{clientName}}،\n\n" +
				"هذا تسليم مشروع {{projectName}} بتاريخ {{date}}.\n\n" +
				"ما يتضمنه التسليم:\n" +
				"- \n- \n\n" +
				"رابط المعاينة: {{previewUrl}}\n" +
				"صفحة حالة المشروع: {{portalUrl}}\n\n" +
				"يرجى مراجعته وإرسال ملاحظاتك خلال فترة المراجعة المتفق عليها.\n\n" +
				"مع التحية",
		},
		{
			id:       "request-credentials",
			title:    "🔑 Request for credentials",
			shortcut: "creds",
			tags:     []string{"client", "access"},
			en: "Hello {{clientName}},\n\n" +
				"To continue with {{projectName}} I need access to:\n" +
				"- Hosting control panel (or SSH)\n" +
				"- Domain / DNS management\n" +
				"- The site administrator account\n\n" +
				"Please send them through a secure channel, or create a temporary account for me " +
				"and revoke it when the work is done. I do not need your personal passwords.\n\n" +
				"Best regards",
			ar: "مرحباً {{clientName}}،\n\n" +
				"لمتابعة العمل على {{projectName}} أحتاج صلاحية الوصول إلى:\n" +
				"- لوحة تحكم الاستضافة (أو SSH)\n" +
				"- إدارة النطاق و DNS\n" +
				"- حساب مدير الموقع\n\n" +
				"يرجى إرسالها عبر قناة آمنة، أو إنشاء حساب مؤقت لي وإلغاؤه بعد انتهاء العمل. " +
				"ولا أحتاج إلى كلمات المرور الشخصية الخاصة بك.\n\n" +
				"مع التحية",
		},
		{
			id:       "quotation-summary",
			title:    "🧾 Quotation summary",
			shortcut: "quote",
			tags:     []string{"client", "sales"},
			en: "Hello {{clientName}},\n\n" +
				"Quotation for {{projectName}}, {{date}}.\n\n" +
				"Scope:\n" +
				"- \n- \n\n" +
				"Timeline: \n" +
				"Total: \n" +
				"Payment: 50% to start, 50% on delivery.\n\n" +
				"This quotation is valid for 14 days. Confirm and I will schedule the work.\n\n" +
				"Best regards",
			ar: "مرحباً {{clientName}}،\n\n" +
				"عرض سعر مشروع {{projectName}} بتاريخ {{date}}.\n\n" +
				"نطاق العمل:\n" +
				"- \n- \n\n" +
				"المدة الزمنية: \n" +
				"الإجمالي: \n" +
				"طريقة الدفع: 50% عند البدء و50% عند التسليم.\n\n" +
				"هذا العرض ساري لمدة 14 يوماً. بالتأكيد يمكنني جدولة العمل.\n\n" +
				"مع التحية",
		},
	}

	out := make([]Snippet, 0, len(templates))
	for _, template := range templates {
		out = append(out, Normalize(Snippet{
			ID:        template.id,
			Title:     template.title,
			Audience:  AudienceClient,
			Variants:  Variants{EN: template.en, AR: template.ar},
			Tags:      template.tags,
			Shortcut:  template.shortcut,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}
	return out
}
