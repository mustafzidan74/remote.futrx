import { useState } from "preact/hooks";
import type { Snapshot } from "../../../models/snapshot";
import type { SnapshotsRecord } from "../../../state/hooks/projects/useProjectSnapshots";
import { snapshotState } from "../../../state/projects/snapshotState";
import { AlertCircle, Archive, Clock, Loader, RotateCcw, Trash } from "../../primitives/icons";
import { Empty, Loading } from "./ProjectContainerPrimitives";
import { formatBytes, formatEpochMillis } from "./projectContainerFormat";

export function ProjectSnapshotsSection({
  record,
  running,
  onCreate,
  onRestore,
  onDelete,
}: {
  record: SnapshotsRecord;
  running: boolean;
  onCreate: (label: string, includeSecrets: boolean) => Promise<void>;
  onRestore: (snapshotId: string) => Promise<void>;
  onDelete: (snapshotId: string) => Promise<void>;
}) {
  const [label, setLabel] = useState("");
  const [includeSecrets, setIncludeSecrets] = useState(false);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const snapshots = record.data ?? [];
  const failedRestore = snapshotState.failedRestore(record.jobs ?? []);

  async function run(operation: () => Promise<void>) {
    setBusy(true);
    setActionError(null);
    try {
      await operation();
    } catch (error) {
      setActionError((error as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const create = () =>
    run(async () => {
      await onCreate(label, includeSecrets);
      setLabel("");
      setIncludeSecrets(false);
    });

  const restore = (snapshot: Snapshot) => {
    const when = formatEpochMillis(snapshot.createdAt);
    if (
      !confirm(
        `Restore the snapshot from ${when}?\n\n` +
          "The project is stopped, its current workspace and agent homes are moved " +
          "aside inside the snapshots directory, the archive is unpacked in their " +
          "place, and the container is started again. A database in the snapshot is " +
          "re-imported over the current one.",
      )
    ) {
      return;
    }
    return run(() => onRestore(snapshot.id));
  };

  const remove = (snapshot: Snapshot) => {
    if (!confirm(`Delete this snapshot permanently? The archive is removed from the host.`)) return;
    return run(() => onDelete(snapshot.id));
  };

  return (
    <>
      {(record.error || actionError) && (
        <ErrorBanner message={record.error ?? actionError ?? ""} />
      )}
      {failedRestore && (
        <ErrorBanner
          message={`The last restore failed: ${failedRestore.error ?? "unknown error"}`}
        />
      )}

      <div class="rounded-md border border-white/[0.08] bg-white/[0.03] p-3 space-y-2.5">
        <label class="block">
          <span class="text-[11.5px] text-ink-400">Label (optional)</span>
          <input
            type="text"
            value={label}
            maxLength={80}
            placeholder="before the plugin upgrade"
            onInput={(event) => setLabel((event.target as HTMLInputElement).value)}
            class="mt-1 w-full h-9 rounded-md border border-white/10 bg-[#0f1217] px-2.5 text-[13px]
                   text-ink-100 placeholder:text-ink-500 focus:outline-none focus:border-accent-blue/50"
          />
        </label>
        <label class="flex items-start gap-2 text-[12.5px] text-ink-200">
          <input
            type="checkbox"
            checked={includeSecrets}
            onChange={(event) => setIncludeSecrets((event.target as HTMLInputElement).checked)}
            class="mt-0.5"
          />
          <span>
            Include this project's secrets in the archive.
            <span class="text-ink-400">
              {" "}
              Off by default — an archive is a plain file that may leave the host.
            </span>
          </span>
        </label>
        <button
          type="button"
          onClick={() => void create()}
          disabled={busy || running}
          class="h-9 w-full rounded-md border border-white/10 bg-white/[0.06] px-3 text-[13px]
                 font-medium text-ink-100 hover:bg-white/[0.10] disabled:opacity-45
                 disabled:cursor-not-allowed inline-flex items-center justify-center gap-2"
        >
          {running ? <Loader class="w-4 h-4 animate-spin" /> : <Archive class="w-4 h-4" />}
          {running ? "Working…" : "Snapshot now"}
        </button>
      </div>

      {record.loading && !record.data ? (
        <Loading text="Loading snapshots…" />
      ) : snapshots.length === 0 ? (
        <Empty text="No snapshots yet. Take one before a risky change." compact />
      ) : (
        <div class="space-y-2">
          {snapshots.map((snapshot) => (
            <SnapshotRow
              key={snapshot.id}
              snapshot={snapshot}
              busy={busy}
              onRestore={() => void restore(snapshot)}
              onDelete={() => void remove(snapshot)}
            />
          ))}
        </div>
      )}

      <p class="text-[11.5px] text-ink-400 leading-relaxed">
        A snapshot archives the project's workspace and agent homes from the host, plus
        a dump of the template's in-container database when it has one. The newest 10
        are kept per project; older ones are deleted automatically.
      </p>
    </>
  );
}

function SnapshotRow({
  snapshot,
  busy,
  onRestore,
  onDelete,
}: {
  snapshot: Snapshot;
  busy: boolean;
  onRestore: () => void;
  onDelete: () => void;
}) {
  const settling = snapshotState.isSettling(snapshot);
  const failed = snapshot.status === "failed";

  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.03] px-3 py-2.5 flex items-start gap-3">
      <div class="mt-0.5 w-8 h-8 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
        {settling ? (
          <Loader class="w-3.5 h-3.5 text-ink-300 animate-spin" />
        ) : (
          <Archive class={`w-3.5 h-3.5 ${failed ? "text-accent-red" : "text-ink-200"}`} />
        )}
      </div>
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2 flex-wrap min-w-0">
          <span class="text-[13px] font-medium text-ink-50 truncate">
            {snapshot.label || formatEpochMillis(snapshot.createdAt)}
          </span>
          {snapshot.kind === "trash" && (
            <span class="text-[10.5px] px-1.5 py-0.5 rounded border border-accent-orange/30 bg-accent-orange/[0.10] text-accent-orange">
              automatic
            </span>
          )}
        </div>
        <div class="mt-0.5 text-[11.5px] text-ink-400 flex items-center gap-1.5 flex-wrap">
          <Clock class="w-3 h-3 flex-none" />
          <span>{formatEpochMillis(snapshot.createdAt)}</span>
          {snapshot.createdBy && <span>· {snapshot.createdBy}</span>}
          {snapshot.sizeBytes ? <span>· {formatBytes(snapshot.sizeBytes)}</span> : null}
        </div>
        <div
          class={`mt-0.5 text-[11.5px] break-words ${failed ? "text-accent-red" : "text-ink-400"}`}
        >
          {snapshotState.describeStatus(snapshot)}
          {snapshot.status === "ready" && ` · ${snapshotState.describeContents(snapshot)}`}
        </div>
      </div>
      <div class="flex-none flex items-center gap-1">
        <button
          type="button"
          onClick={onRestore}
          disabled={busy || !snapshotState.restorable(snapshot)}
          title="Restore this snapshot"
          aria-label="Restore this snapshot"
          class="h-8 w-8 rounded-md text-ink-300 hover:text-ink-50 hover:bg-white/[0.08]
                 disabled:opacity-40 disabled:cursor-not-allowed grid place-items-center"
        >
          <RotateCcw class="w-3.5 h-3.5" />
        </button>
        <button
          type="button"
          onClick={onDelete}
          disabled={busy || settling}
          title="Delete this snapshot"
          aria-label="Delete this snapshot"
          class="h-8 w-8 rounded-md text-ink-300 hover:text-accent-red hover:bg-accent-red/[0.10]
                 disabled:opacity-40 disabled:cursor-not-allowed grid place-items-center"
        >
          <Trash class="w-3.5 h-3.5" />
        </button>
      </div>
    </div>
  );
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <div class="flex items-start gap-2.5 rounded-lg border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[13px]">
      <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-red" />
      <div class="text-accent-red break-words">{message}</div>
    </div>
  );
}
