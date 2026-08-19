import { useEffect, useRef, useState } from "preact/hooks";
import { modelRoutingApi } from "../../../api/modelRoutingApi";
import type { RoutingDecision } from "../../../models/modelRouting";
import type { ApiError } from "../../../api/apiRequest";
import type { ChatMode, ChatProvider, SelectedSkill } from "../../../models/chat";

/** How long typing has to settle before the hint is re-asked. */
const DEBOUNCE_MS = 450;

export interface ModelRoutingPreview {
  /** The model the next turn would use, or null while nothing is known yet. */
  decision: RoutingDecision | null;
  /**
   * True once the server has said it has no routing policy. The composer
   * hides the Auto entry entirely rather than offering a switch that would
   * change nothing.
   */
  unavailable: boolean;
}

export interface ModelRoutingPreviewInput {
  enabled: boolean;
  draft: string;
  mode: ChatMode;
  provider: ChatProvider;
  model: string;
  projectId?: string;
  selectedSkills: SelectedSkill[];
}

/**
 * The composer's "what will the next turn use?" hint.
 *
 * The decision depends on the prompt, so it cannot be computed once and
 * cached — but it also must not be asked on every keystroke, hence the
 * debounce. The evaluation stays on the server so the hint and the run agree;
 * a second copy of the rule engine in TypeScript would drift the first time
 * anyone edited one of them.
 */
export function useModelRoutingPreview({
  enabled,
  draft,
  mode,
  provider,
  model,
  projectId,
  selectedSkills,
}: ModelRoutingPreviewInput): ModelRoutingPreview {
  const [decision, setDecision] = useState<RoutingDecision | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  // Skills are an array rebuilt on every render, so the effect keys off a
  // stable string rather than the identity of the list.
  const skillKey = selectedSkills
    .map((skill) => (skill.command || skill.name || "").trim())
    .filter(Boolean)
    .join(",");
  const latest = useRef(0);

  useEffect(() => {
    if (!enabled || unavailable) {
      setDecision(null);
      return;
    }
    const request = ++latest.current;
    const timer = setTimeout(() => {
      void modelRoutingApi
        .preview({
          prompt: draft,
          mode,
          provider,
          model,
          projectId,
          skills: skillKey ? skillKey.split(",") : undefined,
        })
        .then((next) => {
          // A slower earlier request must never overwrite a later answer.
          if (request === latest.current) setDecision(next);
        })
        .catch((cause: ApiError) => {
          if (request !== latest.current) return;
          setDecision(null);
          // A deployment without a routing policy answers 503 (or 404 when
          // the routes were never registered) and never will answer anything
          // else, so stop asking. A transient failure is left alone: the next
          // keystroke retries it.
          if (cause?.status === 503 || cause?.status === 404) setUnavailable(true);
        });
    }, DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [enabled, unavailable, draft, mode, provider, model, projectId, skillKey]);

  return { decision, unavailable };
}
