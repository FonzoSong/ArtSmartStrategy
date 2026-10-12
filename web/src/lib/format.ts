/**
 * Shared display formatting.
 *
 * The API exposes two different percentage semantics and the UI must not guess:
 * `formatPercent` is for percentage-valued fields (0–100, for example
 * `content.completion`, `stats/category.interaction_rate`), while
 * `formatRatioPercent` is for ratio-valued fields (0–1). Counts stay counts and
 * are never rendered as percentages.
 */

export const MISSING = "—";

/** Reports whether a value is a usable finite number. */
export function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

/**
 * Converts a value to a finite number. Numeric strings are accepted because the
 * API may serialize numbers either way; anything else yields null.
 */
export function toFiniteNumber(value: unknown): number | null {
  if (typeof value === "number") {
    return Number.isFinite(value) ? value : null;
  }
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) {
      return null;
    }
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

/** Formats a percentage-valued metric (0–100) with two decimals. */
export function formatPercent(value: unknown, decimals = 2): string {
  const numeric = toFiniteNumber(value);
  return numeric === null ? MISSING : `${numeric.toFixed(decimals)}%`;
}

/** Formats a ratio-valued metric (0–1) as a percentage with two decimals. */
export function formatRatioPercent(value: unknown, decimals = 2): string {
  const numeric = toFiniteNumber(value);
  return numeric === null ? MISSING : `${(numeric * 100).toFixed(decimals)}%`;
}

/**
 * Formats a difference between two percentage values. The unit (percentage
 * points) is appended by the caller so the wording stays explicit.
 */
export function formatPercentagePoints(value: unknown, decimals = 2): string {
  const numeric = toFiniteNumber(value);
  return numeric === null ? MISSING : numeric.toFixed(decimals);
}

/** Formats a count. Counts are never percentages. */
export function formatCount(value: unknown): string {
  const numeric = toFiniteNumber(value);
  return Math.trunc(numeric ?? 0).toLocaleString("zh-CN");
}

/** Formats a decimal value with a fixed number of decimals. */
export function formatDecimal(value: unknown, decimals = 2): string {
  const numeric = toFiniteNumber(value);
  return numeric === null ? MISSING : numeric.toFixed(decimals);
}

/** Formats a qualitative score, showing a missing state when it is unavailable. */
export function formatScore(value: unknown, decimals = 1): string {
  const numeric = toFiniteNumber(value);
  return numeric === null ? "样本不足" : `${numeric.toFixed(decimals)}分`;
}
