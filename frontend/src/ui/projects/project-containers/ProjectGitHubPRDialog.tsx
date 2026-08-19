import { useState } from "preact/hooks";
import type { CommitMessageSuggestion, GitHubStatus } from "../../../models/github";
import { GitHubDirtyWorkspaceError } from "../../../models/github";
import {
  defaultBase,
  needsNewBranch,
  suggestBranchName,
} from "../../../state/projects/githubPanelState";
import { useAuxModelJob } from "../../../state/hooks/settings/useAuxModelJobs";
import { AlertCircle, Loader, X, Zap } from "../../primitives/icons";

export interface PRDialogSubmit {
  title: string;
  body: string;
  head: string;
  commit: boolean;
  commitMessage: string;
}

/**
 * The "open a pull request" dialog.
 *
 * Its one non-obvious job is the commit question. The server refuses to sweep
 * uncommitted changes into a pull request unless the operator says so, and
 * answers that refusal with a 409 carrying a deterministic default message.
 * This dialog therefore has two ways in: the operator can tick "commit" up
 * front, or discover it from the server's refusal and tick it then — either
 * way the message shown is the server's, never one composed here or by a model.
 */
export function ProjectGitHubPRDialog({
  status,
  busy,
  onClose,
  onSubmit,
  onSuggestCommitMessage,
}: {
  status: GitHubStatus;
  busy: boolean;
  onClose: () => void;
  onSubmit: (input: PRDialogSubmit) => Promise<{ url: string }>;
  /** Drafts a subject from the diff shape. Always answers with something usable. */
  onSuggestCommitMessage: () => Promise<CommitMessageSuggestion>;
}) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [head, setHead] = useState(needsNewBranch(status) ? "" : (status.branch ?? ""));
  const [commit, setCommit] = useState(status.dirty);
  const [commitMessage, setCommitMessage] = useState(status.defaultCommitMessage ?? "");
  const [dirtyCount, setDirtyCount] = useState(status.dirtyCount);
  const [error, setError] = useState<string | null>(null);
  const canSuggest = useAuxModelJob("commitMessage");
  const [suggesting, setSuggesting] = useState(false);
  const [suggestion, setSuggestion] = useState<CommitMessageSuggestion | null>(null);

  const suggest = async () => {
    setSuggesting(true);
    setSuggestion(null);
    try {
      const result = await onSuggestCommitMessage();
      setSuggestion(result);
      setCommitMessage(result.message);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setSuggesting(false);
    }
  };

  const mustBranch = needsNewBranch(status);
  const base = defaultBase(status);
  const effectiveHead =
    head.trim() || (mustBranch ? suggestBranchName(title, new Date()) : (status.branch ?? ""));

  const submit = async () => {
    setError(null);
    try {
      await onSubmit({
        title: title.trim(),
        body,
        head: effectiveHead,
        commit,
        commitMessage: commitMessage.trim(),
      });
    } catch (cause) {
      if (cause instanceof GitHubDirtyWorkspaceError) {
        // The server refused and told us what it would commit. Turn the
        // refusal into the question it actually is, pre-filled with the
        // server's own message.
        setCommit(true);
        setCommitMessage(cause.defaultCommitMessage);
        setDirtyCount(cause.dirtyCount);
        setError(
          `There are ${cause.dirtyCount} uncommitted change${cause.dirtyCount === 1 ? "" : "s"} in /workspace. ` +
            "Review the commit message below, then submit again to commit and push them.",
        );
        return;
      }
      setError((cause as Error).message);
    }
  };

  return (
    <div
      class="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Open a pull request"
    >
      <div class="w-full max-w-lg rounded-lg border border-white/10 bg-[#101318] shadow-xl max-h-full overflow-y-auto">
        <header class="px-4 py-3 border-b border-white/[0.06] flex items-center gap-2">
          <div class="flex-1 min-w-0">
            <div class="text-[14.5px] font-semibold text-ink-50">Open a pull request</div>
            <div class="text-[12px] text-ink-300 truncate">
              {effectiveHead || "(no branch)"} → {base || "the repository default"}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            class="h-8 w-8 rounded-md text-ink-300 hover:text-ink-50 hover:bg-white/[0.08] grid place-items-center"
            aria-label="Close"
          >
            <X class="w-4 h-4" />
          </button>
        </header>

        <div class="p-4 space-y-3">
          {error && (
            <div class="flex items-start gap-2.5 rounded-lg border border-accent-orange/30 bg-accent-orange/[0.08] px-3 py-2.5 text-[13px]">
              <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-orange" />
              <div class="text-accent-orange break-words">{error}</div>
            </div>
          )}

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Title</span>
            <input
              type="text"
              value={title}
              onInput={(event) => setTitle((event.currentTarget as HTMLInputElement).value)}
              placeholder="Leave empty to let gh fill it from the commits"
              maxLength={200}
              autocomplete="off"
              class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100
                     placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
            />
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Description (optional)</span>
            <textarea
              value={body}
              onInput={(event) => setBody((event.currentTarget as HTMLTextAreaElement).value)}
              rows={4}
              class="w-full rounded-md bg-black/30 border border-white/10 px-3 py-2 text-sm text-ink-100
                     placeholder:text-ink-400 focus:outline-none focus:border-accent-blue resize-y"
            />
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">
              Branch{mustBranch ? " (required — you are on the default branch)" : ""}
            </span>
            <input
              type="text"
              value={head}
              onInput={(event) => setHead((event.currentTarget as HTMLInputElement).value)}
              placeholder={effectiveHead}
              autocomplete="off"
              spellcheck={false}
              class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100
                     placeholder:text-ink-400 focus:outline-none focus:border-accent-blue font-mono"
            />
          </label>

          <label class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer">
            <input
              type="checkbox"
              checked={commit}
              onChange={(event) => setCommit((event.currentTarget as HTMLInputElement).checked)}
              class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
            />
            <span class="min-w-0">
              <span class="block text-[13px] text-ink-100">
                Commit uncommitted changes in /workspace
              </span>
              <span class="block text-[12px] text-ink-300 leading-relaxed">
                {dirtyCount > 0
                  ? `${dirtyCount} path${dirtyCount === 1 ? "" : "s"} would be staged with git add -A.`
                  : "Nothing is uncommitted right now."}
              </span>
            </span>
          </label>

          {commit && (
            <label class="block space-y-1.5">
              <span class="flex items-center gap-2 text-xs text-ink-300">
                Commit message
                {canSuggest && (
                  <button
                    type="button"
                    onClick={() => void suggest()}
                    disabled={suggesting || busy}
                    title="Draft a conventional-commit subject from the changed paths and line counts"
                    class="inline-flex h-6 items-center gap-1 rounded border border-white/10 px-1.5
                           text-[11px] text-ink-200 hover:bg-white/[0.07] disabled:opacity-50"
                  >
                    {suggesting ? (
                      <Loader class="h-3 w-3 animate-spin" />
                    ) : (
                      <Zap class="h-3 w-3" />
                    )}
                    Suggest
                  </button>
                )}
              </span>
              <input
                type="text"
                value={commitMessage}
                onInput={(event) =>
                  setCommitMessage((event.currentTarget as HTMLInputElement).value)
                }
                placeholder={status.defaultCommitMessage}
                maxLength={200}
                autocomplete="off"
                class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100
                       placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
              />
              <span class="block text-[11.5px] text-ink-400 leading-relaxed">
                {suggestion?.generated
                  ? "Drafted by the auxiliary model from the changed paths and line counts — it never saw the contents of a file. Edit it before you push."
                  : suggestion
                    ? `No subject was drafted (${suggestion.reason}); the dated default is in the box.`
                    : "The default is generated from today's date, not by an agent, so a repository's history stays predictable. Edit it if you want something more specific."}
              </span>
            </label>
          )}
        </div>

        <footer class="px-4 py-3 border-t border-white/[0.06] flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            class="h-10 px-3 rounded-md border border-white/10 text-ink-300 hover:text-ink-50 hover:bg-white/[0.06] text-[13px] disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void submit()}
            disabled={busy || !effectiveHead}
            class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
          >
            {busy && <Loader class="w-3.5 h-3.5 animate-spin" />}
            Push and open
          </button>
        </footer>
      </div>
    </div>
  );
}
