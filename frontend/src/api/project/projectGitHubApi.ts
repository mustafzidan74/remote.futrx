import { requestJson } from "../apiRequest";
import { sendHttpRequest } from "../../transport/http";
import {
  GitHubDirtyWorkspaceError,
  type CreateGitHubPRInput,
  type CreateGitHubPRResult,
  type GitHubPullRequest,
  type GitHubSettings,
  type GitHubStatus,
  type ImportGitHubCommentsResult,
  type UpdateGitHubSettingsInput,
} from "../../models/github";
import { API_ROUTES } from "../../config/routes";
import {
  API_RESPONSE_STATUS,
  DEFAULT_COMMIT_MESSAGE_FALLBACK,
} from "../../config/api";

export const projectGitHubApi = {
  getGitHubStatus: (id: string) =>
    requestJson<GitHubStatus>("GET", API_ROUTES.projects.github(id)),

  linkGitHub: (id: string, repo: string) =>
    requestJson<GitHubStatus>("PUT", API_ROUTES.projects.github(id), { repo }),

  unlinkGitHub: (id: string) =>
    requestJson<{ ok: boolean }>("DELETE", API_ROUTES.projects.github(id)),

  cloneGitHub: (id: string) =>
    requestJson<GitHubStatus>("POST", API_ROUTES.projects.githubClone(id)),

  getGitHubSettings: (id: string) =>
    requestJson<GitHubSettings>("GET", API_ROUTES.projects.githubSettings(id)),

  saveGitHubSettings: (id: string, body: UpdateGitHubSettingsInput) =>
    requestJson<GitHubSettings>("PUT", API_ROUTES.projects.githubSettings(id), body),

  listGitHubPullRequests: (id: string) =>
    requestJson<GitHubPullRequest[]>("GET", API_ROUTES.projects.githubPullRequests(id)),

  importGitHubComments: (id: string, number: number, chatId: string) =>
    requestJson<ImportGitHubCommentsResult>(
      "POST",
      API_ROUTES.projects.githubImportComments(id, number),
      { chatId },
    ),

  /**
   * Opens a pull request.
   *
   * This one call cannot go through `requestJson`: a 409 here is not a plain
   * failure but the server asking a question — "there are uncommitted changes,
   * shall I commit them?" — and the answer needs the default message and the
   * change count the body carries. Everything else behaves exactly like
   * `requestJson` so callers see the usual Error.
   */
  createGitHubPullRequest: async (
    id: string,
    body: CreateGitHubPRInput,
  ): Promise<CreateGitHubPRResult> => {
    const response = await sendHttpRequest("POST", API_ROUTES.projects.githubPR(id), body);
    if (response.status === API_RESPONSE_STATUS.unauthorized) {
      location.reload();
      return new Promise<CreateGitHubPRResult>(() => {});
    }
    if (!response.ok) {
      let parsed: {
        error?: string;
        dirty?: boolean;
        defaultCommitMessage?: string;
        dirtyCount?: number;
      } = {};
      try {
        parsed = await response.json();
      } catch {}
      if (response.status === API_RESPONSE_STATUS.conflict && parsed.dirty) {
        throw new GitHubDirtyWorkspaceError(
          parsed.error || "there are uncommitted changes in /workspace",
          parsed.defaultCommitMessage || DEFAULT_COMMIT_MESSAGE_FALLBACK,
          parsed.dirtyCount || 0,
        );
      }
      throw new Error(parsed.error || String(response.status));
    }
    return response.json() as Promise<CreateGitHubPRResult>;
  },
};
