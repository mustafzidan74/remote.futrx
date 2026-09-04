import { useState } from "preact/hooks";
import type { DeployTarget } from "../../../models/deploy";
import { deployHeadline, deployTargetLabel } from "../../../models/deploy";
import type { ProjectDeployState } from "../../../state/hooks/projects/useProjectDeploy";
import { AlertCircle, Loader, Upload } from "../../primitives/icons";

/**
 * Whether this project's agents can reach a live server, shown where the work
 * happens rather than three screens away in settings.
 *
 * The switch is not advice. Turning a target off removes the project from that
 * server's vault scope, and the next sync deletes the key and its ssh config
 * block out of the container — so an agent with the switch off cannot deploy
 * for the same reason it cannot deploy to a server it has never heard of.
 * The request does not answer until that has happened.
 */
export function DeployChip({ state }: { state: ProjectDeployState }) {
    const [open, setOpen] = useState(false);
    const { targets, loading, busy, error } = state;

    // A project with no server configured has nothing to say. The chip is
    // about a decision, and there is no decision to make here.
    if (loading || targets.length === 0) return null;

    const live = targets.filter((target) => target.enabled);
    const tone = live.length > 0 ? "text-accent-orange" : "text-ink-400";

    return (
        <div class="relative">
            <button
                type="button"
                onClick={() => setOpen((value) => !value)}
                title="Which live servers this project's agents can reach"
                class={`inline-flex h-7 items-center gap-1.5 rounded-md border border-line px-2 text-[11.5px] transition-colors hover:bg-tint ${tone}`}
            >
                <Upload class="w-3.5 h-3.5" />
                {deployHeadline(targets)}
            </button>

            {open && (
                <div class="absolute right-0 z-30 mt-1 w-72 rounded-card border border-line bg-surface p-2 shadow-lg">
                    <p class="px-1 pb-1.5 text-[11px] text-ink-400">
                        Off removes the key from this project's container, not just the permission.
                    </p>
                    <ul class="space-y-0.5">
                        {targets.map((target) => (
                            <TargetRow
                                key={target.key}
                                target={target}
                                busy={busy}
                                onToggle={(enabled) => void state.setEnabled(target.key, enabled)}
                            />
                        ))}
                    </ul>
                    {error && (
                        <p class="mt-1.5 flex items-start gap-1.5 px-1 text-[11px] text-accent-red">
                            <AlertCircle class="w-3.5 h-3.5 shrink-0 mt-px" />
                            <span>{error}</span>
                        </p>
                    )}
                </div>
            )}
        </div>
    );
}

function TargetRow({
    target,
    busy,
    onToggle,
}: {
    target: DeployTarget;
    busy: boolean;
    onToggle: (enabled: boolean) => void;
}) {
    return (
        <li>
            <label class="flex cursor-pointer items-center gap-2 rounded-md px-1 py-1.5 hover:bg-tint">
                <input
                    type="checkbox"
                    checked={target.enabled}
                    disabled={busy}
                    onChange={(event) => onToggle((event.currentTarget as HTMLInputElement).checked)}
                />
                <span class="min-w-0 flex-1">
                    <span class="block truncate text-[12.5px] text-ink-100">
                        {deployTargetLabel(target)}
                    </span>
                    <span class="block truncate font-mono text-[10.5px] text-ink-400">
                        ssh {target.name}
                    </span>
                </span>
                {busy && <Loader class="w-3.5 h-3.5 shrink-0 animate-spin text-ink-400" />}
            </label>
        </li>
    );
}
