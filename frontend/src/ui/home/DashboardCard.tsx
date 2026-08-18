import type { ComponentChildren, ComponentType } from "preact";
import type { StatusTone } from "../../state/home/dashboardState";

/**
 * The one card shape every panel on the home screen wears, so five very
 * different lists still read as one board rather than five widgets.
 */
export function DashboardCard({
  id,
  title,
  subtitle,
  Icon,
  action,
  children,
}: {
  /** Anchor, so another part of the page can scroll to this card. */
  id?: string;
  title: string;
  subtitle?: string;
  Icon: ComponentType<{ class?: string }>;
  /** Optional control in the header — a link out, a metric toggle. */
  action?: ComponentChildren;
  children: ComponentChildren;
}) {
  return (
    <section id={id} class="flex min-w-0 flex-col rounded-lg border border-white/10 bg-[#101318]">
      <header class="flex flex-none items-center gap-2.5 border-b border-white/[0.06] px-4 py-3">
        <span class="grid h-7 w-7 flex-none place-items-center rounded-md bg-white/[0.06] text-ink-200">
          <Icon class="h-4 w-4" />
        </span>
        <div class="min-w-0 flex-1">
          <h2 class="truncate text-[14px] font-semibold text-ink-50">{title}</h2>
          {subtitle && (
            <p class="mt-0.5 truncate text-[12px] leading-snug text-ink-300">{subtitle}</p>
          )}
        </div>
        {action && <div class="flex-none">{action}</div>}
      </header>
      <div class="min-w-0 flex-1">{children}</div>
    </section>
  );
}

/** What a card says when it has nothing to list. */
export function CardEmpty({ children }: { children: ComponentChildren }) {
  return (
    <p class="px-4 py-6 text-center text-[12.5px] leading-relaxed text-ink-300">{children}</p>
  );
}

/** The rows a card shows before its first payload lands. */
export function CardSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div class="space-y-2 p-3" aria-hidden="true">
      {Array.from({ length: rows }, (_, index) => (
        <div key={index} class="flex items-center gap-3 rounded-md bg-white/[0.04] p-3">
          <span class="h-8 w-8 flex-none animate-pulse rounded-md bg-white/[0.07]" />
          <span class="flex-1 space-y-1.5">
            <span class="block h-3 w-1/3 animate-pulse rounded bg-white/[0.07]" />
            <span class="block h-2.5 w-2/3 animate-pulse rounded bg-white/[0.05]" />
          </span>
        </div>
      ))}
    </div>
  );
}

export const TONE_DOT: Record<StatusTone, string> = {
  green: "bg-accent-green",
  amber: "bg-accent-yellow",
  red: "bg-accent-red",
  grey: "bg-ink-400",
};

export const TONE_TEXT: Record<StatusTone, string> = {
  green: "text-accent-green",
  amber: "text-accent-yellow",
  red: "text-accent-red",
  grey: "text-ink-300",
};

/** The status dot. Colour never carries the meaning on its own — every use
 *  pairs it with the label it is describing. */
export function ToneDot({ tone, title, pulse }: { tone: StatusTone; title?: string; pulse?: boolean }) {
  return (
    <span class="grid h-3 w-3 flex-none place-items-center" title={title}>
      <span
        class={`block h-2 w-2 rounded-full ${TONE_DOT[tone]}${pulse ? " animate-pulse" : ""}`}
        aria-hidden="true"
      />
    </span>
  );
}

/** The small square button used for a card row's quick actions. */
export function RowAction({
  label,
  Icon,
  onClick,
  href,
}: {
  label: string;
  Icon: ComponentType<{ class?: string }>;
  onClick?: () => void;
  /** When set the action is a link — used for preview URLs, which open out. */
  href?: string;
}) {
  const shared =
    "grid h-7 w-7 flex-none place-items-center rounded-md text-ink-300 transition " +
    "hover:bg-white/[0.09] hover:text-ink-100 focus:outline-none";

  if (href) {
    return (
      <a href={href} target="_blank" rel="noopener noreferrer" class={shared} title={label} aria-label={label}>
        <Icon class="h-3.5 w-3.5" />
      </a>
    );
  }
  return (
    <button type="button" onClick={onClick} class={shared} title={label} aria-label={label}>
      <Icon class="h-3.5 w-3.5" />
    </button>
  );
}
