# Arabic interface glossary and style

Every entry in `catalog/ar.json` follows this file. Change a term here first,
then update the catalog; never let two translations of one term coexist.

## Style

- **Register:** Modern Standard Arabic, plain and short. Prefer the word an
  Egyptian developer already uses when MSA would sound stiff.
- **Buttons and menu items:** verbal nouns (مصادر), not imperatives —
  حفظ، إلغاء، حذف، إنشاء مشروع، إعادة تشغيل. Placeholders and hints may
  address the user directly (ابحث في…، اكتب…).
- **No diacritics**, except where a word is ambiguous without them.
- **Digits stay Latin** (0-9). Units stay as developers read them: MB, GB,
  CPU, RAM, ms, s, px.
- **Punctuation:** Arabic comma `،`, semicolon `؛`, question mark `؟`. Keep
  ellipsis `…`, bullets `·`, arrows and em dashes as in the source.
- **Placeholders:** every `{n}` in a key appears the same number of times in
  its translation, in the order the numbers should be read.
- **Fragments:** some keys are pieces of one sentence split around code or a
  link (`". Remote stores it privately and never displays it again."`). The
  pieces render in source order; translate each so the whole reads naturally
  in Arabic, and keep leading/trailing punctuation where the source has it.
- **Title case:** Arabic has none; do not imitate it.

## Never translate

Product and vendor names, protocols and file formats, and anything the user
types or copies literally:

Claude, Codex, Kimi, Antigravity, MiniMax, OpenAI, Anthropic, Google, Gemini,
Groq, OpenRouter, Ollama, GitHub, Playwright, Lighthouse, Chromium, Docker,
LXD, Caddy, WordPress, WooCommerce, Telegram, WhatsApp, CallMeBot, Slack,
UptimeRobot, Better Stack, Jira, MCP, SSH, HTTP(S), URL, API, JSON, YAML,
TOML, CSV, PDF, PNG, TOTP, 2FA (use «المصادقة الثنائية» in prose, keep 2FA in
short labels), VAPID, IDE, CLI, npm, git, Remote (the product name), commands,
paths, environment variables, keyboard keys (Ctrl, Enter, Esc).

## Terms

| English | Arabic | Notes |
| --- | --- | --- |
| agent | وكيل (الوكلاء) | the AI CLI that does the work |
| workspace | مساحة عمل | |
| project | مشروع (المشاريع) | |
| chat | محادثة (المحادثات) | |
| thread | سلسلة المحادثة | only where "chat" does not fit |
| prompt | طلب | a message sent to an agent |
| run | تشغيل | one agent turn; "running" = قيد التشغيل |
| turn | دورة | |
| model | نموذج | |
| provider | مزوّد | |
| skill | مهارة (المهارات) | |
| playbook | دليل تشغيل (أدلة التشغيل) | |
| snippet | مقتطف (المقتطفات) | |
| template | قالب | |
| snapshot | لقطة (اللقطات) | |
| trash | سلة المهملات | "move to Trash" = نقل إلى سلة المهملات |
| restore | استعادة | |
| purge | حذف نهائي | |
| preview | معاينة | |
| share link | رابط مشاركة | |
| secret | سر (الأسرار) | |
| secrets vault | خزنة الأسرار | |
| environment variable | متغير بيئة | |
| endpoint | نقطة اتصال | |
| container | حاوية | |
| terminal | الطرفية | |
| file manager | مدير الملفات | |
| autopilot | الطيار الآلي | |
| auto-test | الاختبار التلقائي | |
| team mode | وضع الفريق | |
| reviewer / tester / implementer | المراجع / المختبِر / المنفِّذ | |
| deploy | نشر | "deploy access" = صلاحية النشر |
| schedule / scheduled task | جدولة / مهمة مجدولة | |
| routing (model routing) | توجيه النماذج | "Auto" routing = تلقائي |
| usage | الاستهلاك | tokens, cost |
| tokens | رموز | "input/output tokens" = رموز الإدخال/الإخراج |
| cost | التكلفة | |
| audit log | سجل التدقيق | |
| settings | الإعدادات | |
| appearance | المظهر | |
| theme: system / dark / light | النظام / داكن / فاتح | |
| notifications | الإشعارات | |
| security | الأمان | |
| session | جلسة | |
| sign in / sign out | تسجيل الدخول / تسجيل الخروج | |
| admin / member | مسؤول / عضو | |
| user | مستخدم | |
| reply language | لغة الرد | |
| resources | الموارد | CPU/memory/disk limits |
| health | الحالة الصحية | "healthy" = سليم |
| client site | موقع العميل | |
| portal | بوابة | client portal = بوابة العميل |
| webhook | Webhook | keep Latin |
| repository | مستودع | |
| branch / commit / pull request | فرع / commit / طلب دمج | commit stays Latin |
| browser (agent browser) | المتصفح (متصفح الوكيل) | |
| screenshot | لقطة شاشة | |
| visual diff | مقارنة بصرية | |
| search | بحث | |
| filter | تصفية | |
| loading… | جارٍ التحميل… | |
| saving / saved | جارٍ الحفظ / تم الحفظ | |
| failed | فشل | |
| enabled / disabled | مفعّل / معطّل | |
| retry | إعادة المحاولة | |
| copy / copied | نسخ / تم النسخ | |
| upload | رفع | |
| download | تنزيل | |
| host / server (the machine) | الخادم | one word for both; never المضيف |
| composer | مربع الكتابة | |
| pill (agent/model pill) | زر | زر الوكيل، زر النموذج |
| loop (team / follow-up loop) | حلقة (الحلقات) | one pass of it = جولة |
| rewind | إرجاع | |
| free tier | الباقة المجانية | |
| provider pool | مجموعة المزوّدين | pooled job = مهمة المجموعة |
| auxiliary model | النموذج المساعد | |
| job | مهمة (المهام) | |
| quota / rate limit | الحصة / حد المعدل | |
| credentials | بيانات الاعتماد | |
| access / bearer token | رمز وصول | same رمز as usage tokens; context separates them |
| recovery code | رمز الاسترداد | |
| device / auth code | الكود | |
| notification sink / destination | وجهة الإشعارات / الوجهة | |
| heartbeat | نبضة | |
| header | ترويسة (الترويسات) | |
| port | منفذ | |
| certificate | الشهادة | |
| check (site check) / audit (Lighthouse) | فحص | not the audit log |
| baseline | خط الأساس | |
| interval / grace period / timeout | الفاصل الزمني / مهلة السماح / المهلة | |
| retention window | مدة الاحتفاظ | |
| override (per project) | تخصيص | |
| fleet default | الافتراضي العام | |
| disk quota / storage pool | حصة القرص / مجمع التخزين | |
| base image | الصورة الأساسية | |
| release | الإصدار | |
| danger zone | منطقة الخطر | |
| actor / action / target (audit) | المنفذ / الإجراء / الهدف | |
| uptime (site) / uptime (host) | التوفر / مدة التشغيل | |
| transcript | سجل المحادثة | |
| transcription / dictation | تحويل الصوت إلى نص / الإملاء الصوتي | |
| scope | النطاق | |
| label (a field or name) / label (GitHub) | التسمية / وسم | |
| issue (GitHub) | Issue | keep Latin |
| push (git) / clone | دفع / استنساخ | push a heartbeat = إرسال |
| rotate / revoke | تجديد / إبطال | |
| placeholder (`${KEY}`) | عنصر نائب | |
| theme | السمة | |
| not configured | غير مهيأ | |
| invalid / required / not found / unavailable | غير صالح / مطلوب / غير موجود / غير متاح | |

## Split sentences that depend on each other

- MCP settings: `{agents}` + «لا» + «يدعم MCP على هذه المنصة، لذا لا يصل أي شيء من
  هنا إلى» + «هذا الوكيل» / «هؤلاء الوكلاء» + `.`
- Client sites: «يقرأ من أسرار كل مشروع» + `HESTIA_DOMAIN` + «وما يشبهه، ويربط…».
- GitHub webhook events: the word «أحداث» lives in the `; send the` piece, so
  `events.` is just `.`.

## Known source issues (phase 3)

Plurals glued in code render wrong in Arabic until the source uses one `{n}` key:
ProjectResourceLimits.tsx (`"s"` after "running container"),
ProjectVisualSection.tsx (page/pages), ScheduleHistoryPanel.tsx, CollaborationCard
(`tool`/`tools`).
