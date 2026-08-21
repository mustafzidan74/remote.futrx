import { API_ROUTES } from "../../../config/routes";
import type { AntigravityAuthStatus } from "../../../models/auth";

/**
 * Antigravity has no login endpoints to call — only a status to read.
 *
 * It gets a plain fetch rather than a DeviceAuthApi because there is no device
 * flow and no websocket: the sign-in happens in a terminal the platform does
 * not drive, so nothing pushes a change.
 */
export const antigravityAuthApi = {
  async status(): Promise<AntigravityAuthStatus | null> {
    const response = await fetch(API_ROUTES.antigravityAuth.status, { credentials: "same-origin" });
    if (!response.ok) return null;
    return (await response.json()) as AntigravityAuthStatus | null;
  },
};
