import { useCallback, useEffect, useState } from "preact/hooks";
import { playbookApi } from "../../../api/playbookApi";
import type { UpdateChatInput } from "../../../models/chat";
import type { Playbook } from "../../../models/playbook";
import {
  firstUnresolvedRange,
  playbookChatPatch,
  resolvePlaybookPrompt,
  sortPlaybooks,
  unresolvedSummary,
  type PlaybookChatState,
  type PlaybookContext,
} from "../../chat/playbookState";

export interface PlaybookLibrary {
  playbooks: Playbook[];
  loading: boolean;
  error: string | null;
  /** Id of the playbook currently being applied, if any. */
  running: string | null;
  /** Set when the last run left placeholders for the user to fill in. */
  notice: string | null;
  dismissNotice: () => void;
  run: (playbook: Playbook, options?: { send?: boolean }) => Promise<void>;
}

/**
 * The composer's Playbooks menu.
 *
 * The library is server-owned and changes only when an admin edits it, so it
 * is fetched once per composer mount rather than polled. Running a playbook
 * applies its chat configuration through the existing chat update API and
 * then hands the resolved prompt to the composer — inserted by default, sent
 * only when the caller asks and nothing is left unfilled.
 */
export function usePlaybooks({
  enabled,
  context,
  current,
  applyMeta,
  insertPrompt,
  submitPrompt,
}: {
  enabled: boolean;
  context: PlaybookContext;
  current: PlaybookChatState;
  applyMeta: (patch: UpdateChatInput) => Promise<void>;
  insertPrompt: (text: string, select?: { start: number; end: number }) => void;
  submitPrompt: (text: string) => boolean;
}): PlaybookLibrary {
  const [playbooks, setPlaybooks] = useState<Playbook[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    setLoading(true);
    playbookApi
      .list()
      .then((list) => {
        if (cancelled) return;
        setPlaybooks(sortPlaybooks(list));
        setError(null);
      })
      .catch((cause) => {
        if (cancelled) return;
        setPlaybooks([]);
        setError((cause as Error).message || "Could not load playbooks");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);

  const run = useCallback(
    async (playbook: Playbook, options: { send?: boolean } = {}) => {
      setRunning(playbook.id);
      setNotice(null);
      try {
        const application = playbookChatPatch(playbook, current);
        if (application.changed) {
          await applyMeta(application.patch);
        }
        const resolved = resolvePlaybookPrompt(playbook.prompt, context);
        // A prompt with an unfilled placeholder is never sent, however the
        // user clicked: the agent would act on a literal "{{askUrl}}".
        if (options.send && resolved.ready) {
          if (!submitPrompt(resolved.text)) {
            insertPrompt(resolved.text);
            setNotice("The chat could not accept a prompt just now, so it was inserted instead.");
          }
          return;
        }
        insertPrompt(resolved.text, firstUnresolvedRange(resolved) ?? undefined);
        if (!resolved.ready) {
          setNotice(`Fill in ${unresolvedSummary(resolved)} before sending.`);
        }
      } catch (cause) {
        setError((cause as Error).message || "Could not apply the playbook");
      } finally {
        setRunning(null);
      }
    },
    [applyMeta, context, current, insertPrompt, submitPrompt],
  );

  const dismissNotice = useCallback(() => setNotice(null), []);

  return { playbooks, loading, error, running, notice, dismissNotice, run };
}
