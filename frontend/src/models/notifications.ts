export interface NotificationEventToggles {
  runFinished: boolean;
  runFailed: boolean;
  needsAttention: boolean;
  scheduledRun: boolean;
}

/** Telegram credentials as the server reports them: the token is never echoed. */
export interface NotificationTelegramSettings {
  configured: boolean;
  botTokenMasked?: string;
  chatId?: string;
}

/** Webhook target as the server reports it: the shared secret is never echoed. */
export interface NotificationWebhookSettings {
  configured: boolean;
  url?: string;
  secretMasked?: string;
}

/** The gateway a WhatsApp message goes through. "" means WhatsApp is off. */
export type WhatsAppProvider = "" | "cloud" | "callmebot";

/** Meta Cloud API credentials as the server reports them. */
export interface NotificationWhatsAppCloudSettings {
  configured: boolean;
  phoneNumberId?: string;
  accessTokenMasked?: string;
  recipient?: string;
  templateName?: string;
  templateLanguage?: string;
}

/** CallMeBot credentials as the server reports them. */
export interface NotificationWhatsAppCallMeBotSettings {
  configured: boolean;
  phone?: string;
  apikeyMasked?: string;
}

export interface NotificationWhatsAppSettings {
  configured: boolean;
  provider?: WhatsAppProvider;
  cloud: NotificationWhatsAppCloudSettings;
  callmebot: NotificationWhatsAppCallMeBotSettings;
}

/** The weekly cost-and-usage digest schedule. 0 = Sunday. */
export interface NotificationDigestSettings {
  enabled: boolean;
  weekday: number;
  hour: number;
  timezone: string;
  lastDigestSentAt?: number;
}

export interface NotificationSettings {
  enabled: boolean;
  telegram: NotificationTelegramSettings;
  webhook: NotificationWebhookSettings;
  whatsapp: NotificationWhatsAppSettings;
  events: NotificationEventToggles;
  digest: NotificationDigestSettings;
  updatedAt?: number;
}

/**
 * Write payload. Blank secrets keep whatever the server already stores, which
 * is why the UI can show a mask instead of the real value; the clear flags are
 * the explicit way to remove one.
 */
export interface UpdateNotificationSettingsInput {
  enabled: boolean;
  telegram: {
    botToken: string;
    clearBotToken?: boolean;
    chatId: string;
  };
  webhook: {
    url: string;
    secret: string;
    clearSecret?: boolean;
  };
  whatsapp: {
    provider: WhatsAppProvider;
    cloud: {
      phoneNumberId: string;
      accessToken: string;
      clearAccessToken?: boolean;
      recipient: string;
      templateName: string;
      templateLanguage: string;
    };
    callmebot: {
      phone: string;
      apikey: string;
      clearApikey?: boolean;
    };
  };
  events: NotificationEventToggles;
  digest: {
    enabled: boolean;
    weekday: number;
    hour: number;
    timezone: string;
  };
}

export interface NotificationTestResult {
  sink: string;
  configured: boolean;
  delivered: boolean;
  error?: string;
}
