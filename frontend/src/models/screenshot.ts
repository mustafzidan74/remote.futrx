/**
 * Preview screenshots: a stored PNG of one project port at one moment.
 *
 * The record is deliberately thin — the bytes live behind `url`, a
 * session-gated route, so a list never carries megabytes of image data.
 */
export interface ProjectScreenshot {
  id: string;
  /** File name on the host; also the download name. */
  file: string;
  /** Session-gated read route: /api/projects/{id}/screenshots/{sid}.png */
  url: string;
  port: number;
  path: string;
  width: number;
  height: number;
  fullPage?: boolean;
  bytes: number;
  createdBy?: string;
  createdAt: number;
}

/** One notification sink's outcome when a capture was sent onward. */
export interface ScreenshotDelivery {
  sink: string;
  delivered: boolean;
  error?: string;
}

export interface CaptureScreenshotInput {
  port: number;
  path?: string;
  width?: number;
  height?: number;
  fullPage?: boolean;
  /** Also push the picture through the configured notification sinks. */
  notify?: boolean;
}

export interface ScreenshotCaptureResult {
  screenshot: ProjectScreenshot;
  delivered?: ScreenshotDelivery[];
  /** False when no notification sink is configured, so "send it" is hidden. */
  notifications: boolean;
  /**
   * A 24-hour login-less link, present only when a sink that cannot carry
   * pictures needed one. Shown once, like a preview share token.
   */
  publicUrl?: string;
}

export interface ScreenshotsPayload {
  screenshots: ProjectScreenshot[];
  /** False when no notification sink is configured, so "send it" is hidden. */
  notifications: boolean;
}
