import { requestJson } from "../apiRequest";
import type {
  CaptureScreenshotInput,
  ScreenshotCaptureResult,
  ScreenshotsPayload,
} from "../../models/screenshot";
import { API_ROUTES } from "../../config/routes";

export const projectScreenshotApi = {
  /**
   * Captures one preview port. Synchronous by design: the whole point is that
   * the user is looking at the result, so there is no job to poll.
   */
  captureScreenshot: (id: string, body: CaptureScreenshotInput) =>
    requestJson<ScreenshotCaptureResult>("POST", API_ROUTES.projects.screenshot(id), body),

  listScreenshots: (id: string) =>
    requestJson<ScreenshotsPayload>("GET", API_ROUTES.projects.screenshots(id)),

  /**
   * Pushes a capture that already exists through the notification sinks.
   * Separate from capturing so "send me that" cannot silently photograph a
   * later moment than the one on screen.
   */
  sendScreenshot: (id: string, screenshotId: string) =>
    requestJson<ScreenshotCaptureResult>(
      "POST",
      API_ROUTES.projects.screenshotSend(id, screenshotId),
    ),
};
