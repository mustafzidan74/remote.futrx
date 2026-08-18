import { useCallback, useEffect, useState } from "preact/hooks";
import { monitoringApi } from "../../../api/monitoringApi";
import type { MonitoringPingResult, MonitoringSettings } from "../../../models/monitoring";
import {
  HEARTBEAT_INTERVAL_BOUNDS,
  validateHeartbeatForm,
  type IntervalBounds,
} from "../../settings/monitoringState";

export interface MonitoringSettingsEditor {
  settings: MonitoringSettings | null;
  enabled: boolean;
  heartbeatUrl: string;
  intervalMinutes: number;
  bounds: IntervalBounds;
  loading: boolean;
  saving: boolean;
  pinging: boolean;
  saved: boolean;
  error: string | null;
  pingResult: MonitoringPingResult | null;
  setEnabled: (enabled: boolean) => void;
  setHeartbeatUrl: (url: string) => void;
  setIntervalMinutes: (minutes: number) => void;
  save: (event: Event) => Promise<void>;
  clearHeartbeatUrl: () => Promise<void>;
  pingNow: () => Promise<void>;
}

/**
 * The Monitoring admin panel's state. It follows the notifications editor: the
 * heartbeat URL is write-only, so its input stays blank after a load and an
 * empty submission means "keep whatever is stored".
 */
export function useMonitoringSettings(): MonitoringSettingsEditor {
  const [settings, setSettings] = useState<MonitoringSettings | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [heartbeatUrl, setHeartbeatUrl] = useState("");
  const [intervalMinutes, setIntervalMinutes] = useState(5);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [pinging, setPinging] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pingResult, setPingResult] = useState<MonitoringPingResult | null>(null);

  const adopt = useCallback((value: MonitoringSettings) => {
    setSettings(value);
    setEnabled(value.enabled);
    setIntervalMinutes(value.intervalMinutes);
    // The URL is never returned, so its input stays empty.
    setHeartbeatUrl("");
  }, []);

  useEffect(() => {
    let cancelled = false;
    monitoringApi
      .get()
      .then((value) => !cancelled && adopt(value))
      .catch((cause) => !cancelled && setError((cause as Error).message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [adopt]);

  const bounds: IntervalBounds = settings
    ? { min: settings.minIntervalMinutes, max: settings.maxIntervalMinutes }
    : HEARTBEAT_INTERVAL_BOUNDS;

  function edit<T>(setter: (value: T) => void) {
    return (value: T) => {
      setter(value);
      setSaved(false);
      setPingResult(null);
    };
  }

  async function submit(clearHeartbeatUrl: boolean): Promise<boolean> {
    const problem = validateHeartbeatForm(
      {
        enabled: clearHeartbeatUrl ? false : enabled,
        heartbeatUrl: clearHeartbeatUrl ? "" : heartbeatUrl,
        intervalMinutes,
        configured: clearHeartbeatUrl ? false : (settings?.configured ?? false),
      },
      bounds,
    );
    if (problem) {
      setError(problem);
      return false;
    }
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      adopt(
        await monitoringApi.save({
          enabled: clearHeartbeatUrl ? false : enabled,
          heartbeatUrl: clearHeartbeatUrl ? "" : heartbeatUrl.trim(),
          clearHeartbeatUrl,
          intervalMinutes,
        }),
      );
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
    setPingResult(null);
    if (await submit(false)) setSaved(true);
  }

  async function clearHeartbeatUrl() {
    setPingResult(null);
    await submit(true);
  }

  /**
   * Sends one heartbeat now. The server reports a rejected push as a result
   * rather than an error, so a wrong URL shows up here as "Failed" with the
   * provider's status code instead of a red request failure.
   */
  async function pingNow() {
    setPinging(true);
    setError(null);
    setPingResult(null);
    try {
      const result = await monitoringApi.ping();
      setPingResult(result);
      // The push moved the schedule and the stored outcome; re-read so the
      // panel's "last ping" line agrees with the server.
      adopt(await monitoringApi.get());
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setPinging(false);
    }
  }

  return {
    settings,
    enabled,
    heartbeatUrl,
    intervalMinutes,
    bounds,
    loading,
    saving,
    pinging,
    saved,
    error,
    pingResult,
    setEnabled: edit(setEnabled),
    setHeartbeatUrl: edit(setHeartbeatUrl),
    setIntervalMinutes: edit(setIntervalMinutes),
    save,
    clearHeartbeatUrl,
    pingNow,
  };
}
