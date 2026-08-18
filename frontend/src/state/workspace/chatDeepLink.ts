/**
 * Chat deep links.
 *
 * The SPA has no URL router, so outbound notifications link to the application
 * root with a `?chat=<id>` query parameter. On boot the workspace reads that
 * parameter once, selects the chat, and strips it from the address bar so a
 * later manual chat switch does not leave a stale link behind.
 *
 * A search hit adds `&at=<unix-ms>`: the chat opens as usual and the thread
 * scrolls to, and briefly flashes, the message at that instant.
 */

const CHAT_QUERY_PARAM = "chat";
const AT_QUERY_PARAM = "at";

/** Chat ids are lowercase hex, 4-32 chars (mirrors the backend's ValidID). */
const CHAT_ID_PATTERN = /^[0-9a-f]{4,32}$/;

/** Timestamps are unix milliseconds: digits only, and no wider than that. */
const AT_PATTERN = /^[0-9]{1,15}$/;

class ChatDeepLinkState {
  /**
   * Reads the requested chat id from a location search string. Anything that
   * is not a well-formed chat id is ignored, so a crafted link cannot steer
   * the app into requesting arbitrary paths.
   */
  parse(search: string): string | null {
    if (!search) return null;
    const value = new URLSearchParams(search).get(CHAT_QUERY_PARAM);
    if (!value) return null;
    const candidate = value.trim().toLowerCase();
    return CHAT_ID_PATTERN.test(candidate) ? candidate : null;
  }

  /**
   * Reads the message instant to scroll to. It is only meaningful alongside a
   * chat id, so it is parsed independently and simply ignored when the chat
   * parameter is absent or malformed.
   */
  parseAt(search: string): number | null {
    if (!search) return null;
    const value = new URLSearchParams(search).get(AT_QUERY_PARAM);
    if (!value) return null;
    const candidate = value.trim();
    if (!AT_PATTERN.test(candidate)) return null;
    const at = Number(candidate);
    return Number.isSafeInteger(at) && at > 0 ? at : null;
  }

  /**
   * Returns the URL to replace the current one with once the deep link has
   * been consumed: the same page without the chat parameters.
   */
  withoutChatParam(pathname: string, search: string, hash: string): string {
    const params = new URLSearchParams(search);
    params.delete(CHAT_QUERY_PARAM);
    params.delete(AT_QUERY_PARAM);
    const query = params.toString();
    return `${pathname}${query ? `?${query}` : ""}${hash}`;
  }
}

export const chatDeepLinkState = new ChatDeepLinkState();
