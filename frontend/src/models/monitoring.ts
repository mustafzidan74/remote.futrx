/**
 * External uptime monitoring. The platform cannot alert about its own death
 * from inside, so these settings are the two ways it lets the outside world
 * notice: a public health endpoint to poll, and an outbound heartbeat pushed
 * to a dead man switch service.
 */

/** Server-reported monitoring settings. The heartbeat URL is never echoed. */
export interface MonitoringSettings {
  enabled: boolean;
  configured: boolean;
  heartbeatUrlMasked?: string;
  heartbeatHost?: string;
  intervalMinutes: number;
  minIntervalMinutes: number;
  maxIntervalMinutes: number;
  lastPingAt?: number;
  /** "ok" or "failed"; absent until something has been tried. */
  lastPingStatus?: string;
  lastPingError?: string;
  updatedAt?: number;
  /** The public health path, echoed so the panel builds the URL from one source. */
  healthPath: string;
}

/**
 * Write payload. A blank `heartbeatUrl` keeps whatever the server already
 * stores, which is why the panel can show a mask instead of the real value;
 * `clearHeartbeatUrl` is the explicit way to remove it.
 */
export interface UpdateMonitoringSettingsInput {
  enabled: boolean;
  heartbeatUrl: string;
  clearHeartbeatUrl?: boolean;
  intervalMinutes: number;
}

/** One heartbeat push, as reported by the "Ping now" action. */
export interface MonitoringPingResult {
  delivered: boolean;
  at: number;
  status: string;
  error?: string;
}
