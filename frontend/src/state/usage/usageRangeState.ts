/**
 * Date-range selection for the Usage page.
 *
 * Every range resolves to a half-open millisecond window the backend can
 * filter on. Days are bounded in UTC because the ledger buckets records by
 * UTC day, so a range picked in any timezone lines up with the bars drawn
 * from the same response.
 */
export type UsageRangePreset = "7d" | "30d" | "month" | "custom";

export interface UsageRange {
  preset: UsageRangePreset;
  from: number;
  to: number;
}

export interface UsageRangeLabels {
  fromDate: string;
  toDate: string;
}

const DAY_MS = 24 * 60 * 60 * 1000;

export const USAGE_RANGE_PRESETS: Array<{ id: UsageRangePreset; label: string }> = [
  { id: "7d", label: "7 days" },
  { id: "30d", label: "30 days" },
  { id: "month", label: "This month" },
  { id: "custom", label: "Custom" },
];

/** Start of the UTC day containing `at`. */
export function startOfUtcDay(at: number): number {
  return Math.floor(at / DAY_MS) * DAY_MS;
}

/** Last millisecond of the UTC day containing `at`. */
export function endOfUtcDay(at: number): number {
  return startOfUtcDay(at) + DAY_MS - 1;
}

/** ISO `YYYY-MM-DD` for the UTC day containing `at` — the `<input type=date>` value. */
export function toDateInputValue(at: number): string {
  return new Date(startOfUtcDay(at)).toISOString().slice(0, 10);
}

/** Parses an ISO `YYYY-MM-DD` as a UTC day start, or null when malformed. */
export function fromDateInputValue(value: string): number | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null;
  const parsed = Date.parse(`${value}T00:00:00.000Z`);
  return Number.isNaN(parsed) ? null : parsed;
}

/**
 * Resolves a preset against "now". `7d` and `30d` include today, so "7 days"
 * spans today plus the six days before it rather than a bare now-minus-7.
 */
export function usageRangeForPreset(preset: UsageRangePreset, now: number): UsageRange {
  const to = endOfUtcDay(now);
  switch (preset) {
    case "7d":
      return { preset, from: startOfUtcDay(now) - 6 * DAY_MS, to };
    case "month": {
      const today = new Date(startOfUtcDay(now));
      const from = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), 1);
      return { preset, from, to };
    }
    case "custom":
    case "30d":
    default:
      return { preset: preset === "custom" ? "custom" : "30d", from: startOfUtcDay(now) - 29 * DAY_MS, to };
  }
}

/**
 * Builds a custom range from two date inputs. Reversed inputs are swapped so
 * the picker cannot produce a window the backend rejects; malformed input
 * leaves the previous range untouched.
 */
export function usageRangeFromDates(
  current: UsageRange,
  fromValue: string,
  toValue: string
): UsageRange {
  const from = fromDateInputValue(fromValue);
  const to = fromDateInputValue(toValue);
  if (from == null || to == null) return current;
  const [start, end] = from <= to ? [from, to] : [to, from];
  return { preset: "custom", from: start, to: endOfUtcDay(end) };
}

export function usageRangeLabels(range: UsageRange): UsageRangeLabels {
  return { fromDate: toDateInputValue(range.from), toDate: toDateInputValue(range.to) };
}

/** Number of whole UTC days the range covers, minimum one. */
export function usageRangeDays(range: UsageRange): number {
  return Math.max(1, Math.round((range.to + 1 - range.from) / DAY_MS));
}
