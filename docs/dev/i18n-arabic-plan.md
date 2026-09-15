# Arabic UI plan (translation + RTL)

> **Status:** phase 1 implemented on `feat/i18n-ar` — see "Phase 1 as built"
> at the end.

Execution plan for a fully Arabic interface of the fork. Written to be
followed by an implementation session; each phase ends in something
shippable on its own.

## The one design rule: no string edits in upstream files

The fork re-syncs with `futrx-com/remote.futrx` `qa` regularly (the last sync
was 793 commits and 184 conflicts). Any approach that rewrites the ~900 UI
strings in 225 `.tsx` files into `t("...")` calls, or swaps physical Tailwind
classes for logical ones across the tree, turns every future sync into a
hand-merge of every component. So:

- **Translation happens at the JSX runtime, keyed by the English text.**
  Source files stay byte-identical to upstream. A missing translation falls
  back to English (visible, never broken), and a CI check lists the strings
  upstream added or reworded since the last catalog update.
- **RTL happens in the CSS build**, not in class names. `postcss-rtlcss`
  emits `[dir="rtl"]` counterparts for every physical utility Tailwind
  generates. Fork-owned files may use logical classes (`ms-`, `pe-`, `start-`)
  freely; upstream files are left alone.

Trade-off accepted: when upstream rewords a string, that string shows English
until the catalog catches up. That is the price of clean syncs, and the check
in phase 1.6 makes it a five-minute chore per sync rather than a surprise.

## Current state (measured on `main` @ e01a4153)

| Fact | Value |
| --- | --- |
| Frontend | Preact 10 + Vite 8 (`@preact/preset-vite`), Tailwind 3.4, `jsxImportSource: "preact"` |
| Component files | 225 `.tsx` |
| Literal UI strings (rough grep: JSX text + `title/placeholder/aria-label/label/description` attrs) | ~900: settings 359, chat 251, projects 127, sidebar 27, home 21, search 21, preview 12, auth 8 |
| Strings outside JSX (config arrays, `confirm({...})`, `alert()`, `document.title`, manifest) | not counted; the extractor in 1.4 finds them |
| Physical spacing classes (`ml/mr/pl/pr/left/right-*`) | 111 sites; alignment/border-side/`rounded-l|r` 145 sites; logical classes already used: 7 |
| Existing bidi | `ui/chat/markdown/bidi.ts` (`isRtlText`, per-message `dir`), code blocks and links forced `dir="ltr"`, textarea `dir="auto"` |
| `index.html` | `lang="en"`, no `dir` |
| Date/number formatting | 33 `toLocale*` / `Intl` sites, no locale passed (browser default) |
| User settings | `appearance.theme` (system/dark/light) with a localStorage bootstrap in `index.html` for first paint — the language switch copies this pattern |
| Agent output language | already solved server-side (`agentprefs.replyLanguage`, per-user override); out of scope here |
| Backend strings reaching the UI | ~1,900 `errors.New`/`SendErr` messages; notification templates (Telegram/WhatsApp) in `service/notify` |
| Docs | `docs/whats-new.html` already ar/en; the rest English |

## Decisions (defaults; change before phase 2 if wanted)

| Decision | Default | Why |
| --- | --- | --- |
| Language setting | `appearance.language`: `"auto" \| "en" \| "ar"`, per user, server-stored, localStorage-bootstrapped | Same shape as theme; `auto` follows `navigator.language` |
| Digits | Latin (`0-9`) via `ar-EG-u-nu-latn` | Ports, sizes, timestamps and token counts read alongside code; Eastern digits would mismatch terminal output |
| Font | Cairo, self-hosted woff2, applied only under `[lang="ar"]` | User's stated preference; no CDN because the app is self-hosted |
| Direction of code, terminal, file paths, URLs, markdown code, log tails | always LTR | already the rule in `bidi.ts`; extend to xterm container and tool-call output |
| Mixed content (English project names / chat titles inside Arabic UI) | `dir="auto"` + `unicode-bidi: plaintext` on user-content nodes | already the pattern in chat; apply to sidebar rows and headers |
| Plural rules | Arabic 6-form via `Intl.PluralRules("ar")` | `{n} chats` → zero/one/two/few/many/other |
| Backend error messages | translated frontend-side through the same catalog for the top ~150 messages; the rest stay English | avoids touching Go strings upstream owns |
| Notifications (Telegram/WhatsApp) | phase 6, opt-in | separate language setting; low priority |

## Phase 1 — infrastructure (no visible change yet)

1. **JSX runtime shim** — `frontend/src/i18n/jsx-runtime.ts` + `jsx-dev-runtime.ts`
   re-exporting preact's `jsx`, `jsxs`, `jsxDEV`, `Fragment`, with a wrapper
   that runs `t()` over: string children, and the props `title`,
   `placeholder`, `aria-label`, `aria-description`, `alt`, `label`. Point
   `jsxImportSource` (tsconfig + `preact()` preset option) at it. Cost: one
   `Map.get` per string node.
   - Skip when locale is `en`. Skip strings that are only digits/punctuation,
     URLs, paths (`/`, `~`, `.`), or shorter than 2 letters.
   - Interpolation: before lookup, normalise runs of digits to `{n}` and
     keep the captured values; catalog entries may be a string or a plural
     object `{zero, one, two, few, many, other}`. Restore values on output.
   - `t()` also exported for the few non-JSX sites (`alert`, `confirm`
     option objects, `document.title`, manifest name).
2. **Locale state** — `frontend/src/i18n/locale.ts`: resolves
   `auto|en|ar` → `en|ar`, sets `document.documentElement.lang` and `dir`,
   emits a change event so mounted components re-render (a Preact context
   `LocaleProvider` around `App`, value `{locale, dir, t}`). Bootstrap
   snippet in `index.html` sets `lang`/`dir` from localStorage before first
   paint, mirroring the theme bootstrap. Storage key added to
   `config/storageKeys.ts`.
3. **Setting** — backend `usersettings.Appearance.Language` (validated
   enum, default `auto`), handler + frontend `models/settings.ts`,
   `api/settingsApi.ts` normaliser, Settings → Appearance segmented control
   (fork-owned file `AppearanceSettings.tsx`; label it in both languages).
4. **Extractor** — `frontend/scripts/i18n-extract.ts` (TypeScript compiler
   API): walks `src/**/*.tsx|ts`, collects JSX text, the attribute set above,
   and string properties named `label|title|description|hint|message|confirmLabel|pendingLabel|placeholder`
   in object literals; writes `src/i18n/catalog/en.keys.json` (sorted,
   with file references as comments in a sidecar). Same normalisation as
   the runtime. `npm run i18n:extract`.
5. **Catalog loading** — `src/i18n/catalog/ar.json`, imported statically
   (~100 KB is fine; lazy-load later if it grows). Dev-only pseudo-locale
   `qps` (`?lang=qps`) that brackets every string the shim reaches in `⟦ ⟧`,
   so text on screen *without* brackets is text the shim never sees and needs
   a catalog-independent fix (a `t()` call or a fork-owned wrapper).
6. **Sync check** — `npm run i18n:check`: extract, diff against `ar.json`,
   print new/orphaned keys, exit non-zero in CI on new keys (warning only at
   first; make it blocking once the catalog is complete). Add to
   `.github/workflows/ci.yml` frontend job.
7. **RTL CSS** — add `postcss-rtlcss` after tailwind in `postcss.config.js`
   (`mode: "combined"`, so one bundle serves both directions under
   `[dir="rtl"]`). Verify bundle size delta; expect +15–25 % of CSS.
8. **Tests** — unit tests for the normaliser, plural selection, the
   skip rules, and the shim (renders `<button title="Sign in">Sign in</button>`
   as Arabic under `ar`, unchanged under `en`). Playwright smoke: `?lang=ar`
   query override for screenshots.

Exit: app runs unchanged in `en`; switching to `ar` flips direction and font
with English text (catalog empty).

## Phase 2 — the catalog

1. Run the extractor; expect 900–1,300 keys. Translate in batches of ~150
   by area, in this order (what users see first): auth/login → sidebar →
   chat thread + composer → home dashboard → projects page → settings →
   search/preview/schedules/team → tool-call renderers → remaining.
2. Glossary first (one file, `src/i18n/GLOSSARY.md`), agreed before batch 1:
   agent (وكيل), workspace (مساحة عمل), project (مشروع), chat (محادثة), run
   (تشغيل), skill (مهارة), playbook (دليل تشغيل), snippet (مقتطف), snapshot
   (لقطة), trash (سلة المهملات), preview (معاينة), secret (سر), vault
   (الخزنة), endpoint (نقطة اتصال), container (حاوية), autopilot (طيار
   آلي), team mode (وضع الفريق), deploy (نشر). Product names (Claude, Codex,
   Kimi, Antigravity, MiniMax, Playwright, Lighthouse, GitHub) stay Latin.
3. Tone: Modern Standard Arabic, short imperative labels, no diacritics,
   Egyptian-friendly wording where MSA is stiff. Keep placeholders `{n}`.
4. Top backend messages: grep the ~150 `SendErr`/`errors.New` strings that
   handlers return for user actions (auth, projects, secrets, shares,
   schedules), add them to the catalog as keys — they render through the
   same shim when the UI shows `error.message`.
5. Review: the owner reads each batch in the running app (pseudo-locale
   off) before the next batch starts.

Exit: `npm run i18n:check` reports 0 missing keys.

## Phase 3 — RTL polish (screenshot-driven)

Walk every screen in `ar` at desktop and phone widths; fix in fork-owned
files or with `[dir="rtl"]` rules in `index.css`, never by editing upstream
components:

- Sidebar drawer: `left-0` / `-translate-x-full` become inset-start /
  `translate-x-full` under RTL (rtlcss handles `left`; the transform sign
  needs a manual `[dir=rtl]` rule).
- Chevrons and arrows (`ChevronLeft/Right`, `CornerDownLeft`, back buttons):
  flip with `[dir="rtl"] .icon-directional { transform: scaleX(-1) }`; add
  the class inside `primitives/icons.tsx` only.
- Composer: send button side, attach button, pills order, textarea keeps
  `dir="auto"`; slash-command menu anchoring.
- Terminal (xterm), file browser paths, diff view, tool-call output, code
  blocks, log tails, URLs in preview chips: force `dir="ltr"` on the
  container (`[dir=rtl] .codex-terminal, .md-code, .file-path { direction:ltr }`).
- Tables (usage, audit): numeric columns stay right-aligned in both
  directions; header alignment flips.
- Toasts, popovers, dropdown anchoring (`right-0` menus), tooltips.
- Scrollbars and `overflow-x` regions; keyboard navigation (ArrowLeft/Right
  semantics in the command palette and skill picker).
- Keyboard shortcut labels (`Ctrl+.`, `Ctrl+K`) stay LTR.

## Phase 4 — locale formatting

- One helper `formatDate/formatTime/formatNumber` in `i18n/format.ts` using
  `Intl` with the active locale (`ar-EG-u-nu-latn` / `en`), replacing the 33
  bare `toLocale*` calls **only in fork-owned files**; upstream files keep
  browser-default formatting until touched by upstream itself.
- `relativeTimeService` ("3h", "2w", "just now") gets Arabic forms.
- `usageFormatService` (tokens, USD): Arabic unit words, Latin digits.

## Phase 5 — shell and PWA

`index.html` `<title>`, `manifest.webmanifest` name/description (served per
language is overkill — bilingual "Remote · ريموت" is enough), `offline.html`
bilingual, login screen strings (already covered by the catalog), email
subjects if any.

## Phase 6 — optional, later

- Notifications language (`notifications.json` gains `language`; Telegram/
  WhatsApp templates in `service/notify` get an Arabic set).
- Agent instructions template (`provisioning/instructions.go`) — leave in
  English; agents read English better and `replyLanguage` already governs
  what the user sees.
- Docs site Arabic mirror (`docs/` is ~40 pages; translate `02-user-guide`
  first if at all).

## Verification per phase

- `npm test` (node:test) for i18n units; `npx tsc --noEmit`.
- `npm run i18n:check` clean.
- Playwright pass: login, home, one chat, settings/appearance, project page,
  in `ar` and `en`, desktop + 400 px; compare against `en` screenshots for
  layout regressions (the direction flip must not move anything in `en`).
- Go side unchanged except the settings enum → `go test ./internal/service/usersettings/...`.
- After the next upstream sync: `npm run i18n:check` is the first thing to
  run; new keys go to the catalog, orphaned keys are deleted.

## Effort

Phase 1 is one focused session. Phase 2 is the bulk (translation plus owner
review; expect 3–4 review rounds). Phase 3 is a day of screenshots and CSS.
Phases 4–5 are small. Total: roughly a week of sessions with review gaps.

## Risks

- **Runtime shim and upstream's own JSX tricks**: components that build
  strings from fragments (`"Delete " + name`) won't match a key. The
  extractor reports such sites; the fix is a catalog entry with `{n}`-style
  placeholders or a fork-owned wrapper, never an upstream edit.
- **rtlcss and hand-written `index.css`**: rules with explicit `left/right`
  are flipped too, which is right for layout but wrong for the few
  intentional ones (code direction). Mark those `/*rtl:ignore*/`.
- **Search**: the workspace search folds text for matching; Arabic
  normalisation (alef forms, taa marbuta, diacritics) belongs in
  `textFoldService` — a small, contained change worth doing in phase 3.
- **Double palettes / shortcuts** from the recent sync are unrelated but will
  show up in the same screenshot pass; fix separately.

## Phase 1 as built

- `src/i18n/jsx-runtime.ts` / `jsx-dev-runtime.ts` wrap Preact's runtime;
  wired via `jsxImportSource: "remote-i18n"` (tsconfig paths + Vite alias).
  Text inside `pre/code/kbd/samp/textarea`, and inside any element carrying
  `dir` or `translate={false}`, is never translated — `dir` is the marker the
  chat and markdown renderers already put on user and agent content.
- `src/i18n/normalize.ts` is the one key rule, shared by runtime and
  extractor; `translate.ts` holds the active catalog; `locale.ts` resolves
  `auto|en|ar` and `?lang=`; `localeStore.ts` applies `lang`/`dir` and
  remounts the app through `LocaleRoot.tsx` on an actual switch.
- `appearance.language` in Go (`usersettings`) and TS, with the choice cached
  in `remote.futrx.language` and read by the `index.html` bootstrap before
  first paint. Control in Settings → Appearance; language names are written
  natively and marked untranslatable.
- Cairo Arabic subset self-hosted at `public/fonts/` (OFL), declared with the
  Arabic unicode-range and first in the stack, in `src/i18n/arabic.css`.
- `postcss-rtlcss` in override mode: every original rule is kept unchanged
  (verified: all 1,140 rules of the pre-change bundle are present verbatim),
  `[dir="rtl"]` rules are added. Rules that set `direction` are exempted
  automatically; content with its own `dir` keeps its own `text-left/right`.
- `npm run i18n:extract` (1,982 keys at build time) and `npm run i18n:check`
  (in CI, informational). Sentences split by inline elements appear as
  fragments (`". Remote stores it privately…"`); phase 2 translates them as
  fragments or wraps the sentence in a fork-owned component.
