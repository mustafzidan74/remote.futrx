import { useEffect, useState } from "preact/hooks";
import { notificationsApi } from "../../../api/notificationsApi";
import type {
  NotificationDigestSettings,
  NotificationEventToggles,
  NotificationSettings,
  NotificationTestResult,
  UpdateNotificationSettingsInput,
  WhatsAppProvider,
} from "../../../models/notifications";

const DEFAULT_EVENTS: NotificationEventToggles = {
  runFinished: true,
  runFailed: true,
  needsAttention: true,
  scheduledRun: true,
};

const DEFAULT_DIGEST: NotificationDigestSettings = {
  enabled: false,
  weekday: 0,
  hour: 9,
  timezone: "Africa/Cairo",
};

/** Secret fields the form leaves blank; blank means "keep what is stored". */
interface WhatsAppFormState {
  provider: WhatsAppProvider;
  phoneNumberId: string;
  accessToken: string;
  recipient: string;
  templateName: string;
  templateLanguage: string;
  callMeBotPhone: string;
  callMeBotApiKey: string;
}

const EMPTY_WHATSAPP: WhatsAppFormState = {
  provider: "",
  phoneNumberId: "",
  accessToken: "",
  recipient: "",
  templateName: "",
  templateLanguage: "",
  callMeBotPhone: "",
  callMeBotApiKey: "",
};

/** Clear flags the "Remove stored …" buttons submit. */
interface ClearFlags {
  clearBotToken?: boolean;
  clearSecret?: boolean;
  clearAccessToken?: boolean;
  clearApikey?: boolean;
}

export function useNotificationsSettingsController() {
  const [settings, setSettings] = useState<NotificationSettings | null>(null);
  const [notificationsEnabled, setNotificationsEnabled] = useState(false);
  const [botToken, setBotToken] = useState("");
  const [chatId, setChatId] = useState("");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");
  const [whatsapp, setWhatsApp] = useState<WhatsAppFormState>(EMPTY_WHATSAPP);
  const [events, setEvents] = useState<NotificationEventToggles>(DEFAULT_EVENTS);
  const [digest, setDigest] = useState<NotificationDigestSettings>(DEFAULT_DIGEST);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [sendingDigest, setSendingDigest] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [testResults, setTestResults] = useState<NotificationTestResult[] | null>(null);

  function adopt(value: NotificationSettings) {
    setSettings(value);
    setNotificationsEnabled(value.enabled);
    setChatId(value.telegram.chatId ?? "");
    setWebhookUrl(value.webhook.url ?? "");
    setEvents(value.events ?? DEFAULT_EVENTS);
    setDigest(value.digest ?? DEFAULT_DIGEST);
    setWhatsApp({
      ...EMPTY_WHATSAPP,
      provider: value.whatsapp?.provider ?? "",
      phoneNumberId: value.whatsapp?.cloud.phoneNumberId ?? "",
      recipient: value.whatsapp?.cloud.recipient ?? "",
      templateName: value.whatsapp?.cloud.templateName ?? "",
      templateLanguage: value.whatsapp?.cloud.templateLanguage ?? "",
      callMeBotPhone: value.whatsapp?.callmebot.phone ?? "",
    });
    // Secrets are never returned, so their inputs stay empty and an empty
    // submission means "keep what the server already has".
    setBotToken("");
    setWebhookSecret("");
  }

  useEffect(() => {
    let cancelled = false;
    notificationsApi
      .get()
      .then((value) => {
        if (cancelled) return;
        adopt(value);
      })
      .catch((cause) => !cancelled && setError((cause as Error).message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  function enableNotifications(value: boolean) {
    setNotificationsEnabled(value);
    setSaved(false);
  }

  function toggleEvent(key: keyof NotificationEventToggles, value: boolean) {
    setEvents((current) => ({ ...current, [key]: value }));
    setSaved(false);
  }

  function updateWhatsApp(patch: Partial<WhatsAppFormState>) {
    setWhatsApp((current) => ({ ...current, ...patch }));
    setSaved(false);
  }

  function updateDigest(patch: Partial<NotificationDigestSettings>) {
    setDigest((current) => ({ ...current, ...patch }));
    setSaved(false);
  }

  function payload(flags: ClearFlags = {}): UpdateNotificationSettingsInput {
    return {
      enabled: notificationsEnabled,
      telegram: {
        botToken: flags.clearBotToken ? "" : botToken.trim(),
        clearBotToken: flags.clearBotToken === true,
        chatId: chatId.trim(),
      },
      webhook: {
        url: webhookUrl.trim(),
        secret: flags.clearSecret ? "" : webhookSecret.trim(),
        clearSecret: flags.clearSecret === true,
      },
      whatsapp: {
        provider: whatsapp.provider,
        cloud: {
          phoneNumberId: whatsapp.phoneNumberId.trim(),
          accessToken: flags.clearAccessToken ? "" : whatsapp.accessToken.trim(),
          clearAccessToken: flags.clearAccessToken === true,
          recipient: whatsapp.recipient.trim(),
          templateName: whatsapp.templateName.trim(),
          templateLanguage: whatsapp.templateLanguage.trim(),
        },
        callmebot: {
          phone: whatsapp.callMeBotPhone.trim(),
          apikey: flags.clearApikey ? "" : whatsapp.callMeBotApiKey.trim(),
          clearApikey: flags.clearApikey === true,
        },
      },
      events,
      digest: {
        enabled: digest.enabled,
        weekday: digest.weekday,
        hour: digest.hour,
        timezone: digest.timezone.trim(),
      },
    };
  }

  async function submit(flags: ClearFlags = {}) {
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      adopt(await notificationsApi.save(payload(flags)));
      return true;
    } catch (cause) {
      setError((cause as Error).message);
      return false;
    } finally {
      setSaving(false);
    }
  }

  async function save(event: Event) {
    event.preventDefault();
    setTestResults(null);
    if (await submit()) setSaved(true);
  }

  async function clearTelegramToken() {
    await submit({ clearBotToken: true });
  }

  async function clearWebhookSecret() {
    await submit({ clearSecret: true });
  }

  async function clearWhatsAppAccessToken() {
    await submit({ clearAccessToken: true });
  }

  async function clearCallMeBotApiKey() {
    await submit({ clearApikey: true });
  }

  async function sendTest() {
    setTesting(true);
    setError(null);
    setTestResults(null);
    try {
      const response = await notificationsApi.test();
      setTestResults(response.results);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setTesting(false);
    }
  }

  async function sendDigestNow() {
    setSendingDigest(true);
    setError(null);
    setTestResults(null);
    try {
      const response = await notificationsApi.sendDigestNow();
      setTestResults(response.results);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setSendingDigest(false);
    }
  }

  return {
    botToken,
    chatId,
    clearCallMeBotApiKey,
    clearTelegramToken,
    clearWebhookSecret,
    clearWhatsAppAccessToken,
    digest,
    error,
    events,
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
    setNotificationsEnabled: enableNotifications,
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
  };
}
