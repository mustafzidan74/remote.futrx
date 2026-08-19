import { useEffect, useState } from "preact/hooks";
import { auxModelApi } from "../../../api/auxModelApi";
import type { AuxModelAvailability, AuxModelJobId } from "../../../models/auxModel";

/**
 * Which auxiliary-model jobs this server can actually take right now.
 *
 * Every button the auxiliary model powers is an extra, so the answer is
 * cached once per page load and a failed lookup reads as "nothing available".
 * That is the correct fallback: the feature the button would have improved
 * still works without it.
 */

let cached: Promise<AuxModelAvailability> | null = null;

function load(): Promise<AuxModelAvailability> {
  if (!cached) {
    cached = auxModelApi
      .availability()
      .catch(() => ({ enabled: false, jobs: {} }) as AuxModelAvailability);
  }
  return cached;
}

/** Forgets the cached answer, so the settings panel's save is visible at once. */
export function refreshAuxModelJobs(): void {
  cached = null;
}

/** Whether one job may be offered. False until the lookup lands. */
export function useAuxModelJob(job: AuxModelJobId): boolean {
  const [available, setAvailable] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void load().then((config) => {
      if (!cancelled) setAvailable(Boolean(config.enabled && config.jobs?.[job]));
    });
    return () => {
      cancelled = true;
    };
  }, [job]);

  return available;
}
