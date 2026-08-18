import type {
  NotificationEventToggles,
  WhatsAppProvider,
} from "../../models/notifications";
import { useNotificationsSettingsController } from "../../state/hooks/notifications/useNotificationsSettingsController";
import { Bell, Check, ExternalLink, Loader, Send, X } from "../primitives/icons";

const WEEKDAYS = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];

const WHATSAPP_PROVIDERS: Array<{ value: WhatsAppProvider; label: string }> = [
  { value: "", label: "Off" },
  { value: "cloud", label: "Meta WhatsApp Cloud API" },
  { value: "callmebot", label: "CallMeBot (free, personal)" },
];

const inputClass =
  "w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100 " +
  "placeholder:text-ink-400 focus:outline-none focus:border-accent-blue";

const EVENT_ROWS: Array<{ key: keyof NotificationEventToggles; label: string; hint: string }> = [
  { key: "runFinished", label: "Run finished", hint: "An agent turn completed successfully." },
  { key: "runFailed", label: "Run failed", hint: "An agent turn errored or was cancelled." },
  {
    key: "needsAttention",
    label: "Needs attention",
    hint: "The agent asked a question or is waiting for a plan approval.",
  },
  { key: "scheduledRun", label: "Scheduled task", hint: "A scheduled task finished or failed." },
  {
    key: "projectHealth",
    label: "Project health",
    hint: "A project container ran short of memory, stopped serving its app, or recovered.",
  },
];

export function NotificationsSettings() {
  const {
    botToken,
    chatId,
    clearCallMeBotApiKey,
    clearTelegramToken,
    clearWebhookSecret,
    clearWhatsAppAccessToken,
    digest,
    error,
    events,
    healthMonitorEnabled,
    loading,
    notificationsEnabled,
    save,
    saved,
    saving,
    sendDigestNow,
    sendingDigest,
    sendTest,
    setBotToken,
    setChatId,
    setNotificationsEnabled,
    setWebhookSecret,
    setWebhookUrl,
    settings,
    testing,
    testResults,
    toggleEvent,
    updateDigest,
    updateWhatsApp,
    webhookSecret,
    webhookUrl,
    whatsapp,
  } = useNotificationsSettingsController();

  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
          <Bell class="w-4 h-4 text-ink-200" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <div class="text-[14.5px] font-semibold text-ink-50">Outbound notifications</div>
            {loading ? (
              <Loader class="w-3.5 h-3.5 text-ink-300 animate-spin" />
            ) : settings?.enabled ? (
              <span class="inline-flex items-center gap-1 text-[11px] text-accent-green">
                <Check class="w-3.5 h-3.5" /> on
              </span>
            ) : (
              <span class="text-[11px] text-ink-400">off</span>
            )}
          </div>
          <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
            Get pinged on your phone when an agent run finishes, fails, or needs you — and when a
            scheduled task runs. Every message links straight back to the chat.
          </div>
        </div>
      </header>

      <form onSubmit={save} class="p-3 space-y-3">
        <label class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer">
          <input
            type="checkbox"
            checked={notificationsEnabled}
            onChange={(event) =>
              setNotificationsEnabled((event.currentTarget as HTMLInputElement).checked)
            }
            class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
          />
          <span class="min-w-0">
            <span class="block text-[13px] text-ink-100">Send notifications</span>
            <span class="block text-[12px] text-ink-300 leading-relaxed">
              Configure at least one destination below before turning this on.
            </span>
          </span>
        </label>

        <fieldset class="space-y-3">
          <legend class="text-xs font-semibold text-ink-200 uppercase tracking-wide">Telegram</legend>
          <div class="rounded-md border border-white/10 bg-white/[0.03] p-2.5 text-[12px] text-ink-300 leading-relaxed">
            Message <span class="text-ink-100">@BotFather</span> on Telegram, send{" "}
            <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11.5px]">
              /newbot
            </code>
            , and paste the token it gives you. Then send your bot a message and read your chat ID
            from{" "}
            <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11.5px] break-all">
              https://api.telegram.org/bot&lt;token&gt;/getUpdates
            </code>
            .
            <a
              href="https://core.telegram.org/bots/features#botfather"
              target="_blank"
              rel="noreferrer"
              class="inline-flex items-center gap-1 mt-2 text-accent-blue hover:underline"
            >
              Telegram bot documentation <ExternalLink class="w-3.5 h-3.5" />
            </a>
          </div>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Bot token</span>
            <input
              type="password"
              value={botToken}
              onInput={(event) => setBotToken((event.currentTarget as HTMLInputElement).value)}
              placeholder={
                settings?.telegram.configured
                  ? `Stored (${settings.telegram.botTokenMasked}) — enter a new token to replace it`
                  : "123456789:AA..."
              }
              autocomplete="new-password"
              spellcheck={false}
              class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
            />
          </label>
          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Chat ID</span>
            <input
              type="text"
              value={chatId}
              onInput={(event) => setChatId((event.currentTarget as HTMLInputElement).value)}
              placeholder="-1001234567890"
              autocomplete="off"
              spellcheck={false}
              class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
            />
          </label>
          {settings?.telegram.configured && (
            <button
              type="button"
              onClick={() => void clearTelegramToken()}
              disabled={saving}
              class="h-8 px-2.5 rounded-md border border-white/10 text-ink-300 hover:text-ink-100 hover:bg-white/[0.06] text-[12px] disabled:opacity-50"
            >
              Remove stored bot token
            </button>
          )}
        </fieldset>

        <fieldset class="space-y-3">
          <legend class="text-xs font-semibold text-ink-200 uppercase tracking-wide">WhatsApp</legend>
          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Provider</span>
            <select
              value={whatsapp.provider}
              onChange={(event) =>
                updateWhatsApp({
                  provider: (event.currentTarget as HTMLSelectElement).value as WhatsAppProvider,
                })
              }
              class={inputClass}
            >
              {WHATSAPP_PROVIDERS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>

          {whatsapp.provider === "cloud" && (
            <>
              <div class="rounded-md border border-white/10 bg-white/[0.03] p-2.5 text-[12px] text-ink-300 leading-relaxed">
                Meta only delivers free-form text inside the 24-hour window that opens when the
                recipient messages your business number. Outside it a pre-approved template is
                required — name one below and Remote sends the message as that template, with the
                summary as its first body parameter.
                <a
                  href="https://developers.facebook.com/docs/whatsapp/cloud-api/get-started"
                  target="_blank"
                  rel="noreferrer"
                  class="inline-flex items-center gap-1 mt-2 text-accent-blue hover:underline"
                >
                  WhatsApp Cloud API setup <ExternalLink class="w-3.5 h-3.5" />
                </a>
              </div>
              <label class="block space-y-1.5">
                <span class="text-xs text-ink-300">Phone number ID</span>
                <input
                  type="text"
                  value={whatsapp.phoneNumberId}
                  onInput={(event) =>
                    updateWhatsApp({
                      phoneNumberId: (event.currentTarget as HTMLInputElement).value,
                    })
                  }
                  placeholder="123456789012345"
                  autocomplete="off"
                  spellcheck={false}
                  class={inputClass}
                />
              </label>
              <label class="block space-y-1.5">
                <span class="text-xs text-ink-300">Access token</span>
                <input
                  type="password"
                  value={whatsapp.accessToken}
                  onInput={(event) =>
                    updateWhatsApp({ accessToken: (event.currentTarget as HTMLInputElement).value })
                  }
                  placeholder={
                    settings?.whatsapp?.cloud.configured
                      ? `Stored (${settings.whatsapp.cloud.accessTokenMasked}) — enter a new token to replace it`
                      : "EAAG..."
                  }
                  autocomplete="new-password"
                  spellcheck={false}
                  class={inputClass}
                />
              </label>
              <label class="block space-y-1.5">
                <span class="text-xs text-ink-300">Recipient (E.164)</span>
                <input
                  type="text"
                  value={whatsapp.recipient}
                  onInput={(event) =>
                    updateWhatsApp({ recipient: (event.currentTarget as HTMLInputElement).value })
                  }
                  placeholder="2010xxxxxxxx"
                  autocomplete="off"
                  spellcheck={false}
                  class={inputClass}
                />
              </label>
              <label class="block space-y-1.5">
                <span class="text-xs text-ink-300">Template name (optional)</span>
                <input
                  type="text"
                  value={whatsapp.templateName}
                  onInput={(event) =>
                    updateWhatsApp({ templateName: (event.currentTarget as HTMLInputElement).value })
                  }
                  placeholder="Leave empty to send plain text inside the 24-hour window"
                  autocomplete="off"
                  spellcheck={false}
                  class={inputClass}
                />
              </label>
              <label class="block space-y-1.5">
                <span class="text-xs text-ink-300">Template language (optional)</span>
                <input
                  type="text"
                  value={whatsapp.templateLanguage}
                  onInput={(event) =>
                    updateWhatsApp({
                      templateLanguage: (event.currentTarget as HTMLInputElement).value,
                    })
                  }
                  placeholder="en_US"
                  autocomplete="off"
                  spellcheck={false}
                  class={inputClass}
                />
              </label>
              {settings?.whatsapp?.cloud.accessTokenMasked && (
                <button
                  type="button"
                  onClick={() => void clearWhatsAppAccessToken()}
                  disabled={saving}
                  class="h-8 px-2.5 rounded-md border border-white/10 text-ink-300 hover:text-ink-100 hover:bg-white/[0.06] text-[12px] disabled:opacity-50"
                >
                  Remove stored access token
                </button>
              )}
            </>
          )}

          {whatsapp.provider === "callmebot" && (
            <>
              <div class="rounded-md border border-white/10 bg-white/[0.03] p-2.5 text-[12px] text-ink-300 leading-relaxed">
                Message the CallMeBot number on WhatsApp with the activation phrase from its
                instructions; the bot replies with a personal API key. Messages then arrive from
                that number as short plain text, one line plus the link.
                <a
                  href="https://www.callmebot.com/blog/free-api-whatsapp-messages/"
                  target="_blank"
                  rel="noreferrer"
                  class="inline-flex items-center gap-1 mt-2 text-accent-blue hover:underline"
                >
                  CallMeBot instructions <ExternalLink class="w-3.5 h-3.5" />
                </a>
              </div>
              <label class="block space-y-1.5">
                <span class="text-xs text-ink-300">Your WhatsApp number (E.164)</span>
                <input
                  type="text"
                  value={whatsapp.callMeBotPhone}
                  onInput={(event) =>
                    updateWhatsApp({
                      callMeBotPhone: (event.currentTarget as HTMLInputElement).value,
                    })
                  }
                  placeholder="+2010xxxxxxxx"
                  autocomplete="off"
                  spellcheck={false}
                  class={inputClass}
                />
              </label>
              <label class="block space-y-1.5">
                <span class="text-xs text-ink-300">API key</span>
                <input
                  type="password"
                  value={whatsapp.callMeBotApiKey}
                  onInput={(event) =>
                    updateWhatsApp({
                      callMeBotApiKey: (event.currentTarget as HTMLInputElement).value,
                    })
                  }
                  placeholder={
                    settings?.whatsapp?.callmebot.configured
                      ? `Stored (${settings.whatsapp.callmebot.apikeyMasked}) — enter a new key to replace it`
                      : "123456"
                  }
                  autocomplete="new-password"
                  spellcheck={false}
                  class={inputClass}
                />
              </label>
              {settings?.whatsapp?.callmebot.apikeyMasked && (
                <button
                  type="button"
                  onClick={() => void clearCallMeBotApiKey()}
                  disabled={saving}
                  class="h-8 px-2.5 rounded-md border border-white/10 text-ink-300 hover:text-ink-100 hover:bg-white/[0.06] text-[12px] disabled:opacity-50"
                >
                  Remove stored API key
                </button>
              )}
            </>
          )}
        </fieldset>

        <fieldset class="space-y-3">
          <legend class="text-xs font-semibold text-ink-200 uppercase tracking-wide">Webhook</legend>
          <div class="rounded-md border border-white/10 bg-white/[0.03] p-2.5 text-[12px] text-ink-300 leading-relaxed">
            Remote POSTs a JSON body to this URL. With a shared secret set, each request carries{" "}
            <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11.5px] break-all">
              X-Remote-Signature: sha256=&lt;hmac&gt;
            </code>{" "}
            over the exact body.
          </div>
          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Webhook URL</span>
            <input
              type="url"
              value={webhookUrl}
              onInput={(event) => setWebhookUrl((event.currentTarget as HTMLInputElement).value)}
              placeholder="https://hooks.example.com/remote"
              autocomplete="off"
              spellcheck={false}
              class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
            />
          </label>
          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Shared secret (optional)</span>
            <input
              type="password"
              value={webhookSecret}
              onInput={(event) => setWebhookSecret((event.currentTarget as HTMLInputElement).value)}
              placeholder={
                settings?.webhook.secretMasked
                  ? `Stored (${settings.webhook.secretMasked}) — enter a new secret to replace it`
                  : "Used to sign each request"
              }
              autocomplete="new-password"
              class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
            />
          </label>
          {settings?.webhook.secretMasked && (
            <button
              type="button"
              onClick={() => void clearWebhookSecret()}
              disabled={saving}
              class="h-8 px-2.5 rounded-md border border-white/10 text-ink-300 hover:text-ink-100 hover:bg-white/[0.06] text-[12px] disabled:opacity-50"
            >
              Remove stored webhook secret
            </button>
          )}
        </fieldset>

        <fieldset class="space-y-2">
          <legend class="text-xs font-semibold text-ink-200 uppercase tracking-wide">Events</legend>
          {EVENT_ROWS.map((row) => (
            <label
              key={row.key}
              class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer"
            >
              <input
                type="checkbox"
                checked={events[row.key]}
                onChange={(event) =>
                  toggleEvent(row.key, (event.currentTarget as HTMLInputElement).checked)
                }
                class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
              />
              <span class="min-w-0">
                <span class="block text-[13px] text-ink-100">{row.label}</span>
                <span class="block text-[12px] text-ink-300 leading-relaxed">{row.hint}</span>
                {row.key === "projectHealth" && !healthMonitorEnabled && (
                  <span class="block text-[12px] text-accent-yellow leading-relaxed">
                    The health monitor is switched off on this server
                    (HEALTH_MONITOR_INTERVAL=0), so nothing will be sent.
                  </span>
                )}
              </span>
            </label>
          ))}
        </fieldset>

        <fieldset class="space-y-3">
          <legend class="text-xs font-semibold text-ink-200 uppercase tracking-wide">
            Weekly cost report
          </legend>
          <label class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer">
            <input
              type="checkbox"
              checked={digest.enabled}
              onChange={(event) =>
                updateDigest({ enabled: (event.currentTarget as HTMLInputElement).checked })
              }
              class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
            />
            <span class="min-w-0">
              <span class="block text-[13px] text-ink-100">Send a weekly usage digest</span>
              <span class="block text-[12px] text-ink-300 leading-relaxed">
                One message covering the last 7 days: total cost, run count, a per-project
                breakdown, and the busiest model.
              </span>
            </span>
          </label>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <label class="block space-y-1.5">
              <span class="text-xs text-ink-300">Day</span>
              <select
                value={String(digest.weekday)}
                onChange={(event) =>
                  updateDigest({
                    weekday: Number((event.currentTarget as HTMLSelectElement).value),
                  })
                }
                class={inputClass}
              >
                {WEEKDAYS.map((label, index) => (
                  <option key={label} value={String(index)}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            <label class="block space-y-1.5">
              <span class="text-xs text-ink-300">Hour</span>
              <select
                value={String(digest.hour)}
                onChange={(event) =>
                  updateDigest({ hour: Number((event.currentTarget as HTMLSelectElement).value) })
                }
                class={inputClass}
              >
                {Array.from({ length: 24 }, (_unused, hour) => (
                  <option key={hour} value={String(hour)}>
                    {String(hour).padStart(2, "0")}:00
                  </option>
                ))}
              </select>
            </label>
            <label class="block space-y-1.5">
              <span class="text-xs text-ink-300">Time zone</span>
              <input
                type="text"
                value={digest.timezone}
                onInput={(event) =>
                  updateDigest({ timezone: (event.currentTarget as HTMLInputElement).value })
                }
                placeholder="Africa/Cairo"
                autocomplete="off"
                spellcheck={false}
                class={inputClass}
              />
            </label>
          </div>
          {digest.lastDigestSentAt ? (
            <div class="text-[11.5px] text-ink-400">
              Last digest covered {new Date(digest.lastDigestSentAt).toLocaleString()}.
            </div>
          ) : null}
        </fieldset>

        {error && <div class="text-xs text-accent-red">{error}</div>}

        {saved && !error && <div class="text-xs text-accent-green">Notification settings saved.</div>}

        <div class="flex flex-wrap items-center gap-2">
          <button
            type="submit"
            disabled={saving || loading}
            class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
          >
            {saving && <Loader class="w-3.5 h-3.5 animate-spin" />}
            Save notifications
          </button>
          <button
            type="button"
            onClick={() => void sendTest()}
            disabled={testing || saving || loading}
            class="h-10 px-3 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06] text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
          >
            {testing ? <Loader class="w-3.5 h-3.5 animate-spin" /> : <Send class="w-3.5 h-3.5" />}
            Send test
          </button>
          <button
            type="button"
            onClick={() => void sendDigestNow()}
            disabled={sendingDigest || testing || saving || loading}
            class="h-10 px-3 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06] text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
          >
            {sendingDigest ? (
              <Loader class="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Send class="w-3.5 h-3.5" />
            )}
            Send digest now
          </button>
        </div>

        {testResults && (
          <ul class="space-y-1.5">
            {testResults.map((result) => (
              <li
                key={result.sink}
                class="flex items-start gap-2 rounded-md border border-white/10 bg-white/[0.03] p-2.5 text-[12px]"
              >
                {result.delivered ? (
                  <Check class="w-3.5 h-3.5 mt-0.5 flex-none text-accent-green" />
                ) : (
                  <X class="w-3.5 h-3.5 mt-0.5 flex-none text-accent-red" />
                )}
                <span class="min-w-0">
                  <span class="text-ink-100">{result.sink}</span>
                  <span class="text-ink-300">
                    {result.delivered ? " delivered the test message." : ` failed: ${result.error}`}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        )}
      </form>
    </section>
  );
}
