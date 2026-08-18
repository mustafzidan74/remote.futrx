/**
 * Project deep links.
 *
 * A health notification is about a project, not a chat, so it links to the
 * application root with a `?project=<id>` query parameter. On boot the
 * workspace reads that parameter once, opens the project's settings page, and
 * strips it from the address bar — the same contract chatDeepLink follows for
 * `?chat=<id>`, kept separate because the two select different views.
 */

const PROJECT_QUERY_PARAM = "project";

/** Project ids are lowercase hex, 4-32 chars (mirrors the backend's ValidID). */
const PROJECT_ID_PATTERN = /^[0-9a-f]{4,32}$/;

class ProjectDeepLinkState {
  /**
   * Reads the requested project id from a location search string. Anything
   * that is not a well-formed project id is ignored, so a crafted link cannot
   * steer the app into requesting arbitrary paths.
   */
  parse(search: string): string | null {
    if (!search) return null;
    const value = new URLSearchParams(search).get(PROJECT_QUERY_PARAM);
    if (!value) return null;
    const candidate = value.trim().toLowerCase();
    return PROJECT_ID_PATTERN.test(candidate) ? candidate : null;
  }

  /** The URL to replace the current one with once the deep link is consumed. */
  withoutProjectParam(pathname: string, search: string, hash: string): string {
    const params = new URLSearchParams(search);
    params.delete(PROJECT_QUERY_PARAM);
    const query = params.toString();
    return `${pathname}${query ? `?${query}` : ""}${hash}`;
  }
}

export const projectDeepLinkState = new ProjectDeepLinkState();
