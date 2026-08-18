import { useState } from "preact/hooks";
import type { GitHubPullRequest, GitHubStatus } from "../../../models/github";
import type {
  GitHubPullsRecord,
  GitHubRecord,
} from "../../../state/hooks/projects/useProjectGitHub";
import {
  checksTone,
  describeBranchState,
  describeChecks,
  githubActions,
  repoFullName,
  repoUrl,
  type ChecksTone,
} from "../../../state/projects/githubPanelState";
import {
  AlertCircle,
  Check,
  Download,
  ExternalLink,
  GitFork,
  Loader,
  MessageSquare,
  RotateCcw,
} from "../../primitives/icons";
import { Loading } from "./ProjectContainerPrimitives";
import { ProjectGitHubPRDialog } from "./ProjectGitHubPRDialog";

/** A chat this project owns, offered as an import destination. */
export interface GitHubImportTarget {
  id: string;
  title: string;
}

export function ProjectGitHubSection({
  record,
  pulls,
  busy,
  chats,
  onLink,
  onUnlink,
  onClone,
  onLoadPulls,
  onCreatePR,
  onImportComments,
  onOpenChat,
}: {
  record: GitHubRecord;
  pulls: GitHubPullsRecord;
  busy: boolean;
  chats: GitHubImportTarget[];
  onLink: (repo: string) => Promise<void>;
  onUnlink: () => Promise<void>;
  onClone: () => Promise<void>;
  onLoadPulls: () => Promise<void>;
  onCreatePR: (input: {
    title: string;
    body: string;
    head: string;
    commit: boolean;
    commitMessage: string;
  }) => Promise<{ url: string }>;
  onImportComments: (
    number: number,
    chatId: string,
  ) => Promise<{ chatId: string; comments: number; started: boolean }>;
  onOpenChat: (chatId: string) => void;
}) {
  const status = record.status;
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [prOpen, setPROpen] = useState(false);

  if (record.loading && !status) return <Loading text="Reading this project's GitHub link…" />;

  const actions = githubActions(status);

  const report = (cause: unknown) => setError((cause as Error).message);

  return (
    <>
      {(record.error || error) && (
        <div class="flex items-start gap-2.5 rounded-lg border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[13px]">
          <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-red" />
          <div class="text-accent-red break-words">{record.error ?? error}</div>
        </div>
      )}
      {notice && (
        <div class="flex items-start gap-2.5 rounded-lg border border-accent-green/30 bg-accent-green/[0.08] px-3 py-2.5 text-[13px]">
          <Check class="w-4 h-4 mt-0.5 flex-none text-accent-green" />
          <div class="text-accent-green break-words">{notice}</div>
        </div>
      )}

      {!status?.linked ? (
        <LinkForm
          busy={busy}
          onLink={async (repo) => {
            setError(null);
            setNotice(null);
            try {
              await onLink(repo);
              setNotice("Repository linked.");
            } catch (cause) {
              report(cause);
            }
          }}
        />
      ) : (
        <>
          <LinkedRepository
            status={status}
            busy={busy}
            onUnlink={async () => {
              if (
                !confirm(
                  "Unlink this repository? The webhook secret and the delivery log are deleted; " +
                    "the files in /workspace are untouched.",
                )
              ) {
                return;
              }
              setError(null);
              setNotice(null);
              try {
                await onUnlink();
                setNotice("Repository unlinked.");
              } catch (cause) {
                report(cause);
              }
            }}
          />

          <div class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={() => {
                setError(null);
                setNotice(null);
                setPROpen(true);
              }}
              disabled={busy || !actions.canOpenPR}
              title={actions.canOpenPR ? "Open a pull request from /workspace" : actions.blockedReason}
              class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
            >
              <GitFork class="w-3.5 h-3.5" />
              Open pull request
            </button>
            {actions.canClone && (
              <button
                type="button"
                onClick={async () => {
                  setError(null);
                  setNotice(null);
                  try {
                    await onClone();
                    setNotice("Repository cloned into /workspace.");
                  } catch (cause) {
                    report(cause);
                  }
                }}
                disabled={busy}
                class="h-10 px-3 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06] text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
              >
                {busy ? <Loader class="w-3.5 h-3.5 animate-spin" /> : <Download class="w-3.5 h-3.5" />}
                Clone into /workspace
              </button>
            )}
            <button
              type="button"
              onClick={() => void onLoadPulls()}
              disabled={pulls.loading || !actions.canListPRs}
              title={actions.canListPRs ? "List the open pull requests" : actions.blockedReason}
              class="h-10 px-3 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06] text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
            >
              {pulls.loading ? (
                <Loader class="w-3.5 h-3.5 animate-spin" />
              ) : (
                <RotateCcw class="w-3.5 h-3.5" />
              )}
              {pulls.data ? "Refresh pull requests" : "Load open pull requests"}
            </button>
          </div>

          {!actions.canOpenPR && actions.blockedReason && (
            <p class="text-[12px] text-accent-orange leading-relaxed">{actions.blockedReason}</p>
          )}

          <PullRequestList
            record={pulls}
            chats={chats}
            busy={busy}
            onImport={async (number, chatId) => {
              setError(null);
              setNotice(null);
              try {
                const result = await onImportComments(number, chatId);
                setNotice(
                  result.started
                    ? `Imported ${result.comments} comment${result.comments === 1 ? "" : "s"} from #${number} and started a run.`
                    : `Imported ${result.comments} comment${result.comments === 1 ? "" : "s"} from #${number}. That chat already has a run in flight, so nothing was started.`,
                );
                onOpenChat(result.chatId);
              } catch (cause) {
                report(cause);
              }
            }}
          />
        </>
      )}

      {prOpen && status && (
        <ProjectGitHubPRDialog
          status={status}
          busy={busy}
          onClose={() => setPROpen(false)}
          onSubmit={async (input) => {
            const created = await onCreatePR(input);
            setPROpen(false);
            setNotice(`Pull request opened: ${created.url}`);
            return created;
          }}
        />
      )}
    </>
  );
}

function LinkForm({
  busy,
  onLink,
}: {
  busy: boolean;
  onLink: (repo: string) => Promise<void>;
}) {
  const [value, setValue] = useState("");

  return (
    <form
      class="space-y-2"
      onSubmit={(event) => {
        event.preventDefault();
        if (!value.trim()) return;
        void onLink(value.trim());
      }}
    >
      <label class="block space-y-1.5">
        <span class="text-xs text-ink-300">Repository</span>
        <input
          type="text"
          value={value}
          onInput={(event) => setValue((event.currentTarget as HTMLInputElement).value)}
          placeholder="owner/repo or https://github.com/owner/repo"
          autocomplete="off"
          spellcheck={false}
          class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100
                 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue font-mono"
        />
      </label>
      <button
        type="submit"
        disabled={busy || !value.trim()}
        class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
      >
        {busy && <Loader class="w-3.5 h-3.5 animate-spin" />}
        Link repository
      </button>
      <p class="text-[11.5px] text-ink-400 leading-relaxed">
        The link is verified with <code class="font-mono">gh repo view</code> inside this project's
        container, using the container's own credential. Add a{" "}
        <code class="font-mono">GITHUB_TOKEN</code> secret to this project (or a vault entry scoped
        to it) first — the platform never holds a GitHub token of its own.
      </p>
    </form>
  );
}

function LinkedRepository({
  status,
  busy,
  onUnlink,
}: {
  status: GitHubStatus;
  busy: boolean;
  onUnlink: () => Promise<void>;
}) {
  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.03] px-3 py-2.5 space-y-2">
      <div class="flex items-start gap-2 flex-wrap">
        <a
          href={repoUrl(status)}
          target="_blank"
          rel="noopener noreferrer"
          class="text-[13.5px] font-mono text-ink-50 hover:text-accent-blue inline-flex items-center gap-1.5 min-w-0"
        >
          <span class="truncate">{repoFullName(status)}</span>
          <ExternalLink class="w-3.5 h-3.5 flex-none" />
        </a>
        {status.defaultBranch && (
          <span class="text-[11px] px-1.5 py-0.5 rounded border border-white/10 text-ink-300 font-mono">
            {status.defaultBranch}
          </span>
        )}
        <span class="flex-1" />
        <button
          type="button"
          onClick={() => void onUnlink()}
          disabled={busy}
          class="h-8 px-2.5 rounded-md border border-white/10 text-ink-300 hover:text-accent-red hover:bg-white/[0.06] text-[12px] disabled:opacity-50"
        >
          Unlink
        </button>
      </div>

      <div class="text-[12.5px] text-ink-300 leading-relaxed">
        {!status.containerRunning ? (
          <span class="text-accent-orange">
            The container is stopped, so the working tree cannot be read.
          </span>
        ) : !status.authOk ? (
          <span class="text-accent-orange">
            {status.authError || "No usable GitHub credential in the container."}
          </span>
        ) : !status.workspaceRepo ? (
          <span class="text-accent-orange">
            {status.workspaceEmpty
              ? "/workspace is empty."
              : "/workspace has files but is not a git repository."}
          </span>
        ) : (
          describeBranchState(status)
        )}
      </div>

      {status.linkedBy && (
        <div class="text-[11px] text-ink-400">Linked by {status.linkedBy}.</div>
      )}
    </div>
  );
}

function PullRequestList({
  record,
  chats,
  busy,
  onImport,
}: {
  record: GitHubPullsRecord;
  chats: GitHubImportTarget[];
  busy: boolean;
  onImport: (number: number, chatId: string) => Promise<void>;
}) {
  if (record.error) {
    return (
      <div class="rounded-md border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[12.5px] text-accent-red break-words">
        {record.error}
      </div>
    );
  }
  if (!record.data) return null;
  if (record.data.length === 0) {
    return (
      <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2.5 text-[12.5px] text-ink-300">
        No open pull requests.
      </div>
    );
  }

  return (
    <div class="space-y-2">
      {record.data.map((pull) => (
        <PullRequestRow
          key={pull.number}
          pull={pull}
          chats={chats}
          busy={busy}
          onImport={onImport}
        />
      ))}
    </div>
  );
}

function PullRequestRow({
  pull,
  chats,
  busy,
  onImport,
}: {
  pull: GitHubPullRequest;
  chats: GitHubImportTarget[];
  busy: boolean;
  onImport: (number: number, chatId: string) => Promise<void>;
}) {
  // Held as an override, not as the value: the initializer runs once, and a
  // row that mounted while the project had no chats would otherwise keep an
  // empty selection — the select would show the first option while the Import
  // button stayed disabled with no explanation.
  const [chosen, setChosen] = useState("");
  const chatId = chosen || chats[0]?.id || "";

  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.03] px-3 py-2.5 space-y-2">
      <div class="flex items-start gap-2 flex-wrap min-w-0">
        <a
          href={pull.url}
          target="_blank"
          rel="noopener noreferrer"
          class="text-[13px] text-ink-50 hover:text-accent-blue min-w-0 inline-flex items-center gap-1.5"
        >
          <span class="font-mono text-ink-300 flex-none">#{pull.number}</span>
          <span class="truncate">{pull.title}</span>
          <ExternalLink class="w-3 h-3 flex-none" />
        </a>
        {pull.draft && (
          <span class="text-[11px] px-1.5 py-0.5 rounded border border-white/10 text-ink-400">
            draft
          </span>
        )}
        <ChecksBadge tone={checksTone(pull)} text={describeChecks(pull)} />
      </div>

      <div class="text-[11.5px] text-ink-400 font-mono truncate">
        {pull.headBranch} → {pull.baseBranch}
        {pull.author ? ` · @${pull.author}` : ""}
      </div>

      <div class="flex flex-wrap items-center gap-2">
        {chats.length === 0 ? (
          <span class="text-[12px] text-ink-400">
            Create a chat in this project to import review comments into.
          </span>
        ) : (
          <>
            <select
              value={chatId}
              onChange={(event) => setChosen((event.currentTarget as HTMLSelectElement).value)}
              class="h-9 rounded-md bg-black/30 border border-white/10 px-2 text-[12.5px] text-ink-100 max-w-[220px]"
            >
              {chats.map((chat) => (
                <option key={chat.id} value={chat.id}>
                  {chat.title}
                </option>
              ))}
            </select>
            <button
              type="button"
              onClick={() => void onImport(pull.number, chatId)}
              disabled={busy || !chatId}
              class="h-9 px-2.5 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06] text-[12.5px] disabled:opacity-50 inline-flex items-center gap-1.5"
            >
              <MessageSquare class="w-3.5 h-3.5" />
              Import review comments
            </button>
          </>
        )}
      </div>
    </div>
  );
}

const TONE_CLASS: Record<ChecksTone, string> = {
  ok: "border-accent-green/30 text-accent-green",
  warn: "border-accent-orange/30 text-accent-orange",
  bad: "border-accent-red/30 text-accent-red",
  muted: "border-white/10 text-ink-400",
};

export function ChecksBadge({ tone, text }: { tone: ChecksTone; text: string }) {
  return (
    <span class={`text-[11px] px-1.5 py-0.5 rounded border ${TONE_CLASS[tone]}`}>{text}</span>
  );
}
