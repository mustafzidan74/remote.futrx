/**
 * Deploy access: which live servers a project's agents can reach.
 *
 * "Enabled" is not a permission flag the agent is trusted to respect — it is
 * whether the key is in the container at all. Everything here is shape plus
 * the small amount of wording the UI needs.
 */

export interface DeployTarget {
  key: string;
  /** What the agent types: `ssh <name>`. */
  name: string;
  /** The site, when one was recorded. */
  domain?: string;
  host: string;
  user: string;
  port: number;
  enabled: boolean;
}

/** The site if we know it, else the address. Nobody thinks in IPs. */
export function deployTargetLabel(target: DeployTarget): string {
  return target.domain || target.host;
}

/**
 * The chip's own text.
 *
 * It names the live target when there is exactly one, because that is the
 * thing worth knowing before you ask an agent to change anything. Two or more
 * is a count: the list below says which.
 */
export function deployHeadline(targets: DeployTarget[]): string {
  const live = targets.filter((target) => target.enabled);
  if (live.length === 0) return "Deploy off";
  if (live.length === 1) return `Deploy → ${deployTargetLabel(live[0])}`;
  return `Deploy → ${live.length} servers`;
}
